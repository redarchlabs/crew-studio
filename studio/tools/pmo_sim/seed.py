"""Seed the blueprint project into KM2 as its day-21 snapshot.

Idempotent: every record is matched by its natural key (person email, project key, cycle number, issue
identifier, link from/to/kind, comment issue/author/sim time) and only missing ones are created, so a seed that
stopped part-way can simply be run again.
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, date, datetime, time, timedelta
from typing import Any

from . import blueprint as bp
from .apply import Writer
from .bundle import DELETE_ORDER, ENTITIES, STATUS_CATEGORY, missing_fields
from .km2 import KM2Client
from .state import iso, parse_dt

STATE_STATUS = {"done": "Done", "review": "In Review", "prog": "In Progress", "todo": "Todo", "backlog": "Backlog"}
ACTIVITY_TIME = time(15, 0)
COMMENT_TIME = time(14, 0)


class SeedError(Exception):
    """The org is not ready for (or already past) seeding."""


@dataclass(frozen=True)
class IssueSeed:
    identifier: str
    number: int
    task_key: str  # blueprint key ("" for the umbrella, "epic:<key>" for epics)
    fields: dict[str, Any]
    parent_key: str
    assignee: str | None  # person handle
    creator: str
    cycle: int | None


def check_schema(client: KM2Client) -> list[str]:
    problems: list[str] = []
    for spec in ENTITIES:
        definition = client.get_entity(spec.slug)
        if definition is None:
            problems.append(f"entity {spec.slug} is missing")
            continue
        missing = missing_fields(spec.slug, definition)
        if missing:
            problems.append(f"entity {spec.slug} lacks fields: {', '.join(missing)}")
    return problems


def cycle_dates(start: date, number: int) -> tuple[date, date]:
    first = start + timedelta(days=(number - 1) * bp.CYCLE_DAYS)
    return first, first + timedelta(days=bp.CYCLE_DAYS - 1)


def _at(start: date, day: int, at: time = ACTIVITY_TIME) -> str:
    return datetime.combine(start + timedelta(days=day), at, UTC).isoformat()


def _task_fields(task: bp.TaskSpec, start: date) -> tuple[dict[str, Any], int, int | None]:
    """Seed field values for a task, plus its activity day and due day."""
    status = STATE_STATUS[task.state]
    start_day: int | None = task.start
    if task.state == "done":
        assert task.done is not None
        start_day = task.start if task.start is not None else max(0, task.done - max(1, (task.estimate + 1) // 2))
    due_day = task.due if task.due is not None else task.cycle * bp.CYCLE_DAYS - 1
    if task.activity is not None:
        activity = task.activity
    elif task.state == "done":
        activity = task.done or 0
    elif task.state in ("prog", "review"):
        activity = bp.SEED_DAY - 1
    elif task.state == "todo":
        activity = (task.cycle - 1) * bp.CYCLE_DAYS
    else:
        activity = 3
    labels = [bp.EPIC_LABEL[task.epic]]
    if task.blocked_by:
        labels.append("blocked")
    fields = {
        "title": task.title,
        "description": task.description,
        "issue_type": task.issue_type,
        "status": status,
        "status_category": STATUS_CATEGORY[status],
        "priority": task.priority,
        "labels": ", ".join(labels),
        "estimate": task.estimate,
        "start_date": iso(start + timedelta(days=start_day)) if start_day is not None else None,
        "due_date": iso(start + timedelta(days=due_day)),
        "completed_on": iso(start + timedelta(days=task.done))
        if task.state == "done" and task.done is not None
        else None,
        "activity_at": _at(start, activity),
    }
    return fields, activity, due_day


def build_issues(start: date) -> list[IssueSeed]:
    """Umbrella, epics and tasks in creation order, with identifiers ERP-1, ERP-2, ..."""
    key = bp.PROJECT_KEY
    task_number = 2 + len(bp.EPICS)
    tasks: dict[str, list[tuple[bp.TaskSpec, dict[str, Any], int, int | None]]] = {}
    out_tasks: list[IssueSeed] = []
    for task in bp.TASKS:
        fields, activity, due_day = _task_fields(task, start)
        tasks.setdefault(task.epic, []).append((task, fields, activity, due_day))
        epic_number = 2 + [e.key for e in bp.EPICS].index(task.epic)
        out_tasks.append(
            IssueSeed(
                f"{key}-{task_number}",
                task_number,
                task.key,
                fields,
                f"{key}-{epic_number}",
                None if task.unassigned else task.owner,
                bp.LEAD,
                task.cycle,
            )
        )
        task_number += 1
    umbrella_key = f"{key}-1"
    out_epics: list[IssueSeed] = []
    for index, epic in enumerate(bp.EPICS):
        children = tasks.get(epic.key, [])
        statuses = {fields["status"] for _, fields, _, _ in children}
        if statuses and statuses <= {"Done"}:
            status = "Done"
        elif statuses & {"Done", "In Progress", "In Review"}:
            status = "In Progress"
        elif "Todo" in statuses:
            status = "Todo"
        else:
            status = "Backlog"
        starts = [f["start_date"] for _, f, _, _ in children if f["start_date"]]
        done_days = [t.done for t, _, _, _ in children if t.done is not None]
        due_days = [d for _, _, _, d in children if d is not None]
        fields = {
            "title": epic.title,
            "description": epic.description,
            "issue_type": "Epic",
            "status": status,
            "status_category": STATUS_CATEGORY[status],
            "priority": epic.priority,
            "labels": bp.EPIC_LABEL[epic.key],
            "estimate": None,
            "start_date": min(starts) if starts else None,
            "due_date": iso(start + timedelta(days=max(due_days))) if due_days else None,
            "completed_on": iso(start + timedelta(days=max(done_days))) if status == "Done" and done_days else None,
            "activity_at": _at(start, max((a for _, _, a, _ in children), default=0)),
        }
        out_epics.append(
            IssueSeed(
                f"{key}-{index + 2}", index + 2, f"epic:{epic.key}", fields, umbrella_key, epic.owner, bp.LEAD, None
            )
        )
    umbrella = IssueSeed(
        umbrella_key,
        1,
        "",
        {
            "title": bp.UMBRELLA_TITLE,
            "description": bp.PROJECT_DESCRIPTION
            + "\n\nRoll-up issue: every epic of the project is a child of this one.",
            "issue_type": "Epic",
            "status": "In Progress",
            "status_category": "in_progress",
            "priority": "High",
            "labels": "pmo",
            "estimate": None,
            "start_date": iso(start),
            "due_date": iso(start + timedelta(days=bp.PROJECT_DAYS - 1)),
            "completed_on": None,
            "activity_at": _at(start, bp.SEED_DAY - 1),
        },
        "",
        bp.LEAD,
        bp.LEAD,
        None,
    )
    return [umbrella, *out_epics, *out_tasks]


@dataclass
class SeedPlan:
    start: date
    issues: list[IssueSeed]


def _index(records: list[dict[str, Any]], field: str) -> dict[Any, dict[str, Any]]:
    return {r.get(field): r for r in records}


class Seeder:
    def __init__(self, client: KM2Client, writer: Writer, out: Callable[[str], None]) -> None:
        self.client = client
        self.writer = writer
        self.out = out

    def _list(self, slug: str, filters: list[str] | None = None) -> list[dict[str, Any]]:
        return self.client.list_records(slug, filters)

    def run(self, today: date, seed_value: int, *, schema_ok: bool = True) -> SeedPlan:
        lists = schema_ok
        sims = self._list("trk_sim_state") if lists else []
        if sims and int(sims[0].get("sim_day") or 0) > bp.SEED_DAY:
            raise SeedError(
                f"the project was already advanced to day {sims[0].get('sim_day')}; "
                "run `reset --yes` before seeding again"
            )
        people = self._people(lists)
        project, start = self._project(lists, people, today)
        cycles = self._cycles(lists, project, start)
        issues = build_issues(start)
        ids = self._issues(lists, issues, project, people, cycles)
        self._links(lists, ids)
        self._comments(lists, start, ids, people)
        if not sims:
            self.writer.create(
                "trk_sim_state",
                {
                    "sim_day": bp.SEED_DAY,
                    "sim_date": iso(start + timedelta(days=bp.SEED_DAY)),
                    "seed": seed_value,
                    "advanced_at": datetime.now(UTC).isoformat(),
                },
            )
        return SeedPlan(start, issues)

    def _people(self, lists: bool) -> dict[str, str]:
        existing = _index(self._list("trk_person"), "email") if lists else {}
        ids: dict[str, str] = {}
        for person in bp.PEOPLE:
            rec = existing.get(person.email) or self.writer.create(
                "trk_person",
                {
                    "name": person.name,
                    "email": person.email,
                    "role": person.role,
                    "active": True,
                    "is_bot": person.is_bot,
                },
            )
            ids[person.handle] = rec["id"]
        return ids

    def _project(self, lists: bool, people: dict[str, str], today: date) -> tuple[dict[str, Any], date]:
        found = self._list("trk_project", [f"key:eq:{bp.PROJECT_KEY}"]) if lists else []
        if found:
            rec = found[0]
            return rec, date.fromisoformat(str(rec["start_date"])[:10])
        start = today - timedelta(days=bp.SEED_DAY)
        rec = self.writer.create(
            "trk_project",
            {
                "key": bp.PROJECT_KEY,
                "name": bp.PROJECT_NAME,
                "description": bp.PROJECT_DESCRIPTION,
                "status": "active",
                "start_date": iso(start),
                "target_date": iso(start + timedelta(days=bp.PROJECT_DAYS - 1)),
                "lead": people[bp.LEAD],
            },
        )
        return rec, start

    def _cycles(self, lists: bool, project: dict[str, Any], start: date) -> dict[int, str]:
        existing = _index(self._list("trk_cycle", [f"project_key:eq:{bp.PROJECT_KEY}"]), "number") if lists else {}
        ids: dict[int, str] = {}
        for number in range(1, bp.CYCLE_COUNT + 1):
            first, last = cycle_dates(start, number)
            rec = existing.get(number) or self.writer.create(
                "trk_cycle",
                {
                    "name": f"Cycle {number}",
                    "number": number,
                    "starts_on": iso(first),
                    "ends_on": iso(last),
                    "project_key": bp.PROJECT_KEY,
                    "project": project["id"],
                },
            )
            ids[number] = rec["id"]
        return ids

    def _issues(
        self,
        lists: bool,
        issues: list[IssueSeed],
        project: dict[str, Any],
        people: dict[str, str],
        cycles: dict[int, str],
    ) -> dict[str, str]:
        existing = _index(self._list("trk_issue", [f"project_key:eq:{bp.PROJECT_KEY}"]), "identifier") if lists else {}
        ids: dict[str, str] = {}
        for seed in issues:
            rec = existing.get(seed.identifier)
            if rec is None:
                payload = {
                    "identifier": seed.identifier,
                    "number": seed.number,
                    "project_key": bp.PROJECT_KEY,
                    "parent_key": seed.parent_key,
                    **seed.fields,
                    "project": project["id"],
                    "parent": ids.get(seed.parent_key),
                    "assignee": people[seed.assignee] if seed.assignee else None,
                    "creator": people[seed.creator],
                    "cycle": cycles[seed.cycle] if seed.cycle else None,
                }
                rec = self.writer.create("trk_issue", payload)
            ids[seed.identifier] = rec["id"]
            if seed.task_key:
                ids[f"task:{seed.task_key}"] = seed.identifier
        return ids

    def _links(self, lists: bool, ids: dict[str, str]) -> None:
        existing = (
            {(r.get("from_key"), r.get("to_key"), r.get("kind")) for r in self._list("trk_issue_link")}
            if lists
            else set()
        )
        for task in bp.TASKS:
            to_key = ids[f"task:{task.key}"]
            for dep in (*task.deps, *task.blocked_by):
                from_key = ids[f"task:{dep}"]
                if (from_key, to_key, "blocks") in existing:
                    continue
                self.writer.create(
                    "trk_issue_link",
                    {
                        "kind": "blocks",
                        "from_key": from_key,
                        "to_key": to_key,
                        "active": True,
                        "from_issue": ids[from_key],
                        "to_issue": ids[to_key],
                    },
                )
                existing.add((from_key, to_key, "blocks"))

    def _comments(self, lists: bool, start: date, ids: dict[str, str], people: dict[str, str]) -> None:
        existing = (
            {
                (r.get("issue_key"), r.get("author_name"), parse_dt(r.get("sim_posted_at")))
                for r in self._list("trk_comment", [f"project_key:eq:{bp.PROJECT_KEY}"])
            }
            if lists
            else set()
        )
        for index, comment in enumerate(bp.SEED_COMMENTS):
            issue_key = ids[f"task:{comment.task}"] if comment.task else f"{bp.PROJECT_KEY}-1"
            author = bp.PERSON_BY_HANDLE[comment.author]
            posted = datetime.combine(start + timedelta(days=comment.day), COMMENT_TIME, UTC) + timedelta(minutes=index)
            if (issue_key, author.name, posted) in existing:
                continue
            self.writer.create(
                "trk_comment",
                {
                    "body": comment.body,
                    "issue_key": issue_key,
                    "project_key": bp.PROJECT_KEY,
                    "author_name": author.name,
                    "mentions": ",".join(people[h] for h in comment.mentions),
                    "sim_posted_at": posted.isoformat(),
                    "issue": ids[issue_key],
                    "author": people[comment.author],
                },
            )


def delete_all(client: KM2Client, writer: Writer, out: Callable[[str], None]) -> dict[str, int]:
    """Delete every trk_* record, children before parents. The schema (entity definitions) is kept."""
    counts: dict[str, int] = {}
    for slug in DELETE_ORDER:
        records = client.list_records(slug)
        for rec in records:
            writer.delete(slug, rec["id"])
        counts[slug] = len(records)
        out(f"  {slug:<16} {len(records):>5} {'to delete' if writer.dry_run else 'deleted'}")
    return counts
