---
title: Audit log
category: Admin
order: 60
permissions: admin.audit
summary: What the audit log records about sign-ins and sign-outs, how to read it, and where to find the history of changes to people, roles, areas and settings.
keywords: audit, audit log, sign-in, login, logout, failed sign-in, security, ip address, sessions revoked, history, compliance, who changed
---
The **Audit log** under Admin records sign-in activity (who signed in or out, failed sign-in attempts and forced sign-outs) and API key activity (keys created, revoked and refused). Use it to check who has been using Crew Studio, investigate failed attempts, or confirm that someone was signed out.

Changes to people, service accounts, roles, areas, API keys and sign-in settings are not in the audit log. They are in [History](/help/history); see [Changes to people, roles and settings](/help/admin-audit#changes-to-people-roles-and-settings) below.

![The Audit log page, listing sign-in events with their time, action, who, IP address and details.](admin-audit.png)

## Reading the audit log

Events are listed newest first, 50 per page. Each row shows:

- **Time**: when it happened, in your browser's time zone.
- **Action**: what happened (see the table below). Hover over it to see the internal action name.
- **Who**: the account involved. For a failed attempt this is the email that was tried, or "unknown" when there is none.
- **IP**: the address the request came from, as the Crew Studio server saw it. If Crew Studio sits behind a proxy, this may be the proxy's address.
- **Details**: extra facts as key and value pairs, such as `provider` (`microsoft`, `clerk` or `local` for password sign-in) or `reason`.

Use the arrows under the table to move between pages; the counter shows which events you are looking at, for example "1–50 of 312". The page has no search or filters.

## What is recorded

| Action | When it is recorded |
|---|---|
| **Signed in** | Someone signed in, through SSO or with an administrator password. |
| **Signed out** | Someone signed out of a browser session. |
| **Sign-in failed** | A sign-in attempt was refused. |
| **Signed out everywhere** | An administrator used **Sign out everywhere** on a person's page. **Who** is the administrator; the details give the affected account's `user_id` and how many sessions `ended`. |
| **First-run setup** | The first administrator account was created when Crew Studio was set up. |
| **API key created** | Someone created a personal key, or an administrator created a service account key. The details give the key's name, prefix and account, never the secret. |
| **API key revoked** | A key was revoked. Revoking a key that is already revoked records nothing. |
| **API key refused** | A request used a key that is revoked, expired or no longer allowed. To keep the log readable, a refused key is recorded at most once every 10 minutes for each key prefix and address. |

### Why a sign-in failed

For SSO sign-ins, the `reason` detail says why the attempt was refused:

- `not_invited`: there is no account for that email and automatic account creation is off. Invite them on the [People](/help/admin-people) page, or see [Sign-in settings](/help/admin-sign-in#new-single-sign-on-users).
- `disabled`: the account exists but is disabled.
- `unverified_email`: the SSO provider has not verified the person's email address.
- `provider_error`: the provider returned an error or the sign-in could not be completed.

For password sign-ins (`provider: local`), a failure means the email and password did not match an administrator account. Password sign-in is only for people who hold `admin.users`, so it also fails for anyone else. After five wrong passwords in a row, password sign-in for that account is locked for 15 minutes; SSO still works.

A run of failures from one IP address against several emails is worth looking into.

## Changes to people, roles and settings

Every create, update and delete of an administrative record is kept in [History](/help/history), with who made the change and a full snapshot. Holding `admin.audit` gives you access to the History page and adds these types to its **Filter by entity type** menu:

- **Person / service account**, including setting or removing administrator passwords and rotating signing secrets,
- **API key**, including creation and revocation,
- **Role**,
- **Functional area**,
- **Sign-in settings**.

The admin pages also have a **History** link in their header, which opens History already filtered to that kind of record. On a person, role or service account page, the link shows the history of that one record.

If you do not also hold `history.view`, History shows only these administrative records, not changes to crews, agents, tasks and other content.
