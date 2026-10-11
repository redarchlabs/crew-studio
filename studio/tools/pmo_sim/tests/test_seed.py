from __future__ import annotations

from datetime import date, timedelta

import pytest

from pmo_sim import blueprint as bp
from pmo_sim import commands
from pmo_sim.bundle import STATUS_CATEGORY
from pmo_sim.seed import build_issues
from pmo_sim.state import load_state
from pmo_sim.tests.conftest import SEED, TODAY, make_client, quiet, seed_project
from pmo_sim.tests.fake_km2 import FakeKM2

START = TODAY - timedelta(days=bp.SEED_DAY)


def test_blueprint_is_consistent() -> None:
    keys = [t.key for t in bp.TASKS]
    assert len(keys) == len(set(keys)) and 80 <= len(keys) <= 90
    assert 12 <= len(bp.EPICS) <= 13
    for task in bp.TASKS:
        assert task.epic in bp.EPIC_BY_KEY
        assert task.owner in bp.PERSON_BY_HANDLE and task.owner != bp.BOT
        assert task.issue_type in ("Story", "Task", "Bug") and task.estimate in (1, 2, 3, 5, 8)
        for dep in (*task.deps, *task.blocked_by):
            assert keys.index(dep) < keys.index(task.key), (dep, task.key)
        if task.state in ("prog", "review", "todo"):  # started or startable work has its dependencies done
            done = {t.key for t in bp.TASKS if t.state == "done"}
            assert all(d in done for d in task.deps) or task.state == "todo", task.key
    assert sum(p.is_bot for p in bp.PEOPLE) == 1
    assert all(p.email.endswith("@example.test") for p in bp.PEOPLE)
    assert len([p for p in bp.PEOPLE if not p.is_bot]) == 8


def test_build_issues_day21_snapshot() -> None:
    issues = build_issues(START)
    assert [i.number for i in issues] == list(range(1, len(issues) + 1))
    by_key = {i.identifier: i for i in issues}
    assert by_key["ERP-1"].fields["title"] == bp.UMBRELLA_TITLE
    epics = [i for i in issues if i.fields["issue_type"] == "Epic"]
    assert len(epics) == 1 + len(bp.EPICS)
    assert all(i.parent_key == "ERP-1" for i in epics[1:])
    assert all(i.fields["status_category"] == STATUS_CATEGORY[i.fields["status"]] for i in issues)
    status = {i.task_key: i.fields["status"] for i in issues}
    assert status["epic:disc"] == "Done"
    assert status["epic:map"] == "In Progress" and status["epic:auth"] == "In Progress"
    assert status["epic:cut"] == "Backlog"
    overdue = [
        i
        for i in issues
        if i.fields["status"] not in ("Done",)
        and i.fields["due_date"]
        and date.fromisoformat(i.fields["due_date"]) < TODAY
        and i.fields["issue_type"] != "Epic"
    ]
    assert 2 <= len(overdue) <= 3
    blocked = [i for i in issues if "blocked" in i.fields["labels"]]
    assert [b.task_key for b in blocked] == ["auth.erp_token"]
    unassigned = [i for i in issues if i.assignee is None and i.fields["status"] == "Todo"]
    assert len(unassigned) >= 2


def test_seed_creates_expected_records(seeded: FakeKM2) -> None:
    store = seeded.store
    assert store.count("trk_person") == 9
    assert store.count("trk_project") == 1
    assert store.count("trk_cycle") == 5
    assert store.count("trk_issue") == 1 + len(bp.EPICS) + len(bp.TASKS)
    assert store.count("trk_issue_link") == sum(len(t.deps) + len(t.blocked_by) for t in bp.TASKS)
    assert store.count("trk_comment") == len(bp.SEED_COMMENTS)
    assert store.count("trk_sim_state") == 1
    assert store.count("trk_event") == 0

    people = {p["id"]: p for p in store.all("trk_person")}
    bot = [p for p in people.values() if p["is_bot"]]
    assert [b["name"] for b in bot] == ["Crew Studio PMO"]
    project = store.all("trk_project")[0]
    assert project["start_date"] == START.isoformat()
    assert project["target_date"] == (START + timedelta(days=69)).isoformat()
    assert people[project["lead"]]["email"] == "dana.whitfield@example.test"
    cycles = sorted(store.all("trk_cycle"), key=lambda c: c["number"])
    assert cycles[0]["starts_on"] == START.isoformat() and cycles[4]["ends_on"] == (START + timedelta(69)).isoformat()

    issues = {i["identifier"]: i for i in store.all("trk_issue")}
    ids = {i["id"]: i for i in issues.values()}
    for issue in issues.values():
        assert issue["project"] == project["id"] and issue["project_key"] == "ERP"
        if issue["parent_key"]:
            assert ids[issue["parent"]]["identifier"] == issue["parent_key"]
        assert issue["assignee"] is None or not people[issue["assignee"]]["is_bot"]
    token = next(i for i in issues.values() if i["title"].startswith("Implement token-based auth"))
    links = [lk for lk in store.all("trk_issue_link") if lk["to_key"] == token["identifier"]]
    assert {ids[lk["from_issue"]]["title"] for lk in links} == {"Obtain ERP vendor sandbox credentials and API user"}
    comments = store.all("trk_comment")
    assert all(c["issue"] in ids and c["author"] in people for c in comments)
    mentions = [c for c in comments if c["mentions"]]
    assert mentions and all(m in people for c in mentions for m in c["mentions"].split(","))
    sim = store.all("trk_sim_state")[0]
    assert (sim["sim_day"], sim["sim_date"], sim["seed"]) == (21, TODAY.isoformat(), SEED)


def test_seed_is_idempotent(seeded: FakeKM2) -> None:
    before = seeded.store.writes
    lines: list[str] = []
    assert commands.cmd_seed(make_client(seeded), TODAY + timedelta(days=3), SEED, False, lines.append) == 0
    assert seeded.store.writes == before
    assert "nothing to create" in lines[-1]
    assert seeded.store.all("trk_project")[0]["start_date"] == START.isoformat()


def test_seed_resumes_after_partial_failure(schema: FakeKM2) -> None:
    schema.store.fail_after_writes = 60
    with pytest.raises(Exception, match="503"):
        commands.cmd_seed(make_client(schema, max_retries=0), TODAY, SEED, False, quiet)
    schema.store.fail_after_writes = None
    seed_project(schema)
    assert schema.store.count("trk_issue") == 1 + len(bp.EPICS) + len(bp.TASKS)
    assert len({i["identifier"] for i in schema.store.all("trk_issue")}) == schema.store.count("trk_issue")


def test_seed_refuses_after_advance(seeded: FakeKM2) -> None:
    commands.cmd_advance(make_client(seeded), TODAY, 1, False, quiet)
    lines: list[str] = []
    assert commands.cmd_seed(make_client(seeded), TODAY, SEED, False, lines.append) == 1
    assert "reset --yes" in lines[-1]


def test_seed_dry_run_writes_nothing(schema: FakeKM2) -> None:
    lines: list[str] = []
    client = make_client(schema, read_only=True)
    assert commands.cmd_seed(client, TODAY, SEED, True, lines.append) == 0
    assert schema.store.writes == 0
    assert any("would create   99 trk_issue" in line for line in lines)


def test_seed_without_schema(fake: FakeKM2) -> None:
    lines: list[str] = []
    assert commands.cmd_seed(make_client(fake), TODAY, SEED, False, lines.append) == 1
    assert lines[-1] == "Run `bootstrap-schema --apply` first."
    lines.clear()
    assert commands.cmd_seed(make_client(fake, read_only=True), TODAY, SEED, True, lines.append) == 0
    assert lines[0].startswith("schema not installed yet")


def test_schema_with_missing_field_is_reported(schema: FakeKM2) -> None:
    schema.store.entities["trk_issue"]["fields"] = [
        f for f in schema.store.entities["trk_issue"]["fields"] if f["slug"] != "activity_at"
    ]
    lines: list[str] = []
    assert commands.cmd_seed(make_client(schema), TODAY, SEED, False, lines.append) == 1
    assert "schema: entity trk_issue lacks fields: activity_at" in lines


def test_loaded_state_matches_seed(seeded: FakeKM2) -> None:
    state = load_state(make_client(seeded))
    assert state.sim.sim_day == 21 and state.sim.sim_date == TODAY
    assert len(state.workers()) == 8 and [b.name for b in state.bots()] == ["Crew Studio PMO"]
    assert state.current_cycle(TODAY).number == 2
    assert state.lead().name == "Dana Whitfield"
    token = next(i for i in state.issues.values() if i.title.startswith("Implement token-based auth"))
    assert token.blocked and [lk.from_key for lk in state.blockers(token.identifier)]
    assert state.epic_group(token) == "eng"
