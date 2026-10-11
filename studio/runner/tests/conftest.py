import copy
import io
import json
import os
import sys
from pathlib import Path
from typing import Any

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

# As the Go service runs the runner (runner/execute.go): CrewAI's telemetry off. Its tracing otherwise waits 5 s for
# a parent span on every memory save made outside a crew run.
for _name, _value in (("CREWAI_DISABLE_TELEMETRY", "true"), ("CREWAI_TRACING_ENABLED", "false"), ("OTEL_SDK_DISABLED", "true")):
    os.environ.setdefault(_name, _value)

from studio_runner.emitter import Emitter  # noqa: E402

PRICES = [
    {"model": "gpt-6-luna", "input_per_mtok": 0.10, "cached_input_per_mtok": 0.01, "output_per_mtok": 0.50},
    {"model": "gemini-3.8-flash", "input_per_mtok": 0.75, "cached_input_per_mtok": 0.075, "output_per_mtok": 3.75},
]
SETTINGS = {
    "default_budget_usd": 2.0,
    "hard_stop_multiplier": 1.25,
    "search_model": "gemini-3.8-flash",
    "grounded_search_cost_per_1000": 14.0,
    "grounded_searches_free": 2,
    "grounding_free_period": "month",
    "max_concurrent_runs": 2,
}


class CapturingEmitter(Emitter):
    def __init__(self) -> None:
        self.buffer = io.StringIO()
        super().__init__(self.buffer)

    def events(self) -> list[dict[str, Any]]:
        return [json.loads(line) for line in self.buffer.getvalue().splitlines()]

    def of_type(self, event_type: str) -> list[dict[str, Any]]:
        return [e["payload"] for e in self.events() if e["type"] == event_type]


@pytest.fixture
def emitter() -> CapturingEmitter:
    return CapturingEmitter()


def make_spec(**overrides: Any) -> dict[str, Any]:
    spec = {
        "crew": {"id": "c1", "name": "Test Crew", "process": "sequential", "manager_llm_id": None},
        "llms": [{"id": "l1", "name": "Luna", "model": "openai/gpt-6-luna", "temperature": None, "max_completion_tokens": 512}],
        "agents": [
            {"id": "a1", "name": "R", "role": "Researcher of {topic}", "goal": "Find {topic}", "backstory": "b",
             "llm_id": "l1", "tools": ["workspace_read"], "max_iter": 3, "allow_delegation": False, "verbose": False},
            {"id": "a2", "name": "W", "role": "Writer", "goal": "Write", "backstory": "b",
             "llm_id": "l1", "tools": [], "max_iter": 3, "allow_delegation": False, "verbose": False},
        ],
        "tasks": [
            {"id": "t1", "name": "Research", "description": "Research {topic}", "expected_output": "notes",
             "agent_id": "a1", "context_task_ids": [], "markdown": False},
            {"id": "t2", "name": "Write", "description": "Write about {topic}", "expected_output": "report",
             "agent_id": "a2", "context_task_ids": ["t1"], "markdown": True},
        ],
        "pricing": copy.deepcopy(PRICES),
        "settings": copy.deepcopy(SETTINGS),
        "inputs": {"topic": "AI"},
        "budget_usd": 1.0,
    }
    spec.update(overrides)
    return spec
