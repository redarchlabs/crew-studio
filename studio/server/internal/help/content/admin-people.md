---
title: People
category: Admin
order: 10
permissions: admin.users
summary: How to invite people, give them roles, permissions and functional areas, disable or delete them, and end their sessions.
keywords: users, invite, people, accounts, disable, deactivate, delete, roles, areas, all areas, direct permissions, sessions, sign out, password, administrator, last administrator, sso
---
The **People** page under Admin lists everyone who can sign in to Crew Studio. People sign in with single sign-on (SSO), so you add them here first by email, then decide what they can do (roles and permissions) and what they can see (functional areas).

Service accounts for other systems are kept separately; see [Service accounts](/help/admin-service-accounts).

![The People page, listing each person with their roles, areas, status, last sign-in and sign-in methods.](admin-people.png)

## The people list

Each row shows a person's **Name**, **Email**, **Roles**, **Areas**, **Status**, **Last sign-in** and **Sign-in methods**. Your own row is marked "(you)".

**Sign-in methods** shows the SSO providers a person has used, a **password** chip if they have an administrator password, or "not linked yet".

Filter by name or email, or by status (**Any status**, **Active** or **Disabled**). Click a name to open that person's page. If you also hold `admin.audit`, a **History** link in the page header shows every change made to people and service accounts.

## Inviting a person

1. Click **Invite person**.
2. Enter their **Name** and **Email**. The email must be the address their SSO provider reports: Crew Studio matches them by it the first time they sign in.
3. Leave **Status** on **Active**.
4. Under **Roles and areas**, pick one or more **Roles**, and either turn on **All areas** or choose **Functional areas**.
5. Add **Direct permissions** only if you need a one-off exception.
6. Click **Create**.

Crew Studio does not send an invitation email. Tell the person to open Crew Studio and sign in with SSO. Their first sign-in with that email links their SSO identity to the account; the **Linked sign-ins** panel on their page then shows the provider. From then on their **Email** is locked and cannot be changed.

If **Create accounts automatically** is turned on in [Sign-in settings](/help/admin-sign-in), people can also get an account on their first SSO sign-in without being invited. Those accounts start with no functional areas, so you still need to come here and give them areas.

## Roles, direct permissions and areas

What a person can **do** comes from their roles plus any direct permissions. What they can **see** comes from their functional areas. They need both: a person with roles but no areas sees no crews, agents, tasks, work orders or runs, and the page warns you about this.

![A person's page with the Person, Roles and areas, and Direct permissions panels, and the Effective permissions summary on the right.](admin-person-editor.png)

- **Roles** are named sets of permissions managed on the [Roles](/help/admin-roles) page. Prefer roles over direct permissions.
- **Direct permissions** are granted on top of roles. The matrix marks permissions the person already has through a role with "via role".
- **All areas** lets the person see every crew, agent, task and work order, including areas added later. When it is on, the **Functional areas** list is greyed out but kept, so turning it off restores the earlier choice.
- **Functional areas** limit them to items in those areas. See [Functional areas](/help/admin-areas).

The **Effective permissions** panel on the right sums up everything the person ends up holding, including unsaved changes.

## What you can and cannot grant

You can only give out what you hold yourself. When you save, Crew Studio refuses:

- a role that includes a permission you do not hold,
- a direct permission you do not hold,
- **All areas** if you do not have all areas yourself,
- a functional area you do not belong to.

Grants the person already had stay in place even if you lack them; the rule applies only to what you add.

You also cannot change someone who holds more than you. If a person has a permission, an area or all areas that you do not, saving, disabling, deleting, setting their password or signing them out is refused with a message saying why. Ask an administrator who holds at least as much as they do.

## Disabling and deleting

To remove access but keep the record, set **Status** to **Disabled** and click **Save**. Disabling signs the person out everywhere and blocks sign-in. Set it back to **Active** to restore access.

To remove a person entirely, click **Delete** in the page header, then click **Confirm?** within a few seconds. You cannot delete your own account, and the Delete button does not appear on your own page. If you disable yourself, the page warns you first.

### The last administrator

At least one active person must always hold `admin.users`, so someone can still manage access. If a save or delete would leave nobody with it (for example disabling, deleting or removing the role of the only administrator), Crew Studio stops the change and shows **This is the last administrator**. Give `admin.users` to another person first, then try again.

## Sessions and administrator passwords

The **Sessions** panel has **Sign out everywhere**, which ends every browser session of that person at once, for example after a lost laptop. Click it, then **Confirm?**. On your own page this includes your current session. It does not affect API keys. Each use is recorded in the [audit log](/help/admin-audit).

The **Administrator password** panel appears only for people who hold `admin.users`. Password sign-in is a fallback for administrators when SSO is unavailable; everyone else signs in with SSO. Enter a password of 12 to 72 characters, repeat it under **Confirm**, and click **Set password** (or **Change password**). **Remove** deletes the password. [Sign-in settings](/help/admin-sign-in) shows whether password sign-in is enabled on the server.

## Personal API keys

A person's page has a **Personal API keys** panel listing the keys they made for their own scripts on **My API keys**, with each key's status, expiry and last use. Click **Revoke**, then **Confirm?**, to stop one immediately, for example when someone leaves or a key may have leaked. The panel shows 20 keys a page. Where you see **Cannot revoke**, the person holds a permission or area you do not, so the key is outside what you can manage.

You cannot create a key for someone else; people create their own, if they hold **Create personal API keys**. Taking that permission away stops all of a person's keys at once. To see every key across Crew Studio, use [API keys](/help/admin-api-keys).
