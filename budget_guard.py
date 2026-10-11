"""Stop a crew run before its estimated spend passes a dollar budget.

Spend is estimated the same way as spend_tracker: OpenAI tokens from each LLM
call as it completes, plus every Google Search. Two limits apply:

- At the budget, GoogleSearchTool refuses new searches and tells the agent to
  write its answer from what it has, so the run can still finish.
- At HARD_STOP_MULTIPLIER x the budget, every further LLM call is blocked and
  the crew stops with LLMCallBlockedError.

These are estimates. Set spend limits in the OpenAI and Google Cloud consoles
as the real backstop.
"""

import os
import threading
from dataclasses import dataclass, replace

from crewai.events import LLMCallCompletedEvent, crewai_event_bus
from crewai.hooks import LLMCallHookContext, register_before_llm_call_hook

from spend_tracker import search_cost, token_cost

BUDGET_ENV_VAR = "RUN_BUDGET_USD"
DEFAULT_RUN_BUDGET_USD = 2.00
# Room past the budget for agents to write their final answers before the hard stop
HARD_STOP_MULTIPLIER = 1.25


@dataclass(frozen=True)
class TokenUsage:
    """OpenAI usage so far, with the attribute names build_spend_report reads."""

    prompt_tokens: int = 0
    cached_prompt_tokens: int = 0
    completion_tokens: int = 0
    successful_requests: int = 0


_lock = threading.Lock()
_usage = TokenUsage()
_budget_usd: float | None = None
_openai_model = ""
_hard_stopped = False


def budget_from_env() -> float:
    """Read the per-run budget from RUN_BUDGET_USD, or fall back to the default."""
    raw = os.getenv(BUDGET_ENV_VAR, "").strip()
    if not raw:
        return DEFAULT_RUN_BUDGET_USD
    try:
        return float(raw)
    except ValueError:
        raise SystemExit(f"{BUDGET_ENV_VAR} must be a dollar amount such as 2.00, not '{raw}'.")


def install(budget_usd: float, openai_model: str) -> None:
    """Start enforcing the budget. Call before creating agents."""
    global _budget_usd, _openai_model
    if budget_usd <= 0:
        raise SystemExit(f"The run budget must be more than $0, not ${budget_usd}.")
    already_installed = _budget_usd is not None
    _budget_usd = budget_usd
    _openai_model = openai_model
    if not already_installed:
        crewai_event_bus.on(LLMCallCompletedEvent)(_record_llm_call)
        register_before_llm_call_hook(_block_when_over_hard_cap)
    print(
        f"[budget] Run budget ${budget_usd:.2f}: searches stop there, "
        f"all calls stop at ${hard_cap():.2f}."
    )


def hard_cap() -> float:
    return (_budget_usd or 0.0) * HARD_STOP_MULTIPLIER


def usage() -> TokenUsage:
    with _lock:
        return _usage


def spent() -> float:
    """Estimated spend of this run so far, in USD."""
    current = usage()
    openai_cost = token_cost(
        _openai_model,
        current.prompt_tokens,
        current.completion_tokens,
        current.cached_prompt_tokens,
    )
    return openai_cost + search_cost()


def searches_allowed() -> bool:
    return _budget_usd is None or spent() < _budget_usd


def hard_stopped() -> bool:
    return _hard_stopped


def _record_llm_call(source: object, event: LLMCallCompletedEvent) -> None:
    global _usage
    tokens = event.usage or {}
    with _lock:
        _usage = replace(
            _usage,
            prompt_tokens=_usage.prompt_tokens + (tokens.get("prompt_tokens") or 0),
            cached_prompt_tokens=_usage.cached_prompt_tokens + (tokens.get("cached_prompt_tokens") or 0),
            completion_tokens=_usage.completion_tokens + (tokens.get("completion_tokens") or 0),
            successful_requests=_usage.successful_requests + 1,
        )


def _block_when_over_hard_cap(context: LLMCallHookContext) -> bool | None:
    global _hard_stopped
    if _budget_usd is None or spent() < hard_cap():
        return None
    if not _hard_stopped:
        print(f"\n[budget] Estimated spend ${spent():.2f} reached the ${hard_cap():.2f} hard cap. Stopping.")
    _hard_stopped = True
    # Returning False tells CrewAI to block this LLM call
    return False
