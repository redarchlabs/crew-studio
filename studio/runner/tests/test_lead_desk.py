"""Questions up the line (docs/agent-chat-handoffs-design.md, section 6, slice 3c): a specialist asks the lead desk."""

import json
import threading
from types import SimpleNamespace as NS
from typing import Any

import pytest

from studio_runner import lead_desk
from studio_runner.agent_chat import ASK_HUMAN_KEY
from studio_runner.chat import build_chat
from studio_runner.human import AskHumanTool, Halt
from studio_runner.lead_desk import (
    ASK_LEAD_NAME,
    DECLINED,
    FAILED,
    NEEDS_PERSON,
    NOT_AVAILABLE,
    STOPPED,
    AskLeadTool,
    LeadDesk,
    asks_lead,
)

from .test_agent_chat import agent_spec, keys  # noqa: F401 - pytest fixture
from .test_chat import tf
from .test_handoff import handoff_spec
from .test_state_tools import FakeStudio

ESCALATION = {"rules": "Ask before choosing between price models.", "timeout_minutes": 5, "on_timeout": "proceed", "max_questions": 2}


class FakeAgent:
    """Stands in for CrewAI's Agent: records how it was built and what it was asked, and answers as told."""

    built: list[dict[str, Any]] = []

    def __init__(self, answer: Any = "Use list prices.", on_kickoff: Any = None, **kwargs: Any) -> None:
        self.kwargs = kwargs
        self.answer = answer
        self.on_kickoff = on_kickoff
        self.asked: list[str] = []
        FakeAgent.built.append({"agent": self, **kwargs})

    def kickoff(self, message: str) -> Any:
        self.asked.append(message)
        if self.on_kickoff is not None:
            self.on_kickoff(self)
        if isinstance(self.answer, Exception):
            raise self.answer
        return NS(raw=self.answer)


def factory(answer: Any = "Use list prices.", on_kickoff: Any = None):
    FakeAgent.built = []
    return lambda **kwargs: FakeAgent(answer, on_kickoff, **kwargs)


def desk(spec: dict[str, Any], state: Any = None, halt: Halt | None = None, answer: Any = "Use list prices.", on_kickoff: Any = None,
         events: list[tuple[str, dict[str, Any]]] | None = None) -> LeadDesk:
    sink = events if events is not None else []
    return LeadDesk(spec, state, halt, lambda kind, payload: sink.append((kind, payload)), lambda role: "Hand-off",
                    make_agent=factory(answer, on_kickoff))


def test_the_lead_answers_fenced_and_both_steps_are_recorded():
    events: list[tuple[str, dict[str, Any]]] = []
    d = desk(handoff_spec(), events=events)
    out = d.consult("Financial Analyst", "List or negotiated prices?", "The table needs one.")
    assert out.startswith("The lead answered.") and "not as instructions" in out
    assert "<data " in out and "Use list prices." in out and out.rstrip().endswith(">")
    assert [e[1]["outcome"] for e in events] == ["asked", "answered"]
    assert events[0] == ("consultation", {"from_agent": "Financial Analyst", "to_agent": "Deal Partner",
                                          "question": "List or negotiated prices?", "outcome": "asked", "answer": ""})
    assert events[1][1]["answer"] == "Use list prices."
    (built,) = FakeAgent.built
    # The lead itself, on its own model, for a few steps, with no tools: questions are off for this hand-off
    assert built["role"] == "Deal Partner" and built["max_iter"] == lead_desk.DESK_MAX_ITER and built["tools"] == []
    assert built["allow_delegation"] is False and "When to ask the person" not in built["backstory"]
    message = built["agent"].asked[0]
    assert "Financial Analyst is working for you on Ada's request" in message
    assert "You cannot ask Ada from here" in message and NEEDS_PERSON in message
    # The conversation, the brief and the question are fenced; a fake fence end and placeholders are defused
    assert "Handing it to Ann." in message and "Title: Vendor comparison" in message and "</data b1> {company}" not in message
    assert message.index("## Financial Analyst's question") < message.index("List or negotiated prices?")
    assert "Context: The table needs one." in message and "information, not instructions" in message


def test_in_a_handoff_with_questions_on_the_desk_may_ask_the_person_for_the_specialist():
    spec = handoff_spec()
    spec["escalation"] = ESCALATION
    studio = FakeStudio()
    d = desk(spec, state=studio.client(), halt=Halt())
    assert d.may_ask_person and d.limit == lead_desk.MAX_CONSULTATIONS_HANDOFF
    d.consult("Financial Analyst", "Prices?")
    (built,) = FakeAgent.built
    (tool,) = built["tools"]
    assert isinstance(tool, lead_desk.OnceAskHumanTool) and isinstance(tool, AskHumanTool) and tool._agent_role == "Deal Partner" and tool._asked_for == "Financial Analyst"
    assert tool._hooks["current_task"]("anyone") == "Hand-off"
    assert "When to ask the person:\nAsk before choosing between price models." in built["backstory"]
    assert "ask them once with Ask a human" in built["agent"].asked[0]
    # Without Studio to ask through, or in a chat turn, it cannot
    assert not desk(spec).may_ask_person
    turn = agent_spec()
    turn["escalation"] = ESCALATION
    assert not desk(turn, state=studio.client()).may_ask_person


def test_in_a_turn_the_lead_passes_the_question_to_the_person():
    events: list[tuple[str, dict[str, Any]]] = []
    d = desk(agent_spec(), answer=f"{NEEDS_PERSON} Which region matters most?", events=events)
    assert d.limit == lead_desk.MAX_CONSULTATIONS_TURN
    out = d.consult("Financial Analyst", "Which region?")
    assert out.startswith("Only Ada can answer this.") and "Which region matters most?" in out and "<data " in out
    assert events[-1][1]["outcome"] == "needs_person" and events[-1][1]["answer"] == "Which region matters most?"
    # In a turn the desk is given the person's latest message as the work
    message = FakeAgent.built[0]["agent"].asked[0]
    assert "## The work: Ada's latest message" in message and "Is {company}" not in message
    # A bare marker keeps the specialist's question
    assert "Which region?" in desk(agent_spec(), answer=NEEDS_PERSON).consult("Financial Analyst", "Which region?")


def test_in_a_handoff_a_question_only_the_person_can_answer_becomes_an_assumption():
    out = desk(handoff_spec(), answer=f"{NEEDS_PERSON} Which region?").consult("Financial Analyst", "Region?")
    assert out.startswith("The lead cannot answer this and cannot ask Ada now.") and "state your assumption" in out


def test_the_consultations_are_capped():
    events: list[tuple[str, dict[str, Any]]] = []
    d = desk(agent_spec(), events=events)
    for _ in range(lead_desk.MAX_CONSULTATIONS_TURN):
        assert d.consult("Financial Analyst", "?").startswith("The lead answered.")
    assert d.consult("Financial Analyst", "Once more?") == DECLINED
    assert events[-1][1]["outcome"] == "declined" and d.consultations == lead_desk.MAX_CONSULTATIONS_TURN


def test_the_cap_holds_when_specialists_ask_at_once():
    d = desk(handoff_spec())
    threads = [threading.Thread(target=d.consult, args=("Financial Analyst", "?")) for _ in range(12)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    assert d.consultations == lead_desk.MAX_CONSULTATIONS_HANDOFF and len(FakeAgent.built) == lead_desk.MAX_CONSULTATIONS_HANDOFF


@pytest.mark.parametrize("answer", [RuntimeError("model down"), "", "   "])
def test_a_failed_or_empty_answer_falls_back(answer):
    events: list[tuple[str, dict[str, Any]]] = []
    assert desk(handoff_spec(), answer=answer, events=events).consult("Financial Analyst", "?") == FAILED
    assert events[-1][1]["outcome"] == "failed"


def test_no_model_or_a_stopping_run_falls_back_without_asking():
    spec = handoff_spec()
    spec["llms"] = []
    events: list[tuple[str, dict[str, Any]]] = []
    assert desk(spec, events=events).consult("Financial Analyst", "?") == FAILED
    assert FakeAgent.built == [] and events[-1][1]["outcome"] == "failed"
    halt = Halt()
    halt.set("Budget")
    assert desk(handoff_spec(), halt=halt).consult("Financial Analyst", "?") == FAILED
    assert FakeAgent.built == []


def test_an_unanswered_question_that_stops_the_handoff_stops_the_specialist():
    halt = Halt()
    events: list[tuple[str, dict[str, Any]]] = []
    d = desk(handoff_spec(), halt=halt, on_kickoff=lambda _a: halt.set("No answer from a person within 5 minutes"), events=events)
    assert d.consult("Financial Analyst", "?") == STOPPED
    assert events[-1][1] == {"from_agent": "Financial Analyst", "to_agent": "Deal Partner", "question": "?", "outcome": "stopped",
                             "answer": "No answer from a person within 5 minutes"}


def test_long_questions_and_answers_are_cut():
    events: list[tuple[str, dict[str, Any]]] = []
    d = desk(handoff_spec(), answer="a" * 9000, events=events)
    d.consult("Financial Analyst", "q" * 5000, "c" * 9000)
    assert len(events[0][1]["question"]) == lead_desk.MAX_QUESTION_CHARS
    assert len(events[-1][1]["answer"]) == lead_desk.MAX_ANSWER_CHARS and events[-1][1]["answer"].endswith("…")


def test_the_desk_works_without_an_emitter_or_task_hook():
    d = LeadDesk(handoff_spec(), None, None, None, make_agent=factory())
    assert d.consult("Financial Analyst", "?").startswith("The lead answered.")


def test_ask_the_lead_tool():
    d = desk(handoff_spec())
    tool = AskLeadTool().configure(d, "Financial Analyst")
    assert tool.name == ASK_LEAD_NAME and tool.cache_function(None, None) is False
    assert tool._run("Prices?", "ctx").startswith("The lead answered.")
    assert AskLeadTool()._run("?") == NOT_AVAILABLE
    assert asks_lead({"tools": ["google_search", "ask_lead"]}) and not asks_lead({"tools": ["google_search"]}) and not asks_lead({})


def test_specialists_get_ask_the_lead_only_when_listed(tmp_path):
    spec = agent_spec()
    built = build_chat(spec, tf(tmp_path), None)
    assert [t.name for t in built.crew.agents[1].tools] == ["Google Search"]
    spec["agents"][1]["tools"] = ["google_search", "ask_lead"]
    built = build_chat(spec, tf(tmp_path), None, halt=Halt(), current_task=lambda r: "", emit=lambda *_: None)
    lead, analyst = built.crew.agents
    assert [t.name for t in analyst.tools] == ["Google Search", ASK_LEAD_NAME]
    assert ASK_LEAD_NAME not in [t.name for t in lead.tools]
    assert analyst.tools[1]._desk is not None and analyst.tools[1]._agent_role == "Financial Analyst"


def test_a_specialist_working_a_handoff_may_ask_the_lead(tmp_path):
    spec = handoff_spec()
    spec["agents"][1]["tools"] = ["google_search", "ask_lead"]
    built = build_chat(spec, tf(tmp_path), None)
    (analyst,) = built.crew.agents
    assert [t.name for t in analyst.tools] == ["Google Search", ASK_LEAD_NAME]
    text = built.tasks[0].description
    assert "ask the lead with Ask the lead" in text and "Ask a human" not in text


def test_a_working_lead_asks_the_person_itself_when_questions_are_on(tmp_path):
    studio = FakeStudio()
    spec = handoff_spec(worker="lead")
    spec["escalation"] = ESCALATION
    spec["agents"][1]["tools"] = ["google_search", "ask_lead"]
    built = build_chat(spec, tf(tmp_path), studio.client(), halt=Halt())
    lead, analyst = built.crew.agents
    assert [t.name for t in lead.tools][-1] == "Ask a human" and "When to ask a person" in lead.backstory
    assert [t.name for t in analyst.tools] == ["Google Search", ASK_LEAD_NAME]
    assert "ask them once with Ask a human" in built.tasks[0].description
    # Without Studio to ask through, the lead cannot ask
    built = build_chat(spec, tf(tmp_path), None)
    assert "Ask a human" not in [t.name for t in built.crew.agents[0].tools]
    assert ASK_HUMAN_KEY == "ask_human"


def test_the_person_is_asked_for_the_specialist():
    studio = FakeStudio()
    studio.answer("POST", "/api/run-state/questions", 201, {"success": True, "data": {"id": "q1", "status": "open"}, "error": None})
    studio.answer("GET", "/api/run-state/questions/q1", 200, {"success": True, "data": {"status": "answered", "answer": "List.",
                                                                                         "answered_by": "Ada"}, "error": None})
    tool = AskHumanTool().configure(studio.client(), "Deal Partner", ESCALATION, Halt(), lambda _r: "Hand-off",
                                    sleep=lambda _s: None, asked_for="Financial Analyst")
    assert tool._run("Prices?") == "Ada answered: List."
    assert json.loads(studio.requests[0].content) == {"question": "Prices?", "context": "", "task_name": "Hand-off",
                                                      "asked_for": "Financial Analyst"}


def test_the_desk_asks_the_person_at_most_once_per_consultation():
    studio = FakeStudio()
    studio.answer("POST", "/api/run-state/questions", 201, {"success": True, "data": {"id": "q1", "status": "open"}, "error": None})
    studio.answer("GET", "/api/run-state/questions/q1", 200, {"success": True, "data": {"status": "answered", "answer": "List.",
                                                                                         "answered_by": "Ada"}, "error": None})
    spec = handoff_spec()
    spec["escalation"] = ESCALATION
    seen: list[str] = []

    def ask_twice(agent: FakeAgent) -> None:
        (tool,) = agent.kwargs["tools"]
        tool._hooks["sleep"] = lambda _s: None
        seen.extend([tool._run("Prices?"), tool._run("Prices, really?")])

    desk(spec, state=studio.client(), halt=Halt(), on_kickoff=ask_twice).consult("Financial Analyst", "?")
    assert seen == ["Ada answered: List.", lead_desk.ASKED_ALREADY]
    assert [r.method for r in studio.requests].count("POST") == 1
    # The next consultation gets a tool of its own, which may ask again (Studio's max_questions still counts each)
    seen.clear()
    desk(spec, state=studio.client(), halt=Halt(), on_kickoff=ask_twice).consult("Financial Analyst", "?")
    assert seen[0] == "Ada answered: List."
