---
title: Report delivery
category: Automate
order: 15
permissions: schedules.view
summary: How a crew's report goes to the work order, Slack, Microsoft Teams, email and KM2, what the designed report shows, how to set a schedule's report destinations, and how to retry a delivery that failed.
keywords: report, weekly report, daily report, designed report, due report, overdue, chart, workload, download html, snapshot, cadence, deliver report, delivery, destination, slack, teams, microsoft teams, workflows, power automate, webhook, email, smtp, km2, retry, failed delivery, dry run, would send, report:slack, report:teams, report:email, report:km2
---
A crew delivers a finished report with the **Deliver Report** tool (the PMO's Status Reporter uses it). Crew Studio does the sending: one report goes to the work order and to every **report destination** set on the schedule that started the run. It also keeps the report as the project's `weekly-report` note, so next week's report builds on it.

A crew can also deliver a **daily report**: the Deliver Report tool takes a cadence, weekly (the default) or daily. A daily report is titled "<project> daily report, <date>" with the date in the schedule's time zone, goes to the same places, and is kept as its own `daily-report` note, so the next day's report can say what changed. It never replaces the weekly report or its note. A run can deliver one weekly and one daily report per project.

| Destination | What arrives |
|---|---|
| Work order | Always. The report is listed on the work order with where it went. |
| Slack | A message in a channel, through an incoming webhook or a bot. Long reports are cut to fit, with a pointer to Crew Studio. |
| Microsoft Teams | A card in a channel, through a Teams workflow. Long reports are cut to fit Teams' message size. |
| Email | A plain-text email (the Markdown as written) to the recipients you list. |
| KM2 | A document named `<project>-weekly-<week>`, such as `atlas-weekly-2026-W41`, or `<project>-daily-<date>`, such as `atlas-daily-2026-10-06`. Saving the same week (or day) again makes a new version, not a copy. |

Writing the report back to Jira or Linear is not available yet.

## Set a schedule's destinations
<!-- permissions: schedules.manage -->
Open the schedule and find **Report destinations**. Click **Add destination**, pick the kind and the connection, and for email list the recipients (at most 50). For KM2, pick the **Folder** from the list: the connection's default folder (shown by its path) or one of the other folders the connection allows. If the connection has no default folder, the list says "No default folder: saving is off" and a warning asks you to pick a folder or set a default on the connection. When Crew Studio cannot list the folders (for example the key lacks `knowledge:read`), it says why and you can type a folder id instead. A schedule can have up to 10 destinations. Only connections the crew may use (its **State** tab) and that share a functional area with the schedule are offered. Click **Save destinations**.

![A schedule's Report destinations panel with Slack, email (with two recipients) and KM2 destinations.](schedule-destinations.png)

## Slack, Teams and email connections
<!-- permissions: connections.manage -->
Add these on [Connections](/connections), then click **Test**: Slack and Teams get a test message, and email sends one to you.

- **Slack.** Either an incoming webhook URL (`https://hooks.slack.com/services/...`) from a Slack app, which posts to the channel it was made for, or a bot token (`xoxb-...` with the `chat:write` scope) plus a channel. Invite the bot to the channel.
- **Microsoft Teams.** Office 365 connectors (the old "Incoming Webhook") stopped working in May 2026. In Teams, open the channel's **Workflows** and create one from the **Send webhook alerts to a channel** template (its trigger is "When a Teams webhook request is received"). Set who can trigger it to **Anyone**, since Crew Studio sends no sign-in token, and paste the workflow's URL as the secret. The URL must be on a `powerplatform.com` address. The URL carries its own signature, so treat it like a password.
- **Email.** The SMTP host, port, security (`starttls`, usually port 587, or `tls`, usually 465), user name, from address and password. `none` (no encryption) only works with a relay on the same machine as Crew Studio. The port must be 25, 465, 587 or 2525. **Allowed recipient domains** (optional) keeps reports to your own domains; recipients do not see each other's addresses.

## Dry run
In a dry-run schedule nothing is sent. Each destination shows **would send** on the work order and gets a dry-run entry in the crew's action log; they never count as sent.

## Check and retry deliveries
<!-- permissions: work_orders.view -->
Open the work order and find **Reports**. Each report shows every destination with **sent**, **would send** or **failed**, and why a delivery failed.

Click **Show report** to read it. When Crew Studio could read the project's tracker as the report was delivered, you see the **designed report**: Crew Studio's own counts (overdue, due within 7 days, blocked, unassigned and due soon, done since the last report), each with its change since the previous report of the same cadence and a small trend line, the crew's summary, charts of workload by person, what is due over the next 14 days and the status mix, and tables of the overdue, due, blocked and unassigned items with links to the tracker. Every chart has a **Table** view. **Text** shows the report as the crew wrote it, and **Download HTML** saves the page to share; it runs no scripts and loads nothing from elsewhere.

![A work order's Reports tab: a weekly report sent to the work order, email and KM2, a failed Slack delivery, the Retry failed deliveries button, and the designed report with its counts and the crew's summary.](work-order-reports.png)

People who can create work orders see **Retry failed deliveries**. It sends only what failed: deliveries that went out are recorded in the action log as `report:slack`, `report:teams`, `report:email`, `report:km2` and `report:work_order`, and are never sent twice. Fix the cause first (a connection turned off, a bot not in its channel, an expired workflow), then retry.

## What the crew writes, and what Crew Studio adds
The figures in a report come from Crew Studio, not the model: when a report is delivered, Crew Studio reads the project's tracked items (the crew's tracking list) itself, in the schedule's time zone, and keeps them with the report. The crew's text is the **narrative**: what is on track, the critical path, risks and asks. It should not re-list items; the report's tables already do. Email gets the designed page with the text as a plain-text copy, Slack and Teams a short summary (the counts, the five most overdue items, then the narrative), and KM2 the tables and the narrative as Markdown.

If the tracker cannot be read within 20 seconds, or fails, the report is still delivered, as the crew wrote it, and the action log says why the figures were left out.

## Limits
- A report is at most 64 KB.
- A run delivers at most 20 reports and makes at most 60 sends.
- A run delivers each project's report once; sending the same report again changes nothing.
- KM2 saves follow the KM2 save limits: 10 documents per run and 200 per connection a day, and the crew must have exactly
  one KM2 connection.
