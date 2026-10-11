from datetime import datetime, timedelta, timezone
from pathlib import Path
from types import SimpleNamespace

import pytest

from studio_runner.build import SpecError, build_crew, check_spec, interpolate
from studio_runner.listeners import Listeners
from studio_runner.meter import Meter
from studio_runner.tools import ToolFactory

from .conftest import PRICES, SETTINGS, make_spec


def factory(tmp_path: Path) -> ToolFactory:
    return ToolFactory("gemini-2.5-flash", lambda: True, lambda *a: None, tmp_path)


def test_interpolate_leaves_unknown_placeholders():
    assert interpolate("{a} and {b}", {"a": "x"}) == "x and {b}"


@pytest.mark.parametrize(
    "spec, message",
    [
        ([], "JSON object"),
        ({"crew": {}}, "missing"),
        (make_spec(tasks=[]), "no tasks"),
        (make_spec(budget_usd=0), r"more than \$0"),
    ],
)
def test_check_spec_rejects_bad_specs(spec, message):
    with pytest.raises(SpecError, match=message):
        check_spec(spec)


def test_build_crew_wires_agents_tasks_and_context(tmp_path: Path):
    built = build_crew(make_spec(), factory(tmp_path))
    research, write = built.tasks
    assert [t.name for t in built.tasks] == ["Research", "Write"]
    assert write.context == [research]
    assert research.agent.tools[0].name == "Read Workspace File"
    assert research.agent.llm.model == "gpt-6-luna"
    assert built.task_agents == {"Research": "Researcher of AI", "Write": "Writer"}
    assert len(built.crew.agents) == 2


def test_build_crew_hierarchical_sets_manager(tmp_path: Path):
    spec = make_spec(crew={"id": "c", "name": "H", "process": "hierarchical", "manager_llm_id": "l1"})
    built = build_crew(spec, factory(tmp_path))
    assert built.crew.manager_llm is not None


def make_listeners(emitter, budget=1.0):
    meter = Meter(PRICES, SETTINGS, budget, 0, lambda state, spent: emitter.emit("budget", {"state": state}))
    return Listeners(emitter, meter), meter


NOW = datetime(2026, 10, 3, tzinfo=timezone.utc)


def test_llm_call_event_carries_cost_and_messages(emitter):
    listeners, meter = make_listeners(emitter)
    event = SimpleNamespace(
        model="openai/gpt-6-luna", usage={"prompt_tokens": 1000, "completion_tokens": 100}, messages=[{"role": "user", "content": "hi"}],
        response="hello", call_id="c1", agent_role="Writer", task_name="Write", timestamp=NOW,
    )
    listeners.on_llm_call(None, event)
    (payload,) = emitter.of_type("llm_call")
    assert payload["agent_role"] == "Writer" and payload["response"] == "hello"
    assert payload["cost_usd"] == pytest.approx((1000 * 0.10 + 100 * 0.50) / 1e6)
    assert payload["messages"] == [{"role": "user", "content": "hi"}]
    assert emitter.events()[0]["at"].startswith("2026-10-03")
    assert meter.usage()["prompt_tokens"] == 1000


def test_delegation_tool_emits_delegation(emitter):
    listeners, _ = make_listeners(emitter)
    event = SimpleNamespace(
        agent_role="Manager", task_name="Plan", tool_name="Delegate work to coworker",
        tool_args={"coworker": "Researcher", "task": "dig"}, timestamp=NOW,
    )
    listeners.on_tool_started(None, event)
    (delegation,) = emitter.of_type("delegation")
    assert delegation == {"from_agent": "Manager", "to_agent": "Researcher", "task_name": "Plan",
                          "tool_name": "Delegate work to coworker", "question": "dig"}


def test_search_is_attributed_to_the_agents_current_task(emitter):
    listeners, _ = make_listeners(emitter)
    listeners.on_task_started(None, SimpleNamespace(task_name="Research", agent_role="Analyst", timestamp=NOW))
    listeners.record_search("Analyst", "q", "gemini-3.8-flash", 100, 100, billable_searches=0)
    (search,) = emitter.of_type("search")
    assert search["task_name"] == "Research" and search["tool_name"] == "Google Search"


def test_task_and_tool_events(emitter):
    listeners, _ = make_listeners(emitter)
    listeners.on_tool_finished(None, SimpleNamespace(agent_role="A", task_name="T", tool_name="Scrape", output="x" * 5000, timestamp=NOW))
    listeners.on_tool_error(None, SimpleNamespace(agent_role="A", task_name="T", tool_name="Scrape", error="boom", timestamp=NOW))
    listeners.on_task_completed(None, SimpleNamespace(task_name="T", agent_role="A", output=SimpleNamespace(raw="done"), timestamp=NOW))
    listeners.on_task_failed(None, SimpleNamespace(task_name="T", agent_role="A", error="bad", timestamp=NOW))
    assert "truncated" in emitter.of_type("tool_finished")[0]["output"]
    assert emitter.of_type("tool_error")[0]["error"] == "boom"
    assert emitter.of_type("task_completed")[0]["output"] == "done"
    assert emitter.of_type("task_failed")[0]["error"] == "bad"


def test_hard_cap_blocks_llm_calls(emitter):
    listeners, meter = make_listeners(emitter, budget=0.000001)
    assert listeners.before_llm_call(None) is None
    meter.record_llm_call("gpt-6-luna", {"prompt_tokens": 1000})
    assert listeners.before_llm_call(None) is False
    assert [p["state"] for p in emitter.of_type("budget")] == ["searches_stopped", "hard_stopped"]


def test_a_hand_off_past_its_deadline_stops_llm_calls(emitter):
    from studio_runner.human import Halt

    halt = Halt()
    meter = Meter(PRICES, SETTINGS, 1.0, 0, lambda *_a: None)
    listeners = Listeners(emitter, meter, halt)
    now = [NOW]
    listeners.set_deadline((NOW + timedelta(minutes=5), 20), clock=lambda: now[0])
    assert listeners.before_llm_call(None) is None and not halt.is_set()
    now[0] = NOW + timedelta(minutes=6)
    assert listeners.before_llm_call(None) is False
    assert halt.reason == "The hand-off ran past its 20-minute limit and was stopped."
    # Without a halt to record it in, the call is still refused
    bare = Listeners(emitter, meter)
    bare.set_deadline((NOW - timedelta(minutes=1), 20), clock=lambda: NOW)
    assert bare.before_llm_call(None) is False
