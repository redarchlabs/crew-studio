import io
import json
from datetime import datetime, timezone

import pytest

from studio_runner import emitter as em
from studio_runner.emitter import Emitter, cap_strings, to_jsonable, truncate


class TestTruncate:
    def test_exact_limit_is_untouched(self):
        assert truncate("a" * 10, 10) == "a" * 10

    def test_one_over_limit_is_cut_and_counted(self):
        out = truncate("a" * 11, 10)
        assert out.startswith("a" * 10 + "\n")
        assert "truncated 1 characters" in out

    def test_empty_and_zero_limit(self):
        assert truncate("", 0) == ""
        assert truncate("abc", 0).startswith("\n... [truncated 3 characters")

    def test_count_is_thousands_separated(self):
        assert "truncated 1,990 characters" in truncate("x" * 2000, 10)

    def test_unicode_counts_characters(self):
        out = truncate("é😀" * 5, 4)
        assert out.startswith("é😀é😀\n")
        assert "truncated 6 characters" in out


class TestCapStrings:
    def test_nested_structures_are_capped_without_mutating_input(self):
        original = {"a": ["xxxx", {"b": "yyyy", "c": ["zzzz"]}], "n": 3, "none": None, "t": True, "f": 1.5}
        snapshot = json.dumps(original)
        out = cap_strings(original, 2)
        assert out["a"][0].startswith("xx\n")
        assert out["a"][1]["b"].startswith("yy\n")
        assert out["a"][1]["c"][0].startswith("zz\n")
        assert (out["n"], out["none"], out["t"], out["f"]) == (3, None, True, 1.5)
        assert json.dumps(original) == snapshot

    def test_boundary_lengths(self):
        assert cap_strings(["abc"], 3) == ["abc"]
        assert cap_strings(["abcd"], 3)[0].startswith("abc\n")

    def test_non_container_non_str_values_pass_through(self):
        marker = object()
        assert cap_strings(marker, 1) is marker
        assert cap_strings(5, 1) == 5
        assert cap_strings((("abcdef",)), 1) == (("abcdef",))  # tuples are not walked

    def test_empty_containers(self):
        assert cap_strings({}, 1) == {}
        assert cap_strings([], 1) == []


class _Dump:
    def model_dump(self):
        return {"k": ("a", "b")}


class _BadDump:
    def model_dump(self):
        raise RuntimeError("nope")

    def __str__(self):
        return "bad-dump"


class TestToJsonable:
    def test_primitives(self):
        for v in (None, "s", 1, 1.5, True):
            assert to_jsonable(v) == v

    def test_dict_keys_are_stringified_and_collections_listed(self):
        assert to_jsonable({1: (1, 2), 2: {3}}) == {"1": [1, 2], "2": [3]}

    def test_pydantic_like_objects(self):
        assert to_jsonable(_Dump()) == {"k": ["a", "b"]}

    def test_failing_model_dump_falls_back_to_str(self):
        assert to_jsonable(_BadDump()) == "bad-dump"

    def test_arbitrary_object_becomes_str(self):
        assert to_jsonable(object()).startswith("<object")

    def test_datetime_is_str(self):
        assert to_jsonable(datetime(2026, 1, 1, tzinfo=timezone.utc)).startswith("2026-01-01")


class TestEmitter:
    def lines(self, buf):
        return [json.loads(line) for line in buf.getvalue().splitlines()]

    def test_line_shape_and_explicit_timestamp(self):
        out = io.StringIO()
        at = datetime(2026, 10, 3, 1, 2, 3, tzinfo=timezone.utc)
        Emitter(out).emit("x", {"a": 1}, at)
        (line,) = self.lines(out)
        assert line == {"type": "x", "at": at.isoformat(), "payload": {"a": 1}}

    def test_default_timestamp_is_utc_now(self):
        out = io.StringIO()
        Emitter(out).emit("x", {})
        assert datetime.fromisoformat(self.lines(out)[0]["at"]).tzinfo is not None

    def test_non_ascii_not_escaped(self):
        out = io.StringIO()
        Emitter(out).emit("x", {"t": "héllo 😀"})
        assert "héllo 😀" in out.getvalue()

    def test_tool_finished_capped_at_tool_limit_but_transcript_full(self):
        stream, transcript = io.StringIO(), io.StringIO()
        long = "q" * (em.MAX_TOOL_OUTPUT_CHARS + 1)
        exact = "r" * em.MAX_TOOL_OUTPUT_CHARS
        e = Emitter(stream, transcript)
        e.emit("tool_finished", {"output": long, "nested": [exact]})
        live, full = self.lines(stream)[0], self.lines(transcript)[0]
        assert "truncated 1 characters" in live["payload"]["output"]
        assert live["payload"]["nested"] == [exact]
        assert full["payload"]["output"] == long

    def test_other_types_use_llm_limit(self, monkeypatch):
        monkeypatch.setattr(em, "MAX_LLM_TEXT_CHARS", 50)
        monkeypatch.setattr(em, "STREAM_LIMITS", {"tool_finished": 5})
        stream, transcript = io.StringIO(), io.StringIO()
        Emitter(stream, transcript).emit("llm_call", {"response": "z" * 60})
        assert "truncated 10 characters" in self.lines(stream)[0]["payload"]["response"]
        assert self.lines(transcript)[0]["payload"]["response"] == "z" * 60

    @pytest.mark.parametrize("kind", ["crew_completed", "crew_failed"])
    def test_final_events_never_capped(self, kind):
        stream = io.StringIO()
        big = "w" * (em.MAX_LLM_TEXT_CHARS + 10)
        Emitter(stream).emit(kind, {"output": big})
        assert self.lines(stream)[0]["payload"]["output"] == big

    def test_transcript_write_error_disables_transcript_but_stream_continues(self):
        class Broken(io.StringIO):
            def write(self, s):
                raise OSError("disk full")

        stream = io.StringIO()
        e = Emitter(stream, Broken())
        e.emit("a", {})
        e.emit("b", {})
        assert [l["type"] for l in self.lines(stream)] == ["a", "b"]
        assert e._transcript is None

    def test_transcript_closed_stream_valueerror_is_tolerated(self):
        transcript = io.StringIO()
        transcript.close()
        stream = io.StringIO()
        Emitter(stream, transcript).emit("a", {})
        assert len(self.lines(stream)) == 1

    def test_stream_flushed_each_event(self):
        class Spy(io.StringIO):
            flushes = 0

            def flush(self):
                Spy.flushes += 1

        Spy.flushes = 0
        s, t = Spy(), Spy()
        Emitter(s, t).emit("a", {})
        assert Spy.flushes == 2

    def test_close_closes_transcript_and_is_idempotent(self):
        t = io.StringIO()
        e = Emitter(io.StringIO(), t)
        e.close()
        assert t.closed and e._transcript is None
        e.close()

    def test_close_without_transcript(self):
        Emitter(io.StringIO()).close()

    def test_close_swallows_oserror(self):
        class Bad(io.StringIO):
            def close(self):
                raise OSError("x")

        e = Emitter(io.StringIO(), Bad())
        e.close()
        assert e._transcript is None

    def test_emit_after_close_still_streams(self):
        stream = io.StringIO()
        e = Emitter(stream, io.StringIO())
        e.close()
        e.emit("a", {})
        assert len(self.lines(stream)) == 1

    def test_concurrent_emits_do_not_interleave(self):
        import threading

        stream = io.StringIO()
        e = Emitter(stream)
        threads = [threading.Thread(target=lambda i=i: [e.emit("n", {"i": i, "pad": "p" * 200}) for _ in range(50)]) for i in range(8)]
        [t.start() for t in threads]
        [t.join() for t in threads]
        assert len(self.lines(stream)) == 400

    def test_large_payload_10k_items(self):
        stream = io.StringIO()
        Emitter(stream).emit("x", {"items": [{"v": str(i)} for i in range(10_000)]})
        assert len(self.lines(stream)[0]["payload"]["items"]) == 10_000
