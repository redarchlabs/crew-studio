---
title: Set up a PMO
category: Automate
order: 14
permissions: crews.create, schedules.manage
summary: How to set up the ready-made PMO crew in one step: a daily follow-up sweep over your Jira, Linear or KM2 issues tracking list, a weekly status report sent to Slack, Teams, email and KM2, and chat with the coordinator.
keywords: pmo, project management office, project manager, status report, weekly report, follow-up, sweep, coordinator, status reporter, tracking list, jira, linear, wizard, template, set up a pmo, dry run, deliver report
---
The **PMO** is a ready-made crew that keeps tracked projects moving. It follows up on due and overdue work in Jira, Linear or KM2 issues, suggests corrections, keeps a risk register, and writes a weekly status report per project. It never changes tracker fields, never marks anything done and never creates issues: it comments, suggests and asks.

It has two agents:

| Agent | Does |
|---|---|
| PMO Coordinator | Runs the daily follow-up sweep: posts follow-up comments and suggested changes, and keeps the risk-register note. It is also the crew's chat front desk, where you tell it what to track. |
| Status Reporter | Writes the weekly report for each project from the roll-ups, last week's report and the risk register (and KM2 context, when the crew has a KM2 connection), and delivers it. |

The template is the crew **PMO (template)** with its two agents and tasks. You can read and change their prompts and models like any other; the wizard copies whatever is there.

## Before you start
<!-- permissions: connections.view -->
You need a [tracker connection](/help/trackers) (Jira, Linear or KM2 issues) in the functional area the PMO will work in. For the weekly report, also add the connections it should go to: Slack, Microsoft Teams, email or KM2 (see [Report delivery](/help/delivery)). You can add destinations later on the weekly schedule.

## Run the wizard
<!-- permissions: crews.create -->
On the [Crews](/crews) page, click **Set up a PMO** and choose:

1. **Name and areas.** The crew, its agents, tasks and schedules are named after it, so one team can run several PMOs. The suggested name follows the tracker connection, such as "Acme Jira PMO". A name already used by another crew, agent, task or schedule (plain "PMO" would clash with the built-in template's "PMO Coordinator") is shown on this step so you can change it.
2. **Tracker.** The Jira, Linear or KM2 issues connection the PMO follows.
3. **Time zone.** The schedules run in it: the sweep on weekdays at 07:00, the report on Mondays at 08:00.
4. **Report destinations.** Where the weekly report goes besides the work order. For email, list the recipients. For KM2, pick the folder from the folders the connection allows; a KM2 destination whose connection has no default folder needs a folder picked, or the wizard stops at this step.
5. **Models.** The coordinator starts on a fast, cheap model (it also answers chat) and the reporter on a stronger writer, picked from your LLMs. If a model's provider key (such as `ANTHROPIC_API_KEY`) is not set in Crew Studio's provider env file, the wizard warns you: runs on that model would fail to sign in to the provider. Whoever runs Crew Studio can add the key.

![The Set up a PMO wizard at its Report destinations step, with a Slack and an email destination.](pmo-setup.png)

![The last step of the wizard, with the two models, Turn the schedules on, and a summary of what will be set up.](pmo-setup-review.png)

Click **Set up**. Crew Studio creates everything at once, or nothing if something is wrong:

- the crew with copies of the two agents and their tasks, allowed to use the connections you chose;
- chat, with the coordinator as front desk;
- a **daily sweep** schedule whose [watch](/help/watches) reads the tracking list, tracks new comments and runs when something changed or a [follow-up](/help/follow-up-ladders) is due. Its ladder: a heads-up to the owner three days before the due date and on the day, the lead two days after, and the lead every week from nine days after; nothing on Saturdays and Sundays;
- a **weekly report** schedule that runs every Monday and sends the report to your destinations.

The daily sweep can also write a **daily report**, if its task asks for one: the Deliver Report tool's daily cadence files it as "<project> daily report, <date>" on the sweep's work order and sends it to the sweep schedule's own report destinations (add them on that schedule). It never touches the weekly report. In dry run it is kept on the work order's **Reports** tab with each destination marked "would send".

Crew Studio adds the figures to every report itself: overdue, due-soon, blocked and unassigned work, who has what, and what changed since the last report, with charts (see [Report delivery](/help/delivery)). So a report task should ask for a short narrative, not a list of items. For example, a daily task could say: "For each tracked project, deliver a daily report with cadence daily. Write three to six sentences: whether the project is on track, the critical path, new risks and what you need from people. Do not list the items; Crew Studio adds the tables and charts."

Each schedule runs only its own job: its `run_tasks` input names the task.

## Tell it what to track
<!-- permissions: crews.chat -->
Open [Chat](/chat), pick the PMO and say, for example, "track PRJ-10 and its sub-tasks as Project Atlas". Click **Confirm** on the card. The next sweep reads the item and its children. Ask "what are you tracking?" or "what's slipping in Atlas?" at any time.

## Dry run first
Both schedules start in **dry run**: comments, suggestions and reports are not sent. The work order lists what would have been posted and where each report would have gone. When the output looks right, open each schedule's **Watch** panel and turn dry run off. Entries recorded in dry run never count as sent.
