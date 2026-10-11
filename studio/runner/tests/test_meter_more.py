import pytest

from studio_runner.meter import Meter, normalize_usage

from .conftest import PRICES, SETTINGS


def test_normalize_usage_variants():
    assert normalize_usage({}) == {"prompt_tokens": 0, "completion_tokens": 0, "cached_prompt_tokens": 0, "cache_write_tokens": 0}
    assert normalize_usage({"prompt_tokens": None, "input_tokens": 7, "output_tokens": 3, "cache_read_input_tokens": 2}) == {
        "prompt_tokens": 7, "completion_tokens": 3, "cached_prompt_tokens": 2, "cache_write_tokens": 0}
    # a present zero wins over a later alias
    assert normalize_usage({"prompt_tokens": 0, "input_tokens": 9})["prompt_tokens"] == 0
    assert normalize_usage({"prompt_tokens": "12"})["prompt_tokens"] == 12


def test_budget_property_and_summary():
    meter = Meter(PRICES, SETTINGS, 2.0, 0, lambda *a: None)
    assert meter.budget == 2.0
    s = meter.summary()
    assert s["budget_usd"] == 2.0 and s["hard_cap_usd"] == 2.5 and s["total_usd"] == 0
    assert s["unpriced_models"] == [] and s["searches"] == 0


def test_search_tokens_priced_with_search_model():
    meter = Meter(PRICES, SETTINGS, 1.0, 0, lambda *a: None)
    cost = meter.record_search(1_000_000, 1_000_000, 0)
    assert cost == pytest.approx(0.75 + 3.75)
    assert meter.summary()["search_token_usd"] == pytest.approx(4.5)


def test_free_allowance_already_used_bills_everything():
    meter = Meter(PRICES, SETTINGS, 5.0, 10, lambda *a: None)
    assert meter.record_search(0, 0, 2) == pytest.approx(0.028)


def test_negative_cached_cannot_make_uncached_negative():
    meter = Meter(PRICES, SETTINGS, 5.0, 0, lambda *a: None)
    cost = meter.record_llm_call("gpt-6-luna", {"prompt_tokens": 10, "cached_prompt_tokens": 50})
    assert cost == pytest.approx(50 * 0.01 / 1e6)


def test_searches_allowed_boundary():
    meter = Meter(PRICES, SETTINGS, 0.0002, 0, lambda *a: None)
    assert meter.searches_allowed()
    meter.record_llm_call("gpt-6-luna", {"prompt_tokens": 2000})  # exactly 0.0002
    assert not meter.searches_allowed()
    assert meter.llm_call_allowed()  # hard cap is 1.25x


def test_concurrent_recording_is_consistent():
    import threading

    meter = Meter(PRICES, SETTINGS, 100.0, 0, lambda *a: None)
    ts = [threading.Thread(target=lambda: [meter.record_llm_call("gpt-6-luna", {"prompt_tokens": 10}) for _ in range(100)]) for _ in range(8)]
    [t.start() for t in ts]
    [t.join() for t in ts]
    assert meter.usage()["prompt_tokens"] == 8000 and meter.usage()["successful_requests"] == 800


def test_match_price_prefixed_model_with_no_match_or_several_candidates():
    from studio_runner.meter import match_price

    prices = [{"model": "a"}, {"model": "b"}]
    assert match_price("openai/zzz", prices) is None
    assert match_price("openai/b", prices) == {"model": "b"}
