"""run_crew.run with a chat turn spec goes through build_chat (in-process, no CrewAI objects or LLM)."""

import importlib
import os
from types import SimpleNamespace as NS

import pytest

import studio_runner.chat as chat_module

from .test_chat import chat_spec
from .test_run_crew_unit import FakeCrew, harness  # noqa: F401 - pytest fixture


@pytest.fixture
def rc():
    """run_crew imported as test_run_crew_unit does, but leaving its events descriptor open: that module's own
    fixture closes it, and closing it twice would fail."""
    saved = os.dup(1)
    try:
        module = importlib.import_module("run_crew")
    finally:
        os.dup2(saved, 1)
        os.close(saved)
    return module


def test_chat_spec_runs_through_build_chat(rc, harness, emitter, tmp_path, monkeypatch):  # noqa: F811
    seen = {}

    def fake_build_chat(spec, tools, state, workspace, halt, current_task, emit):
        seen.update(spec=spec, state=state, workspace=workspace, halt=halt, current_task=current_task, emit=emit)
        return NS(crew=FakeCrew(result=NS(raw="Hi Ada")), tasks=[], task_agents={})

    monkeypatch.setattr(chat_module, "build_chat", fake_build_chat)
    spec = chat_spec()
    req = {"spec": spec, "workspace": str(tmp_path), "state_api": {"url": "http://127.0.0.1:1/api/run-state", "token": "csr_x"}}
    assert rc.run(req, spec, emitter) == 0
    assert harness.built_with is None, "a chat turn must not build the crew's tasks"
    assert seen["spec"] is spec and seen["state"].base_url == "http://127.0.0.1:1/api/run-state"
    # Files sent with the message are read from the run's workspace
    assert seen["workspace"] == tmp_path.resolve()
    # Questions up the line (lead_desk.py) get the run's halt, the current task and the emitter
    assert seen["halt"] is not None and callable(seen["current_task"]) and callable(seen["emit"])
    (done,) = emitter.of_type("crew_completed")
    assert done["output"] == "Hi Ada"


def test_a_handoff_run_compacts_and_stops_at_its_deadline(rc, harness, emitter, tmp_path, monkeypatch):  # noqa: F811
    import studio_runner.listeners as listeners_module

    from .test_handoff import handoff_spec

    seen = {}
    monkeypatch.setattr(chat_module, "build_chat", lambda spec, tools, state, workspace, *_: NS(
        crew=FakeCrew(result=NS(raw="| vendor |")), tasks=[], task_agents={}))
    monkeypatch.setattr(listeners_module.Listeners, "install", lambda self: seen.update(listeners=self))
    spec = handoff_spec()
    assert rc.run({"spec": spec, "workspace": str(tmp_path)}, spec, emitter) == 0
    listeners = seen["listeners"]
    # Unlike a chat turn, a hand-off can build up a long context: compaction is on, and its deadline is enforced
    assert listeners.compactor is not None
    assert listeners._deadline is not None and listeners._deadline[1] == 20


def test_a_chat_turn_has_no_compactor_or_deadline(rc, harness, emitter, tmp_path, monkeypatch):  # noqa: F811
    import studio_runner.listeners as listeners_module

    seen = {}
    monkeypatch.setattr(chat_module, "build_chat", lambda spec, tools, state, workspace, *_: NS(
        crew=FakeCrew(result=NS(raw="Hi")), tasks=[], task_agents={}))
    monkeypatch.setattr(listeners_module.Listeners, "install", lambda self: seen.update(listeners=self))
    spec = chat_spec()
    assert rc.run({"spec": spec, "workspace": str(tmp_path)}, spec, emitter) == 0
    assert seen["listeners"].compactor is None and seen["listeners"]._deadline is None
