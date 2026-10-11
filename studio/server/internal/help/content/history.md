---
title: History
category: Configure
order: 40
permissions: history.view, admin.audit
summary: How to see who created, changed or deleted something, and what it looked like afterwards.
keywords: audit, revisions, changes, edit history, snapshot, who changed, undo, log, track changes
---
Crew Studio records every create, update and delete, with who made the change and when. [History](/history) lists these records, newest first, so you can see what changed and look at the item exactly as it was after each change.

Open it from the **Configure** section of the sidebar. Most pages also have a **History** link near the top that opens this list already filtered to that kind of item, or to the single item you are looking at.

![The History list with when, who, type, action and entity for each change, and one row expanded to show its snapshot.](history.png)

## Reading the list

Each row is one change:

| Column | What it shows |
|---|---|
| When | The date and time of the change. |
| Who | The person who made it. Changes made by a schedule or trigger show the run-as person followed by the schedule or trigger name, for example "Sam Lee (schedule Weekly digest)". |
| Type | What kind of item changed, such as Agent, Crew, Work order, LLM, Price or Settings. |
| Action | **create**, **update** or **delete**. |
| Entity | The item's name, or `WO-42` and its title for a work order. Click it to open the item. |

On the right of each row:

- **only this** narrows the list to that one item, so you can follow its changes over time.
- **open** goes to the item's page. Deleted items have no page, so they have no link.

The list shows 50 changes per page; use the page controls at the bottom to go further back.

## Snapshots

Click a row to expand it. The panel underneath shows the snapshot: the whole item as it was after the change, or just before it was deleted. Compare the snapshots of two neighbouring updates to see what changed between them. Click the row again to close it.

Snapshots never include secrets, such as trigger tokens, signing secrets or API key values.

History is read-only. To put back an earlier version, open the item and re-enter the values from the snapshot.

## Filters

Use the type menu at the top right to show one kind of item, such as **Schedule** or **Price**, or **All types**, when offered, to show everything.

When you have narrowed the list to one item, a line above the table says "Showing one …" with the item's id. Click **clear** to go back to every item of that type.

## What you can see

History follows the same rules as the rest of Crew Studio. Changes to agents, tasks, crews, work orders, schedules and triggers appear only for items in your functional areas (see [Access and areas](/help/access-and-areas)). LLMs, prices and settings are shared, so their changes are visible to everyone who can view history.

Changes to administration records (people, service accounts, roles, functional areas, API keys and sign-in settings) appear only for people with permission to read the audit log.

## Administration changes
<!-- permissions: admin.audit -->
Because you can read the audit log, the type menu also offers **Functional area**, **Role**, **Person / service account**, **API key** and **Sign-in settings**. The description at the top of the page mentions this too.

- **open** on a person or service account goes to their admin page. An API key opens the service account it belongs to, when the snapshot says which one.
- API key snapshots show the key's name and prefix (such as `csk_ab12cd_…`), never the secret.

If you have audit access but not general history access, the list always shows one type at a time and **All types** is not offered; pick the type you need from the menu.

History shows what an item looked like after each change. For sign-in events and other security activity, use the [Audit log](/help/admin-audit).
