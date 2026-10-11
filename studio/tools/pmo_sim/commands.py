"""The CLI commands, as functions of (client, options, output) so tests can drive them directly."""

from __future__ import annotations

from collections import Counter
from collections.abc import Callable
from datetime import date, timedelta
from pathlib import Path
from typing import Any

from . import blueprint as bp
from .apply import Writer, save_clock, write_ops
from .bundle import STATUSES, bundle_bytes, describe_plan, plan_errors
from .engine import STALE_DAYS, plan_day, wants_reply
from .km2 import KM2Client
from .seed import Seeder, SeedError, check_schema, delete_all
from .state import State, is_workday, load_state

Out = Callable[[str], None]

TEARDOWN_STEPS = (
    "Remaining manual teardown (KM2 has no API for deleting entity definitions):",
    "  1. Admin -> API in the PMO Sandbox org: revoke the Simulator, Crew Studio tracker and (if still there)",
    "     Bootstrap keys; remove the km2_tracker connection secret in Crew Studio.",
    "  2. Admin -> Entities: delete the definitions in this order (dropping a definition drops its table):",
    "     trk_comment, trk_event, trk_issue_link, trk_sim_state, trk_issue, trk_cycle, trk_project, trk_person.",
    "  3. Site Admin -> Organizations: delete the PMO Sandbox org.",
)


# ---------------------------------------------------------------------- schema
def cmd_bundle(out_path: Path, out: Out) -> int:
    out_path.write_bytes(bundle_bytes())
    out(f"wrote {out_path} (KM2 bundle format v2; import it in Admin -> Migration -> Import with strategy 'skip')")
    return 0


def cmd_bootstrap(client: KM2Client, apply: bool, out: Out) -> int:
    payload = bundle_bytes()
    out("Dry run (POST /api/v1/config/promotions?dry_run=true&strategy=skip&apply_deletes=false):")
    preview = client.promote(payload, dry_run=True)
    for line in describe_plan(preview):
        out(f"  {line}")
    errors = plan_errors(preview)
    if errors:
        out("The dry run reported problems; not applying.")
        return 1
    if not apply:
        out("Nothing applied. Re-run with --apply to create the entities.")
        return 0
    result = client.promote(payload, dry_run=False)
    out("Applied:")
    for line in describe_plan(result):
        out(f"  {line}")
    if plan_errors(result):
        return 1
    out("Done. Revoke the bootstrap key in Admin -> API now.")
    return 0


# ---------------------------------------------------------------------- seed
def cmd_seed(client: KM2Client, today: date, seed_value: int, dry_run: bool, out: Out) -> int:
    problems = check_schema(client)
    if problems and not dry_run:
        for problem in problems:
            out(f"schema: {problem}")
        out("Run `bootstrap-schema --apply` first.")
        return 1
    writer = Writer(client, dry_run=dry_run)
    if problems:
        out("schema not installed yet; showing what a seed into an empty org would create")
    try:
        plan = Seeder(client, writer, out).run(today, seed_value, schema_ok=not problems)
    except SeedError as exc:
        out(str(exc))
        return 1
    verb = "would create" if dry_run else "created"
    out(
        f"Seed {'(dry run) ' if dry_run else ''}for {bp.PROJECT_NAME}: project start {plan.start}, "
        f"sim day {bp.SEED_DAY} = {plan.start + timedelta(days=bp.SEED_DAY)}"
    )
    for slug, count in sorted(writer.created.items()):
        out(f"  {verb} {count:>4} {slug}")
    if not writer.created:
        out("  nothing to create; the project is already seeded")
    return 0


# ---------------------------------------------------------------------- advance
def pending_bot_comments(client: KM2Client, state: State) -> list[dict[str, Any]]:
    """Bot comments that have not been replied to yet and that the seed says should get a reply."""
    bots = [p.id for p in state.bots()]
    if not bots:
        return []
    comments = client.list_in("trk_comment", "author", bots)
    known = {i.id for i in state.issues.values()}
    comments = [c for c in comments if c.get("issue") in known or c.get("issue_key") in state.issues]
    if not comments:
        return []
    reply_ids = [f"reply.{c['id']}" for c in comments]
    # A reply event dated after sim_day belongs to a day that did not finish: plan it again (the write is ensured).
    done = {e.get("event_id") for e in client.list_in("trk_event", "event_id", reply_ids)} - set(state.pending_events)
    pending = [c for c in comments if f"reply.{c['id']}" not in done and wants_reply(state.sim.seed, c["id"])]
    return sorted(pending, key=lambda c: (str(c.get("created_at") or ""), c["id"]))


def cmd_advance(client: KM2Client, today: date, days: int | None, dry_run: bool, out: Out) -> int:
    state = load_state(client)
    target = days if days is not None else (today - state.sim.sim_date).days
    if target <= 0:
        out(f"Already at sim day {state.sim.sim_day} ({state.sim.sim_date}); nothing to do. Use --days N to run ahead.")
        return 0
    writer = Writer(client, dry_run=dry_run)
    replies = pending_bot_comments(client, state)
    resumed = set(state.pending_events)
    if resumed:
        out(f"Resuming day {state.sim.sim_day + 1}: {len(resumed)} events were already written")
    for _ in range(target):
        day = state.sim.sim_day + 1
        sim_date = state.sim_date_for(day)
        todays_replies = replies if is_workday(sim_date) else []
        ops = plan_day(state, day, state.sim.seed, todays_replies)
        if is_workday(sim_date):
            replies = []
        before = writer.total()
        write_ops(state, ops, writer, resumed)
        resumed = set()
        save_clock(state, writer, day)
        kinds = Counter(op.kind for op in ops)
        detail = ", ".join(f"{k} {v}" for k, v in sorted(kinds.items())) or "quiet"
        out(
            f"day {day:>3} {sim_date} {sim_date.strftime('%a')}: {len(ops):>2} changes, "
            f"{writer.total() - before:>3} writes ({detail})"
        )
    suffix = " (dry run: nothing written)" if dry_run else ""
    out(f"Now at sim day {state.sim.sim_day} ({state.sim.sim_date}){suffix}")
    return 0


# ---------------------------------------------------------------------- status
def status_lines(state: State, client: KM2Client) -> list[str]:
    as_of = state.sim.sim_date
    issues = state.sorted_issues()
    tasks = [i for i in issues if not i.is_epic]
    epics = [i for i in issues if i.is_epic]
    cycle = state.current_cycle(as_of)
    lines = [
        f"{state.project.name} ({state.project.key}): sim day {state.sim.sim_day} = {as_of} {as_of.strftime('%a')}, "
        f"seed {state.sim.seed}",
    ]
    if cycle:
        lines.append(f"Current cycle: {cycle.name} ({cycle.starts_on} -> {cycle.ends_on})")
    counts = Counter(i.status for i in tasks)
    lines.append(f"Issues: {len(tasks)} stories/tasks/bugs, {len(epics)} epics")
    lines.append("  " + " | ".join(f"{s} {counts.get(s, 0)}" for s in STATUSES))
    epic_counts = Counter(i.status for i in epics)
    lines.append("  epics: " + " | ".join(f"{s} {n}" for s, n in sorted(epic_counts.items())))

    def show(title: str, items: list[Any]) -> None:
        lines.append(f"{title}: {len(items)}")
        for issue in items[:8]:
            due = f", due {issue.due_date}" if issue.due_date else ""
            lines.append(
                f"  {issue.identifier:<7} {issue.status:<11} {state.person_name(issue.assignee) or '-':<16} "
                f"{issue.title[:60]}{due}"
            )
        if len(items) > 8:
            lines.append(f"  ... and {len(items) - 8} more")

    open_tasks = [i for i in tasks if i.is_open]
    show(f"Overdue (due before {as_of})", [i for i in open_tasks if i.due_date and i.due_date < as_of])
    show("Blocked", [i for i in open_tasks if i.blocked])
    stale_cut = as_of - timedelta(days=STALE_DAYS)
    show(
        f"Stale (in progress, no activity since {stale_cut})",
        [i for i in open_tasks if i.category == "in_progress" and i.activity_at and i.activity_at.date() < stale_cut],
    )
    show("Unassigned (open)", [i for i in open_tasks if not i.assignee and i.status != "Backlog"])
    active_links = sum(1 for lk in state.links if lk.active)
    comments = client.list_records("trk_comment", [f"project_key:eq:{state.project.key}"])
    day_events = client.list_records("trk_event", [f"sim_day:eq:{state.sim.sim_day}"])
    lines.append(
        f"Links: {len(state.links)} ({active_links} active); comments: {len(comments)}; "
        f"events on day {state.sim.sim_day}: {len(day_events)}"
    )
    return lines


def cmd_status(client: KM2Client, out: Out) -> int:
    state = load_state(client, revert=False)
    for line in status_lines(state, client):
        out(line)
    return 0


# ---------------------------------------------------------------------- reset / teardown
def cmd_reset(client: KM2Client, yes: bool, out: Out, *, teardown: bool = False) -> int:
    writer = Writer(client, dry_run=not yes)
    out(
        "Deleting all trk_* records (children first); the entity definitions stay."
        if yes
        else "Would delete (re-run with --yes to do it):"
    )
    delete_all(client, writer, out)
    if yes and not teardown:
        out("Done. Run `seed` to start the project again.")
    if teardown:
        for line in TEARDOWN_STEPS:
            out(line)
    return 0 if yes else 1


__all__ = [
    "cmd_advance",
    "cmd_bootstrap",
    "cmd_bundle",
    "cmd_reset",
    "cmd_seed",
    "cmd_status",
    "pending_bot_comments",
    "status_lines",
]
