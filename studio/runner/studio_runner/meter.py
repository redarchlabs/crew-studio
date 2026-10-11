"""Estimates what a run costs, using the prices stored in Crew Studio, and enforces its budget.

- At the budget, searches are refused so agents write their answers from what they have.
- At budget x hard_stop_multiplier, every further LLM call is blocked and the crew stops.
- A video is only generated if its estimated cost fits in what is left of the budget.

These are estimates; provider console spend limits remain the real backstop.
"""

import threading
from dataclasses import dataclass, field
from typing import Any, Callable

TOKENS_PER_PRICE_UNIT = 1_000_000

SEARCHES_STOPPED = "searches_stopped"
HARD_STOPPED = "hard_stopped"


def match_price(model: str, prices: list[dict[str, Any]]) -> dict[str, Any] | None:
    """Exact model match first, then the part after the provider prefix ("openai/x" matches "x")."""
    for price in prices:
        if price["model"] == model:
            return price
    if "/" in model:
        bare = model.split("/", 1)[1]
        for price in prices:
            if price["model"] == bare:
                return price
    return None


def normalize_usage(usage: dict[str, Any]) -> dict[str, int]:
    """Map provider usage to prompt/completion/cached/cache-write counts.

    OpenAI-style providers report prompt_tokens/completion_tokens; CrewAI's Anthropic provider reports
    input_tokens/output_tokens (input already includes cache reads and writes) with the writes as
    cache_creation_tokens. Prompt counts include the cached and cache-write tokens.
    """
    def first(*keys: str) -> int:
        for key in keys:
            if usage.get(key) is not None:
                return int(usage[key] or 0)
        return 0

    return {
        "prompt_tokens": first("prompt_tokens", "input_tokens"),
        "completion_tokens": first("completion_tokens", "output_tokens"),
        "cached_prompt_tokens": first("cached_prompt_tokens", "cache_read_input_tokens"),
        "cache_write_tokens": first("cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens"),
    }


def token_cost(price: dict[str, Any] | None, prompt: int, completion: int, cached: int = 0, cache_write: int = 0) -> float:
    """Cost of one call. prompt includes the cached and cache-write tokens. Cache writes (Anthropic charges more for
    them) cost the model's cache-write price, or its input price when it has none."""
    if price is None:
        return 0.0
    uncached = max(prompt - cached - cache_write, 0)
    write_price = price.get("cache_write_per_mtok")
    if write_price is None:
        write_price = price["input_per_mtok"]
    return (
        uncached * price["input_per_mtok"]
        + cached * price["cached_input_per_mtok"]
        + cache_write * write_price
        + completion * price["output_per_mtok"]
    ) / TOKENS_PER_PRICE_UNIT


@dataclass
class Usage:
    prompt_tokens: int = 0
    completion_tokens: int = 0
    cached_prompt_tokens: int = 0
    cache_write_tokens: int = 0
    successful_requests: int = 0

    def as_dict(self) -> dict[str, int]:
        return {
            "prompt_tokens": self.prompt_tokens,
            "completion_tokens": self.completion_tokens,
            "cached_prompt_tokens": self.cached_prompt_tokens,
            "cache_write_tokens": self.cache_write_tokens,
            "successful_requests": self.successful_requests,
        }


@dataclass
class Spend:
    llm_usd: float = 0.0
    search_token_usd: float = 0.0
    grounding_usd: float = 0.0
    video_usd: float = 0.0
    searches: int = 0
    grounded_searches: int = 0
    videos: int = 0
    unpriced_models: set[str] = field(default_factory=set)

    @property
    def total(self) -> float:
        return self.llm_usd + self.search_token_usd + self.grounding_usd + self.video_usd


class Meter:
    def __init__(
        self,
        prices: list[dict[str, Any]],
        settings: dict[str, Any],
        budget_usd: float,
        grounded_searches_in_period: int,
        on_budget: Callable[[str, float], None],
    ):
        self._prices = prices
        self._settings = settings
        self._budget = budget_usd
        self._grounded_before_run = grounded_searches_in_period
        self._on_budget = on_budget
        self._lock = threading.Lock()
        self._usage = Usage()
        self._spend = Spend()
        self._announced: set[str] = set()
        # Budget held for videos being generated, so two at once cannot both fit in the same room
        self._reserved_video_usd = 0.0

    @property
    def budget(self) -> float:
        return self._budget

    @property
    def hard_cap(self) -> float:
        return self._budget * float(self._settings["hard_stop_multiplier"])

    def spent(self) -> float:
        with self._lock:
            return self._spend.total

    def usage(self) -> dict[str, int]:
        with self._lock:
            return self._usage.as_dict()

    def summary(self) -> dict[str, Any]:
        with self._lock:
            s = self._spend
            return {
                "llm_usd": round(s.llm_usd, 6),
                "search_token_usd": round(s.search_token_usd, 6),
                "grounding_usd": round(s.grounding_usd, 6),
                "video_usd": round(s.video_usd, 6),
                "total_usd": round(s.total, 6),
                "searches": s.searches,
                "grounded_searches": s.grounded_searches,
                "videos": s.videos,
                "unpriced_models": sorted(s.unpriced_models),
                "budget_usd": self._budget,
                "hard_cap_usd": round(self.hard_cap, 6),
            }

    def record_llm_call(self, model: str, usage: dict[str, Any]) -> float:
        counts = normalize_usage(usage)
        prompt, completion, cached = counts["prompt_tokens"], counts["completion_tokens"], counts["cached_prompt_tokens"]
        written = counts["cache_write_tokens"]
        price = match_price(model, self._prices)
        cost = token_cost(price, prompt, completion, cached, written)
        with self._lock:
            self._usage.prompt_tokens += prompt
            self._usage.completion_tokens += completion
            self._usage.cached_prompt_tokens += cached
            self._usage.cache_write_tokens += written
            self._usage.successful_requests += 1
            self._spend.llm_usd += cost
            if price is None:
                self._spend.unpriced_models.add(model)
        self._check_thresholds()
        return cost

    def record_search(self, input_tokens: int, output_tokens: int, billable_searches: int) -> float:
        """Record one search tool call. billable_searches is how many grounded queries it ran (0 if ungrounded)."""
        price = match_price(self._settings["search_model"], self._prices)
        cost = token_cost(price, input_tokens, output_tokens)
        free = int(self._settings["grounded_searches_free"])
        per_search = float(self._settings["grounded_search_cost_per_1000"]) / 1000
        with self._lock:
            self._spend.searches += 1
            self._spend.search_token_usd += cost
            used_before = self._grounded_before_run + self._spend.grounded_searches
            self._spend.grounded_searches += billable_searches
            # Only the queries past the free allowance are billed
            billed = max(used_before + billable_searches - max(free, used_before), 0)
            grounding = billed * per_search
            self._spend.grounding_usd += grounding
            cost += grounding
        self._check_thresholds()
        return cost

    def reserve_video(self, estimated_usd: float) -> bool:
        """Hold budget for a video about to be generated, if it fits beside what is spent and already held."""
        with self._lock:
            if self._spend.total + self._reserved_video_usd + estimated_usd > self._budget:
                return False
            self._reserved_video_usd += estimated_usd
            return True

    def release_video(self, reserved_usd: float) -> None:
        """Give back a reservation for a video that cost nothing."""
        with self._lock:
            self._reserved_video_usd = max(self._reserved_video_usd - reserved_usd, 0.0)

    def record_video(self, model: str, input_tokens: int, output_tokens: int, reserved_usd: float = 0.0) -> float:
        """Record one video, billed by its tokens at the video model's price, replacing its reservation."""
        price = match_price(model, self._prices)
        cost = token_cost(price, input_tokens, output_tokens)
        with self._lock:
            self._reserved_video_usd = max(self._reserved_video_usd - reserved_usd, 0.0)
            self._spend.videos += 1
            self._spend.video_usd += cost
            if price is None:
                self._spend.unpriced_models.add(model)
        self._check_thresholds()
        return cost

    def searches_allowed(self) -> bool:
        return self.spent() < self._budget

    def llm_call_allowed(self) -> bool:
        allowed = self.spent() < self.hard_cap
        if not allowed:
            self._announce(HARD_STOPPED)
        return allowed

    def _check_thresholds(self) -> None:
        if self.spent() >= self._budget:
            self._announce(SEARCHES_STOPPED)

    def _announce(self, state: str) -> None:
        with self._lock:
            if state in self._announced:
                return
            self._announced.add(state)
            spent = self._spend.total
        self._on_budget(state, spent)
