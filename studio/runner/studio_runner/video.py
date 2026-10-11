"""The Generate Video tool: Google's Gemini Omni Flash through the Gemini API's Interactions API.

One synchronous request per video (3-10 seconds, always with audio). Small videos come back inline as base64;
1080p and 4k come back as a Gemini Files API link, which is downloaded with the API key. Redirects off Google's
API host are followed without the key, so it never leaves generativelanguage.googleapis.com.

Omni bills by output tokens: a fixed number per second of video for each resolution. The tool refuses a video
whose estimated cost does not fit in the run's remaining budget, and records the real cost from the response.
"""

import base64
import os
import re
import time
from pathlib import Path
from typing import Any, Callable, Literal
from urllib.parse import urljoin, urlparse

import httpx
from crewai.tools import BaseTool
from pydantic import BaseModel, Field, PrivateAttr

from .meter import token_cost
from .tools import _error_message, _open_no_follow, pinned_request, resolve_in_workspace, resolve_public

API_HOST = "generativelanguage.googleapis.com"
INTERACTIONS_URL = f"https://{API_HOST}/v1beta/interactions"
FILE_URL = f"https://{API_HOST}/v1beta/files/{{file_id}}"
DOWNLOAD_URL = FILE_URL + ":download?alt=media"
FILE_ID = re.compile(r"[A-Za-z0-9_-]{1,128}")

# Output tokens billed per second of video (with audio), from Google's pricing pages, October 2026
VIDEO_TOKENS_PER_SECOND = {"360p": 1931, "720p": 5792, "1080p": 8688, "4k": 17376}
# Google recommends file delivery for videos over about 4 MB, which is anything above 720p
INLINE_RESOLUTIONS = frozenset({"360p", "720p"})
IMAGE_INPUT_TOKENS = 1120
CHARS_PER_TOKEN = 4

MIN_SECONDS, MAX_SECONDS = 3, 10
MAX_VIDEO_BYTES = 100 << 20  # the largest file Crew Studio keeps from a run
MAX_START_IMAGE_BYTES = 20 << 20
IMAGE_TYPES = {".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}

# Generation can take several minutes at busy times
GENERATE_TIMEOUT = httpx.Timeout(connect=30.0, read=600.0, write=60.0, pool=30.0)
DOWNLOAD_TIMEOUT = httpx.Timeout(connect=30.0, read=300.0, write=30.0, pool=30.0)
FILE_POLL_SECONDS = 5.0
FILE_POLL_ATTEMPTS = 60
MAX_REDIRECTS = 3

# (agent_role, details) -> cost in USD. details has model, input_tokens, output_tokens, made, and reserved_usd: the
# budget reservation the recorded cost replaces
VideoRecorder = Callable[[str, dict[str, Any]], float]


class VideoError(Exception):
    """A video could not be made; the message is written for the agent."""


class MaybeCharged(Exception):
    """The request reached Google but its outcome is unknown, so Google may have billed it."""


class VideoInput(BaseModel):
    prompt: str = Field(
        ...,
        description=(
            "Detailed description of one shot: subject, action, setting, camera, lighting, style and the sound or "
            "speech to hear. Put anything to avoid in the prompt as well."
        ),
    )
    output_path: str = Field(..., description="Where to save the MP4, relative to the workspace, e.g. videos/scene-01.mp4.")
    duration_seconds: int = Field(8, ge=MIN_SECONDS, le=MAX_SECONDS, description="Length in seconds, 3 to 10.")
    aspect_ratio: Literal["16:9", "9:16"] = Field("16:9", description="16:9 landscape or 9:16 portrait.")
    resolution: Literal["360p", "720p", "1080p", "4k"] = Field(
        "720p", description="720p unless asked otherwise; 1080p costs 1.5x and 4k costs 3x as much."
    )
    start_image: str | None = Field(
        None, description="Optional workspace path of a PNG, JPEG or WebP image to use as the first frame."
    )


def estimate_tokens(prompt: str, seconds: int, resolution: str, has_image: bool) -> tuple[int, int]:
    """(input, output) tokens a video should use; thinking tokens, if any, come on top."""
    input_tokens = len(prompt) // CHARS_PER_TOKEN + (IMAGE_INPUT_TOKENS if has_image else 0)
    return input_tokens, seconds * VIDEO_TOKENS_PER_SECOND[resolution]


def estimate_cost(price: dict[str, Any] | None, prompt: str, seconds: int, resolution: str, has_image: bool) -> float:
    return token_cost(price, *estimate_tokens(prompt, seconds, resolution, has_image))


def build_request(model: str, prompt: str, seconds: int, aspect_ratio: str, resolution: str,
                  image: tuple[bytes, str] | None) -> dict[str, Any]:
    text = {"type": "text", "text": prompt}
    body: dict[str, Any] = {
        "model": model,
        "input": [{"type": "image", "data": base64.b64encode(image[0]).decode(), "mime_type": image[1]}, text]
        if image
        else prompt,
        "response_format": {
            "type": "video",
            "aspect_ratio": aspect_ratio,
            "resolution": resolution,
            "duration": f"{seconds}s",
            "delivery": "inline" if resolution in INLINE_RESOLUTIONS else "uri",
        },
        # Google's advice for a single, fastest, unstored generation
        "background": False,
        "store": False,
        "stream": False,
    }
    if image:
        body["generation_config"] = {"video_config": {"task": "image_to_video"}}
    return body


def find_video(data: dict[str, Any]) -> dict[str, Any] | None:
    """The first video content item in the interaction's model output steps."""
    for step in data.get("steps") or []:
        if step.get("type") != "model_output":
            continue
        for item in step.get("content") or []:
            if item.get("type") == "video" and (item.get("data") or item.get("uri")):
                return item
    return None


def usage_tokens(data: dict[str, Any]) -> tuple[int, int] | None:
    """(input, output) tokens from the interaction's usage, with thinking counted as output; None if absent."""
    usage = data.get("usage")
    if not isinstance(usage, dict):
        return None
    output = int(usage.get("total_output_tokens") or 0) + int(usage.get("total_thought_tokens") or 0)
    return int(usage.get("total_input_tokens") or 0), output


def failure_reason(data: dict[str, Any]) -> str:
    errors = [f"{e.get('code', 'error')}: {e.get('message', '')}".strip(": ") for e in data.get("errors") or []]
    return "; ".join(errors) or f"status {data.get('status', 'unknown')}"


def _file_id(uri: str) -> str | None:
    """The file id in a Files API link such as /v1beta/files/abc123:download."""
    path = urlparse(uri).path
    marker = "/files/"
    if marker not in path:
        return None
    file_id = path.split(marker, 1)[1].split(":", 1)[0].split("/", 1)[0]
    return file_id if FILE_ID.fullmatch(file_id) else None


def _wait_until_active(client: httpx.Client, file_id: str, api_key: str, sleep: Callable[[float], None]) -> None:
    for _ in range(FILE_POLL_ATTEMPTS):
        response = client.get(FILE_URL.format(file_id=file_id), headers={"x-goog-api-key": api_key})
        response.raise_for_status()
        state = response.json().get("state", "ACTIVE")
        if state == "ACTIVE":
            return
        if state == "FAILED":
            raise VideoError("Google could not prepare the video file for download.")
        sleep(FILE_POLL_SECONDS)
    raise VideoError("The video file was still not ready to download after five minutes.")


def _read_capped(response: httpx.Response) -> bytearray:
    body = bytearray()
    for chunk in response.iter_bytes():
        body.extend(chunk)
        if len(body) > MAX_VIDEO_BYTES:
            raise VideoError(f"The video is larger than {MAX_VIDEO_BYTES >> 20} MB; ask for a lower resolution.")
    return body


def _hop(url: str, api_key: str) -> tuple[str, dict[str, str], dict[str, str]]:
    """Where to send one download request: the key only to Google's API host, elsewhere a pinned public address."""
    if urlparse(url).hostname == API_HOST:
        return url, {"x-goog-api-key": api_key}, {}
    reason, ip = resolve_public(url)
    if reason or ip is None:
        raise VideoError(f"The video download redirected somewhere blocked: {reason}")
    return pinned_request(url, ip)


def download_video(uri: str, api_key: str, sleep: Callable[[float], None] = time.sleep) -> bytearray:
    parsed = urlparse(uri)
    file_id = _file_id(uri)
    if parsed.scheme != "https" or parsed.hostname != API_HOST or file_id is None:
        # The key must only ever go to Google's API host, and only to fetch a Gemini file
        raise VideoError("The video link is not a Gemini API file, so it was not downloaded.")
    # trust_env=False: proxy settings must not see the key or route around the address checks
    with httpx.Client(timeout=DOWNLOAD_TIMEOUT, follow_redirects=False, trust_env=False) as client:
        _wait_until_active(client, file_id, api_key, sleep)
        # Fetch the file's content whatever form of link Google returned
        current = DOWNLOAD_URL.format(file_id=file_id)
        for _ in range(MAX_REDIRECTS + 1):
            target, headers, extensions = _hop(current, api_key)
            with client.stream("GET", target, headers=headers, extensions=extensions) as response:
                if response.is_redirect:
                    current = urljoin(current, response.headers.get("location", ""))
                    if urlparse(current).scheme != "https":
                        raise VideoError("The video download redirected to an insecure address.")
                    continue
                response.raise_for_status()
                return _read_capped(response)
    raise VideoError("The video download redirected too many times.")


def check_mp4(content: bytes | bytearray) -> None:
    """MP4 files start with a box whose type, at bytes 4-8, is ftyp."""
    if len(content) < 12 or bytes(content[4:8]) != b"ftyp":
        raise VideoError("Google sent something that is not an MP4 video, so nothing was saved.")


def load_start_image(workspace: Path, relative: str) -> tuple[bytes, str]:
    target = resolve_in_workspace(workspace, relative)
    if target is None:
        raise VideoError("The start image path is outside the workspace folder.")
    mime_type = IMAGE_TYPES.get(target.suffix.lower())
    if mime_type is None:
        raise VideoError("The start image must be a .png, .jpg, .jpeg or .webp file.")
    if not target.is_file():
        raise VideoError(f"No image at {relative}.")
    if target.stat().st_size > MAX_START_IMAGE_BYTES:
        raise VideoError(f"The start image is larger than {MAX_START_IMAGE_BYTES >> 20} MB.")
    try:
        fd = _open_no_follow(target, os.O_RDONLY)
        with os.fdopen(fd, "rb") as f:
            return f.read(MAX_START_IMAGE_BYTES + 1), mime_type
    except OSError as exc:
        raise VideoError(f"Could not read {relative}: {exc.strerror}.") from exc


def video_target(workspace: Path, relative: str) -> Path:
    target = resolve_in_workspace(workspace, relative)
    if target is None or target == workspace.resolve():
        raise VideoError("The output path is outside the workspace folder.")
    if target.suffix.lower() != ".mp4":
        raise VideoError("The output path must end in .mp4.")
    if target.is_dir():
        raise VideoError("The output path is a folder; give a file name ending in .mp4.")
    return target


def save_video(target: Path, content: bytes | bytearray) -> None:
    try:
        target.parent.mkdir(parents=True, exist_ok=True)
        fd = _open_no_follow(target, os.O_WRONLY | os.O_CREAT | os.O_TRUNC)
        with os.fdopen(fd, "wb") as f:
            f.write(content)
    except OSError as exc:
        raise VideoError(f"The video was made but could not be saved: {exc.strerror}.") from exc


class _Charge:
    """A video's budget reservation, settled exactly once: by the real cost, by the estimate, or released."""

    def __init__(self, tool: "GenerateVideoTool", estimate: float, estimate_tokens: tuple[int, int],
                 details: dict[str, Any]) -> None:
        self._tool, self.estimate, self._estimate_tokens, self._details = tool, estimate, estimate_tokens, details
        self.settled = False

    def record(self, tokens: tuple[int, int] | None, made: bool) -> float:
        """Record what Google billed; without usage figures, the estimate."""
        input_tokens, output_tokens = tokens or self._estimate_tokens
        self.settled = True
        return self._tool._hooks["record"](self._tool._agent_role, {
            **self._details, "model": self._tool._model, "input_tokens": input_tokens,
            "output_tokens": output_tokens, "made": made, "reserved_usd": self.estimate,
        })

    def release(self) -> None:
        if not self.settled:
            self.settled = True
            self._tool._hooks["release"](self.estimate)


class GenerateVideoTool(BaseTool):
    name: str = "Generate Video"
    description: str = (
        "Generates one short video clip (3 to 10 seconds, with sound) from a detailed prompt with Google's Gemini "
        "Omni Flash model and saves it as an MP4 in the workspace. Each clip costs money (about $0.10 per second "
        "at 720p), so plan the shot first, generate each clip once, and keep characters and style consistent "
        "across clips by repeating their descriptions."
    )
    args_schema: type[BaseModel] = VideoInput

    _agent_role: str = PrivateAttr(default="")
    _workspace: Path = PrivateAttr(default=Path("."))
    _model: str = PrivateAttr(default="")
    _price: dict[str, Any] | None = PrivateAttr(default=None)
    # Callables live in a dict: stored directly as private attributes they get bound like methods
    _hooks: dict[str, Callable[..., Any]] = PrivateAttr(default_factory=dict)

    def configure(self, agent_role: str, workspace: Path, model: str, price: dict[str, Any] | None,
                  reserve: Callable[[float], bool], release: Callable[[float], None],
                  record: VideoRecorder) -> "GenerateVideoTool":
        """reserve(estimate) holds budget for a video atomically; release(estimate) gives it back unspent."""
        self._agent_role = agent_role
        self._workspace = workspace
        self._model = model
        self._price = price
        self._hooks = {"reserve": reserve, "release": release, "record": record}
        return self

    def _run(self, prompt: str, output_path: str, duration_seconds: int = 8, aspect_ratio: str = "16:9",
             resolution: str = "720p", start_image: str | None = None) -> str:
        try:
            return self._generate(prompt, output_path, duration_seconds, aspect_ratio, resolution, start_image)
        except VideoError as exc:
            return f"No video was made: {exc}"

    def _generate(self, prompt: str, output_path: str, seconds: int, aspect_ratio: str, resolution: str,
                  start_image: str | None) -> str:
        if not prompt.strip():
            raise VideoError("the prompt is empty.")
        if resolution not in VIDEO_TOKENS_PER_SECOND or not MIN_SECONDS <= seconds <= MAX_SECONDS:
            raise VideoError(f"use {MIN_SECONDS} to {MAX_SECONDS} seconds and a resolution of 360p, 720p, 1080p or 4k.")
        if self._price is None:
            # Without a price the budget could not limit what videos cost
            raise VideoError(f"there is no price for {self._model}; add one on the Pricing page.")
        target = video_target(self._workspace, output_path)
        image = load_start_image(self._workspace, start_image) if start_image else None
        api_key = os.getenv("GEMINI_API_KEY")
        if not api_key:
            raise VideoError("GEMINI_API_KEY is not set.")

        tokens = estimate_tokens(prompt, seconds, resolution, image is not None)
        estimate = token_cost(self._price, *tokens)
        if not self._hooks["reserve"](estimate):
            raise VideoError(
                f"it would cost about ${estimate:.2f} and the run's budget has no room for it. Do not try again; "
                "describe the planned video in your answer instead."
            )
        charge = _Charge(self, estimate, tokens, {
            "prompt": prompt, "file": output_path, "duration_seconds": seconds, "aspect_ratio": aspect_ratio,
            "resolution": resolution,
        })
        try:
            body = build_request(self._model, prompt, seconds, aspect_ratio, resolution, image)
            return self._make(charge, body, api_key, target, output_path, seconds, aspect_ratio, resolution)
        finally:
            charge.release()

    def _make(self, charge: _Charge, body: dict[str, Any], api_key: str, target: Path, output_path: str,
              seconds: int, aspect_ratio: str, resolution: str) -> str:
        try:
            data = self._request(body, api_key)
        except MaybeCharged as exc:
            charge.record(None, made=False)
            raise VideoError(
                f"{exc} Google may still have billed it, so about ${charge.estimate:.2f} was counted against the "
                "budget. Do not try again; describe the planned video in your answer instead."
            ) from exc

        tokens = usage_tokens(data)
        status = data.get("status")
        if status not in (None, "completed"):
            # Google does not bill failed generations; anything it reports using, or an unfinished one, is counted
            if tokens and tokens[1] > 0:
                charge.record(tokens, made=False)
            elif status == "incomplete":
                charge.record(None, made=False)
            raise VideoError(f"Google did not finish the video ({failure_reason(data)}).")

        video = find_video(data)
        # A video with no output tokens reported is still paid for: fall back to the estimate
        cost = charge.record(tokens if tokens and tokens[1] > 0 else None, made=video is not None)
        if video is None:
            raise VideoError("Google returned no video, which usually means its safety filters blocked it. "
                             "Rewrite the prompt before trying again.")
        try:
            content = self._content(video, api_key)
            check_mp4(content)
            save_video(target, content)
        except VideoError as exc:
            raise VideoError(f"{exc} It was already paid for (${cost:.2f}), and trying again pays again.") from exc
        return (f"Saved a {seconds}-second {resolution} {aspect_ratio} video to {output_path} "
                f"({len(content) / (1 << 20):.1f} MB). Cost about ${cost:.2f}.")

    def _request(self, body: dict[str, Any], api_key: str) -> dict[str, Any]:
        try:
            # Send the key as a header so it never appears in URLs or logs
            response = httpx.post(INTERACTIONS_URL, headers={"x-goog-api-key": api_key}, json=body,
                                  timeout=GENERATE_TIMEOUT)
            response.raise_for_status()
        except httpx.HTTPStatusError as exc:
            status = exc.response.status_code
            message = f"HTTP {status} - {_error_message(exc.response)}."
            if status >= 500:
                raise MaybeCharged(message) from exc
            hint = (" The API key must be allowed to use the Generative Language API (Gemini API)."
                    if status in (401, 403) else "")
            raise VideoError(message + hint) from exc
        except (httpx.ConnectError, httpx.ConnectTimeout) as exc:
            # The request never reached Google
            raise VideoError(f"could not reach Google ({type(exc).__name__}).") from exc
        except httpx.HTTPError as exc:
            raise MaybeCharged(f"the request failed part-way ({type(exc).__name__}).") from exc
        try:
            data = response.json()
        except ValueError as exc:
            raise MaybeCharged("Google's response was not JSON.") from exc
        if not isinstance(data, dict):
            raise MaybeCharged("Google's response was not a JSON object.")
        return data

    def _content(self, video: dict[str, Any], api_key: str) -> bytes | bytearray:
        if video.get("data"):
            try:
                content = base64.b64decode(video["data"], validate=True)
            except ValueError as exc:
                raise VideoError("Google's video data could not be decoded.") from exc
            if len(content) > MAX_VIDEO_BYTES:
                raise VideoError(f"The video is larger than {MAX_VIDEO_BYTES >> 20} MB; ask for a lower resolution.")
            return content
        try:
            return download_video(video["uri"], api_key)
        except httpx.HTTPStatusError as exc:
            raise VideoError(f"The download failed: HTTP {exc.response.status_code}.") from exc
        except httpx.HTTPError as exc:
            raise VideoError(f"The download failed: {type(exc).__name__}.") from exc
