"""The listeners against a real CrewAI run (in this process, against a fake OpenAI-compatible server): what CrewAI
1.15 actually reports for hand-offs and for the forced final answer after max_iter."""

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

import pytest
from crewai import LLM, Agent, Crew, Task
from crewai.events import crewai_event_bus
from crewai.hooks import unregister_before_llm_call_hook
from crewai.tools import BaseTool
from pydantic import BaseModel

from studio_runner.listeners import Listeners
from studio_runner.meter import Meter

from .conftest import PRICES, SETTINGS

FORCE_FINAL = "MUST give your absolute best final answer"


class FakeModel(BaseHTTPRequestHandler):
    """The lead asks the helper once, then answers; the helper keeps calling its tool until CrewAI forces an answer."""

    def do_POST(self):  # noqa: N802 - http.server naming
        request = json.loads(self.rfile.read(int(self.headers.get("content-length", 0))) or b"{}")
        last = request["messages"][-1]
        names = [t["function"]["name"] for t in request.get("tools") or []]
        if FORCE_FINAL in json.dumps(last) or ("ask_question_to_coworker" in names and last.get("role") == "tool") or not names:
            message: dict[str, Any] = {"role": "assistant", "content": "ANSWER"}
        else:
            name = "ask_question_to_coworker" if "ask_question_to_coworker" in names else names[0]
            args = {"question": "What is x?", "context": "c", "coworker": "Helper"} if name.startswith("ask") else {"q": "x"}
            message = {"role": "assistant", "content": None, "tool_calls": [
                {"id": f"call{len(request['messages'])}", "type": "function", "function": {"name": name, "arguments": json.dumps(args)}}]}
        body = json.dumps({"id": "x", "object": "chat.completion", "created": 0, "model": "gpt-6-luna",
                           "choices": [{"index": 0, "finish_reason": "stop", "message": message}],
                           "usage": {"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11}}).encode()
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class LookupInput(BaseModel):
    q: str


class Lookup(BaseTool):
    name: str = "Lookup"
    description: str = "Looks something up."
    args_schema: type[BaseModel] = LookupInput

    def _run(self, q: str) -> str:
        return "nothing found"


@pytest.fixture
def base_url():
    server = ThreadingHTTPServer(("127.0.0.1", 0), FakeModel)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    yield f"http://127.0.0.1:{server.server_address[1]}/v1"
    server.shutdown()


def test_hand_offs_and_forced_answers_are_attributed(base_url, emitter, monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "sk-test")
    llm = LLM(model="openai/gpt-6-luna", base_url=base_url, api_key="sk-test")
    lead = Agent(role="Lead", goal="g", backstory="b", llm=llm, allow_delegation=True, max_iter=3)
    helper = Agent(role="Helper", goal="g", backstory="b", llm=llm, tools=[Lookup()], max_iter=1)
    task = Task(name="Reply", description="Ask Helper about x", expected_output="an answer", agent=lead)
    listeners = Listeners(emitter, Meter(PRICES, SETTINGS, 1.0, 0, lambda *a: None))
    with crewai_event_bus.scoped_handlers():
        listeners.install()
        try:
            Crew(agents=[lead, helper], tasks=[task], verbose=False).kickoff()
            crewai_event_bus.flush()
        finally:
            unregister_before_llm_call_hook(listeners.before_llm_call)

    (delegation,) = emitter.of_type("delegation")
    assert delegation["from_agent"] == "Lead" and delegation["to_agent"] == "Helper" and delegation["question"] == "What is x?"
    calls = emitter.of_type("llm_call")
    forced = [c for c in calls if FORCE_FINAL in json.dumps(c["messages"])]
    assert forced, "the helper's forced final answer was not seen"
    assert all(c["agent_role"] for c in calls), [(c["agent_role"], c["response"][:20]) for c in calls]
    assert {c["agent_role"] for c in forced} == {"Helper"}
