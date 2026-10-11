---
title: Access, roles and functional areas
category: Getting started
order: 20
summary: How permissions, roles and functional areas decide which screens, buttons and items you see, and who to ask when something is missing.
keywords: permission, permissions, role, roles, administrator, builder, operator, viewer, functional area, areas, finance, IT, HR, training, all areas, access denied, no access, hidden, missing, read-only, cannot see, request access
---
Two people can open Crew Studio and see quite different things. That is deliberate: what you see is decided by your **permissions**, which usually come from your **roles**, and by your **functional areas**. This article explains each one and what to do when something you need is missing.

## Permissions

A permission allows one kind of thing, such as viewing work orders, creating them, or cancelling them. Every screen and every action that changes something checks a permission. They are grouped by feature: Overview, Crews, Agents, Tasks, Work orders, Runs, Schedules, Triggers, LLMs, Pricing, Settings, History and Admin.

Permissions affect what you see in three ways:

- **Sidebar entries** appear only for screens you can open.
- **Buttons** for actions you are not allowed to take are not shown at all.
- **Forms** you can read but not change are shown with a short "Read-only" note naming the permission you would need.

If you follow a link to a screen you cannot use, you see "You don't have access to this page", followed by the permission it needs and a **Back to overview** link.

## Roles

A role is a named bundle of permissions. People are given one or more roles, and can also be given individual permissions on top. Four roles come with Crew Studio:

| Role | In short |
|---|---|
| **Administrator** | Every permission, including the Admin screens. |
| **Builder** | Builds and runs crews, agents, tasks and schedules in their areas. |
| **Operator** | Runs crews, creates work orders and watches runs in their areas, but cannot change crews, agents or tasks. |
| **Viewer** | Read-only access to their areas. |

Your organisation can change these roles or add its own, so treat the table as a guide. The authoritative list is whatever your administrator has set.

## Functional areas

Functional areas are groups such as Finance, IT, Training or HR. Every crew, agent and task belongs to at least one area. Work orders, runs, schedules and triggers belong to the areas of the crew they run.

You see an item only when it shares at least one area with you. Some people are given **All areas**, which shows everything, including areas added later.

Items outside your areas are hidden, not greyed out:

- They do not appear in lists, pickers or counts on the Overview.
- A link to one (for example, a URL a colleague sent you) answers as if the item does not exist.
- When you prepare a work order, inputs used only by tasks or agents outside your areas are not listed. A note says so, and the check happens when the work order is submitted.

Permissions and areas work together. A Builder in the Finance area can edit Finance crews but cannot see IT crews at all; a Viewer with All areas can see everything but change nothing.

LLMs, prices and settings are shared across the organisation and do not belong to areas. Whether you see them depends only on your permissions.

## Getting access

If a screen, button or item you need is missing, ask an administrator in your organisation (someone who manages people in Crew Studio). It helps to tell them:

1. What you were trying to do, and on which screen.
2. The permission named in any "You don't have access" or "Read-only" message.
3. The functional area of the item, if you know it (for example, "the Finance due diligence crew").

Administrators can only give out permissions, roles and areas they hold themselves, so a request may need to go to someone with wider access.

## Managing someone's access
<!-- permissions: admin.users -->
You can change what other people see on the [People](/admin/users) screen. Open a person and use the **Roles and areas** panel to choose their **Roles** and **Functional areas** (or turn on **All areas**), and **Direct permissions** for anything extra.

![A person's page with roles, permissions and functional areas.](admin-person-editor.png)

You cannot grant a permission, role or area you do not hold yourself. For step-by-step instructions, see [People](/help/admin-people).
