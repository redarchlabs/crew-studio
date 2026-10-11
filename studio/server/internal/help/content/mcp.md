---
title: Connect Claude (MCP)
category: Developers
order: 30
permissions: account.api_keys, admin.service_accounts
summary: Let Claude Code read and change Crew Studio's agents, tasks, crews, LLMs, pricing, schedules, connections, watches and settings through MCP, using an API key. Secrets stay in the app.
keywords: mcp, model context protocol, connection, watch, people map, pmo, secret, claude, claude code, ai assistant, connector, tools, agent configuration, automate configuration, /mcp, streamable http
---
Crew Studio has an MCP (Model Context Protocol) endpoint at `/mcp`. Connect Claude Code to it, and you can ask Claude to look through your configuration and change it, for example "raise the budget on every research crew to $3" or "switch the analyst agents to Claude Sonnet 5.5 and check the crews still validate".

Claude works through an API key, so it acts **as the key's owner**: it has that account's permissions and functional areas, and nothing more. A key can also be limited when it is created, to some of the account's permissions and areas, so Claude gets only those. Every change goes through the same checks as the app and appears in [History](/help/history) under the owner's name.

## Connect Claude Code

1. Create an API key with **MCP** ticked under **Works with**. For your own use, create a personal key (see [My API keys](/help/account-api-keys)); consider limiting it to **MCP** only and to the permissions Claude needs, such as **Read only**. For a shared or long-running setup, ask an administrator for a service account key with only the roles it needs.
2. In a terminal, add Crew Studio as an MCP server, using your Crew Studio address and the key:

```
claude mcp add --transport http crew-studio https://your-studio/mcp --header "Authorization: Bearer csk_…"
```

3. Start Claude Code and run `/mcp`. **crew-studio** should be listed as connected. Ask Claude "who am I in Crew Studio?" to check which account it uses.

Keep the key out of shared files: `claude mcp add` stores it in your own Claude Code settings. Revoke the key when you no longer need it, and Claude loses access at once.

## What Claude can do

- **Read**: list and read agents, tasks, crews, LLMs, pricing, schedules, connections, schedule watches, functional areas and the agent tool catalog; read settings, runs and the change history.
- **Change**: create, change and delete agents, tasks, crews, LLMs, pricing, schedules, connections (their settings only) and schedule watches, and change settings. Claude sends only the fields it changes, and sees each field before and after.
- **Trackers and the PMO**: test a connection, read a tracker's people and link them to Crew Studio users, choose which connections a crew may use, preview a watch, run a schedule now, and set up a PMO.
- **Check and run**: check that a crew can run, start a run of a crew and read its result. Runs cost money, so Claude is told to start one only when you ask.

## Secrets stay in the app

Claude never sets or sees a connection's secrets (API keys, tokens, passwords, webhook addresses): an attempt is refused with "set secrets in the Crew Studio UI (Connections → Replace secret)", and nothing is saved. To set up a tracker with Claude, let it create the connection with its settings, then open [Connections](/connections), click **Replace secret** on that connection and paste the key yourself. Claude can then test the connection and carry on. Claude only learns which secrets are set or missing, never their values. If Claude changes where a connection points (its address, or a KM2 issues connection's entity prefix), the saved secret is cleared, as it is in the app, and you paste it again.

Claude cannot manage people, roles, functional areas, API keys, triggers or sign-in settings through MCP.

## Safeguards

- **Your access, no more**: an item outside the key's functional areas looks like it does not exist, and a change the key has no permission for is refused. A key limited to some permissions or areas has only those, even if its account has more.
- **No overwriting other people's edits**: Claude is asked to pass the time it read an item, and the change is then refused if someone saved the item since.
- **Preview**: Claude can ask for a dry run, which shows what would change without saving it.
- **Typos are refused**: a field name that does not exist on the item is refused rather than ignored.
- **A trail**: each change is kept in [History](/help/history), and administrators also see it in the [Audit log](/help/admin-audit) as **Changed through MCP**, with the key that made it.

## When it does not connect

- **401 Unauthorized**: the key is missing, mistyped, expired or revoked, or its owner lost the **Create personal API keys** permission. MCP accepts only API keys, not a signed-in browser session.
- **403 Forbidden** "This API key works only with the REST API": the key was created without **MCP** under **Works with**. Create a key that works with MCP.
- **421 or "host name is not allowed"**: you reached Crew Studio by an address it does not know. Use the address in `STUDIO_PUBLIC_URL`, or have the address added to `STUDIO_ALLOWED_HOSTS`.
- **429 Too many requests**: MCP shares the API's rate limits. Wait a moment and try again.
