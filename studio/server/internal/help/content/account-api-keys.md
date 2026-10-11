---
title: My API keys
category: Developers
order: 20
permissions: account.api_keys
summary: Create, use and revoke personal API keys that let your own scripts call Crew Studio as you.
keywords: personal api key, token, bearer, script, automation, csk, revoke, expiry, authorization header, credentials, limit key, scope, read only key, mcp only, rest api only, key permissions, key areas
---
A personal API key lets a script or tool call the Crew Studio API **as you**. By default it has your permissions and your functional areas, so it can do exactly what you can do in the app, and nothing more. When you create it you can limit it further: to the REST API or to MCP, and to some of your permissions and areas. Open it from **Developers > My API keys** in the sidebar.

![The My API keys page, listing keys with their status, expiry and last use.](account-api-keys.png)

## Create a key

1. Click **New key**.
2. In the **New API key** window, give the key a **Name** that says where it will be used, such as "Nightly report script".
3. Optionally set **Expires (optional)**. A key with an expiry stops working at that time; leave it empty for a key that lasts until you revoke it.
4. Optionally limit the key (see below).
5. Click **Create key**.
6. The key appears once, in the **Key "…" created** window. Click **Copy**, store it somewhere safe (a password manager or your tool's secret store), then click **I have saved it**.

Crew Studio keeps only a one-way hash of the key, so nobody, including administrators, can show it to you again. If you lose it, revoke it and create a new one.

You can have up to 20 active keys. The list shows 20 keys a page, so revoked and expired keys may push older ones onto a second page. You must be signed in to Crew Studio to create one: a request made with an API key cannot create another key.

## Limit what a key can do

Give each key only what its script or tool needs. If the key leaks, that is all it can be used for. The **New API key** window has three choices:

- **Works with**: **REST API** (scripts and other systems calling `/api`), **MCP** (MCP clients such as Claude Code, through `/mcp`), or both. Keep at least one. A key used with only one of them is refused by the other with `403`.
- **Permissions**: **Everything you can do**, or **Only these**, then tick the permissions the key keeps. **Read only** ticks every view permission you hold. Only permissions you hold are offered.
- **Functional areas**: **Every area you are in**, or **Only these**, then tick the areas. A key limited to areas does not see items that have no area.

A key can never have more than you: the server refuses a permission or area you do not hold. The limits are checked against your account on every request, so if you later lose a permission or an area, your keys lose it too. A key's limits cannot be changed after it is created; create a new key and revoke the old one instead.

Work you set up with a key keeps its limits. A schedule or trigger saved with the key runs within them, and an agent chat started with it uses only the connections the key may. When you revoke the key, that work stops: the schedule pauses and the trigger refuses its calls until someone saves them again in the app.

The **Access** column of the list shows each key's limits: where it works (**API and MCP**, **API only** or **MCP only**), its permissions (**All permissions**, or how many it keeps; point at it to see which), and its areas.

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
