import pytest

from studio_runner.meter import HARD_STOPPED, SEARCHES_STOPPED, Meter, match_price, token_cost

from .conftest import PRICES, SETTINGS


def test_match_price_exact_then_after_provider_prefix():
    prices = [{"model": "gpt-6-luna", "x": 1}, {"model": "openai/gpt-6-luna", "x": 2}]
    assert match_price("openai/gpt-6-luna", prices)["x"] == 2
    assert match_price("azure/gpt-6-luna", prices)["x"] == 1
    assert match_price("unknown", prices) is None


def test_token_cost_bills_cached_tokens_at_cached_rate():
    price = PRICES[0]
    # 1M prompt of which 400k cached, 100k completion
    cost = token_cost(price, 1_000_000, 100_000, 400_000)
    assert cost == pytest.approx(0.6 * 0.10 + 0.4 * 0.01 + 0.1 * 0.50)
    assert token_cost(None, 10, 10) == 0.0


def make_meter(budget=1.0, grounded_today=0, settings=None):
    announced = []
    meter = Meter(PRICES, settings or SETTINGS, budget, grounded_today, lambda state, spent: announced.append(state))
    return meter, announced


def test_llm_calls_accumulate_usage_and_flag_unpriced_models():
    meter, _ = make_meter()
    meter.record_llm_call("openai/gpt-6-luna", {"prompt_tokens": 1000, "completion_tokens": 100})
    meter.record_llm_call("mystery-model", {"prompt_tokens": 5})
    usage = meter.usage()
    assert usage["prompt_tokens"] == 1005
    assert usage["successful_requests"] == 2
    summary = meter.summary()
    assert summary["unpriced_models"] == ["mystery-model"]
    assert summary["llm_usd"] == pytest.approx((1000 * 0.10 + 100 * 0.50) / 1e6, abs=1e-6)


def test_anthropic_usage_shape_is_priced():
    meter, _ = make_meter()
    prices = PRICES + [{"model": "claude-opus-5-5", "input_per_mtok": 4.0, "cached_input_per_mtok": 0.2, "output_per_mtok": 20.0}]
    meter = Meter(prices, SETTINGS, 1.0, 0, lambda *a: None)
    cost = meter.record_llm_call("claude-opus-5-5", {"input_tokens": 2000, "output_tokens": 300, "cached_prompt_tokens": 1000})
    assert cost == pytest.approx((1000 * 4.0 + 1000 * 0.2 + 300 * 20.0) / 1e6)
    assert meter.usage()["prompt_tokens"] == 2000


def test_grounding_bills_each_query_past_the_free_allowance():
    meter, _ = make_meter(grounded_today=1)  # allowance is 2 per period in SETTINGS
    # 3 queries: 1 still free, 2 billed at $14 / 1000
    first = meter.record_search(0, 0, billable_searches=3)
    ungrounded = meter.record_search(0, 0, billable_searches=0)
    later = meter.record_search(0, 0, billable_searches=1)
    assert first == pytest.approx(0.028)
    assert ungrounded == 0
    assert later == pytest.approx(0.014)
    assert meter.summary()["grounded_searches"] == 4


def test_budget_stops_searches_then_llm_calls_and_announces_once():
    meter, announced = make_meter(budget=0.0001)
    assert meter.searches_allowed()
    meter.record_llm_call("gpt-6-luna", {"prompt_tokens": 2_000})  # $0.0002, past budget and hard cap
    assert not meter.searches_allowed()
    assert not meter.llm_call_allowed()
    assert not meter.llm_call_allowed()
    assert announced == [SEARCHES_STOPPED, HARD_STOPPED]


def test_hard_cap_uses_multiplier():
    meter, _ = make_meter(budget=2.0)
    assert meter.hard_cap == pytest.approx(2.5)
