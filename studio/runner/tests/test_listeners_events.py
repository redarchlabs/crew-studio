"""Each CrewAI event kind maps to the right emitted event, untruncated."""

from datetime import datetime, timezone
from types import SimpleNamespace as NS

import pytest

from studio_runner import listeners as lm
from studio_runner.listeners import Listeners, _agent_role, _args, _task_name
from studio_runner.meter import Meter

from .conftest import PRICES, SETTINGS

NOW = datetime(2026, 10, 3, tzinfo=timezone.utc)


@pytest.fixture
def L(emitter):
    meter = Meter(PRICES, SETTINGS, 1.0, 0, lambda *a: None)
    return Listeners(emitter, meter)


class TestHelpers:
    def test_task_name_precedence(self):
        assert _task_name(NS(task_name="A", task=NS(name="B"))) == "A"
        assert _task_name(NS(task_name="", task=NS(name="B"))) == "B"
        assert _task_name(NS(task=NS(name=None))) == ""
        assert _task_name(NS()) == ""

    def test_agent_role_precedence(self):
        assert _agent_role(NS(agent_role="R", agent=NS(role="X"))) == "R"
        assert _agent_role(NS(agent=NS(role="X"))) == "X"
        assert _agent_role(NS(from_agent=NS(role="F"))) == "F"
        assert _agent_role(NS(task=NS(agent=NS(role="T")))) == "T"
        assert _agent_role(NS()) == ""
        assert _agent_role(NS(agent=NS(role=None))) == ""

    def test_args(self):
        assert _args({"a": 1}) == {"a": 1}
        assert _args("raw") == {"input": "raw"}
        assert _args(None) == {} and _args("") == {}


def test_crew_started(L, emitter):
    L.on_crew_started(None, NS(crew_name="Crew", timestamp=NOW))
    L.on_crew_started(None, NS(timestamp=NOW))
    assert [p["crew_name"] for p in emitter.of_type("crew_started")] == ["Crew", ""]


def test_task_started_sets_current_task(L, emitter):
    L.on_task_started(None, NS(task_name="T", agent_role="R", timestamp=NOW))
    assert emitter.of_type("task_started") == [{"task_name": "T", "agent_role": "R"}]
    assert L.current_task("R") == "T" and L.current_task("other") == ""


def test_task_started_without_role_does_not_track(L, emitter):
    L.on_task_started(None, NS(task_name="T", timestamp=NOW))
    assert L.current_task("") == ""


def test_agent_started(L, emitter):
    L.on_agent_started(None, NS(agent=NS(role="R"), task=NS(name="T"), timestamp=NOW))
    assert emitter.of_type("agent_started") == [{"agent_role": "R", "task_name": "T"}]
    assert L.current_task("R") == "T"


def test_tool_started_plain_tool_has_no_delegation(L, emitter):
    L.on_tool_started(None, NS(agent_role="R", task_name="T", tool_name="Scrape", tool_args="http://x", timestamp=NOW))
    assert emitter.of_type("tool_started") == [{"agent_role": "R", "task_name": "T", "tool_name": "Scrape", "tool_args": {"input": "http://x"}}]
    assert emitter.of_type("delegation") == []


# CrewAI 1.15 reports its coworker tools by their function names (delegate_work_to_coworker); older versions by title
@pytest.mark.parametrize("name", ["Delegate work to coworker", "  ask question to coworker ", "delegate_work_to_coworker",
                                  "ask_question_to_coworker", "Ask_Question_To_Coworker"])
def test_both_delegation_tools(L, emitter, name):
    L.on_tool_started(None, NS(agent_role="M", task_name="T", tool_name=name,
                               tool_args={"coworker": "W", "question": "why?"}, timestamp=NOW))
    (d,) = emitter.of_type("delegation")
    assert d["to_agent"] == "W" and d["question"] == "why?" and d["from_agent"] == "M"
    assert len(emitter.of_type("tool_started")) == 1


def test_delegation_with_missing_args(L, emitter):
    L.on_tool_started(None, NS(agent_role="M", task_name="T", tool_name="delegate work to coworker", tool_args=None, timestamp=NOW))
    (d,) = emitter.of_type("delegation")
    assert d["to_agent"] == "" and d["question"] == ""


def test_tool_finished_not_truncated_by_listener_but_by_stream_cap(L, emitter):
    out = "o" * 10_000
    L.on_tool_finished(None, NS(agent_role="R", task_name="T", tool_name="X", output=out, from_cache=True, timestamp=NOW))
    (p,) = emitter.of_type("tool_finished")
    assert p["from_cache"] is True and p["output"].startswith("o" * 4000 + "\n")
    L.on_tool_finished(None, NS(agent_role="R", task_name="T", tool_name="X", output=None, timestamp=NOW))
    assert emitter.of_type("tool_finished")[1]["from_cache"] is False


def test_tool_error(L, emitter):
    L.on_tool_error(None, NS(agent_role="R", task_name="T", tool_name="X", error=ValueError("bad"), timestamp=NOW))
    assert emitter.of_type("tool_error")[0]["error"] == "bad"


def test_delegation_names_are_matched_whole():
    assert lm.is_delegation_tool("delegate_work_to_coworker") and lm.is_delegation_tool("Ask question to coworker")
    assert not lm.is_delegation_tool("delegate_work_to_coworker_now") and not lm.is_delegation_tool("")


class TestLLMCall:
    def event(self, **kw):
        base = dict(model="openai/gpt-6-luna", usage={"prompt_tokens": 100, "completion_tokens": 10}, messages=[{"role": "user", "content": "q"}],
                    response="r", call_id="c", agent_role="R", task_name="T", timestamp=NOW)
        base.update(kw)
        return NS(**base)

    def test_string_messages_become_user_message(self, L, emitter):
        L.on_llm_call(None, self.event(messages="plain prompt"))
        assert emitter.of_type("llm_call")[0]["messages"] == [{"role": "user", "content": "plain prompt"}]

    def test_none_fields_default(self, L, emitter):
        L.on_llm_call(None, self.event(model=None, usage=None, messages=None, response=None, call_id=None))
        p = emitter.of_type("llm_call")[0]
        assert p["model"] == "" and p["call_id"] == "" and p["response"] == "" and p["messages"] == []
        assert p["usage"] == {"prompt_tokens": 0, "completion_tokens": 0, "cached_prompt_tokens": 0, "cache_write_tokens": 0}
        assert p["cost_usd"] == 0

    def test_long_prompts_not_truncated_below_llm_limit(self, L, emitter):
        big = "m" * 100_000
        L.on_llm_call(None, self.event(messages=[{"role": "user", "content": big}], response=big))
        p = emitter.of_type("llm_call")[0]
        assert p["messages"][0]["content"] == big and p["response"] == big

    def test_a_call_without_its_agent_is_attributed_by_scope(self, L, emitter):
        # CrewAI's forced final answer after max_iter is an LLM call made without the agent; it shares its CrewAI scope
        # (parent event) with the agent's earlier calls
        L.on_agent_started(None, NS(agent=NS(role="Helper"), task=NS(name="Dig"), timestamp=NOW))
        L.on_llm_call(None, self.event(agent_role="Helper", task_name="Dig", parent_event_id="p1"))
        L.on_llm_call(None, self.event(agent_role="Lead", task_name="Reply", parent_event_id="p2"))
        L.on_llm_call(None, self.event(agent_role=None, task_name=None, parent_event_id="p1"))
        L.on_llm_call(None, self.event(agent_role=None, task_name=None, parent_event_id="unknown"))
        L.on_llm_call(None, self.event(agent_role=None, task_name=None))
        roles = [(p["agent_role"], p["task_name"]) for p in emitter.of_type("llm_call")]
        assert roles == [("Helper", "Dig"), ("Lead", "Reply"), ("Helper", "Dig"), ("", ""), ("", "")]

    def test_spend_accumulates(self, L, emitter):
        L.on_llm_call(None, self.event())
        L.on_llm_call(None, self.event())
        a, b = emitter.of_type("llm_call")
        assert b["spent_usd"] == pytest.approx(2 * a["cost_usd"])


def test_task_completed_variants(L, emitter):
    L.on_task_completed(None, NS(task_name="T", agent_role="R", output=None, timestamp=NOW))
    L.on_task_completed(None, NS(task_name="T", agent_role="R", output=NS(raw=None), timestamp=NOW))
    L.on_task_completed(None, NS(task_name="T", agent_role="R", output=NS(raw="full " * 100_000), timestamp=NOW))
    outs = [p["output"] for p in emitter.of_type("task_completed")]
    assert outs[0] == "" and outs[1] == "" and outs[2].startswith("full ")


def test_task_failed(L, emitter):
    L.on_task_failed(None, NS(task_name="T", agent_role="R", error=RuntimeError("x"), timestamp=NOW))
    assert emitter.of_type("task_failed") == [{"task_name": "T", "agent_role": "R", "error": "x"}]


def test_search_event_fields_and_grounded_flag(L, emitter):
    L.record_search("R", "q", "gemini-3.8-flash", 5, 6, 3)
    (s,) = emitter.of_type("search")
    assert s["grounded"] is True and s["billable_searches"] == 3 and s["cost_usd"] > 0 and s["spent_usd"] == s["cost_usd"]
    assert s["input_tokens"] == 5 and s["output_tokens"] == 6 and s["task_name"] == ""


def test_before_llm_call_allows_then_blocks(L):
    assert L.before_llm_call(None) is None
    L._meter.record_llm_call("gpt-6-luna", {"prompt_tokens": 100_000_000})
    assert L.before_llm_call(None) is False


def test_install_registers_every_handler(L, monkeypatch):
    registered, hooks = [], []

    class Bus:
        def on(self, event_type):
            def deco(fn):
                registered.append((event_type.__name__, fn.__name__))
                return fn
            return deco

    monkeypatch.setattr(lm, "crewai_event_bus", Bus())
    monkeypatch.setattr(lm, "register_before_llm_call_hook", hooks.append)
    L.install()
    assert dict(registered) == {
        "CrewKickoffStartedEvent": "on_crew_started", "TaskStartedEvent": "on_task_started",
        "AgentExecutionStartedEvent": "on_agent_started", "ToolUsageStartedEvent": "on_tool_started",
        "ToolUsageFinishedEvent": "on_tool_finished", "ToolUsageErrorEvent": "on_tool_error",
        "LLMCallCompletedEvent": "on_llm_call", "TaskCompletedEvent": "on_task_completed", "TaskFailedEvent": "on_task_failed",
    }
    assert hooks == [L.before_llm_call]
