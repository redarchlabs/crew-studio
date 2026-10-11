from pathlib import Path

import httpx
import pytest

from studio_runner import tools
from studio_runner.tools import (
    pinned_request,
    GoogleSearchTool,
    ToolFactory,
    WorkspaceReadTool,
    WorkspaceWriteTool,
    check_public_url,
    format_links,
    format_search_result,
    html_to_text,
    load_catalog,
    read_html,
    resolve_in_workspace,
    resolve_source_urls,
)


def test_every_catalog_entry_says_its_effect_and_needs():
    for entry in load_catalog():
        assert entry["effect"] in ("reads", "proposes", "changes"), entry["key"]
        assert set(entry["needs"]) <= {"crew", "tracker", "km2", "schedule"}, entry["key"]
    assert tools.tool_effect("google_search") == "reads" and tools.tool_needs("google_search") == ()
    assert tools.tool_effect("tracker_comment") == "changes" and tools.tool_needs("tracker_comment") == ("tracker",)
    assert tools.tool_needs("deliver_report") == ("crew", "schedule")


def test_unknown_or_unclassified_tools_count_as_changes():
    catalog = {"odd": {"key": "odd"}, "typo": {"key": "typo", "effect": "read"}, "fine": {"key": "fine", "effect": "reads", "needs": None}}
    assert tools.tool_effect("odd", catalog) == "changes"
    assert tools.tool_effect("typo", catalog) == "changes"
    assert tools.tool_effect("missing", catalog) == "changes" and tools.tool_needs("missing", catalog) == ()
    assert tools.tool_effect("fine", catalog) == "reads" and tools.tool_needs("fine", catalog) == ()
    assert tools.tool_effect("rm_rf") == "changes"


def test_catalog_keys_all_buildable(tmp_path: Path):
    factory = ToolFactory("gemini-2.5-flash", lambda: True, lambda *a: None, tmp_path)
    for entry in load_catalog():
        assert factory.build(entry["key"], "Agent").name
    with pytest.raises(ValueError):
        factory.build("rm_rf", "Agent")


@pytest.mark.parametrize("relative", ["../secret", "/../../etc/passwd", "a/../../b"])
def test_workspace_paths_cannot_escape(tmp_path: Path, relative: str):
    assert resolve_in_workspace(tmp_path, relative) is None


def test_workspace_absolute_looking_path_stays_inside(tmp_path: Path):
    assert resolve_in_workspace(tmp_path, "/notes/a.txt") == (tmp_path / "notes/a.txt").resolve()


def test_workspace_write_then_read(tmp_path: Path):
    writer = WorkspaceWriteTool().configure(tmp_path)
    reader = WorkspaceReadTool().configure(tmp_path)
    assert "Wrote" in writer._run("out/report.md", "hello")
    assert reader._run("out/report.md") == "hello"
    assert "outside" in writer._run("../escape.txt", "x")
    assert "outside" in reader._run("../../etc/passwd")
    assert "No file" in reader._run("missing.txt")
    assert "too large" in writer._run("big.txt", "x" * (tools.WORKSPACE_MAX_WRITE_BYTES + 1))


@pytest.mark.parametrize(
    "url",
    ["file:///etc/passwd", "http://127.0.0.1:8088/api/crews", "http://localhost/", "http://10.0.0.1/", "http://[::1]/", "http://169.254.169.254/latest"],
)
def test_scrape_blocks_non_public_urls(url: str):
    assert check_public_url(url) is not None


def test_html_to_text_drops_scripts():
    text = html_to_text("<html><head><title>x</title></head><body><script>evil()</script><p>Hello</p><p>World</p></body></html>")
    assert text == "Hello\nWorld"


SEARCH_RESPONSE = {
    "candidates": [
        {
            "content": {"parts": [{"text": "Answer."}]},
            "groundingMetadata": {
                "groundingChunks": [{"web": {"title": "Src", "uri": "https://example.com"}}],
                "webSearchQueries": ["q1"],
            },
        }
    ],
    "usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 20, "thoughtsTokenCount": 5},
}


def test_read_html_lists_links_once_as_absolute_urls():
    html = (
        '<body><p>Contents</p><a href="/study/01-fast-sunday?lang=eng"> Fast\n Sunday </a>'
        '<a href="https://other.org/talk#part">Talk</a><a href="/study/01-fast-sunday?lang=eng#top">Again</a>'
        '<a href="#top">Top</a><a href="mailto:x@y.org">Mail</a><a href="javascript:void(0)">JS</a>'
        '<a href="https://example.com/page">Here</a><a href="/img"></a></body>'
    )
    text, links = read_html(html, "https://example.com/page")
    assert text.startswith("Contents")
    assert links == [
        ("Fast Sunday", "https://example.com/study/01-fast-sunday?lang=eng"),
        ("Talk", "https://other.org/talk"),
        ("", "https://example.com/img"),
    ]


def test_format_links_caps_the_list():
    links = [(f"Page {i}", f"https://example.com/{i}") for i in range(tools.SCRAPE_MAX_LINKS + 5)]
    text = format_links(links)
    assert text.startswith("Links on this page:\n- [Page 0](https://example.com/0)")
    assert f"- [Page {tools.SCRAPE_MAX_LINKS - 1}]" in text and f"- [Page {tools.SCRAPE_MAX_LINKS}]" not in text
    assert "5 more links not shown" in text
    assert format_links([("", "https://example.com/x")]) == "Links on this page:\n- https://example.com/x"
    assert format_links([]) == ""


def test_format_search_result_lists_sources():
    text = format_search_result(SEARCH_RESPONSE)
    assert "Answer." in text and "[1] Src - https://example.com" in text and "Searches run: q1" in text
    assert "no answer" in format_search_result({})


def test_format_search_result_uses_resolved_urls():
    text = format_search_result(SEARCH_RESPONSE, {"https://example.com": "https://example.com/real"})
    assert "[1] Src - https://example.com/real" in text


def test_resolve_source_urls_follows_only_grounding_redirects(monkeypatch: pytest.MonkeyPatch):
    redirect = tools.GROUNDING_REDIRECT_PREFIX
    targets = {f"{redirect}a": "https://site.org/a", f"{redirect}b": None}
    seen = []

    def fake_head(url, follow_redirects, timeout):
        seen.append(url)
        assert follow_redirects is False
        if targets[url] is None:
            raise httpx.ConnectTimeout("slow")
        return httpx.Response(302, headers={"location": targets[url]}, request=httpx.Request("HEAD", url))

    monkeypatch.setattr(tools.httpx, "head", fake_head)
    resolved = resolve_source_urls([f"{redirect}a", f"{redirect}a", f"{redirect}b", "https://plain.org/x"])
    assert resolved == {f"{redirect}a": "https://site.org/a"}
    assert sorted(seen) == [f"{redirect}a", f"{redirect}b"]


def test_resolve_source_urls_ignores_non_redirects(monkeypatch: pytest.MonkeyPatch):
    url = f"{tools.GROUNDING_REDIRECT_PREFIX}c"
    monkeypatch.setattr(tools.httpx, "head", lambda url, follow_redirects, timeout: httpx.Response(
        200, request=httpx.Request("HEAD", url)))
    assert resolve_source_urls([url]) == {}
    monkeypatch.setattr(tools.httpx, "head", lambda url, follow_redirects, timeout: httpx.Response(
        302, headers={"location": "ftp://x"}, request=httpx.Request("HEAD", url)))
    assert resolve_source_urls([url]) == {}


def test_search_refuses_when_budget_used():
    tool = GoogleSearchTool().configure("Agent", "gemini-2.5-flash", lambda: False, lambda *a: None)
    assert "budget" in tool._run("anything")


def test_search_records_usage(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("GEMINI_API_KEY", "AIza-test")
    calls = []

    def fake_post(url, headers, json, timeout):
        assert "generativelanguage" in url and headers["x-goog-api-key"] == "AIza-test"
        return httpx.Response(200, json=SEARCH_RESPONSE, request=httpx.Request("POST", url))

    monkeypatch.setattr(tools.httpx, "post", fake_post)
    tool = GoogleSearchTool().configure("Analyst", "gemini-2.5-flash", lambda: True, lambda *a: calls.append(a))
    assert "Answer." in tool._run("query")
    assert calls == [("Analyst", "query", "gemini-2.5-flash", 10, 25, 1)]


def test_search_stops_at_the_agent_cap(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("GEMINI_API_KEY", "AIza-test")
    posts = []

    def fake_post(url, headers, json, timeout):
        posts.append(json)
        return httpx.Response(200, json=SEARCH_RESPONSE, request=httpx.Request("POST", url))

    monkeypatch.setattr(tools.httpx, "post", fake_post)
    tool = GoogleSearchTool().configure("A", "m", lambda: True, lambda *a: None, max_searches=2)
    assert "Searches left for you in this run: 1." in tool._run("one")
    assert "Searches left for you in this run: 0." in tool._run("two")
    refused = tool._run("three")
    assert "all 2 of your searches" in refused and "Scrape Website" in refused
    assert len(posts) == 2


def test_tool_factory_gives_each_search_tool_the_agent_cap(tmp_path: Path):
    factory = ToolFactory("m", lambda: True, lambda *a: None, tmp_path)
    tool = factory.build("google_search", "A")
    assert tool._limits["max"] == tools.MAX_SEARCHES_PER_AGENT


def test_search_reports_http_errors(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("GEMINI_API_KEY", "AQ.vertex")

    def fake_post(url, headers, json, timeout):
        assert "aiplatform" in url
        return httpx.Response(403, json={"error": {"message": "denied"}}, request=httpx.Request("POST", url))

    monkeypatch.setattr(tools.httpx, "post", fake_post)
    tool = GoogleSearchTool().configure("A", "m", lambda: True, lambda *a: None)
    assert tool._run("q") == "Google Search failed: HTTP 403 - denied"


def test_search_without_key(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.delenv("GEMINI_API_KEY", raising=False)
    tool = GoogleSearchTool().configure("A", "m", lambda: True, lambda *a: None)
    assert "GEMINI_API_KEY" in tool._run("q")


def test_pinned_request_keeps_host_and_sni():
    url, headers, ext = pinned_request("https://example.com:8443/a?b=1", "93.184.216.34")
    assert url == "https://93.184.216.34:8443/a?b=1"
    assert headers == {"Host": "example.com:8443"} and ext == {"sni_hostname": "example.com"}
    url, headers, ext = pinned_request("http://example.com/x", "2606:2800:220:1::1")
    assert url == "http://[2606:2800:220:1::1]/x" and ext == {}


def test_workspace_rejects_symlinks_and_nul(tmp_path: Path):
    outside = tmp_path.parent / "outside-secret.txt"
    outside.write_text("secret")
    (tmp_path / "link.txt").symlink_to(outside)
    reader = WorkspaceReadTool().configure(tmp_path)
    assert "outside" in reader._run("link.txt")
    assert "outside" in reader._run("bad\x00name")
    # A symlink inside the workspace pointing inside is resolved, then opened without following
    (tmp_path / "real.txt").write_text("ok")
    (tmp_path / "inner.txt").symlink_to(tmp_path / "real.txt")
    assert reader._run("inner.txt") == "ok"


def test_scrape_connects_to_checked_address(monkeypatch: pytest.MonkeyPatch):
    seen = {}

    def fake_resolve(url):
        return None, "93.184.216.34"

    class FakeClient:
        def __init__(self, **kw):
            seen["trust_env"] = kw.get("trust_env")

        def __enter__(self):
            return self

        def __exit__(self, *a):
            return False

        def stream(self, method, url, headers, extensions):
            seen.update(url=url, host=headers["Host"], sni=extensions.get("sni_hostname"))
            request = httpx.Request(method, url)
            response = httpx.Response(200, headers={"content-type": "text/html"},
                                      content=b'<p>Hi</p><a href="/next">Next</a>', request=request)

            class Ctx:
                def __enter__(self_inner):
                    return response

                def __exit__(self_inner, *a):
                    return False
            return Ctx()

    monkeypatch.setattr(tools, "resolve_public", fake_resolve)
    monkeypatch.setattr(tools.httpx, "Client", FakeClient)
    assert tools.ScrapeWebsiteTool()._run("https://example.com/page") == (
        "Hi\nNext\n\nLinks on this page:\n- [Next](https://example.com/next)")
    assert seen == {"trust_env": False, "url": "https://93.184.216.34/page", "host": "example.com", "sni": "example.com"}


def test_catalog_states_the_search_cap():
    entry = next(e for e in load_catalog() if e["key"] == "google_search")
    assert f"at most {tools.MAX_SEARCHES_PER_AGENT} searches" in entry["description"]
