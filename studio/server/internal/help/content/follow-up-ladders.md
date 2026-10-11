---
title: Follow-up ladders
category: Automate
order: 13
permissions: schedules.view
summary: How a watch works out which reminders are due for each item from its date, so the crew sends the right nudge or escalation once.
keywords: follow-up, followup, ladder, reminder, nudge, escalate, escalation, due date, overdue, due soon, quiet days, weekend, repeat, offset, notify, owner, lead, skip, Record Action, action log, dry run, per-check cap
---
A follow-up ladder is a set of reminder steps tied to a date on each item a [watch](/help/watches) reads, such as a due date. Code decides which item is due which step; the crew writes the message in the right tone and posts it. A ladder belongs to a schedule's watch and needs a source with items (JSON over HTTP or a workspace file).

## Steps

Each step has:

| Field | Example | Meaning |
|---|---|---|
| Name | `due_soon` | Lowercase letters, digits, `_` or `-`. The action log records it as `followup:due_soon`. |
| Offset | `-3d`, `0d`, `+2d`, `1w` | Days (or weeks) before or after the item's date. |
| Notify | `owner`, `lead`, `assignee` | Who to tell: a role the crew understands, or a tracked field holding the person. When it names a tracked field, the value is passed along. |
| Repeat | `7d` | Optional. The step is due again once this long has passed since it was last recorded. |

A typical ladder: `due_soon` at -3d to the owner, `due_today` at 0d to the owner, `overdue_escalate` at +2d to the lead, and `overdue_weekly` at +9d to the lead, repeating every 7d.

## When a step is due

On every check, for every item:

- The item is left out when a **skip** rule matches, such as status is one of `Done, Canceled` (compared ignoring case), or when its date is empty or not a date (use `2026-10-20` or a full date and time).
- A step's date is the item's date plus its offset, in the ladder's time zone (the schedule's unless you set one). A step falling on a **quiet day** moves to the next working day, and nothing is due on a quiet day.
- A step is due when its date has passed and the crew's action log has no entry for that item and step recorded on or after that date, or, for a repeating step, none within the repeat period (counted in days, so a reminder recorded a few minutes after a check comes due again at the same check next period).
- When several steps' dates have passed, only the latest one is due. Someone already two days late is escalated, not also told it is due soon.
- At most **Most per check** steps (25 unless you change it) go to one run, oldest first. The rest wait for the next check.

A moved date is handled for you: offsets are worked out from the item's current date on every check, and a step recorded for the old date comes due again for the new one. Items whose date is empty or not a date are counted in the preview and the run's input, so you can spot them.

With **Start a run when** set to **Changed, or a follow-up is due**, a due step starts a run even when nothing else changed.

## What the crew must do

The run's `followups_due` input lists each due step with the item, its subject, the step, the action (such as `followup:due_today`), who to notify and the item's tracked fields. Give the crew the **Record Action** tool and tell it, in its task, to record each follow-up it sends with that subject and action. Until it is recorded, the step stays due and comes back on the next check. Tracker tools in a later release record their posts themselves.

In a [dry run](/help/watches#dry-run), recorded follow-ups are dry-run entries. They stop the step repeating during the dry run, but they do not count as sent once dry run is off.

## Set up a ladder
<!-- permissions: schedules.manage -->
In the schedule's **Watch** panel, turn on **Follow-up ladder**, pick the **Date field** (one of the tracked fields), adjust the steps, skip rules and quiet days, then click **Preview** to see which steps are due today before you save.

![The follow-up ladder editor with the date field, four steps, a skip rule and Saturday and Sunday as quiet days.](watch-ladder.png)
