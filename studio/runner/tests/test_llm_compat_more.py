import pytest

from studio_runner import llm_compat as lc
from studio_runner.llm_compat import (
    _BetaProxy, _ClientProxy, _MessagesProxy, adjust_request, bare_model, build_anthropic_llm, is_anthropic,
)


@pytest.mark.parametrize("model, expected", [("anthropic/claude-x", True), ("claude-opus-5-5", True), ("openai/gpt-6", False), ("gpt-6", False)])
def test_is_anthropic(model, expected):
    assert is_anthropic(model) is expected


def test_bare_model():
    assert bare_model("anthropic/claude-x") == "claude-x" and bare_model("claude-x") == "claude-x"


class TestAdjustRequest:
    def test_input_not_mutated(self):
        kw = {"tool_choice": {"type": "any"}, "temperature": 1, "extra_body": {"a": 1}}
        adjust_request("claude-opus-5-5", "low", kw)
        assert kw == {"tool_choice": {"type": "any"}, "temperature": 1, "extra_body": {"a": 1}}

    @pytest.mark.parametrize("choice", [{"type": "auto"}, {"type": "none"}, "any", None])
    def test_non_forcing_choices_kept(self, choice):
        out = adjust_request("claude-opus-5-5", None, {"tool_choice": choice})
        assert out["tool_choice"] == choice

    def test_any_is_replaced(self):
        assert adjust_request("claude-sonnet-5-5", None, {"tool_choice": {"type": "any"}})["tool_choice"] == {"type": "auto"}

    def test_no_tool_choice_key_added(self):
        assert "tool_choice" not in adjust_request("claude-opus-5-5", None, {})

    @pytest.mark.parametrize("model", ["claude-opus-4-7", "claude-opus-4-8", "claude-fable-5-1", "claude-mythos-5"])
    def test_sampling_removed(self, model):
        out = adjust_request(model, None, {"temperature": 1, "top_p": 1, "top_k": 3, "max_tokens": 5})
        assert out == {"max_tokens": 5}

    def test_opus_4_7_keeps_forced_tools(self):
        out = adjust_request("claude-opus-4-7", None, {"tool_choice": {"type": "tool", "name": "t"}})
        assert out["tool_choice"]["type"] == "tool"

    def test_effort_merges_existing_output_config(self):
        out = adjust_request("claude-haiku-4-5", "max", {"extra_body": {"output_config": {"format": "x"}}})
        assert out["extra_body"]["output_config"] == {"format": "x", "effort": "max"}

    def test_effort_without_extra_body(self):
        assert adjust_request("claude-haiku-4-5", "low", {})["extra_body"] == {"output_config": {"effort": "low"}}


class Recorder:
    def __init__(self):
        self.calls = []
        self.attr = "inner-attr"

    def create(self, **kw):
        self.calls.append(("create", kw))
        return "created"

    def stream(self, **kw):
        self.calls.append(("stream", kw))
        return "streamed"


class FakeClient:
    def __init__(self):
        self.messages = Recorder()
        self.beta = type("B", (), {"messages": Recorder(), "other": 5})()
        self.base_url = "u"


def test_proxies_adjust_create_and_stream_and_delegate_attrs():
    client = FakeClient()
    proxy = _ClientProxy(client, lambda kw: {**kw, "adjusted": True})
    assert proxy.messages.create(a=1) == "created"
    assert proxy.messages.stream(a=2) == "streamed"
    assert proxy.beta.messages.create(a=3) == "created"
    assert client.messages.calls == [("create", {"a": 1, "adjusted": True}), ("stream", {"a": 2, "adjusted": True})]
    assert client.beta.messages.calls == [("create", {"a": 3, "adjusted": True})]
    assert proxy.base_url == "u" and proxy.messages.attr == "inner-attr" and proxy.beta.other == 5
    assert isinstance(proxy.beta, _BetaProxy) and isinstance(proxy.messages, _MessagesProxy)


class TestBuildAnthropicLlm:
    @pytest.fixture(autouse=True)
    def key(self, monkeypatch):
        monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-test")

    def test_temperature_ignored_with_notice_for_new_models(self, capsys):
        llm = build_anthropic_llm({"model": "anthropic/claude-opus-5-5", "temperature": 0.4})
        assert "does not accept temperature" in capsys.readouterr().err
        assert llm.temperature is None

    def test_temperature_kept_for_old_models(self, capsys):
        llm = build_anthropic_llm({"model": "claude-haiku-4-5", "temperature": 0.4})
        assert llm.temperature == 0.4 and capsys.readouterr().err == ""

    def test_no_temperature_no_notice(self, capsys):
        build_anthropic_llm({"model": "claude-opus-5-5"})
        assert capsys.readouterr().err == ""

    def test_max_tokens_and_effort(self):
        llm = build_anthropic_llm({"model": "claude-opus-5-5", "max_completion_tokens": 321, "reasoning_effort": "high"})
        assert llm.max_tokens == 321 and llm.studio_effort == "high"

    def test_async_and_sync_clients_are_proxied(self):
        llm = build_anthropic_llm({"model": "claude-opus-5-5"})
        assert isinstance(llm._build_async_client(), _ClientProxy)
        assert isinstance(llm._build_sync_client(), _ClientProxy)

    def test_adjust_uses_model_and_effort(self):
        llm = build_anthropic_llm({"model": "claude-opus-5-5", "reasoning_effort": "low"})
        assert llm._adjust({"temperature": 1})["extra_body"] == {"output_config": {"effort": "low"}}
        assert "temperature" not in llm._adjust({"temperature": 1})
