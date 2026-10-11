---
title: Watches
category: Automate
order: 12
permissions: schedules.view
summary: How a schedule's watch checks a source before each run and only starts a work order when something changed or a follow-up is due.
keywords: watch, watches, tracker, jira, linear, tracking list, roll-up, rollups, new comments, only when changed, skip, no changes, changes, snapshot, source, JSON, HTTP, web page, CSS selector, CSV, workspace file, preview, dry run, run when, tokens saved, runs skipped, followups_due
---
A schedule normally starts a work order every time it fires. A **watch** makes it check a source first. Code compares what it finds with what it saw last time, with no AI involved. When nothing changed, no work order starts and the check costs no tokens. When something did change, the work order gets the list of changes as its `changes` input, so the crew only reads what is new.

Open a schedule to find its **Watch** panel. A schedule has at most one watch.

![A schedule's Watch panel with its status, the checks of the last 30 days, the tracking-list source and Start a run when.](watch-panel.png)

## What a watch reads

| Source | What it compares |
|---|---|
| JSON over HTTP | A list of items from an API, read through one of the crew's HTTP [connections](/help/connections). You give a path such as `/issues?state=open` (added to the connection's base URL) and the dotted path to the list in the response, such as `data.items`. |
| Web page | The text of a public page, or of the parts matching a CSS selector such as `main .status`. Private and local addresses are blocked, as for the Scrape Website tool. |
| Workspace file | A CSV or JSON file in the Studio workspace's `watch/` folder, such as one another system drops into `watch/courses.csv`. Every CSV row is an item. Files elsewhere in the workspace cannot be used. |
| Tracker project | The open issues of a Jira project, Linear team or KM2 project, read through one of the crew's [tracker connections](/help/trackers). For Jira you can add a JQL clause, such as `labels = atlas`. |
| Tracking list | Every item on the crew's tracking list (its **State** tab) with its child tickets, all levels or direct children only, as each entry says. The run also gets a roll-up per tracked item. |

For lists, the **ID field** says what identifies each item (such as `key`, or a CSV column), and the **Tracked fields** are the fields to compare (such as `status, due_date, assignee`). Use dotted paths such as `fields.status.name` for nested JSON. Only tracked fields are kept, which keeps the snapshot small.

Tracker items are identified by their key, such as `jira:PRJ-15`, and you pick the fields to compare from `title`, `status`, `status_category`, `assignee`, `due_date`, `priority`, `updated_at`, `parent` and `labels` (by default status, assignee, due date and priority). Turn on **New comments** to also pass on comments added since the last check: up to 500 characters each and 50 per check, leaving out the connection's own comments. The crew reads whole threads with the Get Tracker Issue tool.

Long text (over 300 characters) is compared by a fingerprint, so the change list says that it changed but not how. A watch fails with a clear message, instead of silently cutting the list, when a source returns more items than its **Max items** (2,000 unless you change it).

## When it starts a run

| Start a run when | Behaviour |
|---|---|
| Something changed | The default. No changes, no run. |
| Changed, or a follow-up is due | Also runs when a step of the [follow-up ladder](/help/follow-up-ladders) is due. |
| Every time | Always runs, but still passes the changes along. |

On every tick the watch records a check in **Recent checks**: whether it started a run, found no changes, was skipped because the previous work order was still running, or failed (and why). A failed check also shows on the schedule as its last problem; the schedule is not paused. **Run now** always starts a work order and passes the current changes.

![Recent checks: a first check that started a run and a later check that found no changes.](watch-checks.png)

The first check has nothing to compare with, so every item counts as added and `previous_check_at` is empty. Tell the crew to write a baseline in that case, not to announce every item as new.

What the watch saw is saved only after the work order is created, so a crash in between means the next check sees the same changes again rather than losing them.

## What the crew receives

A watched work order gets these inputs:

| Input | What it holds |
|---|---|
| `changes` | JSON: the source, `checked_at`, `previous_check_at`, `added`, `changed` (old and new value of each field), `removed` and `unchanged_count` |
| `followups_due` | JSON: the follow-up steps due now, how many more wait, and how to record them (only with a ladder) |
| `rollups` | JSON, tracking lists only: one roll-up per tracked item with changes beneath it (see below) |
| `dry_run` | `true` or `false` |

The crew does not need to declare these inputs, and only a schedule's watch can set them: a work order started any other way that names them is refused. If a task names `{changes}` or `{followups_due}`, they are filled in there; otherwise the first task is given them in a section of its description. The work order page shows them readably under **What the watch found**.

## Roll-ups

For each tracked item, code counts its children by status (done, in progress, to do, canceled) and lists the ones that are **overdue**, **due after the tracked item itself** (an early warning of a slip), **blocked** (the status or a label says so), **unassigned** and **stale** (open and not updated for 14 days, or the **Stale after** you set). It also counts new comments and says whether anything beneath the item changed since the last check. Only tracked items with changes beneath them are passed to the crew, so a project where nothing moved costs nothing. The work order page shows them under **Roll-ups**.

## Dry run

New watches start in **dry run**. The crew is told not to post, send or publish anything (the [tracker tools](/help/trackers#what-crew-studio-enforces) refuse to), and to put what it would have sent in its output under a **Would post** heading, saying where each message would go and to whom. Action log entries it records are marked dry run on the crew's State tab. Check a few dry runs, then turn dry run off. Dry-run entries do not count as sent once you do, so real reminders go out.

## Set up a watch
<!-- permissions: schedules.manage -->
1. Save the schedule first, then click **Add a watch**.
2. Pick the source and fill in its fields. For JSON over HTTP, the connection must be enabled, chosen for the crew on its State tab, and share a functional area with it.
3. Choose when to start a run, and leave **Dry run** on to begin with.
4. Click **Preview**. Studio fetches the source now and shows the changes and follow-ups a check would see, plus the first items as read, so you can check the field names. Preview saves nothing and starts nothing.
5. Click **Add watch** (or **Save watch**).

![A preview of a tracking-list watch: it would not run, with the roll-up of the tracked item and the first items as read.](watch-preview.png)

Changing the source, ID field or tracked fields starts again from a first check. **Remove watch** deletes the watch, its snapshot and its history; the schedule then runs every time.

## Clear the snapshot
<!-- permissions: schedules.manage, state.manage -->
**Clear snapshot** forgets what the watch saw, so the next check is a first check again. Use it after fixing a source that returned the wrong items. It needs both **schedules.manage** and **state.manage**.

## What a watch saves

The panel and the crew's State tab show, for the last 30 days, how many checks ran, how many runs started and how many were skipped. **Est. tokens saved** multiplies the skipped runs by the crew's average tokens per run over its recent finished runs, so it is a guide, not a bill.
