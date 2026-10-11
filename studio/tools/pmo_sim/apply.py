"""Writes planned operations to KM2: events first, then the change they describe.

Writing the event before the change means a run that stops part-way never leaves a change without its event.
On the next run the day is re-planned from its start-of-day state (see ``state.revert_events``); operations whose
events already exist are only *ensured* (idempotent patches, existence checks for issues, links and comments),
never repeated.
"""

from __future__ import annotations

import uuid
from collections import Counter
from dataclasses import replace
from datetime import UTC, datetime
from typing import Any

from .engine import Event, Op
from .km2 import KM2Client
from .state import Link, State, iso


class Writer:
    """Record writes through the client, or (dry run) only counted and given placeholder ids."""

    def __init__(self, client: KM2Client, *, dry_run: bool = False) -> None:
        self.client = client
        self.dry_run = dry_run
        self.created: Counter[str] = Counter()
        self.updated: Counter[str] = Counter()
        self.deleted: Counter[str] = Counter()

    def create(self, slug: str, data: dict[str, Any]) -> dict[str, Any]:
        self.created[slug] += 1
        if self.dry_run:
            return {"id": str(uuid.uuid4()), **data}
        return self.client.create_record(slug, data)

    def update(self, slug: str, record_id: str, patch: dict[str, Any]) -> None:
        self.updated[slug] += 1
        if not self.dry_run:
            self.client.update_record(slug, record_id, patch)

    def delete(self, slug: str, record_id: str) -> None:
        self.deleted[slug] += 1
        if not self.dry_run:
            self.client.delete_record(slug, record_id)

    def total(self) -> int:
        return sum(self.created.values()) + sum(self.updated.values()) + sum(self.deleted.values())


def _resolved(issue_id: str | None) -> str | None:
    return None if not issue_id or issue_id.startswith("pending:") else issue_id


def event_payload(state: State, event: Event) -> dict[str, Any]:
    issue = state.issues.get(event.issue_key)
    return {
        "event_id": event.event_id,
        "issue_key": event.issue_key,
        "kind": event.kind,
        "from_value": event.from_value,
        "to_value": event.to_value,
        "actor_name": event.actor_name,
        "sim_day": event.sim_day,
        "sim_at": iso(event.sim_at),
        "issue": _resolved(issue.id) if issue else None,
    }


def _ensure_issue(state: State, op: Op, writer: Writer, created_event: dict[str, Any] | None) -> None:
    key = op.issue_key
    assert op.create is not None
    record_id = state.actual_identifiers.get(key)
    if record_id is None:
        payload = {**op.create, "parent": _resolved(op.create.get("parent"))}
        record_id = writer.create("trk_issue", payload)["id"]
        state.actual_identifiers[key] = record_id
    state.issues[key] = replace(state.issues[key], id=record_id)
    if created_event is None:  # resumed: the event came from an earlier, interrupted run
        created_event = next(
            (e for e in state.pending_events.values() if e.get("kind") == "created" and e.get("issue_key") == key),
            None,
        )
    if created_event is not None and created_event.get("issue") != record_id:
        writer.update("trk_event", created_event["id"], {"issue": record_id})


def _ensure_link(state: State, op: Op, writer: Writer) -> None:
    assert op.link_add is not None
    src, dst, kind = op.link_add
    existing = state.actual_links.get(op.link_add)
    if existing is None:
        rec = writer.create(
            "trk_issue_link",
            {
                "kind": kind,
                "from_key": src,
                "to_key": dst,
                "active": True,
                "from_issue": _resolved(state.issues[src].id),
                "to_issue": _resolved(state.issues[dst].id),
            },
        )
        existing = Link(rec["id"], kind, src, dst, True)
    elif not existing.active:
        writer.update("trk_issue_link", existing.id, {"active": True})
        existing = replace(existing, active=True)
    state.actual_links[op.link_add] = existing
    state.links = [existing if (lk.from_key, lk.to_key, lk.kind) == op.link_add else lk for lk in state.links]


def _remove_links(state: State, op: Op, writer: Writer) -> None:
    for triple in op.link_remove:
        existing = state.actual_links.get(triple)
        if existing is None:
            continue
        if existing.active:
            writer.update("trk_issue_link", existing.id, {"active": False})
        state.actual_links[triple] = replace(existing, active=False)
        state.links = [
            replace(lk, id=existing.id) if (lk.from_key, lk.to_key, lk.kind) == triple else lk for lk in state.links
        ]


def _ensure_comment(state: State, op: Op, writer: Writer, event_was_new: bool) -> None:
    assert op.comment is not None
    issue = state.issues[op.issue_key]
    posted = iso(op.at)
    if not event_was_new and not writer.dry_run:
        found = writer.client.list_records(
            "trk_comment", [f"issue_key:eq:{op.issue_key}", f"sim_posted_at:eq:{posted}"]
        )
        author = state.person_name(op.comment.author)
        if any(c.get("author_name") == author for c in found):
            return
    writer.create(
        "trk_comment",
        {
            "body": op.comment.body,
            "issue_key": op.issue_key,
            "project_key": state.project.key,
            "author_name": state.person_name(op.comment.author),
            "mentions": ",".join(op.comment.mentions),
            "sim_posted_at": posted,
            "issue": _resolved(issue.id),
            "author": op.comment.author,
        },
    )


def write_op(state: State, op: Op, writer: Writer, existing_ids: set[str]) -> None:
    created_event: dict[str, Any] | None = None
    new_ids: set[str] = set()
    for event in op.events:
        if event.event_id in existing_ids:
            continue
        rec = writer.create("trk_event", event_payload(state, event))
        existing_ids.add(event.event_id)
        new_ids.add(event.event_id)
        if event.kind == "created":
            created_event = rec
    if op.create is not None:
        _ensure_issue(state, op, writer, created_event)
    elif op.patch:
        issue_id = _resolved(state.issues[op.issue_key].id)
        if issue_id:
            writer.update("trk_issue", issue_id, op.patch)
    if op.link_add is not None:
        _ensure_link(state, op, writer)
    if op.link_remove:
        _remove_links(state, op, writer)
    if op.comment is not None:
        comment_event = next((e for e in op.events if e.kind == "comment"), None)
        _ensure_comment(state, op, writer, comment_event is not None and comment_event.event_id in new_ids)


def write_ops(state: State, ops: list[Op], writer: Writer, existing_ids: set[str]) -> None:
    for op in ops:
        write_op(state, op, writer, existing_ids)


def save_clock(state: State, writer: Writer, day: int) -> None:
    state.sim.sim_day = day
    state.sim.sim_date = state.sim_date_for(day)
    if state.sim.id:
        writer.update(
            "trk_sim_state",
            state.sim.id,
            {"sim_day": day, "sim_date": iso(state.sim.sim_date), "advanced_at": datetime.now(UTC).isoformat()},
        )
