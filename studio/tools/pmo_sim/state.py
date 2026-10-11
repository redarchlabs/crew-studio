"""In-memory model of the tracker, loaded from KM2, plus the record <-> model conversions.

On load, events written for days after ``trk_sim_state.sim_day`` (a run that stopped part-way through a day) are
reversed in memory, so the engine re-plans that day from the state it started with and every write is matched by
its deterministic event id instead of being repeated.
"""

from __future__ import annotations

from dataclasses import dataclass, field, replace
from datetime import UTC, date, datetime, timedelta
from typing import Any

from . import blueprint as bp
from .bundle import STATUS_CATEGORY
from .km2 import KM2Client

DONE_CATEGORIES = frozenset({"done", "canceled"})
BLOCKED_LABEL = "blocked"
# Issue fields that the engine changes and that events record (value encoding in encode_value/decode_value).
FIELD_KINDS = ("status", "assignee", "cycle", "due_date", "start_date", "labels", "priority")
PRIORITY_RANK = {"Urgent": 0, "High": 1, "Medium": 2, "Low": 3, "None": 4}


def parse_date(value: Any) -> date | None:
    if not value:
        return None
    if isinstance(value, date) and not isinstance(value, datetime):
        return value
    text = str(value)
    return datetime.fromisoformat(text).date() if "T" in text else date.fromisoformat(text)


def parse_dt(value: Any) -> datetime | None:
    if not value:
        return None
    dt = value if isinstance(value, datetime) else datetime.fromisoformat(str(value))
    return dt if dt.tzinfo else dt.replace(tzinfo=UTC)


def iso(value: date | datetime | None) -> str | None:
    return value.isoformat() if value is not None else None


def is_workday(day: date) -> bool:
    return day.weekday() < 5


def add_workdays(start: date, count: int) -> date:
    current = start
    while count > 0:
        current += timedelta(days=1)
        if is_workday(current):
            count -= 1
    return current


def label_list(labels: str | None) -> list[str]:
    return [p.strip() for p in (labels or "").split(",") if p.strip()]


def with_label(labels: str | None, label: str) -> str:
    items = label_list(labels)
    return ", ".join([*items, label]) if label not in items else ", ".join(items)


def without_label(labels: str | None, label: str) -> str:
    return ", ".join(p for p in label_list(labels) if p != label)


@dataclass(frozen=True)
class Person:
    id: str
    name: str
    email: str
    role: str
    is_bot: bool
    active: bool
    group: str = "eng"
    capacity: float = 2.5
    wip: int = 2
    handle: str = ""


@dataclass(frozen=True)
class Cycle:
    id: str
    name: str
    number: int
    starts_on: date
    ends_on: date


@dataclass(frozen=True)
class Link:
    id: str
    kind: str
    from_key: str
    to_key: str
    active: bool


@dataclass(frozen=True)
class Issue:
    id: str
    identifier: str
    number: int
    title: str
    issue_type: str
    status: str
    priority: str = "Medium"
    labels: str = ""
    estimate: int | None = None
    start_date: date | None = None
    due_date: date | None = None
    parent: str | None = None
    parent_key: str = ""
    assignee: str | None = None
    creator: str | None = None
    cycle: str | None = None
    activity_at: datetime | None = None
    completed_on: date | None = None
    description: str = ""

    @property
    def category(self) -> str:
        return STATUS_CATEGORY.get(self.status, "to_do")

    @property
    def is_open(self) -> bool:
        return self.category not in DONE_CATEGORIES

    @property
    def is_epic(self) -> bool:
        return self.issue_type == "Epic"

    @property
    def blocked(self) -> bool:
        return BLOCKED_LABEL in label_list(self.labels)


@dataclass(frozen=True)
class Project:
    id: str
    key: str
    name: str
    start_date: date
    target_date: date | None
    lead: str | None


@dataclass
class SimRecord:
    id: str | None
    sim_day: int
    sim_date: date
    seed: int


@dataclass
class State:
    project: Project
    people: dict[str, Person]
    cycles: list[Cycle]
    issues: dict[str, Issue]
    links: list[Link]
    sim: SimRecord
    # What KM2 actually holds, for "ensure" checks when a day is resumed.
    actual_identifiers: dict[str, str] = field(default_factory=dict)
    actual_links: dict[tuple[str, str, str], Link] = field(default_factory=dict)
    pending_events: dict[str, dict[str, Any]] = field(default_factory=dict)

    # ---------------------------------------------------------------- lookups
    def sim_date_for(self, day: int) -> date:
        return self.project.start_date + timedelta(days=day)

    def person_by_name(self, name: str) -> Person | None:
        return next((p for p in self.people.values() if p.name == name), None)

    def person_name(self, person_id: str | None) -> str:
        person = self.people.get(person_id or "")
        return person.name if person else ""

    def workers(self) -> list[Person]:
        return sorted((p for p in self.people.values() if not p.is_bot and p.active), key=lambda p: p.name)

    def bots(self) -> list[Person]:
        return [p for p in self.people.values() if p.is_bot]

    def lead(self) -> Person | None:
        return self.people.get(self.project.lead or "") or next(iter(self.workers()), None)

    def cycle_by_id(self, cycle_id: str | None) -> Cycle | None:
        return next((c for c in self.cycles if c.id == cycle_id), None)

    def cycle_by_name(self, name: str) -> Cycle | None:
        return next((c for c in self.cycles if c.name == name), None)

    def current_cycle(self, on: date) -> Cycle | None:
        if not self.cycles:
            return None
        for cycle in self.cycles:
            if cycle.starts_on <= on <= cycle.ends_on:
                return cycle
        return self.cycles[0] if on < self.cycles[0].starts_on else self.cycles[-1]

    def by_id(self, issue_id: str | None) -> Issue | None:
        return next((i for i in self.issues.values() if i.id == issue_id), None) if issue_id else None

    def parent_key_of(self, issue: Issue) -> str:
        if issue.parent_key:
            return issue.parent_key
        parent = self.by_id(issue.parent)
        return parent.identifier if parent else ""

    def children(self, key: str) -> list[Issue]:
        return [i for i in self.issues.values() if self.parent_key_of(i) == key]

    def blockers(self, key: str) -> list[Link]:
        """Active ``blocks`` links into ``key`` whose blocking issue is still open."""
        out = []
        for link in self.links:
            if link.kind == "blocks" and link.active and link.to_key == key:
                src = self.issues.get(link.from_key)
                if src is not None and src.is_open:
                    out.append(link)
        return out

    def next_number(self) -> int:
        return max((i.number for i in self.issues.values()), default=0) + 1

    def sorted_issues(self) -> list[Issue]:
        return sorted(self.issues.values(), key=lambda i: i.number)

    def epic_group(self, issue: Issue) -> str:
        current: Issue | None = issue
        seen: set[str] = set()
        while current is not None and current.identifier not in seen:
            seen.add(current.identifier)
            if current.is_epic:
                spec = next((e for e in bp.EPICS if e.title == current.title), None)
                if spec is not None:
                    return spec.group
            current = self.issues.get(self.parent_key_of(current))
        return "eng"

    def wip(self, person_id: str) -> int:
        return sum(
            1
            for i in self.issues.values()
            if i.assignee == person_id and not i.is_epic and i.status in ("In Progress", "In Review")
        )


# ---------------------------------------------------------------- event value encoding
def encode_value(state: State, kind: str, value: Any) -> str:
    if value is None:
        return ""
    if kind == "assignee":
        return state.person_name(value)
    if kind == "cycle":
        cycle = state.cycle_by_id(value)
        return cycle.name if cycle else ""
    if isinstance(value, date):
        return value.isoformat()
    return str(value)


def decode_value(state: State, kind: str, text: str | None) -> Any:
    if not text:
        return {"labels": "", "priority": "None"}.get(kind)
    if kind == "assignee":
        person = state.person_by_name(text)
        return person.id if person else None
    if kind == "cycle":
        cycle = state.cycle_by_name(text)
        return cycle.id if cycle else None
    if kind in ("due_date", "start_date"):
        return parse_date(text)
    return text


# ---------------------------------------------------------------- record conversions
def person_from(rec: dict[str, Any]) -> Person:
    spec = next((p for p in bp.PEOPLE if p.email == rec.get("email")), None)
    return Person(
        id=rec["id"],
        name=rec.get("name") or rec.get("email") or rec["id"],
        email=rec.get("email") or "",
        role=rec.get("role") or "",
        is_bot=bool(rec.get("is_bot")),
        active=rec.get("active") is not False,
        group=spec.group if spec else "eng",
        capacity=spec.capacity if spec else 2.0,
        wip=spec.wip if spec else 2,
        handle=spec.handle if spec else "",
    )


def cycle_from(rec: dict[str, Any]) -> Cycle:
    return Cycle(
        id=rec["id"],
        name=rec.get("name") or f"Cycle {rec.get('number')}",
        number=int(rec.get("number") or 0),
        starts_on=parse_date(rec.get("starts_on")) or date.min,
        ends_on=parse_date(rec.get("ends_on")) or date.max,
    )


def issue_from(rec: dict[str, Any]) -> Issue:
    return Issue(
        id=rec["id"],
        identifier=rec["identifier"],
        number=int(rec.get("number") or 0),
        title=rec.get("title") or "",
        issue_type=rec.get("issue_type") or "Task",
        status=rec.get("status") or "Backlog",
        priority=rec.get("priority") or "None",
        labels=rec.get("labels") or "",
        estimate=rec.get("estimate"),
        start_date=parse_date(rec.get("start_date")),
        due_date=parse_date(rec.get("due_date")),
        parent=rec.get("parent"),
        parent_key=rec.get("parent_key") or "",
        assignee=rec.get("assignee"),
        creator=rec.get("creator"),
        cycle=rec.get("cycle"),
        activity_at=parse_dt(rec.get("activity_at")),
        completed_on=parse_date(rec.get("completed_on")),
        description=rec.get("description") or "",
    )


def link_from(rec: dict[str, Any]) -> Link:
    return Link(
        id=rec["id"],
        kind=rec.get("kind") or "relates",
        from_key=rec.get("from_key") or "",
        to_key=rec.get("to_key") or "",
        active=rec.get("active") is not False,
    )


class StateError(Exception):
    """The org does not hold a seeded project."""


def load_state(client: KM2Client, project_key: str = bp.PROJECT_KEY, *, revert: bool = True) -> State:
    projects = client.list_records("trk_project", [f"key:eq:{project_key}"])
    if not projects:
        raise StateError(f"no trk_project with key {project_key!r}; run `seed` first")
    prec = projects[0]
    start = parse_date(prec.get("start_date"))
    if start is None:
        raise StateError(f"project {project_key} has no start_date")
    project = Project(
        prec["id"],
        project_key,
        prec.get("name") or project_key,
        start,
        parse_date(prec.get("target_date")),
        prec.get("lead"),
    )
    sims = client.list_records("trk_sim_state")
    if not sims:
        raise StateError("no trk_sim_state record; run `seed` first")
    if len(sims) > 1:
        raise StateError(f"{len(sims)} trk_sim_state records found; expected exactly one")
    srec = sims[0]
    sim_day = int(srec.get("sim_day") or 0)
    sim = SimRecord(
        srec["id"],
        sim_day,
        parse_date(srec.get("sim_date")) or start + timedelta(days=sim_day),
        int(srec.get("seed") or bp.DEFAULT_SEED),
    )
    people = {p.id: p for p in map(person_from, client.list_records("trk_person"))}
    cycles = sorted(
        map(cycle_from, client.list_records("trk_cycle", [f"project_key:eq:{project_key}"])), key=lambda c: c.number
    )
    issues = {
        i.identifier: i for i in map(issue_from, client.list_records("trk_issue", [f"project_key:eq:{project_key}"]))
    }
    links = [link_from(r) for r in client.list_records("trk_issue_link")]
    links = [lk for lk in links if lk.from_key in issues or lk.to_key in issues]
    state = State(project, people, cycles, issues, links, sim)
    state.actual_identifiers = {key: issue.id for key, issue in issues.items()}
    state.actual_links = {(lk.from_key, lk.to_key, lk.kind): lk for lk in links}
    if revert:
        pending = client.list_records("trk_event", [f"sim_day:gt:{sim_day}"])
        state.pending_events = {e["event_id"]: e for e in pending}
        revert_events(state, pending)
    return state


def revert_events(state: State, events: list[dict[str, Any]]) -> None:
    """Undo, in memory, the changes recorded by ``events`` (newest first)."""
    epoch = datetime.min.replace(tzinfo=UTC)
    ordered = sorted(events, key=lambda e: (parse_dt(e.get("sim_at")) or epoch, e.get("event_id") or ""), reverse=True)
    for ev in ordered:
        kind = ev.get("kind") or ""
        key = ev.get("issue_key") or ""
        if kind == "created":
            state.issues.pop(key, None)
        elif kind == "link_added":
            state.links = [
                lk
                for lk in state.links
                if (lk.from_key, lk.to_key, lk.kind) != (ev.get("from_value"), key, ev.get("to_value"))
            ]
        elif kind == "link_removed":
            state.links = [
                replace(lk, active=True)
                if (lk.from_key, lk.to_key, lk.kind) == (ev.get("from_value"), key, ev.get("to_value"))
                else lk
                for lk in state.links
            ]
        elif kind in FIELD_KINDS and key in state.issues:
            issue = state.issues[key]
            value = decode_value(state, kind, ev.get("from_value"))
            changes: dict[str, Any] = {kind: value}
            if kind == "status" and value != "Done":
                changes["completed_on"] = None
            state.issues[key] = replace(issue, **changes)
