"""The tools agents can be given. Keys match tools_catalog.json.

Each agent gets its own tool instances so a search can be attributed to the agent that ran it.
"""

import functools
import ipaddress
import json
import os
import socket
import threading
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urldefrag, urljoin, urlparse

import httpx
from crewai.tools import BaseTool
from pydantic import BaseModel, Field, PrivateAttr

from .state_tools import STATE_TOOL_KEYS, build_state_tool
from .tracker_tools import TRACKER_TOOL_KEYS, build_tracker_tool
from .report_tools import REPORT_TOOL_KEYS, build_report_tool

CATALOG_PATH = Path(__file__).resolve().parent.parent / "tools_catalog.json"

GEMINI_API_URL = "https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent"
VERTEX_API_URL = "https://aiplatform.googleapis.com/v1/publishers/google/models/{model}:generateContent"
# Gemini API (AI Studio) keys start with this; Vertex AI keys (e.g. "AQ.") do not
GEMINI_API_KEY_PREFIX = "AIza"
SEARCH_TIMEOUT_SECONDS = 60.0
SEARCH_MAX_OUTPUT_TOKENS = 4096
# Searches one agent may run in a run. Each result is resent on every later LLM call of the agent, so they add up
MAX_SEARCHES_PER_AGENT = 10
# Gemini cites its sources through these redirect links; agents need the real page URLs to cite and read them
GROUNDING_REDIRECT_PREFIX = "https://vertexaisearch.cloud.google.com/grounding-api-redirect/"
SOURCE_RESOLVE_TIMEOUT_SECONDS = 5.0
SOURCE_RESOLVE_WORKERS = 8

SCRAPE_TIMEOUT_SECONDS = 20.0
SCRAPE_MAX_BYTES = 2_000_000
SCRAPE_MAX_CHARS = 20_000
SCRAPE_MAX_REDIRECTS = 5
# Links listed after a page's text, so an agent can read on without searching for URLs
SCRAPE_MAX_LINKS = 150
SCRAPE_MAX_LINK_TEXT = 80

WORKSPACE_MAX_READ_BYTES = 200_000
WORKSPACE_MAX_WRITE_BYTES = 1_000_000

# (agent_role, query, model, input_tokens, output_tokens, billable_searches) -> None
SearchRecorder = Callable[[str, str, str, int, int, int], None]


# What a tool does outside the run (docs/agent-chat-handoffs-design.md, section 7). Agent chat gives an agent only
# tools that read; Crew Studio refuses to start with an unclassified tool, and here one counts as changing things.
EFFECT_READS = "reads"
EFFECT_PROPOSES = "proposes"
EFFECT_CHANGES = "changes"
EFFECTS = (EFFECT_READS, EFFECT_PROPOSES, EFFECT_CHANGES)


def load_catalog() -> list[dict[str, Any]]:
    return json.loads(CATALOG_PATH.read_text(encoding="utf-8"))


@functools.lru_cache(maxsize=1)
def catalog_by_key() -> dict[str, dict[str, Any]]:
    """The catalog keyed by tool key, in the catalog's order (read once per run)."""
    return {entry["key"]: entry for entry in load_catalog()}


def tool_effect(key: str, catalog: dict[str, dict[str, Any]] | None = None) -> str:
    """What the tool does outside the run: reads, proposes or changes. A tool the catalog does not know, or whose
    effect is missing or unknown, counts as one that changes things (fail closed)."""
    effect = (catalog if catalog is not None else catalog_by_key()).get(key, {}).get("effect")
    return effect if effect in EFFECTS else EFFECT_CHANGES


def tool_needs(key: str, catalog: dict[str, dict[str, Any]] | None = None) -> tuple[str, ...]:
    """What the tool needs besides keys: crew, tracker, km2, schedule."""
    needs = (catalog if catalog is not None else catalog_by_key()).get(key, {}).get("needs") or []
    return tuple(str(need) for need in needs)


# ---------------------------------------------------------------- Google Search


class SearchInput(BaseModel):
    query: str = Field(..., description="What to search the web for.")


class GoogleSearchTool(BaseTool):
    name: str = "Google Search"
    description: str = (
        "Searches Google for current information and returns a summarized "
        "answer with the source URLs it was based on. Searches are limited, so read "
        "the pages you have found before searching again."
    )
    args_schema: type[BaseModel] = SearchInput

    _agent_role: str = PrivateAttr(default="")
    _search_model: str = PrivateAttr(default="")
    # Callables live in a dict: stored directly as private attributes they get bound like methods
    _hooks: dict[str, Callable[..., Any]] = PrivateAttr(default_factory=dict)
    # Searches this agent may run ("max") and has run ("run")
    _limits: dict[str, int] = PrivateAttr(default_factory=lambda: {"max": MAX_SEARCHES_PER_AGENT, "run": 0})
    # An agent can run several searches at once
    _lock: Any = PrivateAttr(default_factory=threading.Lock)

    def configure(self, agent_role: str, search_model: str, allowed: Callable[[], bool], record: SearchRecorder,
                  max_searches: int = MAX_SEARCHES_PER_AGENT) -> "GoogleSearchTool":
        self._agent_role = agent_role
        self._search_model = search_model
        self._hooks = {"allowed": allowed, "record": record}
        self._limits = {"max": max_searches, "run": 0}
        return self

    def _take_search(self) -> int | None:
        """Count one search against this agent's cap. Returns the searches left after it, or None when none are left."""
        with self._lock:
            if self._limits["run"] >= self._limits["max"]:
                return None
            self._limits["run"] += 1
            return self._limits["max"] - self._limits["run"]

    def _run(self, query: str) -> str:
        if not self._hooks.get("allowed", lambda: True)():
            return (
                "Search budget for this run is used up. Do not search again; "
                "write your final answer now from what you have already found."
            )
        api_key = os.getenv("GEMINI_API_KEY")
        if not api_key:
            return "Google Search unavailable: GEMINI_API_KEY is not set."
        left = self._take_search()
        if left is None:
            return (
                f"You have used all {self._limits['max']} of your searches for this run. Do not search again; read "
                "pages you already have URLs for with Scrape Website, or write your final answer from what you have found."
            )

        url_template = GEMINI_API_URL if api_key.startswith(GEMINI_API_KEY_PREFIX) else VERTEX_API_URL
        payload = {
            "contents": [{"role": "user", "parts": [{"text": query}]}],
            "tools": [{"google_search": {}}],
            "generationConfig": {"temperature": 0.2, "maxOutputTokens": SEARCH_MAX_OUTPUT_TOKENS},
        }
        try:
            response = httpx.post(
                url_template.format(model=self._search_model),
                # Send the key as a header so it never appears in URLs or logs
                headers={"x-goog-api-key": api_key},
                json=payload,
                timeout=SEARCH_TIMEOUT_SECONDS,
            )
            response.raise_for_status()
        except httpx.HTTPStatusError as exc:
            return f"Google Search failed: HTTP {exc.response.status_code} - {_error_message(exc.response)}"
        except httpx.HTTPError as exc:
            return f"Google Search failed: {type(exc).__name__}: {exc}"

        data = response.json()
        record = self._hooks.get("record")
        if record is not None:
            usage = data.get("usageMetadata", {})
            candidate = (data.get("candidates") or [{}])[0]
            metadata = candidate.get("groundingMetadata", {})
            # Gemini 3 bills each search query the model ran; no queries means nothing to bill
            billable = len(metadata.get("webSearchQueries") or [])
            if billable == 0 and metadata.get("groundingChunks"):
                billable = 1
            record(
                self._agent_role,
                query,
                self._search_model,
                int(usage.get("promptTokenCount", 0)),
                # Thinking tokens are billed as output
                int(usage.get("candidatesTokenCount", 0)) + int(usage.get("thoughtsTokenCount", 0)),
                billable,
            )
        resolved = resolve_source_urls([source.get("uri", "") for source in _search_sources(data)])
        return f"{format_search_result(data, resolved)}\n\nSearches left for you in this run: {left}."


def _error_message(response: httpx.Response) -> str:
    try:
        return response.json()["error"]["message"]
    except (ValueError, KeyError, TypeError):
        return response.text[:500]


def _search_sources(data: dict[str, Any]) -> list[dict[str, Any]]:
    candidate = (data.get("candidates") or [{}])[0]
    metadata = candidate.get("groundingMetadata", {})
    return [chunk["web"] for chunk in metadata.get("groundingChunks", []) if "web" in chunk]


def _redirect_target(uri: str) -> str | None:
    try:
        response = httpx.head(uri, follow_redirects=False, timeout=SOURCE_RESOLVE_TIMEOUT_SECONDS)
    except httpx.HTTPError:
        return None
    location = response.headers.get("location", "")
    return location if response.is_redirect and location.startswith(("https://", "http://")) else None


def resolve_source_urls(uris: list[str]) -> dict[str, str]:
    """Map Gemini's grounding redirect links to the pages they lead to. Only the redirect's Location is read; the page
    itself is not fetched. A link that cannot be resolved is left out, so it is shown as given."""
    redirects = list(dict.fromkeys(uri for uri in uris if uri.startswith(GROUNDING_REDIRECT_PREFIX)))
    if not redirects:
        return {}
    with ThreadPoolExecutor(max_workers=min(len(redirects), SOURCE_RESOLVE_WORKERS)) as pool:
        targets = list(pool.map(_redirect_target, redirects))
    return {uri: target for uri, target in zip(redirects, targets) if target}


def format_search_result(data: dict[str, Any], resolved: dict[str, str] | None = None) -> str:
    candidate = (data.get("candidates") or [{}])[0]
    parts = candidate.get("content", {}).get("parts", [])
    answer = "".join(part.get("text", "") for part in parts).strip()
    if not answer:
        return "Google Search returned no answer for this query."

    metadata = candidate.get("groundingMetadata", {})
    sources = _search_sources(data)
    resolved = resolved or {}
    lines = [answer, ""]
    if sources:
        lines.append("Sources:")
        lines.extend(
            f"[{i}] {source.get('title', 'Untitled')} - {resolved.get(source.get('uri', ''), source.get('uri', ''))}"
            for i, source in enumerate(sources, start=1)
        )
    else:
        lines.append("Sources: none returned (answer may not be grounded in a search).")
    queries = metadata.get("webSearchQueries", [])
    if queries:
        lines.extend(["", "Searches run: " + "; ".join(queries)])
    return "\n".join(lines)


# ---------------------------------------------------------------- Scrape Website


class ScrapeInput(BaseModel):
    url: str = Field(..., description="Full http(s) URL of the page to read.")


def resolve_public(url: str) -> tuple[str | None, str | None]:
    """Resolve the URL's host once. Returns (reason it is blocked, None) or (None, public IP to connect to)."""
    parsed = urlparse(url)
    if parsed.scheme not in ("http", "https"):
        return "Only http and https URLs can be fetched.", None
    if not parsed.hostname:
        return "The URL has no host.", None
    try:
        infos = socket.getaddrinfo(parsed.hostname, parsed.port or (443 if parsed.scheme == "https" else 80))
    except (socket.gaierror, UnicodeError):
        return f"Could not resolve {parsed.hostname}.", None
    addresses = [ipaddress.ip_address(info[4][0]) for info in infos]
    if not addresses or any(not a.is_global for a in addresses):
        return f"{parsed.hostname} resolves to a private or local address, which is blocked.", None
    return None, str(addresses[0])


def check_public_url(url: str) -> str | None:
    """Return a reason the URL must not be fetched, or None if it points at a public host."""
    return resolve_public(url)[0]


def pinned_request(url: str, ip: str) -> tuple[str, dict[str, str], dict[str, str]]:
    """Rewrite a URL to connect to an already-checked IP, so DNS cannot change between check and connect.

    The original host name still goes in the Host header and TLS SNI, so virtual hosts and certificate
    checks work as normal.
    """
    parsed = urlparse(url)
    host_for_url = f"[{ip}]" if ":" in ip else ip
    netloc = f"{host_for_url}:{parsed.port}" if parsed.port else host_for_url
    host_header = parsed.netloc.rsplit("@", 1)[-1]
    extensions = {"sni_hostname": parsed.hostname} if parsed.scheme == "https" else {}
    return parsed._replace(netloc=netloc).geturl(), {"Host": host_header}, extensions


def read_html(html: str, base_url: str) -> tuple[str, list[tuple[str, str]]]:
    """A page's readable text, and its http(s) links as (link text, absolute URL) in page order without repeats.
    Links back to the page itself are left out."""
    from bs4 import BeautifulSoup

    soup = BeautifulSoup(html, "html.parser")
    for tag in soup(["script", "style", "noscript", "svg", "head"]):
        tag.decompose()
    seen = {urldefrag(base_url).url}
    links: list[tuple[str, str]] = []
    for anchor in soup.find_all("a", href=True):
        url = urldefrag(urljoin(base_url, anchor["href"].strip())).url
        if urlparse(url).scheme not in ("http", "https") or url in seen:
            continue
        seen.add(url)
        links.append((" ".join(anchor.get_text(" ").split())[:SCRAPE_MAX_LINK_TEXT], url))
    lines = (line.strip() for line in soup.get_text("\n").splitlines())
    return "\n".join(line for line in lines if line), links


def html_to_text(html: str) -> str:
    return read_html(html, "")[0]


def format_links(links: list[tuple[str, str]]) -> str:
    if not links:
        return ""
    shown = links[:SCRAPE_MAX_LINKS]
    lines = ["Links on this page:", *(f"- [{text}]({url})" if text else f"- {url}" for text, url in shown)]
    if len(links) > len(shown):
        lines.append(f"... {len(links) - len(shown)} more links not shown")
    return "\n".join(lines)


class ScrapeWebsiteTool(BaseTool):
    name: str = "Scrape Website"
    description: str = "Fetches a public web page and returns its readable text and the links on it."
    args_schema: type[BaseModel] = ScrapeInput

    def _run(self, url: str) -> str:
        current = url
        try:
            # trust_env=False: proxy environment variables must not route around the address check
            with httpx.Client(timeout=SCRAPE_TIMEOUT_SECONDS, follow_redirects=False, trust_env=False) as client:
                for _ in range(SCRAPE_MAX_REDIRECTS + 1):
                    # Check every hop so a redirect cannot reach a private address, and connect to the
                    # address that was checked
                    reason, ip = resolve_public(current)
                    if reason or ip is None:
                        return f"Cannot fetch {current}: {reason}"
                    target, headers, extensions = pinned_request(current, ip)
                    headers["User-Agent"] = "CrewStudio/1.0"
                    with client.stream("GET", target, headers=headers, extensions=extensions) as response:
                        if response.is_redirect:
                            current = urljoin(current, response.headers.get("location", ""))
                            continue
                        response.raise_for_status()
                        body = b""
                        for chunk in response.iter_bytes():
                            body += chunk
                            if len(body) > SCRAPE_MAX_BYTES:
                                break
                        text = body[:SCRAPE_MAX_BYTES].decode(response.encoding or "utf-8", errors="replace")
                        content_type = response.headers.get("content-type", "")
                        links: list[tuple[str, str]] = []
                        if "html" in content_type:
                            text, links = read_html(text, current)
                        if len(text) > SCRAPE_MAX_CHARS:
                            text = text[:SCRAPE_MAX_CHARS] + "\n... [page truncated]"
                        # Links follow the (possibly cut) text so a long page cannot push them out
                        page = "\n\n".join(part for part in (text, format_links(links)) if part)
                        return page or "The page had no readable text."
                return f"Cannot fetch {url}: too many redirects."
        except httpx.HTTPStatusError as exc:
            return f"Fetching {current} failed: HTTP {exc.response.status_code}."
        except httpx.HTTPError as exc:
            return f"Fetching {current} failed: {type(exc).__name__}: {exc}"


# ---------------------------------------------------------------- Workspace files


def resolve_in_workspace(workspace: Path, relative: str) -> Path | None:
    """Resolve a path inside the workspace, or None if it would escape it (including through symlinks)."""
    if "\x00" in relative:
        return None
    root = workspace.resolve()
    try:
        target = (root / relative.lstrip("/")).resolve()
    except (OSError, ValueError):
        return None
    if target != root and root not in target.parents:
        return None
    return target


def _open_no_follow(path: Path, flags: int) -> int:
    """Open without following a symlink at the final component, so a link swapped in after the check fails."""
    return os.open(path, flags | getattr(os, "O_NOFOLLOW", 0), 0o644)


class ReadFileInput(BaseModel):
    path: str = Field(..., description="Path of the file, relative to the workspace folder.")


class WriteFileInput(BaseModel):
    path: str = Field(..., description="Path of the file, relative to the workspace folder.")
    content: str = Field(..., description="Text to write. Replaces the file if it exists.")


class WorkspaceReadTool(BaseTool):
    name: str = "Read Workspace File"
    description: str = "Reads a text file from the workspace folder."
    args_schema: type[BaseModel] = ReadFileInput
    _workspace: Path = PrivateAttr(default=Path("."))

    def configure(self, workspace: Path) -> "WorkspaceReadTool":
        self._workspace = workspace
        return self

    def _run(self, path: str) -> str:
        target = resolve_in_workspace(self._workspace, path)
        if target is None:
            return "That path is outside the workspace folder."
        if not target.is_file():
            return f"No file at {path}."
        try:
            fd = _open_no_follow(target, os.O_RDONLY)
            with os.fdopen(fd, "rb") as f:
                data = f.read(WORKSPACE_MAX_READ_BYTES)
        except OSError as exc:
            return f"Could not read {path}: {exc.strerror}."
        return data.decode("utf-8", errors="replace")


class WorkspaceWriteTool(BaseTool):
    name: str = "Write Workspace File"
    description: str = "Writes a text file into the workspace folder."
    args_schema: type[BaseModel] = WriteFileInput
    _workspace: Path = PrivateAttr(default=Path("."))

    def configure(self, workspace: Path) -> "WorkspaceWriteTool":
        self._workspace = workspace
        return self

    def _run(self, path: str, content: str) -> str:
        target = resolve_in_workspace(self._workspace, path)
        if target is None or target == self._workspace.resolve():
            return "That path is outside the workspace folder."
        encoded = content.encode("utf-8")
        if len(encoded) > WORKSPACE_MAX_WRITE_BYTES:
            return f"Content is too large ({len(encoded):,} bytes; limit {WORKSPACE_MAX_WRITE_BYTES:,})."
        try:
            target.parent.mkdir(parents=True, exist_ok=True)
            fd = _open_no_follow(target, os.O_WRONLY | os.O_CREAT | os.O_TRUNC)
            with os.fdopen(fd, "wb") as f:
                f.write(encoded)
        except OSError as exc:
            return f"Could not write {path}: {exc.strerror}."
        return f"Wrote {len(encoded):,} bytes to {path}."


# ---------------------------------------------------------------- Factory


class ToolFactory:
    """Builds per-agent tool instances from catalog keys."""

    def __init__(
        self,
        search_model: str,
        searches_allowed: Callable[[], bool],
        record_search: SearchRecorder,
        workspace: Path,
        prices: list[dict[str, Any]] | None = None,
        reserve_video: Callable[[float], bool] = lambda _cost: True,
        release_video: Callable[[float], None] = lambda _cost: None,
        record_video: Callable[[str, dict[str, Any]], float] | None = None,
        state: Any = None,
    ):
        self._search_model = search_model
        self._searches_allowed = searches_allowed
        self._record_search = record_search
        self._workspace = workspace
        self._prices = prices or []
        self._reserve_video = reserve_video
        self._release_video = release_video
        self._record_video = record_video
        # A state_tools.StateClient for the run-state API, or None when Studio did not give the run one
        self._state = state
        self._catalog = {entry["key"]: entry for entry in load_catalog()}

    def priced_model(self, key: str) -> str:
        """The model a tool bills for, from the catalog ("" if none)."""
        return self._catalog.get(key, {}).get("priced_model", "")

    def build(self, key: str, agent_role: str) -> BaseTool:
        if key == "google_search":
            return GoogleSearchTool().configure(agent_role, self._search_model, self._searches_allowed, self._record_search)
        if key == "scrape_website":
            return ScrapeWebsiteTool()
        if key == "workspace_read":
            return WorkspaceReadTool().configure(self._workspace)
        if key == "workspace_write":
            return WorkspaceWriteTool().configure(self._workspace)
        if key == "generate_video":
            # Imported here because the video module builds on this one
            from .meter import match_price
            from .video import GenerateVideoTool

            model = self.priced_model(key)
            return GenerateVideoTool().configure(
                agent_role, self._workspace, model, match_price(model, self._prices),
                self._reserve_video, self._release_video, self._record_video or (lambda _role, _details: 0.0),
            )
        if key in STATE_TOOL_KEYS:
            return build_state_tool(key, agent_role, self._state)
        if key in TRACKER_TOOL_KEYS:
            return build_tracker_tool(key, agent_role, self._state)
        if key in REPORT_TOOL_KEYS:
            return build_report_tool(key, agent_role, self._state)
        # Imported here because the KM2 tools read workspace files with this module's helpers
        from .km2_tools import KM2_TOOL_KEYS, build_km2_tool

        if key in KM2_TOOL_KEYS:
            return build_km2_tool(key, agent_role, self._state, self._workspace)
        raise ValueError(f"Unknown tool '{key}'.")
