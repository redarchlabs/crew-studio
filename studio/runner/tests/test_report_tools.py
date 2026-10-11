import json
from pathlib import Path
from typing import Any

import httpx
import pytest

from studio_runner.chat import CHAT_TOOL_KEYS
from studio_runner.report_tools import (
    MAX_REPORT_BYTES,
    REPORT_TOOL_KEYS,
    DeliverReportTool,
    _answer,
    build_report_tool,
)
from studio_runner.state_tools import NOT_AVAILABLE, StateClient
from studio_runner.tools import ToolFactory, load_catalog

TOKEN = "csr_" + "r" * 43


class FakeStudio:
    """Stands in for /api/run-state/reports."""

    def __init__(self, status: int, body: Any) -> None:
        self.requests: list[httpx.Request] = []
        self.status, self.body = status, body

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        return httpx.Response(self.status, json=self.body)

    def client(self) -> StateClient:
        return StateClient("http://127.0.0.1:8088/api/run-state", TOKEN, transport=httpx.MockTransport(self.handler))


def tool(studio: FakeStudio) -> DeliverReportTool:
    return build_report_tool("deliver_report", "PMO Status Reporter", studio.client())


DELIVERED = {
    "report_id": "r1", "project": "Atlas", "week": "2026-W41", "dry_run": False,
    "message": "Delivered to 2 of 3 destinations; 1 failed and can be retried from the work order.",
    "deliveries": [
        {"destination": "work_order", "target": "the work order output", "status": "sent", "message": "Kept with the work order."},
        {"destination": "slack", "target": "PMO Slack", "status": "sent", "message": "Sent to PMO Slack."},
        {"destination": "teams", "target": "PMO Teams", "status": "failed", "message": "Teams had a problem (HTTP 502)."},
    ],
}


def test_delivers_through_studio_and_reports_each_destination():
    studio = FakeStudio(201, {"success": True, "data": DELIVERED, "error": None})
    out = tool(studio)._run(project="Atlas", markdown="# Atlas\nOn track")
    req = studio.requests[0]
    assert req.method == "POST" and req.url.path == "/api/run-state/reports"
    assert req.headers["Authorization"] == "Bearer " + TOKEN and req.headers["X-Crew-Agent"] == "PMO Status Reporter"
    assert json.loads(req.content) == {"project": "Atlas", "markdown": "# Atlas\nOn track", "cadence": "weekly"}
    assert "Report for Atlas (weekly, 2026-W41):" in out
    assert "- PMO Slack: sent. Sent to PMO Slack." in out
    assert "- PMO Teams: FAILED. Teams had a problem (HTTP 502)." in out


def test_dry_run_answer():
    data = {**DELIVERED, "dry_run": True, "message": "Dry run: nothing was sent.",
            "deliveries": [{"destination": "email", "target": "Mail (2 recipients)", "status": "would_send", "message": "Dry run: would send."}]}
    out = _answer(data)
    assert out.startswith("Dry run: nothing was sent.") and "would be sent (dry run)" in out
    assert "odd" in _answer({"deliveries": [{"status": "odd"}]})


def test_refusals_come_back_as_text():
    studio = FakeStudio(429, {"success": False, "data": None, "error": {"code": "report_cap", "message": "This run has already delivered 20 reports."}})
    assert tool(studio)._run(project="Atlas", markdown="x") == "Nothing was delivered: This run has already delivered 20 reports."
    assert "is empty" in tool(studio)._run(project="Atlas", markdown="  ")
    big = "é" * (MAX_REPORT_BYTES // 2 + 1)
    assert "Shorten it" in tool(studio)._run(project="Atlas", markdown=big)
    assert len(studio.requests) == 1


def test_without_studio_and_registration(tmp_path: Path):
    assert NOT_AVAILABLE in build_report_tool("deliver_report", "A", None)._run(project="x", markdown="y")
    with pytest.raises(ValueError):
        build_report_tool("send_fax", "A", None)
    factory = ToolFactory("m", lambda: True, lambda *a: None, tmp_path)
    assert factory.build("deliver_report", "Agent").name == "Deliver Report"
    assert "deliver_report" in {t["key"] for t in load_catalog()}
    # Chat turns never deliver reports
    assert not set(REPORT_TOOL_KEYS) & set(CHAT_TOOL_KEYS)


def test_passes_the_cadence_through():
    studio = FakeStudio(201, {"success": True, "data": {**DELIVERED, "cadence": "daily", "period": "2026-10-06"}, "error": None})
    out = tool(studio)._run(project="Atlas", markdown="# Today", cadence=" Daily ")
    assert json.loads(studio.requests[0].content)["cadence"] == "daily"
    assert "Report for Atlas (daily, 2026-10-06):" in out
    assert tool(studio)._run(project="Atlas", markdown="x", cadence="hourly") == 'Nothing was delivered: cadence must be "weekly" or "daily".'
    assert len(studio.requests) == 1
    assert "daily" in DeliverReportTool.model_fields["description"].default


def test_asks_for_a_narrative_not_a_list():
    description = DeliverReportTool.model_fields["description"].default
    assert "narrative" in description and "not a list of items" in description
    from studio_runner.report_tools import DeliverInput
    assert "do not re-list items" in DeliverInput.model_fields["markdown"].description
