"""Forwards CrewAI events to the Go service and enforces the run budget on every LLM call."""

import threading
from datetime import datetime, timezone
from typing import Any, Callable

from crewai.events import (
    AgentExecutionStartedEvent,
    CrewKickoffStartedEvent,
    LLMCallCompletedEvent,
    TaskCompletedEvent,
    TaskFailedEvent,
    TaskStartedEvent,
    ToolUsageErrorEvent,
    ToolUsageFinishedEvent,
    ToolUsageStartedEvent,
    crewai_event_bus,
)
from crewai.hooks import LLMCallHookContext, register_before_llm_call_hook

from .emitter import Emitter, to_jsonable
from .meter import Meter, normalize_usage

# CrewAI's built-in delegation tools; calling one means one agent handed work to another. CrewAI 1.15 reports them by
# their function names (delegate_work_to_coworker), older versions by title, so names are compared normalised.
DELEGATION_TOOLS = ("delegate work to coworker", "ask question to coworker")


def is_delegation_tool(name: str) -> bool:
    """Whether a tool name is one of CrewAI's coworker tools: underscores read as spaces, case folded."""
    return " ".join(str(name).replace("_", " ").split()).casefold() in DELEGATION_TOOLS


def _task_name(event: Any) -> str:
    if getattr(event, "task_name", None):
        return event.task_name
    task = getattr(event, "task", None)
    return getattr(task, "name", None) or ""


def _agent_role(event: Any) -> str:
    if getattr(event, "agent_role", None):
        return event.agent_role
    agent = getattr(event, "agent", None) or getattr(event, "from_agent", None)
    if agent is None:
        # Task events carry the task, whose agent did the work
        agent = getattr(getattr(event, "task", None), "agent", None)
    return getattr(agent, "role", None) or ""


def _args(raw: Any) -> dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    return {"input": str(raw)} if raw else {}


class Listeners:
    def __init__(self, emitter: Emitter, meter: Meter, halt: Any = None):
        self._emitter = emitter
        self._meter = meter
        # Compacts an agent's context before its LLM call when asked or over the limit (see compaction.py)
        self.compactor: Any = None
        # Set when an unanswered question stops the run (see human.py); it blocks LLM calls like the hard cap
        self._halt = halt
        self._lock = threading.Lock()
        # agent role -> task it is working on, so searches can be attributed to a task
        self._current_task: dict[str, str] = {}
        # CrewAI scope (an LLM call's parent event) -> the agent whose calls ran in it. CrewAI's forced final answer
        # after max_iter is an LLM call made without its agent; it runs in the same scope as the agent's other calls.
        self._scope_roles: dict[str, str] = {}
        # A hand-off's deadline and time limit in minutes (see handoff.py); no LLM call is made after it
        self._deadline: tuple[datetime, int] | None = None
        self._clock: Callable[[], datetime] = lambda: datetime.now(timezone.utc)
        # The crew's tasks by identity -> position, so a finished task reports where it sits in the crew (the server
        # saves its output straight away, for resuming a failed run). What a task cost is summed by the server from the
        # task's LLM calls, searches and videos, which do not depend on when this thread pool runs a handler.
        # id(task) -> (task, position); the task is kept so a reused id of another object is never mistaken for it
        self._positions: dict[int, tuple[Any, int]] = {}

    def set_task_positions(self, tasks: list[Any]) -> None:
        self._positions = {id(task): (task, position) for position, task in enumerate(tasks)}

    def _position(self, event: Any) -> int | None:
        task = getattr(event, "task", None)
        known = None if task is None else self._positions.get(id(task))
        return known[1] if known is not None and known[0] is task else None

    def set_deadline(self, deadline: tuple[datetime, int] | None, clock: Callable[[], datetime] | None = None) -> None:
        """Stops the run's LLM calls once a hand-off passes its deadline; the halt (when there is one) says why."""
        self._deadline = deadline
        if clock is not None:
            self._clock = clock

    def _past_deadline(self) -> bool:
        if self._deadline is None or self._clock() <= self._deadline[0]:
            return False
        if self._halt is not None and not self._halt.is_set():
            self._halt.set(f"The hand-off ran past its {self._deadline[1]}-minute limit and was stopped.")
        return True

    def current_task(self, agent_role: str) -> str:
        with self._lock:
            return self._current_task.get(agent_role, "")

    def _set_task(self, agent_role: str, task_name: str) -> None:
        if agent_role:
            with self._lock:
                self._current_task[agent_role] = task_name

    def install(self) -> None:
        bus = crewai_event_bus
        bus.on(CrewKickoffStartedEvent)(self.on_crew_started)
        bus.on(TaskStartedEvent)(self.on_task_started)
        bus.on(AgentExecutionStartedEvent)(self.on_agent_started)
        bus.on(ToolUsageStartedEvent)(self.on_tool_started)
        bus.on(ToolUsageFinishedEvent)(self.on_tool_finished)
        bus.on(ToolUsageErrorEvent)(self.on_tool_error)
        bus.on(LLMCallCompletedEvent)(self.on_llm_call)
        bus.on(TaskCompletedEvent)(self.on_task_completed)
        bus.on(TaskFailedEvent)(self.on_task_failed)
        register_before_llm_call_hook(self.before_llm_call)

    def before_llm_call(self, context: LLMCallHookContext) -> bool | None:
        # Returning False tells CrewAI to block the call
        if self._halt is not None and self._halt.is_set():
            return False
        if self._past_deadline():
            return False
        if not self._meter.llm_call_allowed():
            return False
        if self.compactor is not None:
            self.compactor.before_llm_call(context)
        return None

    def on_crew_started(self, _source: Any, event: CrewKickoffStartedEvent) -> None:
        self._emitter.emit("crew_started", {"crew_name": getattr(event, "crew_name", "")}, event.timestamp)

    def on_task_started(self, _source: Any, event: TaskStartedEvent) -> None:
        task, role = _task_name(event), _agent_role(event)
        self._set_task(role, task)
        self._emitter.emit("task_started", {"task_name": task, "agent_role": role}, event.timestamp)

    def on_agent_started(self, _source: Any, event: AgentExecutionStartedEvent) -> None:
        task, role = _task_name(event), _agent_role(event)
        self._set_task(role, task)
        self._emitter.emit("agent_started", {"agent_role": role, "task_name": task}, event.timestamp)

    def on_tool_started(self, _source: Any, event: ToolUsageStartedEvent) -> None:
        role, task = _agent_role(event), _task_name(event)
        args = _args(event.tool_args)
        self._emitter.emit(
            "tool_started",
            {"agent_role": role, "task_name": task, "tool_name": event.tool_name, "tool_args": args},
            event.timestamp,
        )
        if is_delegation_tool(event.tool_name):
            self._emitter.emit(
                "delegation",
                {
                    "from_agent": role,
                    "to_agent": str(args.get("coworker", "")),
                    "task_name": task,
                    "tool_name": event.tool_name,
                    "question": str(args.get("task") or args.get("question") or ""),
                },
                event.timestamp,
            )

    def on_tool_finished(self, _source: Any, event: ToolUsageFinishedEvent) -> None:
        self._emitter.emit(
            "tool_finished",
            {
                "agent_role": _agent_role(event),
                "task_name": _task_name(event),
                "tool_name": event.tool_name,
                "output": str(event.output),
                "from_cache": bool(getattr(event, "from_cache", False)),
            },
            event.timestamp,
        )

    def on_tool_error(self, _source: Any, event: ToolUsageErrorEvent) -> None:
        self._emitter.emit(
            "tool_error",
            {"agent_role": _agent_role(event), "task_name": _task_name(event), "tool_name": event.tool_name, "error": str(event.error)},
            event.timestamp,
        )

    def _llm_call_agent(self, event: Any) -> tuple[str, str]:
        """The agent and task of an LLM call; a call CrewAI made without its agent gets those of its scope."""
        role, task = _agent_role(event), _task_name(event)
        scope = getattr(event, "parent_event_id", None)
        if not scope:
            return role, task
        with self._lock:
            if role:
                self._scope_roles[scope] = role
                return role, task
            role = self._scope_roles.get(scope, "")
        return role, task or (self.current_task(role) if role else "")

    def on_llm_call(self, _source: Any, event: LLMCallCompletedEvent) -> None:
        role, task = self._llm_call_agent(event)
        model = str(event.model or "")
        usage = to_jsonable(event.usage or {})
        cost = self._meter.record_llm_call(model, usage)
        messages = to_jsonable(event.messages)
        if isinstance(messages, str):
            messages = [{"role": "user", "content": messages}]
        self._emitter.emit(
            "llm_call",
            {
                "agent_role": role,
                "task_name": task,
                "model": model,
                "call_id": str(event.call_id or ""),
                "usage": normalize_usage(usage),
                "cost_usd": round(cost, 8),
                "spent_usd": round(self._meter.spent(), 8),
                "messages": messages or [],
                "response": str(event.response or ""),
            },
            event.timestamp,
        )

    def record_search(self, agent_role: str, query: str, model: str, input_tokens: int, output_tokens: int, billable_searches: int) -> None:
        cost = self._meter.record_search(input_tokens, output_tokens, billable_searches)
        self._emitter.emit(
            "search",
            {
                "agent_role": agent_role,
                "task_name": self.current_task(agent_role),
                "tool_name": "Google Search",
                "query": query,
                "model": model,
                "grounded": billable_searches > 0,
                "billable_searches": billable_searches,
                "input_tokens": input_tokens,
                "output_tokens": output_tokens,
                "cost_usd": round(cost, 8),
                "spent_usd": round(self._meter.spent(), 8),
            },
        )

    def record_video(self, agent_role: str, details: dict[str, Any]) -> float:
        cost = self._meter.record_video(
            details["model"], details["input_tokens"], details["output_tokens"], details.get("reserved_usd", 0.0)
        )
        self._emitter.emit(
            "video",
            {
                **details,
                "agent_role": agent_role,
                "task_name": self.current_task(agent_role),
                "tool_name": "Generate Video",
                "cost_usd": round(cost, 8),
                "spent_usd": round(self._meter.spent(), 8),
            },
        )
        return cost

    def on_task_completed(self, _source: Any, event: TaskCompletedEvent) -> None:
        output = getattr(event.output, "raw", None) if event.output is not None else None
        payload: dict[str, Any] = {"task_name": _task_name(event), "agent_role": _agent_role(event), "output": output or ""}
        position = self._position(event)
        if position is not None:
            payload["position"] = position
        self._emitter.emit("task_completed", payload, event.timestamp)

    def on_task_failed(self, _source: Any, event: TaskFailedEvent) -> None:
        self._emitter.emit(
            "task_failed",
            {"task_name": _task_name(event), "agent_role": _agent_role(event), "error": str(event.error)},
            event.timestamp,
        )
