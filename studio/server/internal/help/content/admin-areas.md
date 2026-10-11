---
title: Functional areas
category: Admin
order: 40
permissions: admin.areas
summary: How to add, rename and delete functional areas, and how areas decide who sees which crews, agents, tasks and work orders.
keywords: areas, functional areas, departments, teams, finance, it, hr, scope, visibility, all areas, rename, delete area
---
Functional areas, such as Finance, IT, Training or HR, divide the work in Crew Studio. Every crew, agent and task belongs to at least one area, and people only see and act on items in their own areas. Roles decide what someone can do; areas decide where they can do it.

![The Functional areas page, listing each area with its description and how many crews, agents, tasks and accounts use it.](admin-areas.png)

## How areas scope items

- **Crews, agents and tasks** each belong to one or more areas, chosen in their editor.
- **Work orders, runs, schedules and triggers** follow the crew they run. They have no areas of their own.
- **People and service accounts** are given areas on their own page, or **All areas**, which covers every area including ones added later.

Someone sees an item when they share at least one of its areas, or when they have all areas. Items outside their areas are hidden from lists, and opening a link to one shows it as not found. A person or service account with no areas sees no crews, agents, tasks, work orders or runs at all, whatever their roles.

When people edit an item's areas, they can only add or remove areas they belong to themselves. Areas they are not in stay on the item, shown as locked.

Area names are not secret: everyone who is signed in can see the list of names, so items can be labelled. Only people with `admin.areas` see this page and the usage counts.

See [Access and areas](/help/access-and-areas) for how this looks to everyday users.

## The areas list

Open **Functional areas** under Admin. Each row shows the area's **Name**, **Description** and **In use** summary, for example "3 crews · 12 agents · 9 tasks · 4 accounts". Areas are sorted by name.

If you also hold `admin.audit`, the **History** link in the page header shows every change made to areas.

## Adding an area

1. Click **Add area**. A new row opens at the top of the list.
2. Type a **Name**, such as "Legal". Names must be unique; letter case does not count, so "legal" and "Legal" clash.
3. Optionally add a **Description** of what belongs there.
4. Press **Enter** or click the tick to save. Press **Escape** or click the cross to cancel.

A new area is empty. To use it:

1. Give it to the right people and service accounts on the [People](/help/admin-people) and [Service accounts](/help/admin-service-accounts) pages. You can only grant an area you belong to, so unless you have all areas, someone who does may need to do this.
2. Put crews, agents and tasks into it from their editors.

## Renaming an area

Click the pencil icon on the area's row, or double-click the row. Change the name or description, then press **Enter** or click the tick. Press **Escape** to cancel.

Renaming does not change which items or people are in the area. Everything keeps its membership; only the label changes.

## Deleting an area

Click **Delete** on the area's row, then **Confirm?** within a few seconds.

An area that still holds crews, agents or tasks cannot be deleted. The **Delete** button is greyed out, and hovering over it tells you how many items it holds. Move those items to another area first, by editing them, then delete the area.

People and service accounts in a deleted area simply lose it. Check that they still have the areas they need afterwards; anyone left with no areas will see nothing.
