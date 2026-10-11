"""Studio-stored memory, review fixes: quoting and invisible characters, ranking weights over an oversampled recall,
saves that never scan the crew, which memory tools each mode offers, embedding a memory once, copying, and the
per-record save outcomes of POST /memory/records."""

import copy
from datetime import datetime
from pathlib import Path
from typing import Any

import pytest
from crewai.memory.types import MemoryRecord
from crewai.utilities.i18n import I18N_DEFAULT

from studio_runner import memory as mem
from studio_runner.build import build_crew
from studio_runner.memory import MemoryRememberTool, QuotedMatch, StudioEmbedder, StudioMemoryStorage, safe_metadata, to_wire

from .test_memory import CountingLLM, FakeStudio, cosine, letters, memory_for, memory_spec, tools


@pytest.fixture
def fake() -> FakeStudio:
    return FakeStudio()


@pytest.fixture
def keep_prompt():
    original = I18N_DEFAULT._prompts["slices"]["memory"]
    yield
    I18N_DEFAULT._prompts["slices"]["memory"] = original


def count(fake: FakeStudio, method: str, path: str) -> int:
    return sum(1 for m, p, _, _ in fake.requests if (m, p) == (method, path))


# ------------------------------------------------------------------ 1. quoting and invisible characters


def test_quoted_match_neutralises_writer_task_and_content():
    record = MemoryRecord(
        content="line two three ​zero﻿ ‮evil‬ bell\x07 tab\tend",
        scope="/", source='user:Eve‮"x\nforged⁦', created_at=datetime(2026, 10, 3),
        metadata={"task": "Sweep ‏\x1b[31m"},
    )
    line = QuotedMatch(record=record, score=0.5).format()
    assert line == ('- Memory saved 2026-10-03 by Eve\\"x\\nforged, task "Sweep\\n [31m", scope /: '
                    '"line\\ntwo\\nthree zero evil bell  tab end"')
    for bad in ("\n", "\r", " ", " ", "​", "‏", "‮", "⁦", "﻿", "\x07", "\x1b", "\t"):
        assert bad not in line


def test_clip_and_metadata_drop_invisible_characters():
    assert mem.clean_text("a​b c\x00d\te\r\nf‮ g﻿") == "ab\ncd\te\nf\ng"
    assert mem.clip("  ​keep⁦ ") == "keep"
    # Stripped before clipping: invisible padding does not eat into the 2000 characters
    assert mem.clip("​" * 50 + "x" * mem.MAX_CONTENT) == "x" * mem.MAX_CONTENT
    meta = safe_metadata({"ta​sk": "v w‮", "l": ["a⁦b", 1, {"k": "​"}], "o": {"x": " "}})
    assert meta["task"] == "v\nw" and meta["l"][:2] == ["ab", 1]
    for value in (meta["l"][2], meta["o"]):
        assert not any(ch in value for ch in "​  ‮⁦")
    wire = to_wire(MemoryRecord(content="Sam replies​ late", metadata={"agent": "Co‮ord"}))
    assert wire["content"] == "Sam\nreplies late" and wire["metadata"] == {"agent": "Coord"}


# ------------------------------------------------------------------ 2. ranking weights over an oversampled recall


def test_recall_oversamples_so_recency_and_importance_count(fake: FakeStudio):
    old = [fake.add(f"sam replies late fridays {'x' * i}", days_old=400, importance=0.05) for i in range(5)]
    fresh = fake.add("sam replied at noon today about the vendor", days_old=0, importance=1.0)
    memory = memory_for(fake, CountingLLM(), recall_limit=5, semantic_weight=0.2, recency_weight=0.5,
                        importance_weight=0.3, recency_half_life_days=7)
    query = "sam replies late fridays"
    by_cosine = sorted(fake.records, key=lambda rid: -cosine(letters(query), letters(fake.records[rid]["content"])))
    assert by_cosine[-1] == fresh  # the fake's own search would leave it out of a top 5
    matches = memory.recall(query, limit=50)
    ids = [m.record.id for m in matches]
    assert len(ids) == 5 and ids[0] == fresh and set(ids) < set(old) | {fresh}
    searches = [b for m, p, b, _ in fake.requests if p == "/memory/search"]
    assert searches[-1]["limit"] == 20  # 4 x recall_limit, capped at 50
    touches = [b for m, p, b, _ in fake.requests if p == "/memory/touch"]
    assert touches == [{"record_ids": ids}]


def test_recall_caps_oversampling_and_read_only_never_touches(fake: FakeStudio):
    for i in range(3):
        fake.add(f"note {i}")
    memory = memory_for(fake, CountingLLM(), recall_limit=20, mode="recall_only")
    assert len(memory.recall("note", limit=30)) == 3
    assert [b["limit"] for _, p, b, _ in fake.requests if p == "/memory/search"] == [50]
    assert count(fake, "POST", "/memory/touch") == 0


def test_recall_survives_a_failed_touch(fake: FakeStudio):
    fake.add("Sam replies late")
    memory = memory_for(fake, CountingLLM())
    fake.fail["/memory/touch"] = (500, "boom")
    assert len(memory.recall("Sam")) == 1


def test_storage_touch_still_works_outside_recall(fake: FakeStudio):
    StudioMemoryStorage(fake.client()).touch_records(["a"])
    assert count(fake, "POST", "/memory/touch") == 1


# ------------------------------------------------------------------ 3. saves never scan the crew


def test_saves_make_no_search_calls(fake: FakeStudio):
    fake.add("Sam usually replies late on Fridays")
    memory = memory_for(fake, CountingLLM())
    memory.remember_many(["Sam replies late", "Vendor page under-reports", "Ada reviews on Mondays"],
                         agent_role="Coordinator", root_scope="/agent/coordinator")
    memory.drain_writes()
    assert count(fake, "POST", "/memory/search") == 0 and count(fake, "POST", "/memory/records") == 1
    assert memory.remember("Sam usually replies late on Fridays") is not None
    assert count(fake, "POST", "/memory/search") == 0 and count(fake, "POST", "/memory/records") == 2
    memory.recall("Sam")
    assert count(fake, "POST", "/memory/search") == 1


def test_async_search_is_skipped_while_saving(fake: FakeStudio):
    import asyncio

    storage = StudioMemoryStorage(fake.client())
    fake.add("x")
    token = mem._saving.set(True)
    try:
        assert storage.search(letters("x")) == [] and asyncio.run(storage.asearch(letters("x"))) == []
    finally:
        mem._saving.reset(token)
    assert count(fake, "POST", "/memory/search") == 0 and len(storage.search(letters("x"))) == 1


# ------------------------------------------------------------------ 4. memory tools per mode


def memory_tool_names(built: Any) -> list[list[str]]:
    crew = built.crew
    names = []
    for task in crew.tasks:
        prepared = crew._prepare_tools(task.agent, task, list(task.agent.tools or []))
        names.append(sorted(t.name for t in prepared if t.name in ("Search memory", "Save to memory", "Remember")))
    return names


@pytest.mark.parametrize("mode, expected", [
    ("recall_only", ["Search memory"]),
    ("explicit", ["Remember", "Search memory"]),
    ("automatic", ["Search memory"]),
])
def test_memory_tools_per_mode(fake: FakeStudio, tmp_path: Path, keep_prompt, mode: str, expected: list[str]):
    built = build_crew(memory_spec(mode), tools(tmp_path), state=fake.client())
    assert all(names == expected for names in memory_tool_names(built))
    # A copy of the crew keeps the same tools
    copied = built.crew.copy()
    assert type(copied) is type(built.crew)
    task = copied.tasks[0]
    prepared = copied._prepare_tools(task.agent, task, list(task.agent.tools or []))
    assert sorted(t.name for t in prepared if t.name in ("Search memory", "Save to memory", "Remember")) == expected


def test_crew_without_studio_memory_keeps_crewai_tools(fake: FakeStudio, tmp_path: Path):
    from crewai.memory.unified_memory import Memory

    built = build_crew(memory_spec("off"), tools(tmp_path), state=fake.client())
    plain = Memory(storage=StudioMemoryStorage(fake.client()), embedder=StudioEmbedder(fake.client()))
    assert [t.name for t in built.crew._add_memory_tools([], plain)] == ["Search memory", "Save to memory"]


# ------------------------------------------------------------------ 5. a memory is embedded once


def test_saved_memory_is_embedded_as_sent(fake: FakeStudio):
    memory = memory_for(fake, CountingLLM())
    long = "Sam replies late. " * 200 + "​"
    memory.remember(long)
    embedded = [b["texts"] for _, p, b, _ in fake.requests if p == "/memory/embed"]
    sent = [b["records"][0]["content"] for m, p, b, _ in fake.requests if (m, p) == ("POST", "/memory/records")]
    assert embedded == [[sent[0]]] and len(sent[0]) == mem.MAX_CONTENT
    memory.recall("Sam " * 3000)
    query = [b for _, p, b, _ in fake.requests if p == "/memory/embed"][-1]
    assert query["kind"] == "query" and len(query["texts"][0]) == mem.MAX_EMBED_CHARS


# ------------------------------------------------------------------ 6. copying


def test_deepcopy_memory_shares_the_state_client(fake: FakeStudio):
    memory = memory_for(fake, CountingLLM())
    clone = copy.deepcopy(memory)
    assert clone is not memory and isinstance(clone._storage, StudioMemoryStorage)
    assert clone._storage._state is memory._storage._state and clone._embedder._state is memory._embedder._state
    assert clone.recall_limit == memory.recall_limit
    assert clone.remember("Copied memories still save") is not None
    assert [m.record.content for m in clone.recall("Copied memories")][0] == "Copied memories still save"
    assert copy.deepcopy(StudioMemoryStorage(fake.client()))._state is not None


# ------------------------------------------------------------------ 7. per-record save outcomes


def test_remember_tool_review_and_skips(fake: FakeStudio):
    tool = MemoryRememberTool().configure(fake.client(), "Coordinator")
    fake.review_run_saves = True
    assert tool._run("Sam replies late") == "Saved. A person reviews it before later runs see it."
    fake.review_run_saves = False
    fake.skip_next = ["pending_full"]
    assert tool._run("x") == "Not saved: too many memories are waiting for a person to review them."
    # Nothing could be saved at all: Studio answers 429 with a code
    for code, text in (("memory_save_cap", "this run has reached the most"), ("memory_crew_full", "this crew holds the most"),
                       ("memory_pending_full", "waiting for a person")):
        fake.fail["/memory/records"] = (429, code)
        assert text in tool._run("x")
    fake.fail["/memory/records"] = (429, "rate_limited")
    assert tool._run("x") == "refused: rate_limited"


def test_storage_save_tolerates_skipped_entries(fake: FakeStudio):
    storage = StudioMemoryStorage(fake.client())
    fake.skip_next = ["save_cap"]
    storage.save([MemoryRecord(content="a"), MemoryRecord(content="b")])
    assert [r["content"] for r in fake.records.values()] == ["b"]


def test_deep_recall_also_oversamples_and_touches_only_kept(fake: FakeStudio):
    old = [fake.add(f"sam replies late fridays {'x' * i}", days_old=400, importance=0.05) for i in range(5)]
    fresh = fake.add("sam replied at noon today about the vendor", days_old=0, importance=1.0)
    llm = CountingLLM()
    memory = memory_for(fake, llm, recall_limit=5, recall_depth="deep", semantic_weight=0.2, recency_weight=0.5,
                        importance_weight=0.3, recency_half_life_days=7)
    ids = [m.record.id for m in memory.recall("sam replies late fridays", limit=50)]
    assert len(ids) == 5 and ids[0] == fresh and set(ids) < set(old) | {fresh}
    assert all(b["kind"] == "query" for _, p, b, _ in fake.requests if p == "/memory/embed")
    assert [b for _, p, b, _ in fake.requests if p == "/memory/touch"] == [{"record_ids": ids}]
