---
title: My API keys
category: Developers
order: 20
permissions: account.api_keys
summary: Create, use and revoke personal API keys that let your own scripts call Crew Studio as you.
keywords: personal api key, token, bearer, script, automation, csk, revoke, expiry, authorization header, credentials
---
A personal API key lets a script or tool call the Crew Studio API **as you**. It has your permissions and your functional areas, so it can do exactly what you can do in the app, and nothing more. Open it from **Developers > My API keys** in the sidebar.

![The My API keys page, listing keys with their status, expiry and last use.](account-api-keys.png)

## Create a key

1. Click **New key**.
2. In the **New API key** window, give the key a **Name** that says where it will be used, such as "Nightly report script".
3. Optionally set **Expires (optional)**. A key with an expiry stops working at that time; leave it empty for a key that lasts until you revoke it.
4. Click **Create key**.
5. The key appears once, in the **Key "…" created** window. Click **Copy**, store it somewhere safe (a password manager or your tool's secret store), then click **I have saved it**.

Crew Studio keeps only a one-way hash of the key, so nobody, including administrators, can show it to you again. If you lose it, revoke it and create a new one.

You can have up to 20 active keys. The list shows 20 keys a page, so revoked and expired keys may push older ones onto a second page. You must be signed in to Crew Studio to create one: a request made with an API key cannot create another key.

## Use a key

Send the key in the `Authorization` header of every request:

```
curl -H "Authorization: Bearer csk_…" https://your-studio/api/auth/me
```

The **Using a key** panel on the page shows this example with your Crew Studio address. To try requests interactively, open the [API explorer](/api-docs), click **Authorize** and paste the key. See [API explorer](/help/api-explorer) for the response format and the other things every request can use.

Personal keys cannot ask for run callbacks. Integrations that need callbacks, or that should keep working when you leave, belong on a service account; ask an administrator.

## Revoke a key

Click **Revoke** on the key's row, then **Confirm?** within a few seconds. The key stops working immediately and stays in the list, marked **Revoked**, so you can see when it was last used.

Revoke a key as soon as the script that used it is retired, or if you think it may have leaked.

## When keys stop working

- **Expired**: the expiry you set has passed.
- **Revoked**: you or an administrator revoked it.
- **Permission removed**: if you lose the **Create personal API keys** permission, every key you made stops working at once. The page then explains why and offers no **New key** button; you can still see and revoke your old keys. Ask an administrator if you need the permission back.
- **Your account is disabled**: keys stop with it.
