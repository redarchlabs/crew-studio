import json
from pathlib import Path
from typing import Any

import httpx

from studio_runner.build import build_crew
from studio_runner.human import ASK_HUMAN_NAME, PROCEED, STOP, AskHumanTool, Halt, escalation_note
from studio_runner.listeners import Listeners
from studio_runner.meter import Meter
from studio_runner.state_tools import StateClient
from studio_runner.tools import ToolFactory

from .conftest import PRICES, SETTINGS, make_spec

TOKEN = "csr_" + "t" * 43
ESCALATION = {"rules": "Ask before assuming a budget.", "timeout_minutes": 1, "on_timeout": "proceed", "max_questions": 3}


def ok(data: Any) -> dict[str, Any]:
    return {"success": True, "data": data, "error": None}


class FakeStudio:
    """Answers the ask, then serves the polled statuses in order (the last one repeats)."""

    def __init__(self, polls: list[dict[str, Any]], ask: tuple[int, Any] | None = None) -> None:
        self.polls = polls
        self.ask = ask or (201, ok({"id": "q1", "status": "open", "on_timeout": "proceed"}))
        self.requests: list[httpx.Request] = []

    def handler(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        if request.method == "POST":
            return httpx.Response(self.ask[0], json=self.ask[1])
        poll = self.polls.pop(0) if len(self.polls) > 1 else self.polls[0]
        return httpx.Response(200, json=ok(poll))

    def client(self) -> StateClient:
        return StateClient("http://127.0.0.1:8088/api/run-state", TOKEN, transport=httpx.MockTransport(self.handler))


def tool(studio: FakeStudio, halt: Halt | None = None, escalation: dict[str, Any] | None = None) -> AskHumanTool:
    return AskHumanTool().configure(studio.client(), "Analyst", escalation or ESCALATION, halt or Halt(),
                                    current_task=lambda role: "Research", sleep=lambda _s: None)


def test_asks_then_returns_the_answer():
    studio = FakeStudio([{"status": "open"}, {"status": "answered", "answer": "Assume $5k.", "answered_by": "Sam"}])
    assert tool(studio)._run("What budget?", "The brief has none") == "Sam answered: Assume $5k."
    ask = studio.requests[0]
    assert ask.url.path == "/api/run-state/questions"
    assert json.loads(ask.content) == {"question": "What budget?", "context": "The brief has none", "task_name": "Research"}
    assert [r.url.path for r in studio.requests[1:]] == ["/api/run-state/questions/q1"] * 2
    # Each wait is a long-poll: Studio holds it until the answer arrives
    assert all(r.url.params["wait"] == "25" for r in studio.requests[1:])


def test_timeout_proceeds_with_an_assumption_by_default():
    halt = Halt()
    out = tool(FakeStudio([{"status": "timed_out", "on_timeout": "proceed"}]), halt)._run("What budget?")
    assert "state clearly" in out and "assum" in out
    assert not halt.is_set()


def test_timeout_halts_the_run_when_the_rules_say_fail():
    halt = Halt()
    out = tool(FakeStudio([{"status": "timed_out", "on_timeout": "fail"}]), halt)._run("What budget?")
    assert "stop" in out.lower()
    assert halt.is_set() and "No answer" in halt.reason


def test_refusals_come_back_as_text():
    refused = FakeStudio([{}], ask=(429, {"success": False, "data": None,
                                         "error": {"code": "too_many_requests", "message": "This run has already asked 3 questions."}}))
    assert "already asked 3 questions" in tool(refused)._run("Again?")
    assert len(refused.requests) == 1
    assert "not available" in AskHumanTool()._run("?")


def test_gives_up_waiting_when_studio_stops_answering():
    def down(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(201, json=ok({"id": "q1", "status": "open"}))
        raise httpx.ConnectError("refused")

    client = StateClient("http://127.0.0.1:1/api/run-state", TOKEN, transport=httpx.MockTransport(down))
    t = AskHumanTool().configure(client, "A", ESCALATION, Halt(), sleep=lambda _s: None)
    assert "assum" in t._run("?")


def test_an_unreachable_studio_still_honours_the_fail_rule():
    def down(request: httpx.Request) -> httpx.Response:
        if request.method == "POST":
            return httpx.Response(201, json=ok({"id": "q1", "status": "open"}))
        return httpx.Response(401, json={"success": False, "data": None, "error": {"code": "unauthenticated", "message": "expired"}})

    client = StateClient("http://127.0.0.1:1/api/run-state", TOKEN, transport=httpx.MockTransport(down))
    halt = Halt()
    t = AskHumanTool().configure(client, "A", {**ESCALATION, "on_timeout": "fail"}, halt, sleep=lambda _s: None)
    assert t._run("?") == STOP
    assert halt.is_set()


def test_answers_are_never_served_from_crewais_tool_cache():
    assert AskHumanTool().cache_function({"question": "?"}, "Sam answered: yes") is False


def test_a_closed_question_ends_the_wait():
    assert "ended" in tool(FakeStudio([{"status": "closed"}]))._run("?")


def test_build_adds_the_tool_and_rules_only_when_escalation_is_on(tmp_path: Path):
    factory = ToolFactory(search_model="m", searches_allowed=lambda: True, record_search=lambda *a: None, workspace=tmp_path,
                          prices=PRICES)
    state = FakeStudio([{}]).client()
    plain = build_crew(make_spec(), factory, tmp_path, state=state)
    assert all(ASK_HUMAN_NAME not in [t.name for t in a.tools] for a in plain.crew.agents)

    spec = make_spec()
    spec["escalation"] = ESCALATION
    built = build_crew(spec, factory, tmp_path, state=state, halt=Halt())
    for agent in built.crew.agents:
        assert ASK_HUMAN_NAME in [t.name for t in agent.tools]
        assert "Ask before assuming a budget." in agent.backstory
    # Without a state connection there is nobody to ask
    unwired = build_crew(spec, factory, tmp_path, state=None)
    assert all(ASK_HUMAN_NAME not in [t.name for t in a.tools] for a in unwired.crew.agents)


def test_escalation_note_quotes_the_rules():
    note = escalation_note(ESCALATION)
    assert ASK_HUMAN_NAME in note and "Ask before assuming a budget." in note


def test_stops_waiting_once_the_question_is_long_past_its_timeout():
    now = [0.0]

    def clock() -> float:
        now[0] += 30  # each long-poll takes its full wait
        return now[0]

    studio = FakeStudio([{"status": "open"}])
    t = AskHumanTool().configure(studio.client(), "A", ESCALATION, Halt(), sleep=lambda _s: None, clock=clock)
    assert t._run("?") == PROCEED
    # One minute plus slack, in 30 second steps
    assert 3 <= len(studio.requests) <= 8


def test_a_halt_blocks_further_llm_calls(emitter):
    halt = Halt()
    listeners = Listeners(emitter, Meter(PRICES, SETTINGS, 1.0, 0, lambda *_a: None), halt)
    assert listeners.before_llm_call(None) is None
    halt.set("No answer")
    assert listeners.before_llm_call(None) is False
