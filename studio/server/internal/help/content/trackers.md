---
title: Jira, Linear and KM2 issues
category: Configure
order: 36
permissions: connections.view, state.view, crews.chat
summary: How crews read Jira, Linear and KM2 issues, post comments that mention people, follow a tracking list with roll-ups, and what Crew Studio enforces so they cannot spam anyone.
keywords: jira, linear, km2, km2 issues, km2_tracker, trk_issue, custom entities, records, tracker, issue, ticket, comment, mention, @mention, people map, tracking list, roll-up, rollup, sub-task, epic, children, follow-up, dry run, mention cap, comment cap, suggested change, create issue, JQL, API token, API key, atlassian
---
Crews can work with **Jira Cloud**, **Linear** and **KM2 issues** (projects and issues kept as custom entities in a KM2 organisation) through [connections](/help/connections). Crew Studio calls the tracker itself, so the API token or key never reaches a run. Crews read issues and their comments, post comments that @mention people (the tracker then sends its own email, Slack or Teams notification), and suggest changes. They never change an issue's fields.

## Connect a tracker
<!-- permissions: connections.manage -->
On [Connections](/connections), click **New connection** and pick the kind:

| Kind | Settings | Secret |
|---|---|---|
| Jira Cloud | Site URL (`https://<your-site>.atlassian.net`), the account's email, optionally a Cloud ID | API token |
| Linear | none | A personal API key |
| KM2 issues (tracker) | Base URL, Crew Studio's person email, optionally the entity prefix, activity field and issue link | An org-wide KM2 API key |

The Jira site must be an `atlassian.net` address, unless whoever runs Crew Studio allowed another one with the `STUDIO_JIRA_SITES` setting, so a token can never be sent anywhere else. Leave **Cloud ID** empty for a classic API token. A scoped API token must call Atlassian's API gateway instead of the site: enter the site's cloud id, which `https://<your-site>.atlassian.net/_edge/tenant_info` shows.

Use a dedicated account for Crew Studio. Its own comments are never treated as news, so a crew does not react to itself.

Click **Test**. It answers with the account the credentials belong to, such as "Connected as Studio Bot."

### KM2 issues

A KM2 issues connection reads a KM2 organisation that keeps its work as custom entities: `trk_person`, `trk_project`, `trk_issue` and `trk_comment` (another prefix can be set under **Entity prefix**). Before adding it:

1. In KM2, set up the tracker entities in the organisation (the fields are listed in `docs/trackers.md`, "KM2 issues"), and add a person record for Crew Studio, such as "Crew Studio", `crew-studio@example.com`.
2. Under **Admin → API** in that organisation, mint an org-wide key with **entities:read**, **records:read** and **records:write**. A KM2 profile key (the kind a KM2 knowledge connection uses) cannot read records.
3. On Connections, pick **KM2 issues (tracker)**: the **Base URL** of KM2 (https), **Crew Studio's person email** (the person record from step 1), and the key as **Org API key**. **Activity field** names the issue field that says when an issue last changed, if it is not KM2's own `updated_at`. **Issue link** is an optional address of an issue in KM2, with `{id}` or `{key}` in it.

**Test** answers with the person, and checks that the organisation has the entities and fields Crew Studio needs; if not, it lists what is missing, such as "trk_issue lacks the fields labels". The issue's status category must be a picklist of exactly `to_do`, `in_progress`, `done` and `canceled`, and its identifier should be unique (Test warns when it is not). Whoever runs Crew Studio can limit KM2 issues connections to certain KM2 addresses with the `STUDIO_KM2_TRACKER_SITES` setting. Comments are posted as the Crew Studio person. KM2 sends no notifications, so a mention names the person at the top of the comment ("@Sam Lee") and records who was mentioned. Crew Studio never edits an issue in KM2.

**Let crews create issues** is off by default. Leave it off unless crews really should open issues; otherwise they propose new issues in their reports.

## The people map
<!-- permissions: connections.view -->
Click **People map** on a tracker connection to see the tracker accounts it knows and the Studio user each one is.

Click **Read people from the tracker** to fetch them. Accounts are linked to Studio users with the same email. Jira often hides emails, so choose the Studio user by hand for those; a link you choose is kept when people are read again. Reports use the Studio names, and only people the crew knows can be @mentioned: those on this list, an issue's assignee and project lead, and the leads set on the tracking list.

![The people map of a Jira connection, listing each tracker account with its email (or hidden by the tracker) and the Studio user it is linked to.](connection-people-map.png)

## Tools for crews

Give these tools to the agents that need them, and let the crew use the connection on its **State** tab.

| Tool | What it does |
|---|---|
| List Tracker Projects | Jira or KM2 projects, or Linear teams, with their keys and leads |
| Find Tracker Issues | Issues in a project that are overdue, due soon, not updated for a while, unassigned, blocked or owned by someone |
| Get Tracker Issue | One issue with its description and newest comments |
| Get Tracker Children | An issue's children: epic, stories and sub-tasks in Jira, sub-issues in Linear |
| Comment on Tracker Issue | Posts a comment, @mentioning people by their account id |
| Suggest Tracker Change | Posts a "suggested change" comment for a date, owner, status or other field; the field itself is never changed |
| Create Tracker Issue | Creates an issue, only where **Let crews create issues** is on |
| Read Tracking List | The crew's tracking list |

Text from the tracker (titles, descriptions and comments) is written by people outside the crew, so agents receive it marked as data they must not take instructions from.

## In chat
<!-- permissions: crews.chat -->
When chat is on for a crew that may use a tracker connection you can see, and you have **View crew state**, you can ask its front desk what the crew tracks and about issues: "What's slipping in Atlas?". People with **Manage crew state** can also say "track PRJ-10 and its sub-tasks as Project Atlas" or "stop tracking Birch". The front desk only proposes the change; it happens when you click **Confirm**, after Crew Studio checks the item exists, and **Undo** reverses it. A ticket or comment can never change what a crew tracks.

## What Crew Studio enforces

These rules are applied by Crew Studio's code, whatever an agent or a ticket says:

- **Dry run.** In a dry-run schedule, the comment, suggestion and create tools post nothing. They answer with what would have been posted, and the crew lists it under "Would post" in its output. The action log gets a dry-run entry.
- **Every post is logged.** Each comment is recorded in the action log. A comment that answers a [follow-up](/help/follow-up-ladders) step is recorded as that step, so it is not due again, and it is posted at most once per run.
- **Comments per run.** One run may post at most 30 comments (set **Comments per run** under the watch's **Tracker tool limits**).
- **Mentions per person per day.** The crew may @mention one person at most 3 times in 24 hours (set **Mentions per person per day**). A comment that would go over is refused, not posted without the mention.
- **Only known people.** Mentions must be someone the crew knows (see the people map).
- **Chat cannot post.** Chat turns only read; they cannot comment.

## API facts

Crew Studio uses Jira Cloud REST v3 (issue search with `/rest/api/3/search/jql`, comments in Atlassian Document Format with mention nodes) Linear's GraphQL API (profile links as mentions) and KM2's records API (`/api/v1/entities/.../records`). All back off when the tracker says to slow down. The details are in `docs/trackers.md` in the Crew Studio repository.
