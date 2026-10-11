"""In-process tests of run_crew.py (the e2e tests run it as a subprocess, which coverage does not see)."""

import importlib
import io
import json
import os
import sys
from pathlib import Path
from types import SimpleNamespace as NS

import pytest

from .conftest import make_spec


@pytest.fixture(scope="module")
def rc():
    # run_crew redirects fd 1 to stderr at import; restore it so pytest's output is not disturbed
    saved = os.dup(1)
    try:
        module = importlib.import_module("run_crew")
    finally:
        os.dup2(saved, 1)
        os.close(saved)
    yield module
    os.close(module._EVENTS_FD)


@pytest.fixture
def events_file(rc, monkeypatch, tmp_path):
    path = tmp_path / "events.jsonl"
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC)
    monkeypatch.setattr(rc, "_EVENTS_FD", fd)

    def read():
        return [json.loads(line) for line in path.read_text().splitlines()]

    return read


def stdin(monkeypatch, text):
    monkeypatch.setattr(sys, "stdin", io.StringIO(text))


class TestMainRequestErrors:
    @pytest.mark.parametrize("text, fragment", [
        ("not json", "Could not read the run request"),
        ("{}", "Could not read the run request"),
        ("[]", "Could not read the run request"),
        ("", "Could not read the run request"),
        ("null", "Could not read the run request"),
    ])
    def test_bad_input_exits_2_with_crew_failed(self, rc, events_file, monkeypatch, text, fragment):
        stdin(monkeypatch, text)
        assert rc.main() == 2
        (event,) = events_file()
        assert event["type"] == "crew_failed" and fragment in event["payload"]["error"]
        assert event["payload"]["budget_stopped"] is False


class TestMainDispatch:
    def test_main_runs_and_closes_emitter(self, rc, events_file, monkeypatch, tmp_path):
        seen = {}

        def fake_run(request, spec, emitter):
            seen.update(request=request, spec=spec, transcript=emitter._transcript)
            emitter.emit("hello", {})
            return 0

        monkeypatch.setattr(rc, "run", fake_run)
        transcript = tmp_path / "t.jsonl"
        stdin(monkeypatch, json.dumps({"spec": {"a": 1}, "transcript_path": str(transcript)}))
        assert rc.main() == 0
        assert seen["spec"] == {"a": 1} and seen["transcript"].closed
        assert json.loads(transcript.read_text())["type"] == "hello"

    def test_emitter_closed_even_if_run_raises(self, rc, events_file, monkeypatch, tmp_path):
        captured = {}

        def boom(request, spec, emitter):
            captured["t"] = emitter._transcript
            raise RuntimeError("x")

        monkeypatch.setattr(rc, "run", boom)
        stdin(monkeypatch, json.dumps({"spec": {}, "transcript_path": str(tmp_path / "t.jsonl")}))
        with pytest.raises(RuntimeError):
            rc.main()
        assert captured["t"].closed


class TestTranscript:
    def test_no_path(self, rc):
        assert rc.open_transcript(None) is None and rc.open_transcript("") is None

    def test_appends(self, rc, tmp_path):
        p = tmp_path / "t.jsonl"
        p.write_text("old\n")
        f = rc.open_transcript(str(p))
        f.write("new\n")
        f.close()
        assert p.read_text() == "old\nnew\n"

    def test_unopenable_path_warns_and_returns_none(self, rc, tmp_path, capsys):
        assert rc.open_transcript(str(tmp_path / "missing-dir" / "t.jsonl")) is None
        assert "could not open the transcript file" in capsys.readouterr().err


class TestFinishedTaskOutputs:
    def test_skips_unfinished_and_names_defaults(self, rc):
        tasks = [
            NS(name="A", output=NS(raw="a", agent="Fallback")),
            NS(name="B", output=None),
            NS(name=None, output=NS(raw=None, agent=None)),
            NS(name="D", output=NS(raw="d", agent="AgentObj")),
        ]
        out = rc.finished_task_outputs(tasks, {"A": "Role A"})
        assert out == [
            {"position": 0, "task_name": "A", "agent_role": "Role A", "raw": "a"},
            {"position": 2, "task_name": "Task 3", "agent_role": "", "raw": ""},
            {"position": 3, "task_name": "D", "agent_role": "AgentObj", "raw": "d"},
        ]

    def test_empty(self, rc):
        assert rc.finished_task_outputs([], {}) == []


class FakeCrew:
    def __init__(self, result=None, error=None, on_kickoff=None):
        self.result, self.error, self.on_kickoff, self.inputs = result, error, on_kickoff, "unset"

    def kickoff(self, inputs):
        self.inputs = inputs
        if self.on_kickoff:
            self.on_kickoff()
        if self.error:
            raise self.error
        return self.result


@pytest.fixture
def harness(rc, monkeypatch, emitter):
    """Patches everything run() imports so no CrewAI object or LLM is involved."""
    import studio_runner.build as build
    import studio_runner.listeners as listeners
    from crewai.events import crewai_event_bus

    state = NS(installed=0, flushed=[], crew=FakeCrew(result=NS(raw="final")), tasks=[], built_with=None, meters=[])

    def fake_build(spec, tools, workspace, **memory):
        state.built_with = (spec, tools, workspace, memory)
        return NS(crew=state.crew, tasks=state.tasks, task_agents={})

    monkeypatch.setattr(build, "build_crew", fake_build)
    monkeypatch.setattr(listeners.Listeners, "install", lambda self: setattr(state, "installed", state.installed + 1))
    monkeypatch.setattr(crewai_event_bus, "flush", lambda timeout=None: state.flushed.append(timeout))
    monkeypatch.delenv(rc.PROVIDER_ENV_FILE_VAR, raising=False)
    return state


class TestRun:
    def request(self, tmp_path, **extra):
        return {"spec": make_spec(), "workspace": str(tmp_path), **extra}

    def test_success_path(self, rc, harness, emitter, tmp_path):
        task = NS(name="Research", output=NS(raw="notes", agent="x"))
        harness.tasks = [task]
        req = self.request(tmp_path)
        assert rc.run(req, req["spec"], emitter) == 0
        (done,) = emitter.of_type("crew_completed")
        assert done["output"] == "final" and done["budget_state"] == ""
        assert done["task_outputs"][0]["raw"] == "notes"
        assert set(done) >= {"usage", "cost_usd", "spend"}
        assert harness.crew.inputs == {"topic": "AI"}
        assert harness.installed == 1 and harness.flushed == [rc.EVENT_FLUSH_TIMEOUT_SECONDS]
        assert harness.built_with[2] == tmp_path.resolve()

    def test_empty_inputs_pass_none(self, rc, harness, emitter, tmp_path):
        req = self.request(tmp_path)
        req["spec"]["inputs"] = {}
        rc.run(req, req["spec"], emitter)
        assert harness.crew.inputs is None
        req["spec"]["inputs"] = None
        rc.run(req, req["spec"], emitter)
        assert harness.crew.inputs is None

    def test_missing_workspace_defaults_to_cwd(self, rc, harness, emitter, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        req = {"spec": make_spec()}
        rc.run(req, req["spec"], emitter)
        assert harness.built_with[2] == tmp_path.resolve()

    def test_none_result_raw_becomes_empty(self, rc, harness, emitter, tmp_path):
        harness.crew = FakeCrew(result=NS(raw=None))
        req = self.request(tmp_path)
        rc.run(req, req["spec"], emitter)
        assert emitter.of_type("crew_completed")[0]["output"] == ""

    def test_budget_hard_stopped_flag_on_success(self, rc, harness, emitter, tmp_path):
        spec = make_spec(budget_usd=0.0000001)
        harness.crew = FakeCrew(result=NS(raw="x"), on_kickoff=None)
        import studio_runner.meter as meter_mod

        orig = meter_mod.Meter.llm_call_allowed
        meter_mod.Meter.llm_call_allowed = lambda self: False
        try:
            rc.run({"spec": spec, "workspace": str(tmp_path)}, spec, emitter)
        finally:
            meter_mod.Meter.llm_call_allowed = orig
        assert emitter.of_type("crew_completed")[0]["budget_state"] == "hard_stopped"

    def test_exception_emits_failure_with_partial_outputs(self, rc, harness, emitter, tmp_path, capsys):
        harness.crew = FakeCrew(error=ValueError("kaboom"))
        harness.tasks = [NS(name="Research", output=NS(raw="partial", agent=None)), NS(name="Write", output=None)]
        req = self.request(tmp_path)
        assert rc.run(req, req["spec"], emitter) == 1
        (failed,) = emitter.of_type("crew_failed")
        assert failed["error"] == "ValueError: kaboom" and failed["budget_stopped"] is False
        assert [o["task_name"] for o in failed["task_outputs"]] == ["Research"]
        assert harness.flushed, "pending events are flushed before reporting failure"
        assert "Traceback" in capsys.readouterr().err
        assert emitter.of_type("crew_completed") == []

    def test_exception_after_budget_stop_is_flagged(self, rc, harness, emitter, tmp_path, monkeypatch):
        import studio_runner.meter as meter_mod

        monkeypatch.setattr(meter_mod.Meter, "llm_call_allowed", lambda self: False)
        harness.crew = FakeCrew(error=RuntimeError("blocked"))
        req = self.request(tmp_path)
        assert rc.run(req, req["spec"], emitter) == 1
        assert emitter.of_type("crew_failed")[0]["budget_stopped"] is True

    def test_unanswered_question_with_fail_rule_fails_the_run(self, rc, harness, emitter, tmp_path):
        harness.tasks = [NS(name="Research", output=NS(raw="partial", agent=None))]
        harness.crew = FakeCrew(result=NS(raw="final"), on_kickoff=lambda: harness.built_with[3]["halt"].set("No answer in time; stop."))
        req = self.request(tmp_path)
        assert rc.run(req, req["spec"], emitter) == 1
        (failed,) = emitter.of_type("crew_failed")
        assert failed["error"] == "No answer in time; stop." and failed["budget_stopped"] is False
        assert [o["task_name"] for o in failed["task_outputs"]] == ["Research"]
        assert emitter.of_type("crew_completed") == []

    def test_halt_reason_replaces_the_blocked_call_error(self, rc, harness, emitter, tmp_path):
        def halt_then_fail() -> None:
            harness.built_with[3]["halt"].set("No answer in time; stop.")
            raise RuntimeError("LLM call blocked by before_llm_call hook")

        harness.crew = FakeCrew(on_kickoff=halt_then_fail)
        req = self.request(tmp_path)
        assert rc.run(req, req["spec"], emitter) == 1
        assert emitter.of_type("crew_failed")[0]["error"] == "No answer in time; stop."

    def test_keyboard_interrupt_is_not_swallowed(self, rc, harness, emitter, tmp_path):
        harness.crew = FakeCrew(error=KeyboardInterrupt())
        req = self.request(tmp_path)
        with pytest.raises(KeyboardInterrupt):
            rc.run(req, req["spec"], emitter)
        assert emitter.of_type("crew_failed") == []

    @pytest.mark.parametrize("break_spec, fragment", [
        (lambda s: s.pop("pricing"), "Could not build the crew"),
        (lambda s: s.__setitem__("budget_usd", "abc"), "Could not build the crew"),
        (lambda s: s["settings"].pop("search_model"), "Could not build the crew"),
    ])
    def test_spec_problems_exit_2(self, rc, harness, emitter, tmp_path, break_spec, fragment):
        spec = make_spec()
        break_spec(spec)
        assert rc.run({"spec": spec, "workspace": str(tmp_path)}, spec, emitter) == 2
        (failed,) = emitter.of_type("crew_failed")
        assert fragment in failed["error"] and failed["budget_stopped"] is False
        assert harness.installed == 0

    def test_spec_error_from_build_exits_2(self, rc, harness, emitter, tmp_path, monkeypatch):
        import studio_runner.build as build

        def bad(*a, **k):
            raise build.SpecError("Run spec has no tasks.")

        monkeypatch.setattr(build, "build_crew", bad)
        req = self.request(tmp_path)
        assert rc.run(req, req["spec"], emitter) == 2
        assert "Run spec has no tasks." in emitter.of_type("crew_failed")[0]["error"]

    def test_unexpected_build_error_propagates(self, rc, harness, emitter, tmp_path, monkeypatch):
        import studio_runner.build as build

        monkeypatch.setattr(build, "build_crew", lambda *a, **k: (_ for _ in ()).throw(RuntimeError("unexpected")))
        req = self.request(tmp_path)
        with pytest.raises(RuntimeError):
            rc.run(req, req["spec"], emitter)

    def test_provider_env_file_is_loaded_without_override(self, rc, harness, emitter, tmp_path, monkeypatch):
        env = tmp_path / "p.env"
        env.write_text("STUDIO_TEST_NEW=fromfile\nSTUDIO_TEST_KEEP=fromfile\n")
        monkeypatch.setenv(rc.PROVIDER_ENV_FILE_VAR, str(env))
        monkeypatch.setenv("STUDIO_TEST_KEEP", "fromenv")
        monkeypatch.delenv("STUDIO_TEST_NEW", raising=False)
        req = self.request(tmp_path)
        try:
            rc.run(req, req["spec"], emitter)
            assert os.environ["STUDIO_TEST_NEW"] == "fromfile"
            assert os.environ["STUDIO_TEST_KEEP"] == "fromenv"
        finally:
            os.environ.pop("STUDIO_TEST_NEW", None)

    def test_budget_callback_emits_budget_events(self, rc, harness, emitter, tmp_path, monkeypatch):
        import studio_runner.meter as meter_mod

        captured = {}
        orig_init = meter_mod.Meter.__init__

        def spy(self, *a, **k):
            orig_init(self, *a, **k)
            captured["m"] = self

        monkeypatch.setattr(meter_mod.Meter, "__init__", spy)
        harness.crew = FakeCrew(result=NS(raw="x"), on_kickoff=lambda: captured["m"].record_llm_call("gpt-6-luna", {"prompt_tokens": 20_000_000}))
        req = self.request(tmp_path)
        rc.run(req, req["spec"], emitter)
        states = [b["state"] for b in emitter.of_type("budget")]
        assert states[0] == "searches_stopped"
        assert emitter.of_type("budget")[0]["budget_usd"] == 1.0
        assert emitter.of_type("crew_completed")[0]["budget_state"] == "hard_stopped"


def test_signal_handling_is_absent_but_sigterm_terminates_runner(rc):
    """The runner installs no signal handlers: the Go service cancels a run by killing the process."""
    src = Path(rc.__file__).read_text()
    assert "signal" not in src
