import httpx
import pytest

from studio_runner.build import build_llm
from studio_runner.llm_compat import StudioAnthropicCompletion, adjust_request


def test_adjust_request_for_claude_5_models():
    out = adjust_request(
        "claude-opus-5-5", "high",
        {"tool_choice": {"type": "tool", "name": "Google Search"}, "temperature": 0.2, "extra_body": {"x": 1}},
    )
    assert out["tool_choice"] == {"type": "auto"}
    assert "temperature" not in out
    assert out["extra_body"] == {"x": 1, "output_config": {"effort": "high"}}


def test_adjust_request_leaves_older_models_alone():
    kwargs = {"tool_choice": {"type": "tool", "name": "t"}, "temperature": 0.2}
    assert adjust_request("claude-haiku-4-5", None, kwargs) == kwargs


def test_openai_reasoning_llm(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("OPENAI_API_KEY", "sk-test")
    llm = build_llm({"model": "openai/gpt-6.1-sol", "temperature": None, "max_completion_tokens": 1000,
                     "reasoning_effort": "high", "api": "responses"})
    assert llm.reasoning_effort == "high"
    assert llm.api == "responses"


def test_anthropic_requests_carry_effort_and_auto_tool_choice(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-test")
    llm = build_llm({"model": "anthropic/claude-opus-5-5", "temperature": 0.5, "max_completion_tokens": 2000,
                     "reasoning_effort": "high", "api": "completions"})
    assert isinstance(llm, StudioAnthropicCompletion) and llm.model == "claude-opus-5-5"

    seen = {}

    def handler(request: httpx.Request) -> httpx.Response:
        import json
        seen.update(json.loads(request.content))
        return httpx.Response(200, json={
            "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
            "content": [{"type": "text", "text": "ok"}], "stop_reason": "end_turn", "stop_sequence": None,
            "usage": {"input_tokens": 10, "output_tokens": 2},
        })

    client = llm._get_sync_client()
    client._inner._client = httpx.Client(transport=httpx.MockTransport(handler))
    client.messages.create(model="claude-opus-5-5", max_tokens=10, messages=[{"role": "user", "content": "hi"}],
                           tools=[{"name": "t", "description": "d", "input_schema": {"type": "object"}}],
                           tool_choice={"type": "tool", "name": "t"}, temperature=0.5)
    assert seen["output_config"] == {"effort": "high"}
    assert seen["tool_choice"] == {"type": "auto"}
    assert "temperature" not in seen
