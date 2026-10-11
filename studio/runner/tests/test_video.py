"""The Generate Video tool (Gemini Omni Flash), its budget reservation, metering and run event."""

import base64
import threading
from pathlib import Path
from typing import Any

import httpx
import pytest

from studio_runner import video
from studio_runner.listeners import Listeners
from studio_runner.meter import Meter
from studio_runner.tools import ToolFactory
from studio_runner.video import (
    GenerateVideoTool,
    VideoError,
    build_request,
    check_mp4,
    download_video,
    estimate_cost,
    find_video,
    usage_tokens,
)

from .conftest import PRICES, SETTINGS

OMNI = "gemini-omni-1.1-flash"
OMNI_PRICE = {"model": OMNI, "input_per_mtok": 1.50, "cached_input_per_mtok": 1.50, "output_per_mtok": 17.50}
VIDEO_BYTES = b"\x00\x00\x00\x18ftypmp42 fake video"
FILE_META = "https://generativelanguage.googleapis.com/v1beta/files/abc"
FILE_URI = FILE_META + ":download?alt=media"
STORAGE_IP = "203.0.113.5"


def completed(content: dict[str, Any], output_tokens: int = 46336) -> dict[str, Any]:
    return {
        "id": "int-1",
        "status": "completed",
        "steps": [
            {"type": "thought", "content": []},
            {"type": "model_output", "content": [{"type": "text", "text": "ok"}, content]},
        ],
        "usage": {"total_input_tokens": 20, "total_output_tokens": output_tokens, "total_thought_tokens": 0},
    }


INLINE = {"type": "video", "mime_type": "video/mp4", "data": base64.b64encode(VIDEO_BYTES).decode()}


class Ledger:
    """Stands in for the meter: records reservations, releases and recorded charges."""

    def __init__(self, allow: bool = True) -> None:
        self.allow = allow
        self.reserved: list[float] = []
        self.released: list[float] = []
        self.calls: list[tuple[str, dict[str, Any]]] = []

    def reserve(self, estimate: float) -> bool:
        self.reserved.append(estimate)
        return self.allow

    def release(self, estimate: float) -> None:
        self.released.append(estimate)

    def record(self, role: str, details: dict[str, Any]) -> float:
        self.calls.append((role, details))
        return 0.81

    def settled_once(self) -> bool:
        """Every granted reservation was either recorded or released, never both."""
        return len(self.reserved) == len(self.released) + len(self.calls) if self.allow else not self.calls


def make_tool(tmp_path: Path, ledger: Ledger, price: dict[str, Any] | None = OMNI_PRICE) -> GenerateVideoTool:
    return GenerateVideoTool().configure("Video Producer", tmp_path, OMNI, price, ledger.reserve, ledger.release, ledger.record)


def fake_post(monkeypatch: pytest.MonkeyPatch, response: httpx.Response | Exception) -> list[dict[str, Any]]:
    sent: list[dict[str, Any]] = []

    def post(url: str, headers: dict[str, str], json: dict[str, Any], timeout: Any) -> httpx.Response:
        sent.append({"url": url, "headers": headers, "json": json})
        if isinstance(response, Exception):
            raise response
        response.request = httpx.Request("POST", url)
        return response

    monkeypatch.setattr(video.httpx, "post", post)
    return sent


@pytest.fixture(autouse=True)
def gemini_key(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GEMINI_API_KEY", "AIza-test")


class TestRequestShape:
    def test_small_video_is_delivered_inline(self):
        body = build_request(OMNI, "a cat", 6, "9:16", "720p", None)
        assert body["model"] == OMNI and body["input"] == "a cat"
        assert body["response_format"] == {
            "type": "video", "aspect_ratio": "9:16", "resolution": "720p", "duration": "6s", "delivery": "inline",
        }
        assert body["store"] is False and body["background"] is False and body["stream"] is False
        assert "generation_config" not in body

    @pytest.mark.parametrize("resolution", ["1080p", "4k"])
    def test_large_video_is_delivered_as_a_file(self, resolution: str):
        assert build_request(OMNI, "a cat", 8, "16:9", resolution, None)["response_format"]["delivery"] == "uri"

    def test_start_image_goes_first_and_sets_image_to_video(self):
        body = build_request(OMNI, "a cat", 8, "16:9", "720p", (b"png-bytes", "image/png"))
        assert body["input"][0] == {"type": "image", "data": base64.b64encode(b"png-bytes").decode(), "mime_type": "image/png"}
        assert body["input"][1] == {"type": "text", "text": "a cat"}
        assert body["generation_config"] == {"video_config": {"task": "image_to_video"}}


class TestHelpers:
    def test_estimate_is_tokens_per_second_at_the_output_price(self):
        # 8 s at 720p = 46,336 tokens at $17.50 per million, about $0.81
        assert estimate_cost(OMNI_PRICE, "", 8, "720p", False) == pytest.approx(0.81088)
        assert estimate_cost(OMNI_PRICE, "", 8, "4k", False) == pytest.approx(3 * 0.81088)
        assert estimate_cost(None, "x" * 400, 8, "720p", True) == 0.0

    def test_find_video_skips_other_steps_and_content(self):
        assert find_video(completed(INLINE)) == INLINE
        assert find_video({"steps": [{"type": "model_output", "content": [{"type": "text", "text": "no"}]}]}) is None
        assert find_video({}) is None

    def test_usage_counts_thinking_as_output(self):
        assert usage_tokens({"usage": {"total_input_tokens": 5, "total_output_tokens": 10, "total_thought_tokens": 3}}) == (5, 13)
        assert usage_tokens({}) is None

    def test_file_id_only_accepts_plain_ids(self):
        assert video._file_id(FILE_URI) == "abc"
        assert video._file_id(FILE_META) == "abc"
        assert video._file_id("https://generativelanguage.googleapis.com/v1beta/other") is None
        assert video._file_id("https://generativelanguage.googleapis.com/v1beta/files/a%2F..:download") is None

    def test_check_mp4(self):
        check_mp4(VIDEO_BYTES)
        for bad in (b"", b'{"name": "files/abc"}', b"<html>error</html>"):
            with pytest.raises(VideoError, match="not an MP4"):
                check_mp4(bad)


class TestGenerate:
    def test_saves_inline_video_and_records_real_usage(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        sent = fake_post(monkeypatch, httpx.Response(200, json=completed(INLINE)))
        ledger = Ledger()
        out = make_tool(tmp_path, ledger)._run("a cat on a skateboard", "videos/cat.mp4", 8, "16:9", "720p")

        assert (tmp_path / "videos/cat.mp4").read_bytes() == VIDEO_BYTES
        assert "Saved a 8-second 720p 16:9 video to videos/cat.mp4" in out and "$0.81" in out
        assert sent[0]["url"] == video.INTERACTIONS_URL
        assert sent[0]["headers"] == {"x-goog-api-key": "AIza-test"}
        estimate = estimate_cost(OMNI_PRICE, "a cat on a skateboard", 8, "720p", False)
        assert ledger.reserved == [pytest.approx(estimate)] and ledger.released == []
        role, details = ledger.calls[0]
        assert role == "Video Producer" and details["reserved_usd"] == pytest.approx(estimate)
        assert details["model"] == OMNI and details["input_tokens"] == 20 and details["output_tokens"] == 46336
        assert details["file"] == "videos/cat.mp4" and details["made"] is True

    def test_refuses_when_the_budget_has_no_room(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        sent = fake_post(monkeypatch, httpx.Response(200, json=completed(INLINE)))
        ledger = Ledger(allow=False)
        out = make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert out.startswith("No video was made") and "budget" in out
        assert sent == [] and ledger.calls == [] and ledger.released == []

    def test_refuses_without_a_price(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        sent = fake_post(monkeypatch, httpx.Response(200, json=completed(INLINE)))
        ledger = Ledger()
        assert "no price for gemini-omni-1.1-flash" in make_tool(tmp_path, ledger, price=None)._run("a cat", "cat.mp4")
        assert sent == [] and ledger.reserved == []

    def test_failed_generation_releases_the_reservation(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json={"status": "failed", "errors": [{"code": "safety", "message": "blocked"}]}))
        ledger = Ledger()
        out = make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert "safety: blocked" in out and ledger.calls == [] and len(ledger.released) == 1
        assert not (tmp_path / "cat.mp4").exists()

    def test_failed_generation_with_usage_is_charged_what_it_used(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json={"status": "failed", "usage": {"total_input_tokens": 9, "total_output_tokens": 500}}))
        ledger = Ledger()
        make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert ledger.calls[0][1]["output_tokens"] == 500 and ledger.calls[0][1]["made"] is False
        assert ledger.settled_once()

    def test_incomplete_generation_is_charged_the_estimate(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json={"status": "incomplete"}))
        ledger = Ledger()
        make_tool(tmp_path, ledger)._run("a cat", "cat.mp4", 5, "16:9", "360p")
        assert ledger.calls[0][1]["output_tokens"] == 5 * video.VIDEO_TOKENS_PER_SECOND["360p"]
        assert ledger.settled_once()

    def test_completed_without_video_is_charged_and_explained(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json=completed({"type": "text", "text": "sorry"})))
        ledger = Ledger()
        out = make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert "safety filters" in out
        assert ledger.calls[0][1]["made"] is False and ledger.settled_once()

    @pytest.mark.parametrize("usage", [None, {"total_input_tokens": 3, "total_output_tokens": 0}])
    def test_video_without_output_usage_charges_the_estimate(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, usage: Any):
        data = completed(INLINE)
        if usage is None:
            del data["usage"]
        else:
            data["usage"] = usage
        fake_post(monkeypatch, httpx.Response(200, json=data))
        ledger = Ledger()
        make_tool(tmp_path, ledger)._run("a cat", "cat.mp4", 5, "16:9", "360p")
        assert ledger.calls[0][1]["output_tokens"] == 5 * video.VIDEO_TOKENS_PER_SECOND["360p"]

    @pytest.mark.parametrize("status", [400, 403])
    def test_client_errors_are_not_charged(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, status: int):
        fake_post(monkeypatch, httpx.Response(status, json={"error": {"message": "Requests to this API are blocked."}}))
        ledger = Ledger()
        out = make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert f"HTTP {status}" in out and ledger.calls == [] and len(ledger.released) == 1
        assert ("Generative Language API" in out) == (status == 403)

    @pytest.mark.parametrize("failure", [
        httpx.Response(503, json={"error": {"message": "overloaded"}}),
        httpx.ReadTimeout("slow"),
        httpx.RemoteProtocolError("dropped"),
        httpx.Response(200, content=b"not json"),
        httpx.Response(200, json=[1, 2]),
    ])
    def test_unknown_outcomes_charge_the_estimate_and_say_not_to_retry(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, failure: Any):
        fake_post(monkeypatch, failure)
        ledger = Ledger()
        out = make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert "may still have billed it" in out and "Do not try again" in out
        assert ledger.calls[0][1]["made"] is False and ledger.released == []

    @pytest.mark.parametrize("error", [httpx.ConnectError("down"), httpx.ConnectTimeout("no route")])
    def test_never_reaching_google_is_not_charged(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, error: Exception):
        fake_post(monkeypatch, error)
        ledger = Ledger()
        assert "could not reach Google" in make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert ledger.calls == [] and len(ledger.released) == 1

    def test_without_key(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        monkeypatch.delenv("GEMINI_API_KEY")
        ledger = Ledger()
        assert "GEMINI_API_KEY is not set" in make_tool(tmp_path, ledger)._run("a cat", "cat.mp4")
        assert ledger.reserved == []

    @pytest.mark.parametrize("path, reason", [("../escape.mp4", "outside"), ("clip.mov", ".mp4"), ("", "outside"), ("dir.mp4", "folder")])
    def test_bad_output_paths(self, tmp_path: Path, path: str, reason: str):
        (tmp_path / "dir.mp4").mkdir()
        assert reason in make_tool(tmp_path, Ledger())._run("a cat", path)

    @pytest.mark.parametrize("seconds, resolution", [(2, "720p"), (11, "720p"), (8, "8k")])
    def test_out_of_range_settings(self, tmp_path: Path, seconds: int, resolution: str):
        assert make_tool(tmp_path, Ledger())._run("a cat", "c.mp4", seconds, "16:9", resolution).startswith("No video")

    def test_empty_prompt(self, tmp_path: Path):
        assert "prompt is empty" in make_tool(tmp_path, Ledger())._run("  ", "c.mp4")

    def test_start_image_is_sent(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        (tmp_path / "frame.png").write_bytes(b"png")
        sent = fake_post(monkeypatch, httpx.Response(200, json=completed(INLINE)))
        make_tool(tmp_path, Ledger())._run("a cat", "c.mp4", start_image="frame.png")
        assert sent[0]["json"]["input"][0]["mime_type"] == "image/png"

    @pytest.mark.parametrize("image, reason", [("../x.png", "outside"), ("notes.txt", ".png"), ("missing.png", "No image")])
    def test_bad_start_images(self, tmp_path: Path, image: str, reason: str):
        (tmp_path / "notes.txt").write_text("hi")
        assert reason in make_tool(tmp_path, Ledger())._run("a cat", "c.mp4", start_image=image)

    def test_start_image_too_large(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        monkeypatch.setattr(video, "MAX_START_IMAGE_BYTES", 2)
        (tmp_path / "big.png").write_bytes(b"png!")
        assert "larger than" in make_tool(tmp_path, Ledger())._run("a cat", "c.mp4", start_image="big.png")

    def test_start_image_unreadable(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        (tmp_path / "frame.png").write_bytes(b"png")
        monkeypatch.setattr(video, "_open_no_follow", lambda *a: (_ for _ in ()).throw(OSError(13, "Permission denied")))
        assert "Could not read frame.png" in make_tool(tmp_path, Ledger())._run("a cat", "c.mp4", start_image="frame.png")

    def test_save_failure_says_it_was_paid_for(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json=completed(INLINE)))
        monkeypatch.setattr(video, "_open_no_follow", lambda *a: (_ for _ in ()).throw(OSError(28, "No space left")))
        ledger = Ledger()
        out = make_tool(tmp_path, ledger)._run("a cat", "c.mp4")
        assert "could not be saved" in out and "already paid for" in out and ledger.settled_once()

    @pytest.mark.parametrize("item, reason", [
        ({"type": "video", "data": "!!not base64!!"}, "could not be decoded"),
        ({"type": "video", "data": base64.b64encode(b"x" * 20).decode()}, "larger than"),
        ({"type": "video", "data": base64.b64encode(b'{"e": 1}').decode()}, "not an MP4"),
    ])
    def test_bad_inline_video(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, item: dict[str, Any], reason: str):
        monkeypatch.setattr(video, "MAX_VIDEO_BYTES", 16)
        fake_post(monkeypatch, httpx.Response(200, json=completed(item)))
        out = make_tool(tmp_path, Ledger())._run("a cat", "c.mp4")
        assert reason in out and not (tmp_path / "c.mp4").exists()

    @pytest.mark.parametrize("error, reason", [
        (httpx.HTTPStatusError("x", request=httpx.Request("GET", FILE_URI), response=httpx.Response(410)), "HTTP 410"),
        (httpx.ReadTimeout("slow"), "ReadTimeout"),
    ])
    def test_download_errors_reach_the_agent(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch, error: Exception, reason: str):
        fake_post(monkeypatch, httpx.Response(200, json=completed({"type": "video", "uri": FILE_URI})))

        def fail(uri: str, key: str) -> bytes:
            raise error

        monkeypatch.setattr(video, "download_video", fail)
        assert reason in make_tool(tmp_path, Ledger())._run("a cat", "hd.mp4", 8, "16:9", "1080p")


class FakeStreamClient:
    """Stands in for httpx.Client: serves canned responses by URL and remembers the headers each request sent."""

    def __init__(self, routes: dict[str, httpx.Response], seen: list[tuple[str, dict[str, str]]]) -> None:
        self.routes, self.seen = routes, seen

    def __enter__(self) -> "FakeStreamClient":
        return self

    def __exit__(self, *exc: Any) -> None:
        return None

    def _respond(self, method: str, url: str, headers: dict[str, str]) -> httpx.Response:
        self.seen.append((url, dict(headers)))
        response = self.routes[url]
        response.request = httpx.Request(method, url)
        return response

    def get(self, url: str, headers: dict[str, str]) -> httpx.Response:
        return self._respond("GET", url, headers)

    def stream(self, method: str, url: str, headers: dict[str, str], extensions: dict[str, str] | None = None) -> Any:
        response = self._respond(method, url, headers)

        class Ctx:
            def __enter__(self) -> httpx.Response:
                return response

            def __exit__(self, *exc: Any) -> None:
                return None

        return Ctx()


def patch_client(monkeypatch: pytest.MonkeyPatch, routes: dict[str, httpx.Response],
                 resolved: tuple[str | None, str | None] = (None, STORAGE_IP)) -> list[tuple[str, dict[str, str]]]:
    seen: list[tuple[str, dict[str, str]]] = []
    monkeypatch.setattr(video.httpx, "Client", lambda **kw: FakeStreamClient(routes, seen))
    monkeypatch.setattr(video, "resolve_public", lambda url: resolved)
    return seen


ACTIVE = httpx.Response(200, json={"state": "ACTIVE"})


class TestDownload:
    def test_waits_for_the_file_then_drops_the_key_and_pins_the_address_off_google(self, monkeypatch: pytest.MonkeyPatch):
        states = iter(["PROCESSING", "ACTIVE"])

        class MetaResponse(httpx.Response):
            def json(self, **kw: Any) -> Any:
                return {"state": next(states)}

        seen = patch_client(monkeypatch, {
            FILE_META: MetaResponse(200),
            FILE_URI: httpx.Response(302, headers={"location": "https://storage.example.com/v.mp4"}),
            f"https://{STORAGE_IP}/v.mp4": httpx.Response(200, content=VIDEO_BYTES),
        })
        sleeps: list[float] = []
        assert download_video(FILE_META, "AIza-test", sleep=sleeps.append) == VIDEO_BYTES
        assert sleeps == [video.FILE_POLL_SECONDS]
        assert seen[0] == (FILE_META, {"x-goog-api-key": "AIza-test"})
        # The download URL is built from the file id, whichever form of link Google returned
        assert seen[-2] == (FILE_URI, {"x-goog-api-key": "AIza-test"})
        assert seen[-1] == (f"https://{STORAGE_IP}/v.mp4", {"Host": "storage.example.com"})

    @pytest.mark.parametrize("uri", [
        "https://evil.example.com/v1beta/files/abc:download",
        "http://generativelanguage.googleapis.com/v1beta/files/abc:download",
        "https://generativelanguage.googleapis.com/v1beta/loop",
    ])
    def test_never_sends_the_key_anywhere_but_a_gemini_file(self, uri: str, monkeypatch: pytest.MonkeyPatch):
        seen = patch_client(monkeypatch, {})
        with pytest.raises(VideoError, match="not a Gemini API file"):
            download_video(uri, "AIza-test")
        assert seen == []

    def test_refuses_insecure_redirect(self, monkeypatch: pytest.MonkeyPatch):
        patch_client(monkeypatch, {
            FILE_META: ACTIVE, FILE_URI: httpx.Response(302, headers={"location": "http://storage.example.com/v.mp4"}),
        })
        with pytest.raises(VideoError, match="insecure"):
            download_video(FILE_URI, "AIza-test")

    def test_redirect_to_blocked_address(self, monkeypatch: pytest.MonkeyPatch):
        patch_client(monkeypatch, {
            FILE_META: ACTIVE, FILE_URI: httpx.Response(302, headers={"location": "https://internal.example/v.mp4"}),
        }, resolved=("internal.example resolves to a private or local address, which is blocked.", None))
        with pytest.raises(VideoError, match="blocked"):
            download_video(FILE_URI, "AIza-test")

    def test_too_many_redirects(self, monkeypatch: pytest.MonkeyPatch):
        patch_client(monkeypatch, {FILE_META: ACTIVE, FILE_URI: httpx.Response(302, headers={"location": FILE_URI})})
        with pytest.raises(VideoError, match="too many"):
            download_video(FILE_URI, "AIza-test")

    def test_failed_file(self, monkeypatch: pytest.MonkeyPatch):
        patch_client(monkeypatch, {FILE_META: httpx.Response(200, json={"state": "FAILED"})})
        with pytest.raises(VideoError, match="could not prepare"):
            download_video(FILE_URI, "AIza-test")

    def test_file_never_ready(self, monkeypatch: pytest.MonkeyPatch):
        patch_client(monkeypatch, {FILE_META: httpx.Response(200, json={"state": "PROCESSING"})})
        with pytest.raises(VideoError, match="five minutes"):
            download_video(FILE_URI, "AIza-test", sleep=lambda _s: None)

    def test_download_size_cap(self, monkeypatch: pytest.MonkeyPatch):
        monkeypatch.setattr(video, "MAX_VIDEO_BYTES", 4)
        patch_client(monkeypatch, {FILE_META: ACTIVE, FILE_URI: httpx.Response(200, content=VIDEO_BYTES)})
        with pytest.raises(VideoError, match="larger than"):
            download_video(FILE_URI, "AIza-test")

    def test_tool_downloads_file_delivery(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json=completed({"type": "video", "uri": FILE_URI})))
        patch_client(monkeypatch, {FILE_META: ACTIVE, FILE_URI: httpx.Response(200, content=VIDEO_BYTES)})
        out = make_tool(tmp_path, Ledger())._run("a cat", "hd.mp4", 8, "16:9", "1080p")
        assert "Saved" in out and (tmp_path / "hd.mp4").read_bytes() == VIDEO_BYTES

    def test_tool_refuses_to_save_a_non_video_download(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
        fake_post(monkeypatch, httpx.Response(200, json=completed({"type": "video", "uri": FILE_URI})))
        patch_client(monkeypatch, {FILE_META: ACTIVE, FILE_URI: httpx.Response(200, json={"name": "files/abc"})})
        out = make_tool(tmp_path, Ledger())._run("a cat", "hd.mp4", 8, "16:9", "1080p")
        assert "not an MP4" in out and not (tmp_path / "hd.mp4").exists()


class TestMeteringAndEvents:
    def meter(self, budget: float = 1.0) -> Meter:
        return Meter(PRICES + [OMNI_PRICE], SETTINGS, budget, 0, lambda *a: None)

    def test_reservations_hold_budget_until_recorded_or_released(self):
        meter = self.meter()
        assert meter.reserve_video(0.81)
        # A second video cannot fit while the first is being generated
        assert not meter.reserve_video(0.81)
        cost = meter.record_video(OMNI, 20, 46336, reserved_usd=0.81)
        assert cost == pytest.approx((20 * 1.50 + 46336 * 17.50) / 1_000_000)
        assert meter.reserve_video(0.15)
        meter.release_video(0.15)
        assert meter.reserve_video(0.18)
        summary = meter.summary()
        assert summary["videos"] == 1 and summary["video_usd"] == pytest.approx(cost, abs=1e-6)
        assert summary["total_usd"] == pytest.approx(cost, abs=1e-6)

    def test_concurrent_reservations_never_exceed_the_budget(self):
        meter = self.meter(budget=3.0)
        granted: list[bool] = []
        lock = threading.Lock()

        def reserve() -> None:
            ok = meter.reserve_video(0.81)
            with lock:
                granted.append(ok)

        threads = [threading.Thread(target=reserve) for _ in range(20)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        assert granted.count(True) == 3

    def test_unpriced_video_model_is_flagged(self):
        meter = Meter(PRICES, SETTINGS, 1.0, 0, lambda *a: None)
        assert meter.record_video(OMNI, 1, 1) == 0.0
        assert meter.summary()["unpriced_models"] == [OMNI]

    def test_listener_emits_a_video_event_and_settles_the_reservation(self, emitter):
        meter = self.meter(budget=2.0)
        assert meter.reserve_video(0.81)
        listeners = Listeners(emitter, meter)
        cost = listeners.record_video("Video Producer", {
            "model": OMNI, "input_tokens": 20, "output_tokens": 46336, "prompt": "a cat", "file": "cat.mp4",
            "duration_seconds": 8, "aspect_ratio": "16:9", "resolution": "720p", "made": True, "reserved_usd": 0.81,
        })
        [event] = emitter.of_type("video")
        assert event["tool_name"] == "Generate Video" and event["agent_role"] == "Video Producer"
        assert event["cost_usd"] == pytest.approx(cost) and event["file"] == "cat.mp4"
        # The reservation was replaced by the real cost, so about $1.19 is left
        assert meter.reserve_video(1.18) and not meter.reserve_video(0.02)

    def test_factory_builds_the_tool_with_the_catalog_model_and_its_price(self, tmp_path: Path):
        meter = self.meter()
        factory = ToolFactory("gemini-3.8-flash", lambda: True, lambda *a: None, tmp_path, prices=[OMNI_PRICE],
                              reserve_video=meter.reserve_video, release_video=meter.release_video)
        assert factory.priced_model("generate_video") == OMNI
        tool = factory.build("generate_video", "Video Producer")
        assert isinstance(tool, GenerateVideoTool) and tool._model == OMNI and tool._price == OMNI_PRICE
        assert tool._hooks["reserve"] == meter.reserve_video
        assert tool._hooks["record"]("R", {}) == 0.0
