"""Per-day state machine: plans one simulated day of the project as a list of operations.

Randomness comes from a ``random.Random`` seeded with hash(seed, day), and every step walks issues and people in a
fixed order, so the same start-of-day state always produces the same operations and event ids. Planning updates
the in-memory state as it goes (so later steps see earlier ones); ``apply.write_ops`` then writes the operations
to KM2. Weekends are quiet. Each issue gets at most one change per day (epic roll-ups and replies aside).
"""

from __future__ import annotations

import hashlib
import random
from dataclasses import dataclass, field, replace
from datetime import UTC, date, datetime, time, timedelta
from typing import Any

from . import blueprint as bp
from . import texts
from .state import (
    BLOCKED_LABEL,
    PRIORITY_RANK,
    Cycle,
    Issue,
    Link,
    Person,
    State,
    add_workdays,
    encode_value,
    is_workday,
    iso,
    with_label,
    without_label,
)

DAY_START = time(13, 0)
MINUTES_PER_OP = 5
STALE_DAYS = 5
# Daily probabilities (spec section 6).
P_RAISE_BLOCKER = 0.03
P_CLEAR_BLOCKER = 0.25
P_CLEAR_RESOLVED = 0.6
P_REVIEW_DONE = 0.45
P_REWORK = 0.18
P_START = 0.75
P_PICKUP = 0.35
P_SLIP_OVERDUE = 0.3
P_SLIP_NEAR = 0.2
P_NEW_TICKET = 0.15
P_REASSIGN = 0.02
P_PULL_UNPLANNED = 0.5
P_REPLY = 0.5
MAX_PROGRESS_P = 0.85
DIRECT_TO_DONE_GROUPS = frozenset({"pm", "qa"})


def day_seed(seed: int, day: int) -> int:
    return int(hashlib.sha256(f"{seed}:{day}".encode()).hexdigest()[:16], 16)


def unit_hash(*parts: object) -> float:
    """A stable number in [0, 1) for a tuple of values."""
    digest = hashlib.sha256(":".join(str(p) for p in parts).encode()).hexdigest()
    return int(digest[:13], 16) / float(1 << 52)


def wants_reply(seed: int, comment_id: str) -> bool:
    return unit_hash(seed, "reply", comment_id) < P_REPLY


@dataclass(frozen=True)
class Event:
    event_id: str
    issue_key: str
    kind: str
    from_value: str
    to_value: str
    actor_name: str
    sim_day: int
    sim_at: datetime


@dataclass(frozen=True)
class CommentSpec:
    author: str
    body: str
    mentions: tuple[str, ...] = ()


@dataclass
class Op:
    seq: int
    kind: str
    issue_key: str
    at: datetime
    summary: str
    events: list[Event] = field(default_factory=list)
    patch: dict[str, Any] = field(default_factory=dict)
    create: dict[str, Any] | None = None
    comment: CommentSpec | None = None
    link_add: tuple[str, str, str] | None = None
    link_remove: tuple[tuple[str, str, str], ...] = ()


_RECORD_FIELD = {"due_date": iso, "start_date": iso}


class DayPlanner:
    def __init__(self, state: State, day: int, seed: int) -> None:
        self.state = state
        self.day = day
        self.seed = seed
        self.date = state.sim_date_for(day)
        self.rng = random.Random(day_seed(seed, day))
        self.ops: list[Op] = []
        self.touched: set[str] = set()
        self.cycle: Cycle | None = state.current_cycle(self.date)

    # ------------------------------------------------------------------ helpers
    def _at(self, seq: int) -> datetime:
        return datetime.combine(self.date, DAY_START, UTC) + timedelta(minutes=MINUTES_PER_OP * seq)

    def _event(
        self, seq: int, key: str, kind: str, old: str, new: str, actor: str, event_id: str | None = None
    ) -> Event:
        eid = event_id or f"d{self.day:03d}.{seq:03d}.{kind}.{key}"
        return Event(eid, key, kind, old, new, actor, self.day, self._at(seq))

    def change(
        self,
        kind: str,
        key: str,
        actor: Person | None,
        changes: dict[str, Any] | None = None,
        *,
        summary: str,
        comment: CommentSpec | None = None,
        link_add: tuple[str, str, str] | None = None,
        link_remove: tuple[tuple[str, str, str], ...] = (),
        create: Issue | None = None,
        event_id: str | None = None,
    ) -> Op:
        state = self.state
        seq = len(self.ops) + 1
        at = self._at(seq)
        actor_name = actor.name if actor else ""
        op = Op(seq, kind, key, at, summary, comment=comment, link_add=link_add, link_remove=link_remove)
        if create is not None:
            create = replace(create, activity_at=at)
            state.issues[key] = create
            op.events.append(self._event(seq, key, "created", "", key, actor_name))
            op.create = self._create_payload(create)
        else:
            before = state.issues[key]
            after_changes: dict[str, Any] = {}
            for name, value in (changes or {}).items():
                old = getattr(before, name)
                if old == value:
                    continue
                after_changes[name] = value
                op.events.append(
                    self._event(
                        seq, key, name, encode_value(state, name, old), encode_value(state, name, value), actor_name
                    )
                )
                op.patch[name] = _RECORD_FIELD.get(name, lambda v: v)(value)
            if "status" in after_changes:
                done = after_changes["status"] == "Done"
                after_changes["completed_on"] = self.date if done else None
            after = replace(before, **after_changes, activity_at=at)
            state.issues[key] = after
            if "status" in after_changes:
                op.patch["status_category"] = after.category
                op.patch["completed_on"] = iso(after.completed_on)
            op.patch["activity_at"] = iso(at)
        if link_add is not None:
            src, dst, link_kind = link_add
            op.events.append(self._event(seq, dst, "link_added", src, link_kind, actor_name))
            state.links.append(Link(f"pending:{src}:{dst}", link_kind, src, dst, True))
        for src, dst, link_kind in link_remove:
            op.events.append(self._event(seq, dst, "link_removed", src, link_kind, actor_name))
            state.links = [
                replace(lk, active=False) if (lk.from_key, lk.to_key, lk.kind) == (src, dst, link_kind) else lk
                for lk in state.links
            ]
        if comment is not None:
            author = state.person_name(comment.author)
            op.events.append(self._event(seq, key, "comment", "", comment.body[:200], author, event_id))
        self.ops.append(op)
        self.touched.add(key)
        return op

    def _create_payload(self, issue: Issue) -> dict[str, Any]:
        return {
            "identifier": issue.identifier,
            "number": issue.number,
            "project_key": self.state.project.key,
            "title": issue.title,
            "description": issue.description,
            "issue_type": issue.issue_type,
            "status": issue.status,
            "status_category": issue.category,
            "priority": issue.priority,
            "labels": issue.labels,
            "estimate": issue.estimate,
            "due_date": iso(issue.due_date),
            "parent_key": issue.parent_key,
            "activity_at": iso(issue.activity_at),
            "project": self.state.project.id,
            "parent": issue.parent,
            "assignee": issue.assignee,
            "creator": issue.creator,
            "cycle": issue.cycle,
        }

    def _open_tasks(self) -> list[Issue]:
        return [i for i in self.state.sorted_issues() if i.is_open and not i.is_epic]

    def _free(self, key: str) -> bool:
        return key not in self.touched

    def _in_window(self, issue: Issue) -> bool:
        """Planned for the current cycle or earlier (or not planned at all)."""
        cycle = self.state.cycle_by_id(issue.cycle)
        return cycle is None or self.cycle is None or cycle.number <= self.cycle.number

    def _actor(self, issue: Issue) -> Person | None:
        return self.state.people.get(issue.assignee or "") or self.state.lead()

    def _reviewer(self, person: Person | None) -> Person | None:
        workers = self.state.workers()
        arch = next((p for p in workers if p.handle == "arch"), None)
        backup = next((p for p in workers if p.handle == "int1"), None)
        if person is not None and arch is not None and person.id == arch.id:
            return backup
        return arch or backup

    def _pick(self, options: tuple[str, ...] | list[Any]) -> Any:
        return options[self.rng.randrange(len(options))]

    def _group(self, group: str) -> list[Person]:
        people = [p for p in self.state.workers() if p.group == group]
        return people or [p for p in self.state.workers() if p.group == "eng"]

    # ------------------------------------------------------------------ steps
    def cycle_boundary(self) -> None:
        cycle = self.cycle
        if cycle is None or cycle.number <= min(c.number for c in self.state.cycles):
            return
        first = cycle.starts_on
        while not is_workday(first):
            first += timedelta(days=1)
        if self.date != first:
            return
        lead = self.state.lead()
        carried = planned = 0
        previous = cycle.number - 1
        for issue in self._open_tasks():
            current = self.state.cycle_by_id(issue.cycle)
            if current is not None and current.number < cycle.number:
                changes: dict[str, Any] = {"cycle": cycle.id}
                if issue.status in ("Backlog", "Todo"):  # not started: re-planned for the new cycle
                    changes["status"] = "Todo"
                    changes["due_date"] = cycle.ends_on
                self.change("carry", issue.identifier, lead, changes, summary=f"carried over to {cycle.name}")
                carried += 1
            elif current is not None and current.id == cycle.id and issue.status == "Backlog":
                self.change("plan", issue.identifier, lead, {"status": "Todo"}, summary=f"planned into {cycle.name}")
                planned += 1
            elif current is None and issue.status == "Backlog" and self.rng.random() < P_PULL_UNPLANNED:
                self.change(
                    "plan",
                    issue.identifier,
                    lead,
                    {"status": "Todo", "cycle": cycle.id},
                    summary=f"pulled into {cycle.name}",
                )
                planned += 1
        umbrella = self._umbrella()
        if umbrella is not None and lead is not None and self._free(umbrella.identifier):
            body = texts.CYCLE_PLANNED.format(number=cycle.number, planned=planned, carried=carried, previous=previous)
            self.change(
                "cycle_note",
                umbrella.identifier,
                lead,
                {},
                summary="cycle planning note",
                comment=CommentSpec(lead.id, body),
            )

    def _umbrella(self) -> Issue | None:
        epics = [i for i in self.state.sorted_issues() if i.is_epic and not self.state.parent_key_of(i)]
        return next((i for i in epics if i.title == bp.UMBRELLA_TITLE), epics[0] if epics else None)

    def clear_blockers(self) -> None:
        for issue in self._open_tasks():
            if not issue.blocked or not self._free(issue.identifier):
                continue
            open_blockers = self.state.blockers(issue.identifier)
            if self.rng.random() >= (P_CLEAR_BLOCKER if open_blockers else P_CLEAR_RESOLVED):
                continue
            actor = self._actor(issue)
            lead = self.state.lead()
            if open_blockers:
                template = self._pick(texts.UNBLOCK[1:])
                blocker = open_blockers[0].from_key
            else:
                template = texts.UNBLOCK[0]
                done = [lk.from_key for lk in self.state.links if lk.kind == "blocks" and lk.to_key == issue.identifier]
                blocker = done[-1] if done else "the dependency"
            body = template.format(blocker=blocker, lead=lead.name if lead else "the lead")
            self.change(
                "unblock",
                issue.identifier,
                actor,
                {"labels": without_label(issue.labels, BLOCKED_LABEL)},
                summary="blocker cleared",
                comment=CommentSpec(actor.id, body) if actor else None,
                link_remove=tuple((lk.from_key, lk.to_key, lk.kind) for lk in open_blockers),
            )

    def raise_blockers(self) -> None:
        for issue in self._open_tasks():
            if issue.status != "In Progress" or issue.blocked or not issue.assignee or not self._free(issue.identifier):
                continue
            if self.rng.random() >= P_RAISE_BLOCKER:
                continue
            blocker = self._blocker_for(issue)
            if blocker is None:
                continue
            owner = self.state.person_name(blocker.assignee) or "the team"
            context = self._pick(texts.BLOCKER_CONTEXT).format(owner=owner)
            body = texts.BLOCKED_ON.format(blocker=blocker.identifier, title=blocker.title, context=context)
            actor = self._actor(issue)
            self.change(
                "block",
                issue.identifier,
                actor,
                {"labels": with_label(issue.labels, BLOCKED_LABEL)},
                summary=f"blocked by {blocker.identifier}",
                comment=CommentSpec(actor.id, body) if actor else None,
                link_add=(blocker.identifier, issue.identifier, "blocks"),
            )

    def _blocker_for(self, issue: Issue) -> Issue | None:
        parent = self.state.parent_key_of(issue)
        downstream = {lk.to_key for lk in self.state.links if lk.from_key == issue.identifier}
        candidates = [
            i
            for i in self._open_tasks()
            if i.identifier != issue.identifier
            and i.identifier not in downstream
            and not i.id.startswith("pending:")
            and i.status in ("Todo", "In Progress", "In Review")
        ]
        same_epic = [i for i in candidates if self.state.parent_key_of(i) == parent]
        pool = same_epic or candidates
        return self._pick(pool) if pool else None

    def reviews(self) -> None:
        for issue in self._open_tasks():
            if issue.status != "In Review" or issue.blocked or not self._free(issue.identifier):
                continue
            if self.rng.random() >= P_REVIEW_DONE:
                continue
            reviewer = self._reviewer(self.state.people.get(issue.assignee or ""))
            if self.rng.random() < P_REWORK:
                body = self._pick(texts.REWORK)
                self.change(
                    "rework",
                    issue.identifier,
                    reviewer,
                    {"status": "In Progress"},
                    summary="review: rework",
                    comment=CommentSpec(reviewer.id, body) if reviewer else None,
                )
                continue
            comment = None
            if reviewer is not None and self.rng.random() < 0.4:
                comment = CommentSpec(reviewer.id, self._pick(texts.DONE))
            self.change("done", issue.identifier, reviewer, {"status": "Done"}, summary="done", comment=comment)

    def progress(self) -> None:
        for person in self.state.workers():
            active = [
                i for i in self._open_tasks() if i.assignee == person.id and i.status == "In Progress" and not i.blocked
            ]
            share = person.capacity / max(1, len(active))
            for issue in active:
                if not self._free(issue.identifier):
                    continue
                chance = min(MAX_PROGRESS_P, share / max(1, issue.estimate or 2))
                if self.rng.random() >= chance:
                    continue
                if person.group in DIRECT_TO_DONE_GROUPS:
                    comment = (
                        CommentSpec(person.id, self._pick(texts.DONE_NON_DEV)) if self.rng.random() < 0.3 else None
                    )
                    self.change("done", issue.identifier, person, {"status": "Done"}, summary="done", comment=comment)
                    continue
                reviewer = self._reviewer(person)
                comment = None
                if reviewer is not None and self.rng.random() < 0.5:
                    body = self._pick(texts.TO_REVIEW).format(reviewer=f"@{reviewer.name}")
                    comment = CommentSpec(person.id, body, (reviewer.id,))
                self.change(
                    "review", issue.identifier, person, {"status": "In Review"}, summary="in review", comment=comment
                )

    def _startable(self, issue: Issue) -> bool:
        return (
            issue.status == "Todo"
            and not issue.blocked
            and self._free(issue.identifier)
            and self._in_window(issue)
            and not self.state.blockers(issue.identifier)
        )

    def starts(self) -> None:
        for person in self.state.workers():
            if self.state.wip(person.id) >= person.wip:
                continue
            candidates = [i for i in self._open_tasks() if i.assignee == person.id and self._startable(i)]
            if not candidates or self.rng.random() >= P_START:
                continue
            issue = min(candidates, key=lambda i: (PRIORITY_RANK.get(i.priority, 4), i.due_date or date.max, i.number))
            changes: dict[str, Any] = {"status": "In Progress"}
            if issue.start_date is None:
                changes["start_date"] = self.date
            comment = CommentSpec(person.id, self._pick(texts.START)) if self.rng.random() < 0.3 else None
            self.change("start", issue.identifier, person, changes, summary="started", comment=comment)

    def pickups(self) -> None:
        for issue in self._open_tasks():
            if issue.assignee or not self._startable(issue):
                continue
            free = [
                p
                for p in self._group(self.state.epic_group(issue))
                if self.state.wip(p.id) < p.wip
                and sum(1 for i in self._open_tasks() if i.assignee == p.id and i.status == "Todo") < 2
            ]
            if not free or self.rng.random() >= P_PICKUP:
                continue
            person = self._pick(free)
            comment = CommentSpec(person.id, "Taking this one.") if self.rng.random() < 0.5 else None
            self.change(
                "pickup",
                issue.identifier,
                person,
                {"assignee": person.id},
                summary=f"picked up by {person.name}",
                comment=comment,
            )

    def slips(self) -> None:
        for issue in self._open_tasks():
            if issue.due_date is None or not issue.assignee or not self._free(issue.identifier):
                continue
            if issue.status not in ("Todo", "In Progress", "In Review"):
                continue
            if issue.due_date < self.date:
                chance = P_SLIP_OVERDUE
            elif (issue.due_date - self.date).days <= 2 and issue.status != "In Review":
                chance = P_SLIP_NEAR
            else:
                continue
            if self.rng.random() >= chance:
                continue
            new_due = add_workdays(max(issue.due_date, self.date), self.rng.randint(2, 5))
            reason = self._pick(texts.SLIP_REASON)
            body = self._pick(texts.SLIP).format(
                date=new_due.isoformat(), reason=reason, reason_cap=reason[0].upper() + reason[1:]
            )
            actor = self._actor(issue)
            self.change(
                "slip",
                issue.identifier,
                actor,
                {"due_date": new_due},
                summary=f"due date -> {new_due}",
                comment=CommentSpec(actor.id, body) if actor else None,
            )

    def new_ticket(self) -> None:
        if self.rng.random() >= P_NEW_TICKET:
            return
        is_bug = self.rng.random() < 0.7
        used = {i.title for i in self.state.issues.values()}
        pool = [item for item in (texts.NEW_BUGS if is_bug else texts.NEW_SCOPE) if item[0] not in used]
        if not pool:
            return
        title, description, area = self._pick(pool)
        epic_spec = bp.EPIC_BY_KEY.get(area)
        epic = next((i for i in self.state.issues.values() if epic_spec and i.title == epic_spec.title), None)
        umbrella = self._umbrella()
        parent = epic if epic is not None and epic.is_open else umbrella
        lead = self.state.lead()
        qa = next((p for p in self.state.workers() if p.group == "qa"), lead)
        creator = qa if is_bug else lead
        group = epic_spec.group if epic_spec else "eng"
        assignee = self._pick(self._group(group)) if self.rng.random() < 0.5 else None
        planned = self.rng.random() < 0.6 and self.cycle is not None
        if is_bug:
            priority = self.rng.choices(["Urgent", "High", "Medium", "Low"], weights=[1, 3, 4, 1])[0]
        else:
            priority = self._pick(["Medium", "Low"])
        number = self.state.next_number()
        key = f"{self.state.project.key}-{number}"
        issue = Issue(
            id=f"pending:{key}",
            identifier=key,
            number=number,
            title=title,
            issue_type="Bug" if is_bug else "Task",
            status="Todo" if planned else "Backlog",
            priority=priority,
            labels=", ".join(x for x in (bp.EPIC_LABEL.get(area), "bug" if is_bug else "scope-change") if x),
            estimate=self._pick([1, 2, 3, 5]),
            due_date=add_workdays(self.date, self.rng.randint(5, 10)) if planned else None,
            parent=parent.id if parent else None,
            parent_key=parent.identifier if parent else "",
            assignee=assignee.id if assignee else None,
            creator=creator.id if creator else None,
            cycle=self.cycle.id if planned and self.cycle else None,
            description=f"{description}\n\nRaised by {creator.name if creator else 'the team'} on {self.date}.",
        )
        self.change("new", key, creator, summary=f"new {issue.issue_type.lower()}: {title}", create=issue)

    def reassign(self) -> None:
        if self.rng.random() >= P_REASSIGN:
            return

        def peers(issue: Issue) -> list[Person]:
            old = self.state.people.get(issue.assignee or "")
            group = old.group if old else "eng"
            return [p for p in self._group(group) if old is None or p.id != old.id]

        candidates = [
            i
            for i in self._open_tasks()
            if i.assignee
            and i.status in ("Todo", "In Progress")
            and not i.blocked
            and self._free(i.identifier)
            and peers(i)
        ]
        if not candidates:
            return
        issue = self._pick(candidates)
        old = self.state.people.get(issue.assignee or "")
        others = peers(issue)
        new = self._pick(others)
        lead = self.state.lead()
        other_work = next(
            (
                i.title
                for i in self._open_tasks()
                if old and i.assignee == old.id and i.identifier != issue.identifier and i.status == "In Progress"
            ),
            "other work this cycle",
        )
        body = self._pick(texts.REASSIGN).format(
            new=new.name, old=old.name if old else "the previous owner", other=other_work
        )
        self.change(
            "reassign",
            issue.identifier,
            lead,
            {"assignee": new.id},
            summary=f"reassigned to {new.name}",
            comment=CommentSpec(lead.id, body) if lead else None,
        )

    def epics(self) -> None:
        epics = [i for i in self.state.sorted_issues() if i.is_epic]
        # Deepest first, so an umbrella sees its epics' same-day changes.
        epics.sort(key=lambda i: (-self._depth(i), i.number))
        for epic in epics:
            current = self.state.issues[epic.identifier]
            children = self.state.children(epic.identifier)
            if not children or not self._free(epic.identifier):
                continue
            actor = self._actor(current)
            if current.is_open and all(not c.is_open for c in children):
                self.change(
                    "epic_done",
                    epic.identifier,
                    actor,
                    {"status": "Done"},
                    summary="epic closed",
                    comment=CommentSpec(actor.id, "All child issues are done; closing.") if actor else None,
                )
            elif current.status in ("Backlog", "Todo") and any(c.category != "to_do" for c in children):
                changes: dict[str, Any] = {"status": "In Progress"}
                if current.start_date is None:
                    changes["start_date"] = self.date
                self.change("epic_start", epic.identifier, actor, changes, summary="epic in progress")

    def _depth(self, issue: Issue) -> int:
        depth, current, seen = 0, issue, set()
        while current is not None and current.identifier not in seen:
            seen.add(current.identifier)
            parent = self.state.parent_key_of(current)
            current = self.state.issues.get(parent) if parent else None
            depth += 1 if current is not None else 0
        return depth

    def replies(self, bot_comments: list[dict[str, Any]]) -> None:
        for comment in bot_comments:
            key = comment.get("issue_key") or ""
            issue = self.state.by_id(comment.get("issue")) or self.state.issues.get(key)
            if issue is None:
                continue
            responder = self._responder(comment, issue)
            if responder is None:
                continue
            body = self._reply_text(issue)
            self.change(
                "reply",
                issue.identifier,
                responder,
                {},
                summary=f"{responder.name} replied to the PMO bot",
                comment=CommentSpec(responder.id, body),
                event_id=f"reply.{comment['id']}",
            )

    def _responder(self, comment: dict[str, Any], issue: Issue) -> Person | None:
        workers = {p.id: p for p in self.state.workers()}
        for mention in str(comment.get("mentions") or "").split(","):
            if mention.strip() in workers:
                return workers[mention.strip()]
        return workers.get(issue.assignee or "") or self.state.lead()

    def _reply_text(self, issue: Issue) -> str:
        blockers = self.state.blockers(issue.identifier)
        eta = add_workdays(self.date, self.rng.randint(2, 4)).isoformat()
        reviewer = self._reviewer(self.state.people.get(issue.assignee or ""))
        if issue.blocked:
            bucket = "blocked"
        elif issue.is_open and issue.due_date is not None and issue.due_date < self.date:
            bucket = "overdue"
        elif not issue.is_open:
            bucket = "done"
        else:
            bucket = issue.status if issue.status in texts.REPLY else "other"
        return self._pick(texts.REPLY[bucket]).format(
            blocker=blockers[0].from_key if blockers else "the dependency",
            eta=eta,
            reviewer=reviewer.name if reviewer else "the reviewer",
            other="my current item",
        )


def plan_day(state: State, day: int, seed: int, bot_comments: list[dict[str, Any]] | None = None) -> list[Op]:
    """Plan (and apply to ``state`` in memory) one simulated day. Returns no operations on weekends."""
    planner = DayPlanner(state, day, seed)
    if not is_workday(planner.date):
        return []
    planner.cycle_boundary()
    planner.clear_blockers()
    planner.raise_blockers()
    planner.reviews()
    planner.progress()
    planner.starts()
    planner.pickups()
    planner.slips()
    planner.new_ticket()
    planner.reassign()
    planner.epics()
    planner.replies(bot_comments or [])
    return planner.ops
