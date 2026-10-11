"""Makes CrewAI's LLM classes work with current reasoning models.

OpenAI (gpt-6-astra, gpt-6.1-sol): CrewAI's own `reasoning_effort` and `api="responses"` options cover it.

Anthropic (claude-opus-5-5, claude-sonnet-5-5, claude-fable-5-1, ...): CrewAI 1.15 forces `tool_choice` when an
agent has exactly one tool, which these models reject with a 400; depth is set with `output_config.effort`,
which CrewAI does not send; and they reject sampling parameters. AnthropicCompletion below fixes all three by
wrapping the Anthropic client calls CrewAI makes.
"""

import re
import sys
from typing import Any, Callable

from crewai.llms.providers.anthropic.completion import AnthropicCompletion

ANTHROPIC_PREFIX = "anthropic/"
# Models that reject forced tool_choice ("any"/"tool") and sampling parameters
_NO_FORCED_TOOLS = re.compile(r"^claude-(fable|mythos|opus|sonnet)-5")
_NO_SAMPLING = re.compile(r"^claude-(fable|mythos|opus|sonnet)-5|^claude-opus-4-[78]")


def is_anthropic(model: str) -> bool:
    return model.startswith(ANTHROPIC_PREFIX) or model.startswith("claude-")


def bare_model(model: str) -> str:
    return model[len(ANTHROPIC_PREFIX):] if model.startswith(ANTHROPIC_PREFIX) else model


def adjust_request(model: str, effort: str | None, kwargs: dict[str, Any]) -> dict[str, Any]:
    """Return a copy of Anthropic request kwargs that the target model accepts."""
    out = dict(kwargs)
    if _NO_FORCED_TOOLS.match(model):
        choice = out.get("tool_choice")
        if isinstance(choice, dict) and choice.get("type") in ("any", "tool"):
            out["tool_choice"] = {"type": "auto"}
    if _NO_SAMPLING.match(model):
        for key in ("temperature", "top_p", "top_k"):
            out.pop(key, None)
    if effort:
        # This SDK version has no output_config argument, so it rides in the request body
        extra = dict(out.get("extra_body") or {})
        extra["output_config"] = {**extra.get("output_config", {}), "effort": effort}
        out["extra_body"] = extra
    return out


class _MessagesProxy:
    def __init__(self, inner: Any, adjust: Callable[[dict[str, Any]], dict[str, Any]]):
        self._inner = inner
        self._adjust = adjust

    def create(self, **kwargs: Any) -> Any:
        return self._inner.create(**self._adjust(kwargs))

    def stream(self, **kwargs: Any) -> Any:
        return self._inner.stream(**self._adjust(kwargs))

    def __getattr__(self, name: str) -> Any:
        return getattr(self._inner, name)


class _BetaProxy:
    def __init__(self, inner: Any, adjust: Callable[[dict[str, Any]], dict[str, Any]]):
        self._inner = inner
        self.messages = _MessagesProxy(inner.messages, adjust)

    def __getattr__(self, name: str) -> Any:
        return getattr(self._inner, name)


class _ClientProxy:
    def __init__(self, inner: Any, adjust: Callable[[dict[str, Any]], dict[str, Any]]):
        self._inner = inner
        self.messages = _MessagesProxy(inner.messages, adjust)
        self.beta = _BetaProxy(inner.beta, adjust)

    def __getattr__(self, name: str) -> Any:
        return getattr(self._inner, name)


class StudioAnthropicCompletion(AnthropicCompletion):
    """AnthropicCompletion whose requests are adjusted for the configured model and effort."""

    studio_effort: str | None = None

    def _adjust(self, kwargs: dict[str, Any]) -> dict[str, Any]:
        return adjust_request(self.model, self.studio_effort, kwargs)

    def _build_sync_client(self) -> Any:
        return _ClientProxy(super()._build_sync_client(), self._adjust)

    def _build_async_client(self) -> Any:
        return _ClientProxy(super()._build_async_client(), self._adjust)


def build_anthropic_llm(config: dict[str, Any]) -> StudioAnthropicCompletion:
    model = bare_model(config["model"])
    if config.get("temperature") is not None and _NO_SAMPLING.match(model):
        print(f"[crew studio] {model} does not accept temperature; ignoring it.", file=sys.stderr)
    kwargs: dict[str, Any] = {"model": model, "studio_effort": config.get("reasoning_effort")}
    if config.get("max_completion_tokens") is not None:
        kwargs["max_tokens"] = config["max_completion_tokens"]
    if config.get("temperature") is not None and not _NO_SAMPLING.match(model):
        kwargs["temperature"] = config["temperature"]
    return StudioAnthropicCompletion(**kwargs)
