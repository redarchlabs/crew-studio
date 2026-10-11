---
title: Roles
category: Admin
order: 30
permissions: admin.roles, admin.users
summary: What roles are, the roles Crew Studio starts with, how the permission matrix works, and how to create, change and delete roles.
keywords: roles, permissions, permission matrix, administrator, builder, operator, viewer, built-in, access control, rbac
---
A role is a named set of permissions, such as "Builder" or "Finance analyst". People and service accounts can hold several roles. They hold every permission of every role they have, plus any permissions granted to them directly.

Roles control what someone can **do**. What they can **see** is still limited to their functional areas; see [Functional areas](/help/admin-areas) and [Access and areas](/help/access-and-areas).

![The Roles page, listing each role with its description, number of permissions and number of holders.](admin-roles.png)

## The roles list

Open **Roles** under Admin. Each row shows the **Role** name, its **Description**, how many **Permissions** it grants and how many **People** (accounts, including service accounts) hold it. The built-in role is listed first with a **built-in** chip and "all" permissions.

Anyone with `admin.users` can open this page, so they can see what each role grants before assigning it. Only people with `admin.roles` can create, change or delete roles. Without it, the page shows a read-only note, and each role opens with a **read-only** label instead of Save and Delete buttons.

## Roles Crew Studio starts with

A new installation has four roles:

| Role | What it is for |
|---|---|
| **Administrator** | Every permission. Pair it with **All areas** for full access. |
| **Builder** | Builds and runs crews, agents, tasks, schedules and triggers in their areas, creates work orders, and can create personal API keys. Can view LLMs, pricing, settings and history, but not change them. |
| **Operator** | Runs crews, creates and cancels work orders, and watches and cancels runs in their areas. Can view crews, agents, tasks, schedules, triggers, LLMs and pricing, but cannot change them. |
| **Viewer** | Read-only access to their areas, including history. |

The Builder, Operator and Viewer roles are ordinary roles: you can change or delete them to suit your organisation. Open a role to see exactly what it grants today.

### The built-in Administrator role

**Administrator** is built in. It always holds every permission, including ones added in later versions of Crew Studio, and it cannot be changed or deleted, so there is always a way back in. Its page is read-only for everyone.

## The permission matrix

A role's page shows its permissions as a matrix with one row per feature: Overview, Crews, Agents, Tasks, Work orders, Runs, Schedules, Triggers, LLMs, Pricing, Settings, History, Account and Admin. Each row has a checkbox per permission, for example **View crews**, **Edit crews** or **Run crews**.

- Hover over a permission to see what it allows and its key, such as `crews.run`.
- The checkbox next to the feature name turns the whole row on or off. It shows a partly filled state when only some of the row is selected.
- The **Effective permissions** panel on the right summarises the role by feature, and shows "All" where every permission of a feature is selected.
- The **Holders** panel shows how many accounts hold the role. If you also hold `admin.users`, **Manage people** takes you to the People page.

![A role's page with the permission matrix grouped by feature and the Effective permissions and Holders panels.](admin-role-editor.png)

Some permissions depend on others. For example, **Manage schedules** and **Manage triggers** also need **Create work orders**, because schedules and triggers start work orders. The hover text says when this applies.

## Creating a role
<!-- permissions: admin.roles -->
1. On the Roles page, click **New role**.
2. Enter a **Name** and, optionally, a **Description** of who the role is for.
3. Tick the permissions the role should grant.
4. Click **Create**.

Then assign the role to people on the [People](/help/admin-people) page or to [service accounts](/help/admin-service-accounts).

You can only put permissions into a role that you hold yourself. If you tick one you do not hold, saving fails with a message naming it. When you edit an existing role, permissions it already had may stay even if you do not hold them.

## Changing and deleting a role
<!-- permissions: admin.roles -->
Open the role, change its name, description or permissions, and click **Save**. Changes apply at once to everyone who holds the role. If you hold it yourself, your own permissions refresh straight away.

To delete a role, click **Delete** in the page header, then **Confirm?**. Everyone who held it loses the permissions it granted, unless they hold them another way.

Crew Studio refuses a change or deletion that would leave no active person with `admin.users` (for example removing **Manage users** from the role the only administrator relies on). Give `admin.users` to someone else first.

Every change to a role is recorded. If you hold `admin.audit`, the **History** link on the Roles page and on each role shows who changed what.
