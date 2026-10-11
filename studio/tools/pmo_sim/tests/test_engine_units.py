from __future__ import annotations

from datetime import UTC, date, datetime, timedelta

import pytest

from pmo_sim import commands, engine, texts
from pmo_sim.engine import plan_day
from pmo_sim.state import StateError, decode_value, encode_value, load_state, parse_date, parse_dt
from pmo_sim.tests.conftest import SEED, TODAY, make_client, quiet
from pmo_sim.tests.fake_km2 import FakeKM2


def test_forced_reassignment_and_new_ticket(seeded: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(engine, "P_REASSIGN", 1.0)
    monkeypatch.setattr(engine, "P_NEW_TICKET", 1.0)
    state = load_state(make_client(seeded))
    ops = plan_day(state, 22, SEED)
    kinds = [op.kind for op in ops]
    assert "reassign" in kinds and "new" in kinds
    reassign = next(op for op in ops if op.kind == "reassign")
    assert reassign.comment is not None and reassign.comment.author == state.lead().id
    assert [e.kind for e in reassign.events] == ["assignee", "comment"]
    new = next(op for op in ops if op.kind == "new")
    assert new.create is not None and new.create["identifier"] == "ERP-100"
    assert new.create["status_category"] in ("to_do",)
    assert new.events[0].event_id == f"d022.{new.seq:03d}.created.ERP-100"


def test_new_ticket_skipped_when_pools_are_used_up(seeded: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(engine, "P_NEW_TICKET", 1.0)
    monkeypatch.setattr(texts, "NEW_BUGS", ())
    monkeypatch.setattr(texts, "NEW_SCOPE", ())
    state = load_state(make_client(seeded))
    assert all(op.kind != "new" for op in plan_day(state, 22, SEED))


def test_cycle_starting_on_a_weekend_is_planned_on_the_next_workday() -> None:
    saturday = date(2026, 10, 3)
    with FakeKM2() as server:
        from pmo_sim.tests.conftest import install_schema

        install_schema(server)
        assert commands.cmd_seed(make_client(server), saturday, SEED, False, quiet) == 0
        lines: list[str] = []
        assert commands.cmd_advance(make_client(server), saturday, 9, False, lines.append) == 0
        # day 28 (cycle 3 start) is a Saturday; the carry-over happens on Monday, day 30
        carry_days = {e["sim_day"] for e in server.store.all("trk_event") if e["kind"] == "cycle"}
        assert carry_days == {30}
        assert any("carry" in line for line in lines if line.startswith("day  30"))


def test_value_encoding_round_trip(seeded: FakeKM2) -> None:
    state = load_state(make_client(seeded))
    person = state.workers()[0]
    cycle = state.cycles[1]
    assert encode_value(state, "assignee", person.id) == person.name
    assert decode_value(state, "assignee", person.name) == person.id
    assert decode_value(state, "assignee", "Nobody Here") is None
    assert encode_value(state, "cycle", cycle.id) == cycle.name
    assert decode_value(state, "cycle", cycle.name) == cycle.id
    assert decode_value(state, "cycle", "Cycle 99") is None
    assert encode_value(state, "cycle", "missing") == ""
    assert encode_value(state, "due_date", date(2026, 1, 2)) == "2026-01-02"
    assert decode_value(state, "due_date", "2026-01-02") == date(2026, 1, 2)
    assert encode_value(state, "labels", None) == ""
    assert decode_value(state, "labels", "") == "" and decode_value(state, "status", "") is None
    assert decode_value(state, "status", "Done") == "Done"


def test_parsers() -> None:
    assert parse_date(date(2026, 1, 1)) == date(2026, 1, 1)
    assert parse_date("2026-01-01T10:00:00+00:00") == date(2026, 1, 1)
    assert parse_date(None) is None
    naive = datetime(2026, 1, 1, 9)
    assert parse_dt(naive).tzinfo is UTC
    assert parse_dt("2026-01-01T09:00:00Z") == datetime(2026, 1, 1, 9, tzinfo=UTC)
    assert parse_dt("") is None


def test_load_state_errors(schema: FakeKM2) -> None:
    client = make_client(schema)
    with pytest.raises(StateError, match="no trk_project"):
        load_state(client)
    project = client.create_record("trk_project", {"key": "ERP", "name": "x"})
    with pytest.raises(StateError, match="no start_date"):
        load_state(client)
    client.update_record("trk_project", project["id"], {"start_date": "2026-09-14"})
    with pytest.raises(StateError, match="no trk_sim_state"):
        load_state(client)


def test_state_lookups_without_cycles_or_lead(seeded: FakeKM2) -> None:
    state = load_state(make_client(seeded))
    first, last = state.cycles[0], state.cycles[-1]
    assert state.current_cycle(first.starts_on - timedelta(days=5)) == first
    assert state.current_cycle(last.ends_on + timedelta(days=5)) == last
    state.cycles = []
    assert state.current_cycle(TODAY) is None
    state.project = type(state.project)(**{**state.project.__dict__, "lead": None})
    assert state.lead() is not None


def test_pending_bot_comments_filters(seeded: FakeKM2) -> None:
    client = make_client(seeded)
    state = load_state(client)
    assert commands.pending_bot_comments(client, state) == []
    bot = state.bots()[0]
    other = client.create_record("trk_project", {"key": "OPS", "name": "Other"})
    stray = client.create_record(
        "trk_issue", {"identifier": "OPS-1", "project_key": "OPS", "status_category": "to_do", "project": other["id"]}
    )
    client.create_record("trk_comment", {"body": "?", "issue": stray["id"], "issue_key": "OPS-1", "author": bot.id})
    assert commands.pending_bot_comments(client, state) == []  # not on this project
    state.people = {k: v for k, v in state.people.items() if not v.is_bot}
    assert commands.pending_bot_comments(client, state) == []


def test_replies_ignore_unknown_issues(seeded: FakeKM2) -> None:
    state = load_state(make_client(seeded))
    planner = engine.DayPlanner(state, 22, SEED)
    planner.replies([{"id": "x", "issue": None, "issue_key": "ERP-999"}])
    assert planner.ops == []
    key = "ERP-15"
    planner.replies([{"id": "y", "issue": None, "issue_key": key, "mentions": ""}])
    assert planner.ops[-1].events[-1].event_id == "reply.y"


def _rewind(server: FakeKM2, day: int) -> None:
    client = make_client(server)
    sim = server.store.all("trk_sim_state")[0]
    client.update_record("trk_sim_state", sim["id"], {"sim_day": day, "sim_date": None})


def test_resume_recreates_a_missing_reply_comment(seeded: FakeKM2) -> None:
    client = make_client(seeded)
    state = load_state(client)
    bot = state.bots()[0]
    issue = state.issues["ERP-29"]
    posted = []
    for n in range(6):
        posted.append(
            client.create_record(
                "trk_comment",
                {
                    "body": f"status? {n}",
                    "issue": issue.id,
                    "issue_key": "ERP-29",
                    "author": bot.id,
                    "author_name": bot.name,
                },
            )
        )
    assert commands.cmd_advance(client, TODAY, 1, False, quiet) == 0
    replies = [
        c
        for c in seeded.store.all("trk_comment")
        if c["author_name"] != bot.name
        and c["sim_posted_at"]
        and c["sim_posted_at"].startswith("2026-10-06")
        and "status?" not in c["body"]
    ]
    expected = sum(engine.wants_reply(SEED, c["id"]) for c in posted)
    assert expected >= 1
    # Crash simulation: reply events exist, one reply comment is gone, and day 22 never finished.
    victim = next(
        c
        for c in replies
        if any(
            e["event_id"].startswith("reply.") and e["sim_at"] == c["sim_posted_at"]
            for e in seeded.store.all("trk_event")
        )
    )
    client.delete_record("trk_comment", victim["id"])
    _rewind(seeded, 21)
    count = seeded.store.count("trk_comment")
    assert commands.cmd_advance(client, TODAY, 1, False, quiet) == 0
    assert seeded.store.count("trk_comment") == count + 1
    reply_events = [e for e in seeded.store.all("trk_event") if e["event_id"].startswith("reply.")]
    assert len(reply_events) == expected


def test_resume_links_the_created_event_to_the_new_issue(seeded: FakeKM2, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(engine, "P_NEW_TICKET", 1.0)
    client = make_client(seeded)
    assert commands.cmd_advance(client, TODAY, 1, False, quiet) == 0
    created = next(e for e in seeded.store.all("trk_event") if e["kind"] == "created")
    new_issue = next(i for i in seeded.store.all("trk_issue") if i["identifier"] == created["issue_key"])
    # Crash simulation: the event was written, the issue never was.
    client.update_record("trk_event", created["id"], {"issue": None})
    client.delete_record("trk_issue", new_issue["id"])
    _rewind(seeded, 21)
    assert commands.cmd_advance(client, TODAY, 1, False, quiet) == 0
    again = next(i for i in seeded.store.all("trk_issue") if i["identifier"] == created["issue_key"])
    event = next(e for e in seeded.store.all("trk_event") if e["id"] == created["id"])
    assert event["issue"] == again["id"]
    assert sum(1 for e in seeded.store.all("trk_event") if e["kind"] == "created") == 1


def test_more_than_one_sim_state_is_an_error(seeded: FakeKM2) -> None:
    client = make_client(seeded)
    client.create_record("trk_sim_state", {"sim_day": 3})
    with pytest.raises(StateError, match="expected exactly one"):
        load_state(client)
