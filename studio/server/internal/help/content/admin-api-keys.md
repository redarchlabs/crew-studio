---
title: API keys (admin)
category: Admin
order: 25
permissions: admin.users, admin.service_accounts
summary: See every API key in Crew Studio, personal and service account, find unused or expired ones and revoke them.
keywords: api keys, all keys, revoke, audit keys, service account key, personal key, last used, expired, leaked key
---
**Admin > API keys** lists every API key in Crew Studio in one place: personal keys people made for themselves and keys for service accounts. Use it to find keys nobody uses any more, keys that have expired, or the key behind a suspected leak, and revoke them.

![The API keys page, listing every key with its account, status, expiry and last use.](admin-api-keys.png)

## What the list shows

Each row shows the key's **Name** and prefix, the **Account** it belongs to (a person or a service account), its **Status** (**Active**, **Expired** or **Revoked**), when and by whom it was **Created**, when it **Expires**, and when it was **Last used**.

Active keys come first, then the newest. The list shows 50 keys a page; use the arrows below it to move between pages.

Narrow the list with:

- the search box (**Filter by key, account or prefix…**), which matches the key name, prefix or account name;
- **All accounts**, **People** or **Service accounts**;
- **Any status**, **Active**, **Expired** or **Revoked**.

Keys are stored only as one-way hashes, so nobody can see a key's secret here or anywhere else.

## Revoke a key

Click **Revoke** on the row, then **Confirm?**. The key stops working immediately.

Some rows show a faint **Cannot revoke** instead. That key belongs to someone who holds a permission or a functional area you do not, and you can only act on accounts within your own access. Ask an administrator who holds everything that account does. Revoking a person's key does not stop them creating a new one; to stop that, take away their **Create personal API keys** permission (see [People](/help/admin-people)), which also stops every key they have.

## New service account keys

**New service account key** lets you pick a service account and create a key for it, using the same window as on the service account's own page. The key is shown once. See [Service accounts](/help/admin-service-accounts) for how to replace a key without downtime.

Administrators can never create a personal key for someone else: people create their own on **My API keys**.

## Admin changes need you signed in

A request made with an API key, personal or service account, can only **read** admin screens' data. Creating or changing people, roles, areas, keys and sign-in settings, and revealing a service account's signing secret, need you signed in to Crew Studio; with a key the API answers `403` "This needs you signed in to Crew Studio; API keys cannot do it." That way a stolen key cannot create new credentials that outlive it.
