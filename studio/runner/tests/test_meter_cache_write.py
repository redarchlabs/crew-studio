import pytest

from studio_runner.meter import Meter, normalize_usage, token_cost

from .conftest import SETTINGS

CLAUDE = {"model": "claude-sonnet-5-5", "input_per_mtok": 2.0, "cached_input_per_mtok": 0.2, "output_per_mtok": 10.0,
          "cache_write_per_mtok": 2.5}
NO_WRITE_PRICE = {**CLAUDE, "cache_write_per_mtok": None}


def test_normalize_reads_cache_writes_from_crewai_and_anthropic_names():
    # CrewAI's Anthropic provider: input_tokens already includes cache reads and writes
    crewai = normalize_usage({"input_tokens": 30_000, "output_tokens": 100, "cached_prompt_tokens": 5_000, "cache_creation_tokens": 20_000})
    assert crewai == {"prompt_tokens": 30_000, "completion_tokens": 100, "cached_prompt_tokens": 5_000, "cache_write_tokens": 20_000}
    raw = normalize_usage({"prompt_tokens": 10, "completion_tokens": 1, "cache_creation_input_tokens": 4})
    assert raw["cache_write_tokens"] == 4
    assert normalize_usage({"prompt_tokens": 10})["cache_write_tokens"] == 0


def test_cache_writes_are_priced_at_the_cache_write_rate():
    # 5k uncached at $2, 5k cache reads at $0.20, 20k cache writes at $2.50, 100 output at $10
    cost = token_cost(CLAUDE, prompt=30_000, completion=100, cached=5_000, cache_write=20_000)
    assert cost == pytest.approx((5_000 * 2.0 + 5_000 * 0.2 + 20_000 * 2.5 + 100 * 10.0) / 1_000_000)


def test_without_a_cache_write_price_writes_cost_the_input_price():
    cost = token_cost(NO_WRITE_PRICE, prompt=30_000, completion=0, cached=0, cache_write=20_000)
    assert cost == pytest.approx(30_000 * 2.0 / 1_000_000)
    # Old specs have no cache_write_per_mtok key at all
    legacy = {k: v for k, v in CLAUDE.items() if k != "cache_write_per_mtok"}
    assert token_cost(legacy, 30_000, 0, 0, 20_000) == pytest.approx(0.06)


def test_meter_counts_and_prices_cache_writes():
    meter = Meter([CLAUDE], SETTINGS, 10.0, 0, lambda *_a: None)
    cost = meter.record_llm_call("anthropic/claude-sonnet-5-5",
                                 {"input_tokens": 20_000, "output_tokens": 0, "cache_creation_tokens": 20_000})
    assert cost == pytest.approx(0.05)
    assert meter.usage()["cache_write_tokens"] == 20_000
    assert meter.usage()["prompt_tokens"] == 20_000
