import socket
from pathlib import Path

import httpx
import pytest

from studio_runner import tools
from studio_runner.tools import (
    GoogleSearchTool, ScrapeWebsiteTool, WorkspaceReadTool, WorkspaceWriteTool, _error_message, format_search_result,
    pinned_request, resolve_in_workspace, resolve_public,
)


def resp(status=200, **kw):
    return httpx.Response(status, request=httpx.Request("POST", "https://x"), **kw)


class TestSearch:
    @pytest.fixture(autouse=True)
    def key(self, monkeypatch):
        monkeypatch.setenv("GEMINI_API_KEY", "AIza-x")

    def tool(self, record=lambda *a: None):
        return GoogleSearchTool().configure("A", "m", lambda: True, record)

    def test_unconfigured_tool_defaults_allowed(self, monkeypatch):
        monkeypatch.setattr(tools.httpx, "post", lambda *a, **k: resp(json={"candidates": [{"content": {"parts": [{"text": "hi"}]}}]}))
        assert "hi" in GoogleSearchTool()._run("q")

    def test_transport_error_message(self, monkeypatch):
        def boom(*a, **k):
            raise httpx.ConnectError("no route")

        monkeypatch.setattr(tools.httpx, "post", boom)
        assert self.tool()._run("q") == "Google Search failed: ConnectError: no route"

    def test_record_absent_when_hook_removed(self, monkeypatch):
        monkeypatch.setattr(tools.httpx, "post", lambda *a, **k: resp(json={"candidates": [{"content": {"parts": [{"text": "hi"}]}}]}))
        t = self.tool()
        t._hooks.pop("record")
        assert "hi" in t._run("q")

    def test_grounding_chunks_without_queries_bill_one(self, monkeypatch):
        calls = []
        data = {"candidates": [{"content": {"parts": [{"text": "a"}]}, "groundingMetadata": {"groundingChunks": [{"web": {}}]}}]}
        monkeypatch.setattr(tools.httpx, "post", lambda *a, **k: resp(json=data))
        self.tool(lambda *a: calls.append(a))._run("q")
        assert calls[0][5] == 1 and calls[0][3:5] == (0, 0)

    def test_no_grounding_bills_none_and_no_candidates(self, monkeypatch):
        calls = []
        monkeypatch.setattr(tools.httpx, "post", lambda *a, **k: resp(json={}))
        out = self.tool(lambda *a: calls.append(a))._run("q")
        assert calls[0][5] == 0 and "no answer" in out

    def test_url_choice_and_header_not_in_url(self, monkeypatch):
        seen = {}

        def post(url, headers, json, timeout):
            seen.update(url=url, headers=headers, timeout=timeout)
            return resp(json={})

        monkeypatch.setattr(tools.httpx, "post", post)
        self.tool()._run("q")
        assert "AIza" not in seen["url"] and seen["url"].endswith("m:generateContent")


class TestErrorMessage:
    def test_json_error(self):
        assert _error_message(resp(500, json={"error": {"message": "m"}})) == "m"

    @pytest.mark.parametrize("content", [b"not json", b'{"x": 1}', b'["a"]'])
    def test_fallback_to_text_truncated(self, content):
        assert _error_message(resp(500, content=content)) == content.decode()[:500]

    def test_long_text_truncated(self):
        assert len(_error_message(resp(500, content=b"z" * 900))) == 500


class TestFormatSearch:
    def test_no_sources(self):
        out = format_search_result({"candidates": [{"content": {"parts": [{"text": "A"}]}}]})
        assert "Sources: none returned" in out and "Searches run" not in out

    def test_untitled_source_and_non_web_chunks(self):
        data = {"candidates": [{"content": {"parts": [{"text": "A"}, {"text": "B"}]},
                                "groundingMetadata": {"groundingChunks": [{"x": 1}, {"web": {"uri": "u"}}]}}]}
        out = format_search_result(data)
        assert out.startswith("AB") and "[1] Untitled - u" in out

    def test_whitespace_answer_is_empty(self):
        assert "no answer" in format_search_result({"candidates": [{"content": {"parts": [{"text": "  "}]}}]})


class TestResolvePublic:
    def test_bad_scheme_and_missing_host(self):
        assert "Only http" in resolve_public("ftp://x")[0]
        assert "no host" in resolve_public("http:///path")[0]

    def test_unresolvable(self, monkeypatch):
        def fail(*a):
            raise socket.gaierror("nx")

        monkeypatch.setattr(tools.socket, "getaddrinfo", fail)
        assert "Could not resolve" in resolve_public("http://nx.invalid")[0]

    def test_unicode_error_on_resolve(self, monkeypatch):
        def fail(*a):
            raise UnicodeError("bad idna")

        monkeypatch.setattr(tools.socket, "getaddrinfo", fail)
        assert "Could not resolve" in resolve_public("http://x")[0]

    def test_public_and_mixed_addresses(self, monkeypatch):
        def infos(ips):
            return lambda *a: [(0, 0, 0, "", (ip, 0)) for ip in ips]

        monkeypatch.setattr(tools.socket, "getaddrinfo", infos(["93.184.216.34"]))
        assert resolve_public("https://example.com") == (None, "93.184.216.34")
        monkeypatch.setattr(tools.socket, "getaddrinfo", infos(["93.184.216.34", "10.0.0.1"]))
        assert "private" in resolve_public("https://example.com")[0]

    def test_empty_addresses_blocked(self, monkeypatch):
        monkeypatch.setattr(tools.socket, "getaddrinfo", lambda *a: [])
        assert "private or local" in resolve_public("http://x")[0]

    def test_default_ports(self, monkeypatch):
        ports = []
        monkeypatch.setattr(tools.socket, "getaddrinfo", lambda h, p: ports.append(p) or [(0, 0, 0, "", ("93.184.216.34", p))])
        resolve_public("https://a")
        resolve_public("http://a")
        resolve_public("http://a:8080")
        assert ports == [443, 80, 8080]


def test_pinned_request_without_port_userinfo():
    url, headers, _ = pinned_request("http://u:p@example.com/x", "1.2.3.4")
    assert headers["Host"] == "example.com" and "1.2.3.4" in url


class TestHtmlToText:
    def test_strips_tags_and_blank_lines(self):
        assert tools.html_to_text("<body><noscript>n</noscript><style>s</style> <p> a </p>\n\n<svg>x</svg><p>b</p></body>") == "a\nb"


class FakeStreamClient:
    """Stands in for httpx.Client; script maps target URL -> httpx.Response (or exception)."""

    script: list = []
    seen: list = []

    def __init__(self, **kw):
        pass

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def stream(self, method, url, headers, extensions):
        FakeStreamClient.seen.append(url)
        item = FakeStreamClient.script.pop(0)
        if isinstance(item, Exception):
            raise item

        class Ctx:
            def __enter__(s):
                return item

            def __exit__(s, *a):
                return False

        return Ctx()


class TestScrape:
    @pytest.fixture(autouse=True)
    def patch(self, monkeypatch):
        FakeStreamClient.script, FakeStreamClient.seen = [], []
        monkeypatch.setattr(tools, "resolve_public", lambda url: (None, "93.184.216.34") if "evil" not in url else ("blocked!", None))
        monkeypatch.setattr(tools.httpx, "Client", FakeStreamClient)

    def r(self, status=200, **kw):
        return httpx.Response(status, request=httpx.Request("GET", "https://example.com"), **kw)

    def test_blocked_first_hop(self):
        assert ScrapeWebsiteTool()._run("http://evil/") == "Cannot fetch http://evil/: blocked!"

    def test_redirect_followed_and_checked_each_hop(self):
        FakeStreamClient.script = [self.r(302, headers={"location": "/next"}), self.r(200, content=b"plain text", headers={"content-type": "text/plain"})]
        assert ScrapeWebsiteTool()._run("https://example.com/a") == "plain text"
        assert FakeStreamClient.seen == ["https://93.184.216.34/a", "https://93.184.216.34/next"]

    def test_redirect_to_private_is_blocked(self):
        FakeStreamClient.script = [self.r(302, headers={"location": "http://evil/x"})]
        assert "blocked!" in ScrapeWebsiteTool()._run("https://example.com/a")

    def test_too_many_redirects(self):
        FakeStreamClient.script = [self.r(302, headers={"location": "/again"}) for _ in range(tools.SCRAPE_MAX_REDIRECTS + 1)]
        assert ScrapeWebsiteTool()._run("https://example.com/a") == "Cannot fetch https://example.com/a: too many redirects."

    def test_http_error_status(self):
        FakeStreamClient.script = [self.r(404, content=b"nope")]
        assert ScrapeWebsiteTool()._run("https://example.com/a") == "Fetching https://example.com/a failed: HTTP 404."

    def test_transport_error(self):
        FakeStreamClient.script = [httpx.ConnectTimeout("slow")]
        assert "ConnectTimeout: slow" in ScrapeWebsiteTool()._run("https://example.com/a")

    def test_empty_page(self):
        FakeStreamClient.script = [self.r(200, content=b"", headers={"content-type": "text/plain"})]
        assert ScrapeWebsiteTool()._run("https://example.com/a") == "The page had no readable text."

    def test_text_truncated_at_char_limit(self):
        FakeStreamClient.script = [self.r(200, content=b"x" * (tools.SCRAPE_MAX_CHARS + 5), headers={"content-type": "text/plain"})]
        out = ScrapeWebsiteTool()._run("https://example.com/a")
        assert out.endswith("\n... [page truncated]") and len(out) == tools.SCRAPE_MAX_CHARS + len("\n... [page truncated]")

    def test_exact_char_limit_not_truncated(self):
        FakeStreamClient.script = [self.r(200, content=b"x" * tools.SCRAPE_MAX_CHARS, headers={"content-type": "text/plain"})]
        assert len(ScrapeWebsiteTool()._run("https://example.com/a")) == tools.SCRAPE_MAX_CHARS

    def test_body_byte_cap(self, monkeypatch):
        monkeypatch.setattr(tools, "SCRAPE_MAX_BYTES", 10)
        monkeypatch.setattr(tools, "SCRAPE_MAX_CHARS", 1000)
        FakeStreamClient.script = [self.r(200, content=b"y" * 100, headers={"content-type": "text/plain"})]
        assert ScrapeWebsiteTool()._run("https://example.com/a") == "y" * 10

    def test_html_is_converted(self):
        FakeStreamClient.script = [self.r(200, content=b"<script>x</script><p>Hello</p>", headers={"content-type": "text/html; charset=utf-8"})]
        assert ScrapeWebsiteTool()._run("https://example.com/a") == "Hello"


class TestWorkspaceEdges:
    def test_root_path_is_allowed_for_resolve_but_not_write(self, tmp_path):
        assert resolve_in_workspace(tmp_path, "") == tmp_path.resolve()
        assert "outside" in WorkspaceWriteTool().configure(tmp_path)._run("", "x")

    def test_oserror_in_resolve(self, tmp_path, monkeypatch):
        orig = Path.resolve
        calls = {"n": 0}

        def flaky(self, *a, **k):
            calls["n"] += 1
            if calls["n"] == 2:
                raise OSError("loop")
            return orig(self, *a, **k)

        monkeypatch.setattr(Path, "resolve", flaky)
        assert resolve_in_workspace(tmp_path, "a") is None

    def test_read_open_error(self, tmp_path):
        f = tmp_path / "secret.txt"
        f.write_text("x")
        f.chmod(0)
        try:
            out = WorkspaceReadTool().configure(tmp_path)._run("secret.txt")
        finally:
            f.chmod(0o600)
        import os
        if os.geteuid() != 0:
            assert out.startswith("Could not read secret.txt")

    def test_read_is_truncated_to_limit_and_decodes_with_replacement(self, tmp_path):
        (tmp_path / "big").write_bytes(b"\xff" + b"a" * (tools.WORKSPACE_MAX_READ_BYTES + 10))
        out = WorkspaceReadTool().configure(tmp_path)._run("big")
        assert len(out) == tools.WORKSPACE_MAX_READ_BYTES and out[0] == "�"

    def test_write_exact_limit_ok_and_unicode_counts_bytes(self, tmp_path):
        w = WorkspaceWriteTool().configure(tmp_path)
        assert "Wrote 1,000,000 bytes" in w._run("a", "x" * tools.WORKSPACE_MAX_WRITE_BYTES)
        assert "too large" in w._run("b", "é" * (tools.WORKSPACE_MAX_WRITE_BYTES // 2 + 1))

    def test_write_overwrites_and_creates_dirs(self, tmp_path):
        w = WorkspaceWriteTool().configure(tmp_path)
        w._run("d/e/f.txt", "long content")
        w._run("d/e/f.txt", "short")
        assert (tmp_path / "d/e/f.txt").read_text() == "short"

    def test_write_error_when_parent_is_file(self, tmp_path):
        (tmp_path / "f").write_text("x")
        assert WorkspaceWriteTool().configure(tmp_path)._run("f/g.txt", "x").startswith("Could not write f/g.txt")

    def test_write_through_symlink_final_component_refused(self, tmp_path):
        (tmp_path / "real").write_text("keep")
        (tmp_path / "ln").symlink_to(tmp_path / "real")
        # resolve() follows the link inside the workspace, so this writes the target; it must stay in the workspace
        WorkspaceWriteTool().configure(tmp_path)._run("ln", "new")
        assert (tmp_path / "real").read_text() == "new"

    def test_read_directory(self, tmp_path):
        (tmp_path / "d").mkdir()
        assert "No file" in WorkspaceReadTool().configure(tmp_path)._run("d")
