"""Turns a frozen Crew Studio RunSpec (JSON) into CrewAI objects."""

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

from crewai import LLM, Agent, Crew, Process, Task
from crewai.tasks.task_output import TaskOutput
from pydantic import PrivateAttr

from .human import AskHumanTool, Halt, escalation_note
from .llm_compat import build_anthropic_llm, is_anthropic
from .memory import MemoryRememberTool, build_memory, explicit_mode, hide_crewai_save_tool
from .tools import ToolFactory
from .watch import run_inputs, task_additions
from .workorder import WorkOrderContext, build_context

# Same pattern CrewAI uses for {placeholders}
PLACEHOLDER = re.compile(r"\{([A-Za-z_][A-Za-z0-9_\-]*)}")

REQUIRED_SPEC_KEYS = ("crew", "agents", "tasks", "llms", "pricing", "settings", "inputs", "budget_usd")


class SpecError(ValueError):
    """The spec from the Go service is malformed."""


def check_spec(spec: Any) -> None:
    if not isinstance(spec, dict):
        raise SpecError("Run spec must be a JSON object.")
    missing = [key for key in REQUIRED_SPEC_KEYS if key not in spec]
    if missing:
        raise SpecError(f"Run spec is missing: {', '.join(missing)}.")
    if not spec["tasks"]:
        raise SpecError("Run spec has no tasks.")
    if float(spec["budget_usd"]) <= 0:
        raise SpecError("Run budget must be more than $0.")


def resume_start(spec: dict[str, Any]) -> int:
    """The position of the first task a resumed run executes (0 when it is not a resume). Every task before it must
    have the output the earlier run stored, so it is not run (or paid for) again."""
    resume = spec.get("resume")
    if not resume:
        return 0
    start = resume.get("start_position")
    if not isinstance(start, int) or isinstance(start, bool) or not 0 <= start < len(spec["tasks"]):
        raise SpecError("Resume must start at one of the run's tasks.")
    have = {o.get("position") for o in resume.get("outputs") or []}
    missing = [str(position + 1) for position in range(start) if position not in have]
    if missing:
        raise SpecError(f"Resume has no stored output for task {', '.join(missing)}.")
    return start


def interpolate(text: str, inputs: dict[str, str]) -> str:
    return PLACEHOLDER.sub(lambda m: inputs.get(m.group(1), m.group(0)), text)


def build_llm(config: dict[str, Any]) -> Any:
    llm = _build_llm(config)
    if config.get("vision"):
        # CrewAI's built-in list of vision models predates GPT-6 and Claude 5; trust the LLM setting instead
        object.__setattr__(llm, "supports_multimodal", lambda: True)
    return llm


def _build_llm(config: dict[str, Any]) -> Any:
    if is_anthropic(config["model"]):
        return build_anthropic_llm(config)
    kwargs: dict[str, Any] = {"model": config["model"]}
    # Some models only accept their default temperature, so it is only sent when set
    if config.get("temperature") is not None:
        kwargs["temperature"] = config["temperature"]
    if config.get("max_completion_tokens") is not None:
        kwargs["max_completion_tokens"] = config["max_completion_tokens"]
    if config.get("reasoning_effort"):
        kwargs["reasoning_effort"] = config["reasoning_effort"]
    if config.get("api") == "responses":
        # Reasoning models such as gpt-6.1-sol only support tool calling through the Responses API
        kwargs["api"] = "responses"
    return LLM(**kwargs)


class StudioCrew(Crew):
    """CrewAI's Crew without CrewAI's own "Save to memory" tool when memory is Studio's (see memory.py). A resumed run
    starts at _resume_start: CrewAI skips the tasks before it and passes their preset outputs on as context."""

    _resume_start: int = PrivateAttr(default=0)

    def _run_sequential_process(self) -> Any:
        return self._execute_tasks(self.tasks, self._resume_start)

    def _run_hierarchical_process(self) -> Any:
        self._create_manager_agent()
        return self._execute_tasks(self.tasks, self._resume_start)

    def _add_memory_tools(self, tools: list[Any], memory: Any) -> list[Any]:
        return hide_crewai_save_tool(super()._add_memory_tools(tools, memory), memory)

    def copy(self) -> "StudioCrew":  # type: ignore[override]
        # Crew.copy always builds a plain Crew; the copy has no fields of its own to add, so only its class changes
        clone = super().copy()
        object.__setattr__(clone, "__class__", type(self))
        return clone


@dataclass(frozen=True)
class BuiltCrew:
    crew: Crew
    tasks: list[Task]
    # Task name -> resolved agent role, for attributing task outputs
    task_agents: dict[str, str]
    # Position of the first task this run executes; earlier ones were completed by the run it resumes
    start: int = 0


def _model_files(context: WorkOrderContext) -> dict[str, Any]:
    if not context.model_files:
        return {}
    from crewai_files import File  # optional dependency, only needed when a work order has images or PDFs

    return {name: File(source=str(path)) for name, path in context.model_files.items()}


def build_crew(spec: dict[str, Any], tools: ToolFactory, workspace: Path | None = None, state: Any = None,
               current_task: Callable[[str], str] | None = None, halt: Halt | None = None) -> BuiltCrew:
    """Builds the crew. state (the run-state client) and current_task (Listeners.current_task) are for memory and
    asking a person; halt is set when an unanswered question stops the run."""
    check_spec(spec)
    start = resume_start(spec)
    work_order = build_context(spec.get("work_order"), workspace or Path("."))
    input_files = _model_files(work_order)
    inputs: dict[str, str] = spec["inputs"] or {}
    llms = {cfg["id"]: build_llm(cfg) for cfg in spec["llms"]}
    vision_llms = {cfg["id"] for cfg in spec["llms"] if cfg.get("vision")}
    agent_sees_files: dict[str, bool] = {}
    # Explicit memory: every agent may save lessons with the Remember tool (no LLM call)
    remember = explicit_mode(spec) and state is not None
    # Escalation on: every agent gets the crew's rules for asking a person and the tool to ask with
    escalation = spec.get("escalation") if state is not None else None

    agents: dict[str, Agent] = {}
    roles: dict[str, str] = {}
    for cfg in spec["agents"]:
        role = interpolate(cfg["role"], inputs)
        roles[cfg["id"]] = role
        kwargs: dict[str, Any] = {
            "role": cfg["role"],
            "goal": cfg["goal"],
            "backstory": cfg["backstory"] + (escalation_note(escalation) if escalation else ""),
            "tools": [tools.build(key, role) for key in cfg.get("tools", [])]
            + ([MemoryRememberTool().configure(state, role, current_task)] if remember else [])
            + ([AskHumanTool().configure(state, role, escalation, halt or Halt(), current_task)] if escalation else []),
            "max_iter": cfg["max_iter"],
            "allow_delegation": cfg["allow_delegation"],
            "verbose": cfg["verbose"],
        }
        if cfg.get("llm_id") in llms:
            kwargs["llm"] = llms[cfg["llm_id"]]
        agent_sees_files[cfg["id"]] = cfg.get("llm_id") in vision_llms
        agents[cfg["id"]] = Agent(**kwargs)

    tasks: dict[str, Task] = {}
    ordered: list[Task] = []
    task_agents: dict[str, str] = {}
    # A watched schedule's changes, follow-ups and dry-run note (see watch.py)
    watch_extra = task_additions([[t["description"], t["expected_output"]] for t in spec["tasks"]], run_inputs(inputs))
    for position, cfg in enumerate(spec["tasks"]):
        agent_id = cfg.get("agent_id")
        sees_files = agent_sees_files.get(agent_id, False)
        kwargs = {
            "name": cfg["name"],
            "description": cfg["description"] + watch_extra[position]
            + (work_order.section if sees_files else work_order.section_without_files),
            "expected_output": cfg["expected_output"],
            "markdown": cfg.get("markdown", False),
        }
        if input_files and sees_files:
            kwargs["input_files"] = dict(input_files)
        if agent_id in agents:
            kwargs["agent"] = agents[agent_id]
            task_agents[cfg["name"]] = roles[agent_id]
        # Without an explicit context CrewAI passes earlier task outputs along, so only set it when chosen
        context = [tasks[ctx_id] for ctx_id in cfg.get("context_task_ids", []) if ctx_id in tasks]
        if context:
            kwargs["context"] = context
        task = Task(**kwargs)
        if position < start:
            task.output = _stored_output(spec, position, task, task_agents.get(cfg["name"], ""))
        tasks[cfg["id"]] = task
        ordered.append(task)

    crew_cfg = spec["crew"]
    crew_kwargs: dict[str, Any] = {
        "name": crew_cfg["name"],
        "agents": list(agents.values()),
        "tasks": ordered,
        "process": Process.hierarchical if crew_cfg["process"] == "hierarchical" else Process.sequential,
        "verbose": True,
    }
    if crew_cfg["process"] == "hierarchical" and crew_cfg.get("manager_llm_id") in llms:
        crew_kwargs["manager_llm"] = llms[crew_cfg["manager_llm_id"]]
    first_llm = next((llms[cfg["llm_id"]] for cfg in spec["agents"] if cfg.get("llm_id") in llms), None)
    memory = build_memory(spec, state, llms, first_llm, current_task)
    if memory is not None:
        crew_kwargs["memory"] = memory
    crew = StudioCrew(**crew_kwargs)
    crew._resume_start = start
    return BuiltCrew(crew=crew, tasks=ordered, task_agents=task_agents, start=start)


def _stored_output(spec: dict[str, Any], position: int, task: Task, role: str) -> TaskOutput:
    stored = next(o for o in spec["resume"]["outputs"] if o.get("position") == position)
    return TaskOutput(description=task.description, name=task.name, expected_output=task.expected_output,
                      raw=stored.get("raw") or "", agent=stored.get("agent_role") or role)
