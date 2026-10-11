"""Crew Studio runner: runs one crew from a frozen spec and streams its events.

The Go service starts this script once per run. It writes one JSON object to stdin:

    {"spec": <RunSpec>, "grounded_searches_in_period": 12, "workspace": "/abs/run/workspace",
     "transcript_path": "/abs/private/transcript.jsonl",
     "state_api": {"url": "http://127.0.0.1:8088/api/run-state", "token": "csr_..."}}

state_api (optional) lets the notes and action log tools (and, in a chat turn, the proposal tools) reach Studio with
this run's token; crew memory (spec "memory", see studio_runner/memory.py) is stored through it too. A spec with a "chat" section is a chat turn: see studio_runner/chat.py. The token comes only on
stdin, never in the environment. A chat section with "handoff" is a hand-off from an agent chat (studio_runner/handoff.py):
it compacts like a crew run and stops at its deadline. In agent chat a specialist may ask the lead (studio_runner/lead_desk.py),
whose consultations are recorded as "consultation" events.

Events go out as JSON lines on stdout, and in full (never truncated) to the transcript file. Everything CrewAI prints is redirected to stderr,
which the Go service stores as "log" events. Exit code 0 means the crew completed.
"""

import json
import os
import sys
import traceback
from pathlib import Path
from typing import Any

# Capture the real stdout for events before anything prints, then point fd 1 at stderr.
_EVENTS_FD = os.dup(1)
os.dup2(2, 1)

from dotenv import load_dotenv  # noqa: E402

sys.path.insert(0, str(Path(__file__).resolve().parent))

from studio_runner.emitter import Emitter  # noqa: E402

PROVIDER_ENV_FILE_VAR = "STUDIO_PROVIDER_ENV_FILE"
EVENT_FLUSH_TIMEOUT_SECONDS = 30.0


def finished_task_outputs(tasks: list[Any], task_agents: dict[str, str]) -> list[dict[str, Any]]:
    outputs = []
    for position, task in enumerate(tasks):
        if task.output is None:
            continue
        outputs.append(
            {
                "position": position,
                "task_name": task.name or f"Task {position + 1}",
                "agent_role": task_agents.get(task.name or "", "") or str(getattr(task.output, "agent", "") or ""),
                "raw": task.output.raw or "",
            }
        )
    return outputs


def open_transcript(path: str | None) -> Any:
    if not path:
        return None
    try:
        return open(path, "a", encoding="utf-8")  # noqa: SIM115 - closed by Emitter.close
    except OSError as exc:
        print(f"[crew studio] could not open the transcript file: {exc}", file=sys.stderr)
        return None


def main() -> int:
    events = os.fdopen(_EVENTS_FD, "w", encoding="utf-8")
    try:
        request = json.load(sys.stdin)
        spec = request["spec"]
    except (ValueError, KeyError, TypeError) as exc:
        Emitter(events).emit("crew_failed", {"error": f"Could not read the run request: {exc}", "budget_stopped": False})
        return 2
    emitter = Emitter(events, open_transcript(request.get("transcript_path")))
    try:
        return run(request, spec, emitter)
    finally:
        emitter.close()


def run(request: dict[str, Any], spec: dict[str, Any], emitter: Emitter) -> int:
    env_file = os.environ.get(PROVIDER_ENV_FILE_VAR, "")
    if env_file:
        load_dotenv(env_file, override=False)

    # Imported after the fd swap and env load so CrewAI's console output goes to stderr
    from crewai.events import crewai_event_bus

    from studio_runner.build import SpecError, build_crew
    from studio_runner.chat import build_chat
    from studio_runner.compaction import Compactor
    from studio_runner.handoff import deadline_of
    from studio_runner.human import Halt
    from studio_runner.listeners import Listeners
    from studio_runner.meter import HARD_STOPPED, Meter
    from studio_runner.state_tools import StateClient
    from studio_runner.tools import ToolFactory

    def on_budget(state: str, spent: float) -> None:
        emitter.emit("budget", {"state": state, "spent_usd": round(spent, 6), "budget_usd": spec.get("budget_usd")})

    try:
        meter = Meter(
            prices=spec["pricing"],
            settings=spec["settings"],
            budget_usd=float(spec["budget_usd"]),
            grounded_searches_in_period=int(request.get("grounded_searches_in_period", 0)),
            on_budget=on_budget,
        )
        halt = Halt()
        listeners = Listeners(emitter, meter, halt)
        workspace = Path(request.get("workspace") or ".").resolve()
        state = StateClient.from_request(request)
        tools = ToolFactory(
            search_model=spec["settings"]["search_model"],
            searches_allowed=meter.searches_allowed,
            record_search=listeners.record_search,
            workspace=workspace,
            prices=spec["pricing"],
            reserve_video=meter.reserve_video,
            release_video=meter.release_video,
            record_video=listeners.record_video,
            state=state,
        )
        # A chat turn (spec has "chat"): one front desk agent answers one message with only the chat tools; a hand-off
        # (chat.handoff) is one agent's background work
        built = build_chat(spec, tools, state, workspace, halt, listeners.current_task, emitter.emit) if spec.get("chat") else build_crew(
            spec, tools, workspace, state=state, current_task=listeners.current_task, halt=halt)
        handoff = deadline_of(spec)
        listeners.set_deadline(handoff)
        if not spec.get("chat") or handoff is not None:
            # A chat turn is one short reply; a crew's agents, or a hand-off's, can build up long contexts worth compacting
            listeners.compactor = Compactor(state, int(spec["settings"].get("compact_context_tokens") or 0),
                                            emitter.emit, meter.llm_call_allowed, listeners.current_task)
    except (SpecError, KeyError, TypeError, ValueError, ImportError) as exc:
        emitter.emit("crew_failed", {"error": f"Could not build the crew: {exc}", "budget_stopped": False})
        return 2

    listeners.install()
    inputs = spec.get("inputs") or {}
    try:
        # CrewAI refuses an empty inputs dict when text contains braces, so pass None instead
        result = built.crew.kickoff(inputs=inputs or None)
    except Exception as exc:  # noqa: BLE001 - every failure must reach the Go service
        crewai_event_bus.flush(timeout=EVENT_FLUSH_TIMEOUT_SECONDS)
        traceback.print_exc()
        emitter.emit(
            "crew_failed",
            {
                "error": halt.reason if halt.is_set() else f"{type(exc).__name__}: {exc}",
                "budget_stopped": not meter.llm_call_allowed(),
                "usage": meter.usage(),
                "cost_usd": round(meter.spent(), 6),
                "spend": meter.summary(),
                "task_outputs": finished_task_outputs(built.tasks, built.task_agents),
            },
        )
        return 1

    crewai_event_bus.flush(timeout=EVENT_FLUSH_TIMEOUT_SECONDS)
    task_outputs = finished_task_outputs(built.tasks, built.task_agents)
    if halt.is_set():
        # An unanswered question stopped the run (the crew's rules say fail); what the agents wrote is kept
        emitter.emit("crew_failed", {"error": halt.reason, "budget_stopped": False, "usage": meter.usage(),
                                     "cost_usd": round(meter.spent(), 6), "spend": meter.summary(), "task_outputs": task_outputs})
        return 1
    emitter.emit(
        "crew_completed",
        {
            "output": result.raw or "",
            "usage": meter.usage(),
            "cost_usd": round(meter.spent(), 6),
            "spend": meter.summary(),
            "task_outputs": task_outputs,
            "budget_state": HARD_STOPPED if not meter.llm_call_allowed() else "",
        },
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
