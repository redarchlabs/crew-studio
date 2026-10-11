from __future__ import annotations

from collections import Counter
from datetime import timedelta

import pytest

from pmo_sim import commands, engine
from pmo_sim.bundle import STATUS_CATEGORY
from pmo_sim.engine import plan_day, unit_hash, wants_reply
from pmo_sim.km2 import KM2Error
from pmo_sim.state import add_workdays, is_workday, label_list, load_state, revert_events, with_label, without_label
from pmo_sim.tests.conftest import SEED, TODAY, install_schema, make_client, quiet, seed_project
from pmo_sim.tests.fake_km2 import FakeKM2

EVENT_KEYS = ("event_id", "issue_key", "kind", "from_value", "to_value", "actor_name", "sim_day", "sim_at")


def events(server: FakeKM2) -> list[tuple]:
    return sorted(tuple(e[k] for k in EVENT_KEYS) for e in server.store.all("trk_event"))


def issue_view(server: FakeKM2) -> dict[str, tuple]:
    return {
        i["identifier"]: (
            i["status"],
            i["status_category"],
            i["labels"],
            i["due_date"],
            i["completed_on"],
            i["activity_at"],
            i["title"],
        )
        for i in server.store.all("trk_issue")
    }


def advance(server: FakeKM2, days: int | None, **kw: object) -> list[str]:
    lines: list[str] = []
    assert commands.cmd_advance(make_client(server, **kw), TODAY, days, False, lines.append) == 0
    return lines


def fresh(seed: int = SEED) -> FakeKM2:
    server = FakeKM2().__enter__()
    install_schema(server)
    seed_project(server, seed)
    return server


def test_same_seed_and_days_give_the_same_events() -> None:
    a, b, c = fresh(), fresh(), fresh(seed=7)
    try:
        for server in (a, b, c):
            advance(server, 10)
        assert events(a) and events(a) == events(b)
        assert issue_view(a) == {k: v for k, v in issue_view(b).items()}
        assert events(a) != events(c)
        ids = [e[0] for e in events(a)]
        assert len(ids) == len(set(ids))
        days = {e[6] for e in events(a)}
        assert days <= set(range(22, 32))
    finally:
        for server in (a, b, c):
            server.__exit__()


def test_weekends_are_quiet(seeded: FakeKM2) -> None:
    lines = advance(seeded, 7)
    weekend_days = [22 + i for i in range(7) if not is_workday(TODAY + timedelta(days=1 + i))]
    assert len(weekend_days) == 2
    by_day = Counter(e[6] for e in events(seeded))
    assert all(by_day.get(d, 0) == 0 for d in weekend_days)
    assert sum(1 for line in lines if "quiet" in line) == 2
    sim = seeded.store.all("trk_sim_state")[0]
    assert (sim["sim_day"], sim["sim_date"]) == (28, (TODAY + timedelta(days=7)).isoformat())


def test_plan_day_returns_nothing_on_saturday(seeded: FakeKM2) -> None:
    state = load_state(make_client(seeded))
    saturday = next(d for d in range(22, 30) if state.sim_date_for(d).weekday() == 5)
    assert plan_day(state, saturday, SEED) == []


def test_rerun_after_a_crash_mid_day_converges_to_the_clean_run() -> None:
    clean, crashed = fresh(), fresh()
    try:
        advance(clean, 3)
        crashed.store.fail_after_writes = crashed.store.writes + 23  # dies part-way through day 22
        with pytest.raises(KM2Error):
            commands.cmd_advance(make_client(crashed, max_retries=0), TODAY, 3, False, quiet)
        assert crashed.store.all("trk_sim_state")[0]["sim_day"] == 21
        partial = len(events(crashed))
        assert 0 < partial < len(events(clean))
        crashed.store.fail_after_writes = None
        lines = advance(crashed, 3)
        assert lines[0].startswith("Resuming day 22")
        assert events(crashed) == events(clean)
        assert issue_view(crashed) == issue_view(clean)
        bodies = Counter(
            (c["issue_key"], c["author_name"], c["sim_posted_at"]) for c in crashed.store.all("trk_comment")
        )
        assert max(bodies.values()) == 1
        assert crashed.store.count("trk_comment") == clean.store.count("trk_comment")
        assert crashed.store.count("trk_issue_link") == clean.store.count("trk_issue_link")
    finally:
        clean.__exit__()
        crashed.__exit__()


def test_rerun_after_crash_between_event_and_change(seeded: FakeKM2) -> None:
    """Crash right after an op's events were written: the change itself is ensured on the re-run."""
    reference = fresh()
    try:
        advance(reference, 2)
        writes = seeded.store.writes
        seeded.store.fail_after_writes = writes + 1  # first event of day 22 lands, then everything fails
        with pytest.raises(KM2Error):
            commands.cmd_advance(make_client(seeded, max_retries=0), TODAY, 2, False, quiet)
        seeded.store.fail_after_writes = None
        advance(seeded, 2)
        assert events(seeded) == events(reference)
        assert issue_view(seeded) == issue_view(reference)
    finally:
        reference.__exit__()


def test_advance_dry_run_writes_nothing(seeded: FakeKM2) -> None:
    before = seeded.store.writes
    lines: list[str] = []
    client = make_client(seeded, read_only=True)
    assert commands.cmd_advance(client, TODAY, 5, True, lines.append) == 0
    assert seeded.store.writes == before
    assert lines[-1].endswith("(dry run: nothing written)")
    assert seeded.store.all("trk_sim_state")[0]["sim_day"] == 21


def test_advance_without_days_catches_up_to_today(seeded: FakeKM2) -> None:
    lines: list[str] = []
    client = make_client(seeded)
    assert commands.cmd_advance(client, TODAY, None, False, lines.append) == 0
    assert lines[0].startswith("Already at sim day 21")
    assert commands.cmd_advance(client, TODAY + timedelta(days=3), None, False, lines.append) == 0
    assert seeded.store.all("trk_sim_state")[0]["sim_day"] == 24


def test_bot_comments_get_deterministic_replies(seeded: FakeKM2) -> None:
    client = make_client(seeded)
    people = {p["email"]: p for p in seeded.store.all("trk_person")}
    bot = people["pmo-bot@example.test"]
    issues = {i["identifier"]: i for i in seeded.store.all("trk_issue")}
    targets = ["ERP-29", "ERP-31", "ERP-33", "ERP-35", "ERP-40", "ERP-41", "ERP-44", "ERP-45"]
    posted = []
    for key in targets:
        issue = issues[key]
        mentions = people["aisha.bello@example.test"]["id"] if key == "ERP-44" else ""
        posted.append(
            client.create_record(
                "trk_comment",
                {
                    "body": f"@owner any update on {key}?",
                    "issue": issue["id"],
                    "issue_key": key,
                    "project_key": "ERP",
                    "author": bot["id"],
                    "author_name": bot["name"],
                    "mentions": mentions,
                },
            )
        )
    expected = {c["id"] for c in posted if wants_reply(SEED, c["id"])}
    advance(seeded, 1)
    replies = [e for e in seeded.store.all("trk_event") if e["event_id"].startswith("reply.")]
    assert {e["event_id"].removeprefix("reply.") for e in replies} == expected
    names = {p["id"]: p["name"] for p in people.values()}
    for reply in replies:
        comment = next(c for c in posted if c["id"] == reply["event_id"].removeprefix("reply."))
        issue = issues[comment["issue_key"]]
        responder = "Aisha Bello" if comment["mentions"] else names.get(issue["assignee"], "Dana Whitfield")
        assert reply["actor_name"] == responder
    reply_comments = [
        c
        for c in seeded.store.all("trk_comment")
        if c["author_name"] != bot["name"]
        and c["sim_posted_at"]
        and c["sim_posted_at"].startswith((TODAY + timedelta(1)).isoformat())
    ]
    assert len(reply_comments) >= len(expected)
    count = seeded.store.count("trk_comment")
    advance(seeded, 2)  # no second reply to the same bot comment
    replies_after = [e for e in seeded.store.all("trk_event") if e["event_id"].startswith("reply.")]
    assert len(replies_after) == len(replies)
    assert seeded.store.count("trk_comment") >= count


def test_long_run_invariants(seeded: FakeKM2) -> None:
    advance(seeded, 63)  # to the end of the planned ten weeks
    issues = seeded.store.all("trk_issue")
    by_key = {i["identifier"]: i for i in issues}
    for issue in issues:
        assert issue["status_category"] == STATUS_CATEGORY[issue["status"]]
        assert (issue["completed_on"] is not None) == (issue["status"] == "Done"), issue["identifier"]
    epics = [i for i in issues if i["issue_type"] == "Epic"]
    for epic in epics:
        children = [i for i in issues if i["parent_key"] == epic["identifier"]]
        if children and all(c["status"] in ("Done", "Canceled", "Duplicate") for c in children):
            assert epic["status"] == "Done", epic["identifier"]
    kinds = Counter(e["kind"] for e in seeded.store.all("trk_event"))
    assert kinds["created"] >= 3  # new bugs / scope tickets
    assert kinds["link_added"] >= 1 and kinds["link_removed"] + kinds["labels"] >= 1
    assert kinds["assignee"] >= 1 and kinds["due_date"] >= 1 and kinds["cycle"] >= 1
    assert kinds["comment"] >= 30
    done = [i for i in issues if i["status"] == "Done"]
    assert len(done) > 40
    # started work never had open dependencies when it started
    started = [
        e
        for e in seeded.store.all("trk_event")
        if e["kind"] == "status" and e["to_value"] == "In Progress" and e["from_value"] == "Todo"
    ]
    assert started
    new = [i for i in issues if i["number"] > 99]
    assert all(by_key[i["parent_key"]]["issue_type"] == "Epic" for i in new if i["parent_key"])
    sim = seeded.store.all("trk_sim_state")[0]
    assert sim["sim_day"] == 84


def test_revert_events_restores_start_of_day(seeded: FakeKM2) -> None:
    client = make_client(seeded)
    before = load_state(client)
    snapshot = {
        k: (i.status, i.labels, i.assignee, i.due_date, i.cycle, i.start_date, i.completed_on)
        for k, i in before.issues.items()
    }
    links_before = {(lk.from_key, lk.to_key, lk.kind, lk.active) for lk in before.links}
    advance(seeded, 1)
    # Pretend day 22 never finished: rewind the clock, then load (which reverts the day's events in memory).
    sim = seeded.store.all("trk_sim_state")[0]
    client.update_record("trk_sim_state", sim["id"], {"sim_day": 21})
    after = load_state(client)
    assert after.pending_events
    restored = {
        k: (i.status, i.labels, i.assignee, i.due_date, i.cycle, i.start_date, i.completed_on)
        for k, i in after.issues.items()
    }
    assert restored == snapshot
    assert {(lk.from_key, lk.to_key, lk.kind, lk.active) for lk in after.links} == links_before


def test_revert_handles_created_and_link_events(seeded: FakeKM2) -> None:
    state = load_state(make_client(seeded))
    key = "ERP-29"
    count = len(state.issues)
    link = state.links[0]
    revert_events(
        state,
        [
            {
                "event_id": "a",
                "kind": "link_removed",
                "issue_key": link.to_key,
                "from_value": link.from_key,
                "to_value": link.kind,
                "sim_at": "2026-10-06T13:01:00+00:00",
            },
            {"event_id": "b", "kind": "created", "issue_key": key, "sim_at": "2026-10-06T13:00:00+00:00"},
            {"event_id": "c", "kind": "status", "issue_key": "ERP-404", "from_value": "Todo"},
        ],
    )
    assert len(state.issues) == count - 1 and key not in state.issues
    assert next(lk for lk in state.links if lk.id == link.id).active


def test_helpers() -> None:
    assert add_workdays(TODAY, 1) == TODAY + timedelta(days=1)
    friday = TODAY + timedelta(days=4)
    assert add_workdays(friday, 1) == friday + timedelta(days=3)
    assert label_list(" a, ,b ") == ["a", "b"]
    assert with_label("a", "blocked") == "a, blocked" and with_label("a, blocked", "blocked") == "a, blocked"
    assert without_label("a, blocked", "blocked") == "a"
    assert 0 <= unit_hash("x", 1) < 1 and unit_hash("x", 1) == unit_hash("x", 1)
    assert engine.day_seed(1, 2) == engine.day_seed(1, 2) != engine.day_seed(1, 3)
