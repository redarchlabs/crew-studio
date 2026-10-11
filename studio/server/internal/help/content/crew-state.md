---
title: Crew state
category: Build
order: 15
permissions: state.view, connections.view
summary: How crews remember what they saw and did between runs with notes and an action log, and how to see, correct and reset that memory on the crew's State tab.
keywords: memory, state, notes, tracking list, track, stop tracking, tracker, jira, linear, working memory, action log, log, remember, reminder, follow-up, idempotency, version, rollback, restore, schedule, subject, Read Note, Write Note, List Notes, Check Action Log, Record Action, connections
---
Each run of a crew normally starts from nothing. Crew state lets a crew keep two kinds of memory between runs, both visible to people:

- **Notes** are named texts the crew writes and reads again later, such as last week's report or a list of risks. Every save is a new version, so nothing is lost.
- The **action log** is an exact record of what the crew did, to what, and when, such as "reminded Sam about PRJ-15 on Oct 4". A crew checks it before acting, so it does not do the same thing twice.

Open a crew and click the **State** tab to see its notes, action log and connections.

For softer lessons a crew picks up over time, such as "Sam usually replies late on Fridays", see [Memory](/help/memory) and the crew's **Memory** tab.

## Giving a crew memory

Give the crew's agents these tools in the agent editor:

| Tool | What it does |
|---|---|
| Read Note | Reads the latest version of a note, with its version number |
| Write Note | Saves a new version of a note (up to 64 KB) |
| List Notes | Lists the notes with their versions and sizes |
| Check Action Log | Says when the crew last did something, optionally within a period such as 7d |
| Record Action | Adds an entry to the action log |

Tell the agents in their task how to use them, for example: "Read the note weekly-report first and only report what changed. Save the new report as weekly-report."

A note or log entry can be about a **subject**, a free-form key such as `project:atlas` or `jira:PRJ-15`, so one crew can keep separate notes per project.

## Where state belongs

State belongs to the crew. Runs started by a [schedule](/help/schedules) keep their own notes and log entries, separate from other schedules of the same crew. Runs started by hand or by a work order use the crew-wide state.

People see a crew's state only when the crew is in one of their functional areas. A run can only read and write its own crew's state; it cannot reach any other crew's.

Every call a tool makes is written in that run's journal, such as "Notes: read weekly-report v12" or "Log: recorded followup:due_soon for jira:PRJ-15", so you can see what the crew looked at and changed.

## Watches
<!-- permissions: state.view -->
The **Watches** panel lists the [watches](/help/watches) on this crew's schedules: what each reads, when it last checked and last saw a change, any problem, and how many runs it skipped in the last 30 days. Follow-ups a watch passes to the crew are recorded in the action log as `followup:<step>` entries.

![A crew's State tab with the Watches, Tracking list, Notes and Recent KM2 saves panels.](crew-state.png)

## Tracking list
<!-- permissions: state.view -->
The **Tracking list** panel shows the tracker items this crew follows, such as PRJ-10 for project Atlas, with whether their children are included, the lead escalations go to (by name once the connection's [people](/help/trackers) are read, otherwise by tracker account id), a note, and who added each one and when. A schedule with a **Tracking list** [watch](/help/watches) reads each item with its children and rolls their status up. **Show removed items** shows the history.

Runs can only read the list. Only people change it, here or in chat, so a ticket comment cannot make a crew stop tracking a project.

## Track or stop tracking an item
<!-- permissions: state.manage -->
1. Click **Track an item**.
2. Pick the Jira or Linear connection, type the item's key (such as `PRJ-10`), and optionally a project name for reports, how many levels of children to include, a lead and a note.
3. Click **Look up**. Crew Studio checks that the crew may use the connection and that the item exists, and shows its title, status and number of children.
4. Click **Confirm and track** if it is the right item.

![The Tracking list panel with an item looked up and waiting for Confirm and track.](crew-state-track-item.png)

Click **Stop tracking** on an item, then **Confirm?**, to remove it. It stays in the history. Changes take effect on the next check.

## Notes and their versions
<!-- permissions: state.view -->
The **Notes** panel lists the latest version of each note with its subject, schedule (or **Crew-wide**), version, size and who wrote it last: **run** for a crew run, or a person's name.

Click **History** to see every version. Click a version on the left to read it.

![A note's history, with its versions and who wrote each on the left and the selected version's text on the right.](crew-state-note-history.png)

When two runs (or a run and a person) change the same note at once, the second save is refused instead of overwriting the first, and the agent is told to read the note again.

## Edit, roll back and delete notes
<!-- permissions: state.manage -->
In a note's history:

- **Edit** changes the latest version and saves it as a new one, with your name.
- **Restore** on an older version saves its text as the newest version. The versions in between are kept.
- **Delete note**, then **Confirm?**, removes the note and all its versions. The next run starts that note afresh. Deleting is recorded in the audit log.

## The action log
<!-- permissions: state.view -->
The **Action log** panel lists entries newest first: when, the subject, the action, the schedule, any details, and a link to the run that recorded it. Type part of a subject or action and click **Filter** to narrow the list.

![The Action log panel with report, KM2 save, tracker suggestion and dry-run follow-up entries, below the Recent KM2 saves panel.](crew-state-action-log.png)

An agent can give each entry an idempotency key, such as `PRJ-15/due_soon/2026-10-04`. Recording the same key again adds nothing, so a retried tool call is never logged twice.

## Delete log entries
<!-- permissions: state.manage -->
Click **Delete** on an entry, then **Confirm?**. Do this when an entry is wrong, for example a reminder that was never actually sent, so the crew will act again. Deleting is recorded in the audit log.

## Connections a crew may use
<!-- permissions: connections.view -->
The **Connections** panel lists the [connections](/help/connections) this crew's tools may use. A run only gets the ones that are enabled and share a functional area with the crew.

![The Connections panel of a crew's State tab, listing the connections it may use.](crew-state-connections.png)

## Change a crew's connections
<!-- permissions: crews.edit -->
Click **Change**, pick the connections the crew may use, then click **Save connections**. You can only pick connections in your own areas; links to connections outside your areas stay as they are.
