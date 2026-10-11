---
title: Service accounts
category: Admin
order: 20
permissions: admin.service_accounts
summary: How to create service accounts for other systems, issue and revoke their API keys, and manage their callback signing secret.
keywords: service account, api key, integration, token, bearer, revoke, rotate, expiry, signing secret, callback, webhook, machine account
---
A service account is an account for another system, such as an ERP or a script, rather than a person. It never signs in to the web app. Instead it calls the Crew Studio API with an API key you create here, and it can only do what its roles and permissions allow, in its functional areas.

Create one service account per system, so you can see what each one does and cut one off without affecting the others.

![The Service accounts page, listing each account with its roles, areas, status, last activity and creation date.](admin-service-accounts.png)

## The service account list

Open **Service accounts** under Admin. Each row shows the account's **Name**, **Roles**, **Areas**, **Status**, **Last active** and **Created** date. Use the filter box to search by name and the status menu to show active or disabled accounts. Click a name to open the account.

## Creating a service account

1. Click **New service account**.
2. Enter a **Name** that says which system uses it, for example "ERP integration".
3. Leave **Status** on **Active**.
4. Under **Roles and areas**, choose **Roles**, and either turn on **All areas** or choose **Functional areas**.
5. Add **Direct permissions** if the roles do not cover what the system needs.
6. Click **Create**.

API keys can only be added once the account exists. After you click **Create**, the page reloads as the saved account and shows the **API keys** panel.

Give the account only what the system needs. For example, a system that submits work orders needs `work_orders.create` and `work_orders.view` in the areas of the crews it runs, and nothing under Admin.

The same limits apply as for people: you cannot grant a role, permission, area or all areas that you do not hold yourself, and you cannot change an account that holds something you do not. See [What you can and cannot grant](/help/admin-people#what-you-can-and-cannot-grant).

The **Roles** list is only shown if you also hold `admin.users` or `admin.roles`. Without either, the page says roles are hidden and keeps the account's existing roles; you can still set areas and direct permissions.

## API keys

The **API keys** panel lists every key of the account with its **Name**, a masked **Key** (for example `csk_ab12cd_…`), **Status** (active, expired or revoked), **Created**, **Expires** and **Last used**.

To create a key:

1. Click **New key**.
2. Enter a **Name**, usually the system or environment that will use it.
3. Optionally pick an **Expires** date. The key stops working at the end of that day. Leave it empty for a key that does not expire.
4. Click **Create key**.

The full key is shown **once**. Click **Copy**, store it somewhere safe (such as the other system's secret store), then click **I have saved it**. Crew Studio cannot show it again; if it is lost, revoke it and create another.

The calling system sends the key in an `Authorization: Bearer` header with each request. The [API explorer](/help/api-explorer) describes the available endpoints.

## Revoking and rotating keys

To revoke a key, click **Revoke** on its row, then **Confirm?**. It stops working immediately and stays in the list as revoked.

There is no single rotate action for API keys. To replace a key without downtime:

1. Create a new key.
2. Put the new key into the other system and check that it works.
3. Revoke the old key.

The **Last used** column helps you confirm the old key is no longer in use before revoking it.

## Callback signing secret

When a service account asks Crew Studio to call it back when a run finishes, the callback is signed with the account's signing secret (header `X-CrewStudio-Signature`), so the receiving system can check it came from Crew Studio. Each service account gets a secret when it is created.

In the **Callback signing secret** panel:

- **Reveal** shows the secret, with a copy button. **Hide** masks it again.
- **Rotate** (then **Confirm?**) replaces it. This takes effect at once: callbacks signed with the old secret will fail verification, so update the receiving system straight away.

Callbacks normally go only to public `https` addresses. To let them reach internal hosts, add those hosts under [Sign-in settings](/help/admin-sign-in#callback-allowed-hosts).

## Disabling and deleting

Set **Status** to **Disabled** and click **Save** to stop every API key of the account at once while keeping the account and its keys. Set it back to **Active** to restore them.

To remove the account entirely, click **Delete** in the page header, then **Confirm?**.

Changes to service accounts and their keys are recorded in [History](/help/history) for people who hold `admin.audit`.
