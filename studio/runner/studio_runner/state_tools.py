"""Crew state tools: notes (versioned working memory) and the action log (an exact record of what the crew did).

They call Crew Studio's run-state API with the run's own token, which arrives in the stdin request (never in the
environment). Studio limits every call to this run's crew and schedule and writes a line for it in the run's
journal. Every failure comes back as text the agent can act on; nothing here raises into CrewAI.
"""

import json
from typing import Any
from urllib.parse import quote

import httpx
from crewai.tools import BaseTool
from pydantic import BaseModel, Field, PrivateAttr

STATE_TIMEOUT_SECONDS = 15.0
STATE_TOOL_KEYS = ("notes_get", "notes_put", "notes_list", "log_check", "log_record")
AGENT_HEADER = "X-Crew-Agent"
NOT_AVAILABLE = "Crew state is not available in this run (Crew Studio did not provide a state connection)."


class StateClient:
    """Calls /api/run-state with the run token. Answers are (ok, data, message)."""

    def __init__(self, base_url: str, token: str, transport: httpx.BaseTransport | None = None):
        self.base_url = base_url.rstrip("/")
        self._token = token
        self._transport = transport

    @classmethod
    def from_request(cls, request: dict[str, Any]) -> "StateClient | None":
        api = request.get("state_api")
        if not isinstance(api, dict) or not api.get("url") or not api.get("token"):
            return None
        return cls(str(api["url"]), str(api["token"]))

    def call(self, method: str, path: str, agent_role: str, params: dict[str, Any] | None = None,
             body: dict[str, Any] | None = None, timeout: float = STATE_TIMEOUT_SECONDS) -> tuple[bool, Any, str]:
        headers = {
            "Authorization": f"Bearer {self._token}",
            "Accept": "application/json",
            # Percent-encoded so any language fits in a header; Studio decodes it for the journal
            AGENT_HEADER: quote(agent_role, safe=" "),
        }
        clean_params = {k: v for k, v in (params or {}).items() if v not in (None, "")}
        try:
            # trust_env=False: Studio is on this machine, so proxy settings must not apply
            with httpx.Client(timeout=timeout, trust_env=False, transport=self._transport) as client:
                response = client.request(method, self.base_url + path, headers=headers, params=clean_params, json=body)
        except httpx.HTTPError as exc:
            return False, None, f"Could not reach Crew Studio's state API: {type(exc).__name__}: {exc}"
        return _read_envelope(response)


def _read_envelope(response: httpx.Response) -> tuple[bool, Any, str]:
    try:
        envelope = response.json()
    except ValueError:
        return False, None, f"Crew Studio's state API answered HTTP {response.status_code}."
    if response.is_success and isinstance(envelope, dict) and envelope.get("success"):
        return True, envelope.get("data"), ""
    error = envelope.get("error") if isinstance(envelope, dict) else None
    if not isinstance(error, dict):
        return False, None, f"Crew Studio's state API answered HTTP {response.status_code}."
    message = str(error.get("message") or f"HTTP {response.status_code}")
    details = [str(d) for d in error.get("details") or []]
    return False, {"code": error.get("code", ""), "status": response.status_code}, " ".join([message, *details])


def _note_path(name: str) -> str:
    # Escape everything that is not a plain name character, so a name can never reach another endpoint
    return "/notes/" + quote(name, safe=":._-")


class _StateTool(BaseTool):
    _client: StateClient | None = PrivateAttr(default=None)
    _agent_role: str = PrivateAttr(default="")

    def configure(self, client: StateClient | None, agent_role: str) -> "_StateTool":
        self._client = client
        self._agent_role = agent_role
        return self

    def _call(self, method: str, path: str, params: dict[str, Any] | None = None,
              body: dict[str, Any] | None = None) -> tuple[bool, Any, str]:
        if self._client is None:
            return False, None, NOT_AVAILABLE
        return self._client.call(method, path, self._agent_role, params=params, body=body)


# ---------------------------------------------------------------- Notes


class NotesGetInput(BaseModel):
    name: str = Field(..., description="Name of the note, such as weekly-report.")
    subject: str = Field("", description="What the note is about, such as project:atlas. Empty for the crew's own notes.")


class NotesGetTool(_StateTool):
    name: str = "Read Note"
    description: str = (
        "Reads the latest version of one of this crew's notes (its working memory between runs), such as last "
        "week's report. The answer includes the version number to pass to Write Note."
    )
    args_schema: type[BaseModel] = NotesGetInput

    def _run(self, name: str, subject: str = "") -> str:
        ok, data, message = self._call("GET", _note_path(name), params={"subject": subject})
        if not ok:
            return message
        about = f", subject {data['subject']}" if data.get("subject") else ""
        return (f"Note {data['name']} (version {data['version']}{about}, saved {data.get('created_at', '')} "
                f"by {data.get('written_by', '')}):\n{data.get('content', '')}")


class NotesPutInput(BaseModel):
    name: str = Field(..., description="Name of the note, such as weekly-report (letters, digits, '.', '_', ':' or '-').")
    content: str = Field(..., description="The whole new text of the note (up to 64 KB). Replaces the previous version.")
    subject: str = Field("", description="What the note is about, such as project:atlas. Empty for the crew's own notes.")
    expected_version: int | None = Field(
        None, description="The version you read and changed (0 for a new note). The save is refused if it changed since.")


class NotesPutTool(_StateTool):
    name: str = "Write Note"
    description: str = (
        "Saves a new version of one of this crew's notes. Earlier versions are kept. Pass the version you read as "
        "expected_version so you never overwrite a change you have not seen."
    )
    args_schema: type[BaseModel] = NotesPutInput

    def _run(self, name: str, content: str, subject: str = "", expected_version: int | None = None) -> str:
        body: dict[str, Any] = {"content": content, "subject": subject}
        if expected_version is not None:
            body["expected_version"] = expected_version
        ok, data, message = self._call("PUT", _note_path(name), body=body)
        if ok:
            return f"Saved note {data['name']} as version {data['version']}."
        if isinstance(data, dict) and data.get("code") == "version_conflict":
            return f"{message} Read the note again with Read Note, merge your change into it, then save."
        return message


class NotesListInput(BaseModel):
    subject: str | None = Field(None, description="Only notes about this subject, such as project:atlas. Leave out for all.")


class NotesListTool(_StateTool):
    name: str = "List Notes"
    description: str = "Lists this crew's notes with their subjects, versions and sizes (not their text)."
    args_schema: type[BaseModel] = NotesListInput

    def _run(self, subject: str | None = None) -> str:
        ok, data, message = self._call("GET", "/notes", params={"subject": subject})
        if not ok:
            return message
        if not data:
            return "No notes yet."
        lines = []
        for note in data:
            about = f" (subject {note['subject']})" if note.get("subject") else ""
            lines.append(f"- {note['name']}{about}: version {note['version']}, {note['bytes']:,} bytes, "
                         f"updated {note.get('updated_at', '')}")
        return "\n".join(lines)


# ---------------------------------------------------------------- Action log


class LogCheckInput(BaseModel):
    subject: str = Field(..., description="What the action was about, such as jira:PRJ-15 or person:sam@example.com.")
    action: str = Field(..., description="The action, such as followup:due_soon or report:weekly.")
    within: str = Field("", description="Only look back this far, such as 24h, 7d or 2w. Empty for any time.")


class LogCheckTool(_StateTool):
    name: str = "Check Action Log"
    description: str = (
        "Answers exactly whether this crew already did something, such as reminding someone about an issue: "
        "returns the latest matching action log entry, or says there is none."
    )
    args_schema: type[BaseModel] = LogCheckInput

    def _run(self, subject: str, action: str, within: str = "") -> str:
        ok, data, message = self._call("GET", "/log/check", params={"subject": subject, "action": action, "within": within})
        if not ok:
            return message
        entry = data.get("entry")
        if not entry:
            period = f" within {within}" if within else ""
            return f"No {action} recorded for {subject}{period}."
        details = json.dumps(entry.get("details") or {}, ensure_ascii=False)
        dry = " (dry run)" if entry.get("dry_run") else ""
        return f"Last {action} for {subject} was recorded at {entry['at']}{dry}. Details: {details}"


class LogRecordInput(BaseModel):
    subject: str = Field(..., description="What the action was about, such as jira:PRJ-15.")
    action: str = Field(..., description="The action, such as followup:due_soon or report:weekly.")
    details: dict[str, Any] | None = Field(None, description="Facts worth keeping, such as who was told and a link.")
    idempotency_key: str = Field(
        "", description="A key unique to this action (such as PRJ-15/due_soon/2026-10-04); recording it twice does nothing.")


class LogRecordTool(_StateTool):
    name: str = "Record Action"
    description: str = (
        "Adds an entry to this crew's action log after it did something (sent a reminder, published a report), "
        "so later runs can check it with Check Action Log. Tracker comments, tracker follow-ups and report deliveries are "
        "recorded by their own tools and cannot be recorded here."
    )
    args_schema: type[BaseModel] = LogRecordInput

    def _run(self, subject: str, action: str, details: dict[str, Any] | None = None, idempotency_key: str = "") -> str:
        body = {"subject": subject, "action": action, "details": details or {}, "idempotency_key": idempotency_key}
        ok, data, message = self._call("POST", "/log", body=body)
        if not ok:
            return message
        if data.get("duplicate"):
            return f"{action} for {subject} was already recorded (same idempotency key); nothing was added."
        if data["entry"].get("dry_run"):
            return (f"Recorded {action} for {subject} at {data['entry']['at']} as a dry-run entry (this run is a dry run, "
                    "so nothing should have been sent).")
        return f"Recorded {action} for {subject} at {data['entry']['at']}."


_STATE_TOOLS: dict[str, type[_StateTool]] = {
    "notes_get": NotesGetTool,
    "notes_put": NotesPutTool,
    "notes_list": NotesListTool,
    "log_check": LogCheckTool,
    "log_record": LogRecordTool,
}


def build_state_tool(key: str, agent_role: str, client: StateClient | None) -> BaseTool:
    """One state tool for one agent. Without a client the tool explains that state is unavailable."""
    if key not in _STATE_TOOLS:
        raise ValueError(f"Unknown state tool '{key}'.")
    return _STATE_TOOLS[key]().configure(client, agent_role)
