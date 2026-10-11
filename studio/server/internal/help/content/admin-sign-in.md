---
title: Sign-in settings
category: Admin
order: 50
permissions: admin.access
summary: How new people get accounts through single sign-on, which role they start with, which internal hosts run callbacks may reach, and where sign-in providers are configured.
keywords: sso, single sign-on, auto-provisioning, provisioning, microsoft, entra, clerk, password sign-in, local login, callback, allowed hosts, allow-list, internal hosts
---
The **Sign-in settings** page under Admin controls how people who sign in with single sign-on (SSO) get an account, and which internal hosts run callbacks may reach. It also shows which sign-in methods the server offers.

![The Sign-in settings page, with the New single sign-on users and Callback allowed hosts panels and the Sign-in methods panel on the right.](admin-sign-in.png)

## Sign-in methods

The **Sign-in methods** panel on the right lists each SSO provider the server is set up for, such as Microsoft or Clerk, or says "No single sign-on provider configured". It also shows whether **Administrator password sign-in** is enabled or disabled.

These are read-only here. Providers and password sign-in are configured by whoever runs the Crew Studio server, in the server's environment settings, and take effect after a restart. Ask them if you need a provider added or password sign-in turned off.

Password sign-in is only ever available to people who hold `admin.users` and have a password set on their page. It is meant for first-time setup and for when SSO is down. Everyone else signs in with SSO. See [Sessions and administrator passwords](/help/admin-people#sessions-and-administrator-passwords).

## New single sign-on users

When someone signs in through SSO, Crew Studio looks for an account:

1. an account already linked to that SSO identity, then
2. a person with the same email address, who gets the identity linked on this first sign-in, then
3. if neither exists, it creates an account only when **Create accounts automatically** is on.

### Create accounts automatically

When **Create accounts automatically** is off, people must be invited on the [People](/help/admin-people) page first. Anyone else who tries is turned away with a message asking them to get an administrator to invite them, and the attempt appears in the [audit log](/help/admin-audit) as a failed sign-in.

When it is on, anyone who can sign in through a configured provider gets an account. Crew Studio does not filter by email domain, so this lets in everyone your SSO provider accepts. Turn it on only if the provider is already limited to the right people.

### Role for automatically created accounts

Choose the role new accounts get from **Role for automatically created accounts**, or **No role (they see nothing until given one)**. This menu is only available while **Create accounts automatically** is on.

Automatically created accounts never get functional areas, so they see no crews, agents, tasks or work orders until an administrator gives them areas. Give the role as little as possible; you can grant more later.

You can only choose a role whose permissions you hold yourself.

## Callback allowed hosts

Service accounts can ask Crew Studio to call them back when a run finishes. By default these callbacks only go to public `https` addresses. Hosts on this list may also be reached over plain `http` and on private networks, for internal systems.

To add a host:

1. Type a bare host name or IP address into the box, for example `erp.internal` or `10.0.0.5`. Leave out `http://`, the port and any path; any port on an allowed host is accepted.
2. Press **Enter** or click **Add**.

Remove a host with the cross on its chip. An empty list means callbacks may only reach public https addresses.

Only add hosts you trust: anything on the list can receive run results. See [Service accounts](/help/admin-service-accounts#callback-signing-secret) for how callbacks are signed.

## Saving changes

Nothing takes effect until you click **Save sign-in settings**. **Discard changes** puts back the saved values. If you try to leave the page with unsaved changes, you are asked to confirm.

If you hold `admin.audit`, the **History** link in the page header shows every change to these settings and who made it.
