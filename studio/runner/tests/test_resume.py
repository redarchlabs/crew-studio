"""Resuming a failed run: completed tasks keep their outputs and are not run again (see build.py, spec "resume")."""

from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace as NS

import pytest

from studio_runner.build import SpecError, build_crew, resume_start
from studio_runner.listeners import Listeners
from studio_runner.meter import Meter
from studio_runner.tools import ToolFactory

from .conftest import PRICES, SETTINGS, make_spec
from .test_run_crew_e2e import FakeOpenAI, fake_openai, run_runner  # noqa: F401 - fixture

NOW = datetime(2026, 10, 11, tzinfo=timezone.utc)


def resume_spec(**resume_overrides):
    spec = make_spec()
    spec["agents"][0]["tools"] = []
    spec["resume"] = {
        "from_run_id": "r-old",
        "start_position": 1,
        "outputs": [{"position": 0, "task_name": "Research", "agent_role": "Researcher of AI", "raw": "PRIOR NOTES on AI"}],
        **resume_overrides,
    }
    return spec


def tools(tmp_path: Path) -> ToolFactory:
    return ToolFactory(search_model="gemini", searches_allowed=lambda: True, record_search=lambda *a: 0.0,
                       workspace=tmp_path, prices=PRICES)


class TestResumeStart:
    def test_no_resume_starts_at_zero(self):
        assert resume_start(make_spec()) == 0

    def test_start_position_is_read(self):
        assert resume_start(resume_spec()) == 1

    @pytest.mark.parametrize("start", [-1, 2, 5, "1", None])
    def test_start_outside_the_tasks_is_refused(self, start):
        with pytest.raises(SpecError):
            resume_start(resume_spec(start_position=start))

    def test_every_task_before_the_start_needs_an_output(self):
        with pytest.raises(SpecError, match="task 1"):
            resume_start(resume_spec(outputs=[]))


def test_build_presets_completed_outputs(tmp_path: Path):
    built = build_crew(resume_spec(), tools(tmp_path), tmp_path)
    first, second = built.tasks
    assert first.output is not None and first.output.raw == "PRIOR NOTES on AI"
    assert first.output.agent == "Researcher of AI"
    assert second.output is None
    assert built.start == 1
    assert built.crew._resume_start == 1


def test_build_without_resume_starts_at_zero(tmp_path: Path):
    built = build_crew(make_spec(), tools(tmp_path), tmp_path)
    assert built.start == 0
    assert all(t.output is None for t in built.tasks)


@pytest.mark.parametrize("process", ["sequential", "hierarchical"])
def test_both_processes_execute_from_the_start_position(tmp_path: Path, monkeypatch, process):
    spec = resume_spec()
    spec["crew"]["process"] = process
    spec["crew"]["manager_llm_id"] = "l1"
    crew = build_crew(spec, tools(tmp_path), tmp_path).crew
    seen = {}
    monkeypatch.setattr(type(crew), "_create_manager_agent", lambda self: None)
    monkeypatch.setattr(type(crew), "_execute_tasks", lambda self, tasks, start_index=0, was_replayed=False: seen.update(start=start_index))
    if process == "sequential":
        crew._run_sequential_process()
    else:
        crew._run_hierarchical_process()
    assert seen["start"] == 1


class TestTaskCompletedPosition:
    @pytest.fixture
    def L(self, emitter):
        return Listeners(emitter, Meter(PRICES, SETTINGS, 1.0, 0, lambda *a: None))

    def test_position_of_each_task(self, L, emitter):
        a, b = NS(name="A"), NS(name="B")
        L.set_task_positions([a, b])
        L.on_task_completed(None, NS(task_name="B", agent_role="R", task=b, output=NS(raw="done"), timestamp=NOW))
        done = emitter.of_type("task_completed")[0]
        assert done["position"] == 1
        assert "cost_usd" not in done, "the server sums each task's cost from its LLM calls"

    def test_unknown_task_has_no_position(self, L, emitter):
        # A task of another crew, such as the one compaction runs, is not one of the run's tasks
        L.set_task_positions([NS(name="A")])
        L.on_task_completed(None, NS(task_name="X", agent_role="R", task=NS(name="X"), output=NS(raw="x"), timestamp=NOW))
        L.on_task_completed(None, NS(task_name="Y", agent_role="R", output=NS(raw="y"), timestamp=NOW))
        assert all("position" not in done for done in emitter.of_type("task_completed"))


def test_resumed_run_skips_completed_tasks(tmp_path: Path, fake_openai: str):  # noqa: F811 - fixture
    request = {"spec": resume_spec(), "grounded_searches_in_period": 0, "workspace": str(tmp_path)}
    FakeOpenAI.prompts.clear()
    code, events, stderr = run_runner(request, {"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": fake_openai}, tmp_path)
    assert code == 0, stderr[-3000:]

    sent = "\n".join(FakeOpenAI.prompts)
    assert "Research AI" not in sent, "the completed task must not run again"
    assert "PRIOR NOTES on AI" in sent, "its output is the next task's context"

    started = [e["payload"]["task_name"] for e in events if e["type"] == "task_started"]
    assert started == ["Write"]
    completed = [e["payload"] for e in events if e["type"] == "task_completed"]
    assert [(c["task_name"], c["position"]) for c in completed] == [("Write", 1)]

    final = events[-1]
    assert final["type"] == "crew_completed"
    # Reused outputs are already stored on the server; only new ones are reported
    assert [o["position"] for o in final["payload"]["task_outputs"]] == [1]
    assert "REPORT about AI" in final["payload"]["output"]


def test_resumed_hierarchical_run_skips_completed_tasks(tmp_path: Path, fake_openai: str):  # noqa: F811 - fixture
    spec = resume_spec()
    spec["crew"]["process"] = "hierarchical"
    spec["crew"]["manager_llm_id"] = "l1"
    request = {"spec": spec, "grounded_searches_in_period": 0, "workspace": str(tmp_path)}
    FakeOpenAI.prompts.clear()
    code, events, stderr = run_runner(request, {"OPENAI_API_KEY": "sk-test", "OPENAI_BASE_URL": fake_openai}, tmp_path)
    assert code == 0, stderr[-3000:]

    sent = "\n".join(FakeOpenAI.prompts)
    assert "Research AI" not in sent, "the manager must not be given the completed task again"
    assert "Write about AI" in sent and "PRIOR NOTES on AI" in sent
    completed = [e["payload"] for e in events if e["type"] == "task_completed"]
    assert [c.get("position") for c in completed] == [1]
    final = events[-1]
    assert final["type"] == "crew_completed"
    assert [o["position"] for o in final["payload"]["task_outputs"]] == [1]
