"""Human input: the Ask a human tool, for crews whose escalation rules say when an agent should ask a person.

The question goes to Crew Studio's run-state API, which shows it to the person who started the run. The tool then
waits on a long-poll that Studio answers the moment the person replies (or when the question times out), so the
agent hears the answer straight away. Every outcome comes back as text the agent can act on.

When nobody answers in time, the crew's rules decide: carry on and state the assumption ("proceed"), or stop the run
("fail"). Stopping sets the run's Halt, which blocks every further LLM call (see Listeners.before_llm_call).
"""

import threading
import time
from typing import Any, Callable

from crewai.tools import BaseTool
from pydantic import BaseModel, Field, PrivateAttr

ASK_HUMAN_NAME = "Ask a human"
# Studio holds each wait for up to this long; the HTTP timeout leaves room on top
WAIT_SECONDS = 25
WAIT_HTTP_TIMEOUT = WAIT_SECONDS + 15.0
# After this many failed waits in a row (Studio unreachable), the agent carries on
MAX_WAIT_FAILURES = 10
RETRY_SECONDS = 5.0
# Waits beyond the question's own timeout, in case Studio's clock and ours differ
DEADLINE_SLACK_SECONDS = 120

NOT_AVAILABLE = "Asking a person is not available in this run. Decide yourself and state your assumption in your answer."
PROCEED = ("No answer from a person within the time allowed. Carry on with your best judgement and state clearly in "
           "your answer what you assumed.")
STOP = "No answer from a person within the time allowed, and the rules for asking say to stop. Stop working now."
ENDED = "The run has ended, so nobody will answer. Stop working now."


class Halt:
    """Set when the run must stop because a question went unanswered and the rules say fail."""

    def __init__(self) -> None:
        self._event = threading.Event()
        self.reason = ""

    def set(self, reason: str) -> None:
        self.reason = reason
        self._event.set()

    def is_set(self) -> bool:
        return self._event.is_set()


def escalation_note(escalation: dict[str, Any]) -> str:
    """The instructions added to every agent's backstory when escalation is on."""
    return (
        f"\n\nWhen to ask a person (use the {ASK_HUMAN_NAME} tool):\n{escalation['rules']}\n"
        "Ask only when one of these applies and you cannot reasonably decide yourself. Ask one specific question, "
        "say why you need it, and wait for the answer. If nobody answers in time, follow what the tool tells you."
    )


class AskHumanInput(BaseModel):
    question: str = Field(..., description="One specific question for the person, answerable in a sentence or two.")
    context: str = Field("", description="What you need the answer for and what you already know, so they can answer quickly.")


def _never_cache(_args: Any = None, _result: Any = None) -> bool:
    # A repeated question must reach the person again, not get the earlier reply from CrewAI's tool cache
    return False


class AskHumanTool(BaseTool):
    name: str = ASK_HUMAN_NAME
    cache_function: Callable[..., bool] = _never_cache
    description: str = (
        "Asks the person who started this run a question and waits for their answer. Use it only when the crew's "
        "rules for asking a person apply; otherwise decide yourself."
    )
    args_schema: type[BaseModel] = AskHumanInput
    _state: Any = PrivateAttr(default=None)
    _agent_role: str = PrivateAttr(default="")
    _escalation: dict[str, Any] = PrivateAttr(default_factory=dict)
    _halt: Halt | None = PrivateAttr(default=None)
    _hooks: dict[str, Callable[..., Any]] = PrivateAttr(default_factory=dict)
    _asked_for: str = PrivateAttr(default="")

    def configure(self, state: Any, agent_role: str, escalation: dict[str, Any], halt: Halt,
                  current_task: Callable[[str], str] | None = None,
                  sleep: Callable[[float], None] = time.sleep, clock: Callable[[], float] = time.monotonic,
                  asked_for: str = "") -> "AskHumanTool":
        """asked_for names the agent the questions are asked for, when the asking agent asks on another's behalf (the lead
        desk of a hand-off asks for a specialist; see lead_desk.py)."""
        self._state = state
        self._agent_role = agent_role
        self._asked_for = asked_for
        self._escalation = escalation
        self._halt = halt
        # Callables live in a dict: stored directly as private attributes they get bound like methods
        self._hooks = {"current_task": current_task or (lambda _role: ""), "sleep": sleep, "clock": clock}
        return self

    def _run(self, question: str, context: str = "") -> str:
        if self._state is None:
            return NOT_AVAILABLE
        task = self._hooks["current_task"](self._agent_role)
        body = {"question": question, "context": context, "task_name": task}
        if self._asked_for:
            body["asked_for"] = self._asked_for
        ok, data, message = self._state.call("POST", "/questions", self._agent_role, body=body)
        if not ok:
            return message
        return self._wait(str(data["id"]))

    def _wait(self, question_id: str) -> str:
        clock, sleep = self._hooks["clock"], self._hooks["sleep"]
        give_up = clock() + int(self._escalation.get("timeout_minutes", 30)) * 60 + DEADLINE_SLACK_SECONDS
        failures = 0
        while clock() < give_up:
            ok, data, _message = self._state.call("GET", f"/questions/{question_id}", self._agent_role,
                                                  params={"wait": WAIT_SECONDS}, timeout=WAIT_HTTP_TIMEOUT)
            if not ok:
                failures += 1
                if failures >= MAX_WAIT_FAILURES:
                    break
                sleep(RETRY_SECONDS)
                continue
            failures = 0
            outcome = self._outcome(data or {})
            if outcome is not None:
                return outcome
        # No word from Studio (unreachable, or the wait outlasted the question): treat it as no answer in time
        return self._timed_out(None)

    def _outcome(self, data: dict[str, Any]) -> str | None:
        status = data.get("status")
        if status == "answered":
            who = data.get("answered_by") or "The person"
            return f"{who} answered: {data.get('answer', '')}"
        if status == "timed_out":
            return self._timed_out(data.get("on_timeout"))
        if status == "closed":
            return ENDED
        return None

    def _timed_out(self, on_timeout: str | None) -> str:
        """No answer in time: carry on with an assumption, or stop the run when the crew's rules say fail."""
        if (on_timeout or self._escalation.get("on_timeout")) != "fail":
            return PROCEED
        if self._halt is not None:
            minutes = self._escalation.get("timeout_minutes", 30)
            self._halt.set(f"No answer from a person within {minutes} minutes; the rules for asking say to stop.")
        return STOP
