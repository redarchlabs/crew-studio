"""The deliver_report tool: hand Crew Studio one project's finished report to send everywhere it should go.

Studio does the sending (design section 12): the report goes to the work order output and to every destination
set on the schedule that started the run (Slack, Teams, email, KM2), and is kept as the project's weekly-report note
(or, for a daily report, its daily-report note) so the next one builds on it. Studio enforces, in code, everything delivery must respect: dry run (nothing is
sent; each destination comes back as "would send"), the size and per-run caps, the crew's right to use each
connection, and one report of each cadence per project per run. A failed destination can be retried from the work order without
sending the others again.
"""

from typing import Any

from pydantic import BaseModel, Field

from .state_tools import NOT_AVAILABLE, StateClient, _StateTool

REPORT_TOOL_KEYS = ("deliver_report",)

# Studio sends to every destination (in parallel, each bounded) and records the results; it finishes deliveries it
# started even if this times out, and the same report sent again in the run is not delivered twice.
DELIVER_TIMEOUT_SECONDS = 150.0

# The largest report Studio accepts (64 KB), checked here too so the agent hears about it straight away
MAX_REPORT_BYTES = 64 << 10

CADENCES = ("weekly", "daily")

STATUS_WORDS = {"sent": "sent", "would_send": "would be sent (dry run)", "failed": "FAILED", "pending": "pending"}


class DeliverInput(BaseModel):
    project: str = Field(..., description="The project's name as on the tracking list, such as Atlas.")
    markdown: str = Field(..., description=(
        "The narrative in Markdown (at most 64 KB): a short summary with the status, the critical path, the risks and "
        "the asks. Crew Studio reads the tracker itself and adds the overdue, due-soon, blocked and unassigned items, "
        "the changes since the last report and charts, so do not re-list items or dates."))
    cadence: str = Field("weekly", description=(
        'Which report this is: "weekly" (the default: the weekly status report, titled by ISO week) or "daily" (a daily '
        "report, titled by today's date; it never replaces the weekly one)."))


class DeliverReportTool(_StateTool):
    name: str = "Deliver Report"
    description: str = (
        "Delivers one project's finished report: Crew Studio adds the project's tracked-item data and charts to your "
        "narrative, sends it to the work order output and every destination set on this schedule (Slack, Teams, email, "
        "KM2) and keeps it as the next report's starting point. Write the narrative (summary, critical path, risks, "
        "asks), not a list of items. cadence is "
        '"weekly" (the default) for the weekly status report or "daily" for a daily report. Call it once per project '
        "and cadence with the final text. In a dry run nothing is sent and the answer lists what would have been."
    )
    args_schema: type[BaseModel] = DeliverInput

    def _run(self, project: str, markdown: str, cadence: str = "weekly") -> str:
        if self._client is None:
            return "Nothing was delivered: " + NOT_AVAILABLE
        cadence = (cadence or "weekly").strip().lower()
        if cadence not in CADENCES:
            return 'Nothing was delivered: cadence must be "weekly" or "daily".'
        if not markdown.strip():
            return "Nothing was delivered: the report is empty."
        size = len(markdown.encode("utf-8"))
        if size > MAX_REPORT_BYTES:
            return f"Nothing was delivered: the report is {size:,} bytes; the most is {MAX_REPORT_BYTES:,}. Shorten it."
        ok, data, message = self._client.call("POST", "/reports", self._agent_role,
                                              body={"project": project, "markdown": markdown, "cadence": cadence},
                                              timeout=DELIVER_TIMEOUT_SECONDS)
        if not ok:
            return "Nothing was delivered: " + message
        return _answer(data or {})


def _answer(data: dict[str, Any]) -> str:
    lines = [str(data.get("message") or "Delivered."),
             f"Report for {data.get('project', '')} ({data.get('cadence') or 'weekly'}, {data.get('period') or data.get('week', '')}):"]
    for d in data.get("deliveries") or []:
        status = STATUS_WORDS.get(str(d.get("status")), str(d.get("status")))
        lines.append(f"- {d.get('target', d.get('destination', ''))}: {status}. {d.get('message', '')}".rstrip())
    return "\n".join(lines)


def build_report_tool(key: str, agent_role: str, client: StateClient | None) -> DeliverReportTool:
    """The deliver_report tool for one agent. Without a client it explains that Studio's state API is unavailable."""
    if key not in REPORT_TOOL_KEYS:
        raise ValueError(f"Unknown report tool '{key}'.")
    tool = DeliverReportTool()
    tool.configure(client, agent_role)
    return tool
