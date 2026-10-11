import json
from pathlib import Path
from typing import Any

import httpx
import pytest

from studio_runner.state_tools import (
    STATE_TOOL_KEYS,
    LogCheckTool,
    LogRecordTool,
    NotesGetTool,
    NotesListTool,
    NotesPutTool,
    StateClient,
    build_state_tool,
)
from studio_runner.tools import ToolFactory, load_catalog

TOKEN = "csr_" + "t" * 43


class FakeStudio:
    """Stands in for /api/run-state: records each request and answers from a table."""

    def __init__(self) -> None:
        self.requests: list[httpx.Request] = []
        self.answers: dict[tuple[str, str], tuple[int, Any]] = {}

    def answer(self, method: str, path: str, status: int, body: Any) -> None:
        self.answers[(method, path)] = (status, body)

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        status, body = self.answers.get((request.method, request.url.path), (404, {"success": False, "data": None,
                                                                              "error": {"code": "not_found", "message": "No such API endpoint."}}))
        return httpx.Response(status, json=body)

    def client(self) -> StateClient:
        return StateClient("http://127.0.0.1:8088/api/run-state", TOKEN, transport=httpx.MockTransport(self.handler))


def ok(data: Any) -> dict[str, Any]:
    return {"success": True, "data": data, "error": None}


def err(code: str, message: str) -> dict[str, Any]:
    return {"success": False, "data": None, "error": {"code": code, "message": message}}


@pytest.fixture
def studio() -> FakeStudio:
    return FakeStudio()


NOTE = {"name": "weekly-report", "subject": "project:atlas", "version": 12, "content": "Last week: on track.",
        "written_by": "run", "created_at": "2026-10-05T07:00:00Z"}


def test_from_request_needs_url_and_token():
    assert StateClient.from_request({}) is None
    assert StateClient.from_request({"state_api": {"url": "http://x", "token": ""}}) is None
    assert StateClient.from_request({"state_api": "nope"}) is None
    client = StateClient.from_request({"state_api": {"url": "http://127.0.0.1:1/api/run-state/", "token": TOKEN}})
    assert client is not None and client.base_url == "http://127.0.0.1:1/api/run-state"


def test_notes_get_sends_token_agent_and_subject(studio: FakeStudio):
    studio.answer("GET", "/api/run-state/notes/weekly-report", 200, ok(NOTE))
    out = NotesGetTool().configure(studio.client(), "Analyste financière")._run("weekly-report", "project:atlas")
    assert "version 12" in out and "Last week: on track." in out
    req = studio.requests[0]
    assert req.headers["Authorization"] == f"Bearer {TOKEN}"
    assert req.headers["X-Crew-Agent"] == "Analyste financi%C3%A8re"
    assert req.url.params["subject"] == "project:atlas"


def test_notes_get_missing_and_bad_names(studio: FakeStudio):
    studio.answer("GET", "/api/run-state/notes/risk-register", 404, err("not_found", 'There is no note named "risk-register" yet.'))
    assert "no note named" in NotesGetTool().configure(studio.client(), "A")._run("risk-register")
    # Names are escaped into the path, so they cannot reach another endpoint
    NotesGetTool().configure(studio.client(), "A")._run("../log")
    assert studio.requests[-1].url.raw_path.startswith(b"/api/run-state/notes/..%2Flog")


def test_notes_put_and_conflict(studio: FakeStudio):
    studio.answer("PUT", "/api/run-state/notes/weekly-report", 200, ok({**NOTE, "version": 13}))
    out = NotesPutTool().configure(studio.client(), "A")._run("weekly-report", "new text", "project:atlas", 12)
    assert "version 13" in out
    body = json.loads(studio.requests[0].content)
    assert body == {"content": "new text", "subject": "project:atlas", "expected_version": 12}
    studio.answer("PUT", "/api/run-state/notes/weekly-report", 409, err("version_conflict", "The note is at version 14, not 12."))
    out = NotesPutTool().configure(studio.client(), "A")._run("weekly-report", "x", expected_version=12)
    assert "version 14" in out and "Read Note" in out
    body = json.loads(studio.requests[-1].content)
    assert "subject" in body and body["expected_version"] == 12


def test_notes_list(studio: FakeStudio):
    studio.answer("GET", "/api/run-state/notes", 200, ok([
        {"name": "weekly-report", "subject": "project:atlas", "version": 3, "bytes": 2345, "updated_at": "2026-10-05T07:00:00Z"},
        {"name": "people-preferences", "subject": "", "version": 1, "bytes": 10, "updated_at": "2026-10-01T07:00:00Z"},
    ]))
    out = NotesListTool().configure(studio.client(), "A")._run()
    assert "weekly-report (subject project:atlas): version 3, 2,345 bytes" in out and "people-preferences: version 1" in out
    assert "subject" not in studio.requests[0].url.params
    studio.answer("GET", "/api/run-state/notes", 200, ok([]))
    assert "No notes" in NotesListTool().configure(studio.client(), "A")._run("person:sam")
    assert studio.requests[-1].url.params["subject"] == "person:sam"


def test_log_check(studio: FakeStudio):
    entry = {"subject": "jira:PRJ-15", "action": "followup:due_soon", "at": "2026-10-04T07:02:00Z", "details": {"to": "sam"}, "dry_run": False}
    studio.answer("GET", "/api/run-state/log/check", 200, ok({"entry": entry}))
    out = LogCheckTool().configure(studio.client(), "A")._run("jira:PRJ-15", "followup:due_soon", "7d")
    assert "2026-10-04T07:02:00Z" in out and '"to": "sam"' in out
    assert studio.requests[0].url.params["within"] == "7d"
    studio.answer("GET", "/api/run-state/log/check", 200, ok({"entry": None}))
    out = LogCheckTool().configure(studio.client(), "A")._run("jira:PRJ-15", "followup:due_soon")
    assert out.startswith("No ") and "within" not in studio.requests[-1].url.params


def test_log_record(studio: FakeStudio):
    entry = {"subject": "jira:PRJ-15", "action": "followup:due_soon", "at": "2026-10-04T07:02:00Z"}
    studio.answer("POST", "/api/run-state/log", 201, ok({"entry": entry, "duplicate": False}))
    out = LogRecordTool().configure(studio.client(), "A")._run("jira:PRJ-15", "followup:due_soon", {"to": "sam"}, "k1")
    assert out.startswith("Recorded")
    assert json.loads(studio.requests[0].content) == {"subject": "jira:PRJ-15", "action": "followup:due_soon",
                                                      "details": {"to": "sam"}, "idempotency_key": "k1"}
    studio.answer("POST", "/api/run-state/log", 200, ok({"entry": entry, "duplicate": True}))
    assert "already recorded" in LogRecordTool().configure(studio.client(), "A")._run("jira:PRJ-15", "followup:due_soon")
    studio.answer("POST", "/api/run-state/log", 422, err("validation_failed", "Please fix the highlighted fields."))
    studio.answers[("POST", "/api/run-state/log")][1]["error"]["details"] = ["action: use letters"]
    assert "action: use letters" in LogRecordTool().configure(studio.client(), "A")._run("s", "bad action")


def test_errors_are_explained_not_raised():
    def boom(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused")

    client = StateClient("http://127.0.0.1:1/api/run-state", TOKEN, transport=httpx.MockTransport(boom))
    assert "Could not reach Crew Studio" in NotesListTool().configure(client, "A")._run()

    client = StateClient("http://127.0.0.1:1/api/run-state", TOKEN,
                         transport=httpx.MockTransport(lambda r: httpx.Response(500, text="<html>oops</html>")))
    assert "HTTP 500" in NotesListTool().configure(client, "A")._run()
    expired = StateClient("http://127.0.0.1:1/api/run-state", TOKEN,
                          transport=httpx.MockTransport(lambda r: httpx.Response(401, json=err("unauthenticated", "The run token has expired."))))
    assert "expired" in NotesGetTool().configure(expired, "A")._run("n")


def test_tools_without_a_client_say_so():
    for key in STATE_TOOL_KEYS:
        tool = build_state_tool(key, "A", None)
        assert tool.name
    assert "not available" in NotesListTool()._run()
    assert "not available" in NotesGetTool()._run("n")
    with pytest.raises(ValueError):
        build_state_tool("rm_rf", "A", None)


def test_catalog_lists_the_state_tools_and_factory_builds_them(tmp_path: Path):
    keys = {entry["key"] for entry in load_catalog()}
    assert set(STATE_TOOL_KEYS) <= keys
    factory = ToolFactory("m", lambda: True, lambda *a: None, tmp_path, state=None)
    for key in STATE_TOOL_KEYS:
        assert factory.build(key, "Agent").name


def test_less_common_answers(studio: FakeStudio):
    # A new note without a version, a refused save, a failed check, a dry-run entry and an odd error envelope
    studio.answer("PUT", "/api/run-state/notes/n", 200, ok({**NOTE, "name": "n", "version": 1}))
    NotesPutTool().configure(studio.client(), "A")._run("n", "text")
    assert "expected_version" not in json.loads(studio.requests[-1].content)
    studio.answer("PUT", "/api/run-state/notes/n", 422, err("validation_failed", "content: is over the limit"))
    assert NotesPutTool().configure(studio.client(), "A")._run("n", "x") == "content: is over the limit"
    studio.answer("GET", "/api/run-state/log/check", 422, err("validation_failed", "within: use a number"))
    assert "within" in LogCheckTool().configure(studio.client(), "A")._run("s", "a", "soon")
    studio.answer("GET", "/api/run-state/log/check", 200, ok({"entry": {"at": "2026-10-04T07:02:00Z", "dry_run": True}}))
    assert "(dry run)" in LogCheckTool().configure(studio.client(), "A")._run("s", "a")
    studio.answer("GET", "/api/run-state/notes", 400, {"success": False, "data": None, "error": "flat"})
    assert "HTTP 400" in NotesListTool().configure(studio.client(), "A")._run()
