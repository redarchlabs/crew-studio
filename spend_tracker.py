"""Estimate the spend of each crew run and keep a running log of it.

OpenAI usage comes from CrewAI's token counts (result.token_usage). Google
Search usage is recorded by GoogleSearchTool after every search.

Prices are estimates in USD per 1M tokens; check the provider pricing pages
and update PRICING when they change. Exact charges are in the OpenAI usage
dashboard and Google Cloud Billing.
"""

import csv
from dataclasses import dataclass
from datetime import date, datetime
from pathlib import Path

PRICING = {
    # OpenAI: https://openai.com/api/pricing
    "gpt-6-luna": {"input": 0.10, "cached_input": 0.01, "output": 0.50},
    # Reasoning tokens bill as output: https://developers.openai.com/api/docs/models/gpt-6.1-sol
    "gpt-6.1-sol": {"input": 2.00, "cached_input": 0.10, "output": 10.00},
    # Gemini on Vertex AI: https://cloud.google.com/vertex-ai/generative-ai/pricing
    "gemini-2.5-flash": {"input": 0.30, "cached_input": 0.03, "output": 2.50},
}
TOKENS_PER_PRICE_UNIT = 1_000_000

# Grounding with Google Search on Gemini 2.5 Flash: free daily allowance, then per 1,000
GROUNDED_SEARCHES_FREE_PER_DAY = 1500
GROUNDED_SEARCH_COST_PER_1000 = 35.00

SPEND_LOG_PATH = Path(__file__).parent / "spend_log.csv"
LOG_FIELDS = [
    "timestamp", "openai_model", "openai_requests", "openai_input_tokens",
    "openai_cached_tokens", "openai_output_tokens", "openai_cost",
    "searches", "grounded_searches", "gemini_input_tokens", "gemini_output_tokens",
    "gemini_token_cost", "grounding_cost", "total_cost",
]


@dataclass(frozen=True)
class SearchUsage:
    model: str
    input_tokens: int
    output_tokens: int
    grounded: bool


_searches: list[SearchUsage] = []


def record_search(model: str, response: dict) -> None:
    """Record one Gemini search call from its raw API response."""
    usage = response.get("usageMetadata", {})
    candidate = (response.get("candidates") or [{}])[0]
    # Google only bills grounding when the response cites at least one web source
    grounded = bool(candidate.get("groundingMetadata", {}).get("groundingChunks"))
    _searches.append(SearchUsage(
        model=model,
        input_tokens=usage.get("promptTokenCount", 0),
        # Thinking tokens are billed as output
        output_tokens=usage.get("candidatesTokenCount", 0) + usage.get("thoughtsTokenCount", 0),
        grounded=grounded,
    ))


def token_cost(model: str, input_tokens: int, output_tokens: int, cached_tokens: int = 0) -> float:
    prices = PRICING.get(model)
    if prices is None:
        print(f"[spend] No price for '{model}' in PRICING; counting its tokens as $0.")
        return 0.0
    uncached = max(input_tokens - cached_tokens, 0)
    return (
        uncached * prices["input"]
        + cached_tokens * prices["cached_input"]
        + output_tokens * prices["output"]
    ) / TOKENS_PER_PRICE_UNIT


def _grounded_searches_logged_today() -> int:
    if not SPEND_LOG_PATH.exists():
        return 0
    today = date.today().isoformat()
    with SPEND_LOG_PATH.open(newline="") as f:
        return sum(
            int(row["grounded_searches"])
            for row in csv.DictReader(f)
            if row["timestamp"].startswith(today)
        )


def _grounding_cost(grounded_this_run: int, grounded_before_today: int) -> float:
    free = GROUNDED_SEARCHES_FREE_PER_DAY
    billable_before = max(grounded_before_today - free, 0)
    billable_after = max(grounded_before_today + grounded_this_run - free, 0)
    return (billable_after - billable_before) * GROUNDED_SEARCH_COST_PER_1000 / 1000


def search_cost() -> float:
    """Estimated cost of the searches recorded so far in this run."""
    tokens = sum(token_cost(s.model, s.input_tokens, s.output_tokens) for s in _searches)
    grounded = sum(s.grounded for s in _searches)
    return tokens + _grounding_cost(grounded, _grounded_searches_logged_today())


def build_spend_report(token_usage, openai_model: str) -> dict:
    """Combine CrewAI's OpenAI token usage with recorded searches into one report."""
    openai_cost = token_cost(
        openai_model,
        token_usage.prompt_tokens,
        token_usage.completion_tokens,
        token_usage.cached_prompt_tokens,
    )
    gemini_input = sum(s.input_tokens for s in _searches)
    gemini_output = sum(s.output_tokens for s in _searches)
    gemini_token_cost = sum(token_cost(s.model, s.input_tokens, s.output_tokens) for s in _searches)
    grounded = sum(s.grounded for s in _searches)
    grounded_today = _grounded_searches_logged_today()
    grounding_cost = _grounding_cost(grounded, grounded_today)

    return {
        "timestamp": datetime.now().isoformat(timespec="seconds"),
        "openai_model": openai_model,
        "openai_requests": token_usage.successful_requests,
        "openai_input_tokens": token_usage.prompt_tokens,
        "openai_cached_tokens": token_usage.cached_prompt_tokens,
        "openai_output_tokens": token_usage.completion_tokens,
        "openai_cost": round(openai_cost, 6),
        "searches": len(_searches),
        "grounded_searches": grounded,
        "gemini_input_tokens": gemini_input,
        "gemini_output_tokens": gemini_output,
        "gemini_token_cost": round(gemini_token_cost, 6),
        "grounding_cost": round(grounding_cost, 6),
        "total_cost": round(openai_cost + gemini_token_cost + grounding_cost, 6),
        "grounded_searches_today": grounded_today + grounded,
    }


def print_spend_report(report: dict) -> None:
    free = GROUNDED_SEARCHES_FREE_PER_DAY
    print("\n===== Run spend (estimate) =====")
    print(
        f"OpenAI {report['openai_model']}: {report['openai_requests']} calls, "
        f"{report['openai_input_tokens']:,} in ({report['openai_cached_tokens']:,} cached) / "
        f"{report['openai_output_tokens']:,} out  ->  ${report['openai_cost']:.4f}"
    )
    print(
        f"Google Search: {report['searches']} searches ({report['grounded_searches']} grounded), "
        f"{report['gemini_input_tokens']:,} in / {report['gemini_output_tokens']:,} out  ->  "
        f"${report['gemini_token_cost']:.4f} tokens + ${report['grounding_cost']:.4f} grounding"
    )
    print(f"Grounded searches today: {report['grounded_searches_today']:,} of {free:,} free")
    print(f"TOTAL: ${report['total_cost']:.4f}")
    print(f"Logged to {SPEND_LOG_PATH.name}")


def append_to_log(report: dict) -> None:
    is_new = not SPEND_LOG_PATH.exists()
    with SPEND_LOG_PATH.open("a", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=LOG_FIELDS, extrasaction="ignore")
        if is_new:
            writer.writeheader()
        writer.writerow(report)
