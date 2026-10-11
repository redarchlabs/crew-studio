---
title: Connections
category: Configure
order: 35
permissions: connections.view
summary: How connections hold the addresses and credentials of outside systems, how their secrets are protected, and how crews are allowed to use them.
keywords: connection, credentials, secret, api key, token, password, jira, linear, km2, knowledge manager, slack, teams, email, smtp, http, webhook, mcp server, test connection, encrypted, STUDIO_SECRETS_KEY
---
A connection holds what Crew Studio needs to reach an outside system: its address, an account, and secrets such as an API token. Each connection belongs to one or more functional areas, like crews do, and can be turned off without deleting it.

Open [Connections](/connections) from the **Configure** section of the sidebar to see the connections in your functional areas.

![The Connections page, listing each connection with its areas, kind, status, secrets and number of crews, with a passed test on the Jira row.](connections.png)

## Kinds of connection

| Kind | Settings | Secrets |
|---|---|---|
| Jira Cloud | Site URL, account email | API token |
| Linear | none (the key decides the workspace) | API key |
| KM2 issues (tracker) | Base URL, Crew Studio's person email, entity prefix, activity field, issue link | Org API key |
| KM2 (Knowledge Manager) | Base URL, default folder for saves, other folders crews may save to (picked from lists once the key is saved) | Profile API key |
| HTTP (JSON API) | Base URL, auth header name | Auth header value |
| MCP server (tools for agent chat) | Server URL, auth header name | Auth header value |
| Slack | Channel | Incoming webhook URL or bot token |
| Microsoft Teams | none | Channel posting URL |
| Email (SMTP) | Host, port, user name, from address | Password |

KM2 issues and KM2 (Knowledge Manager) are different kinds: KM2 issues reads projects and issues kept as records in a KM2 organisation, with an org-wide key; see [Jira, Linear and KM2 issues](/help/trackers). For KM2 knowledge, make one connection per functional area, each with the key of the KM2 profile for that area, so a run only reads what its area may see. See [KM2 knowledge](/help/km2) for setting up the key.

An MCP server connection reaches an outside MCP server whose reading tools agents may use in chat; its **Tools** button lists the tools and what each one does. See [MCP server connections](/help/mcp-connections). It is not Crew Studio's own MCP endpoint for Claude, which needs no connection.

## Reading the connections list

Each row shows the connection's **name** and areas, its **kind**, whether it is **enabled**, which **secrets** are set and when they were last replaced, and how many **crews** may use it. A warning such as **missing API token** means a required secret has not been set yet.

Secret values are never shown, on this page or anywhere in the API. Not even the people who saved them can read them back.

## How secrets are protected

Secrets are encrypted in the database with AES-256-GCM, using a key the server reads from its `STUDIO_SECRETS_KEY` setting. Each secret is also tied to its own connection, so it cannot be copied onto another one.

Secrets are only ever typed here, in the app. [Claude through MCP](/help/mcp) can create a connection and change its settings, but never set or see a secret: after Claude creates one, click **Replace secret** and paste the key yourself.

If the server has no key, the page says that connections are off and nobody can add one. Once connections exist, Crew Studio will not start without the key they were saved with, so whoever runs Crew Studio must keep that key safe and back it up with the database.

## Add a connection
<!-- permissions: connections.manage -->
1. On [Connections](/connections), click **New connection**.
2. Enter a **Name**, such as "Acme Jira", and pick the **Kind**.
3. Fill in the settings the kind needs. Fields marked optional can stay empty.
4. Enter the **Secrets**. They are saved encrypted and cannot be read back.
5. Pick the **Functional areas** that may use it. You can only pick areas you belong to.
6. Leave **Enabled** on, then click **Create**.

![The New connection window for Jira Cloud, with the name, kind, site URL and account email filled in.](connection-editor.png)

Every change to a connection is recorded in the audit log, without the secret values.

## Edit, replace a secret or turn off
<!-- permissions: connections.manage -->
Click **Edit** on a connection's row to change its name, settings, areas or **Enabled** switch. Its kind cannot change; make a new connection instead.

Changing where the secrets go (a URL, host, port or auth header name) removes the saved secrets, so they are never sent to a new address. Replace them afterwards.

To change a connection that is also in functional areas you are not in, ask someone who is in all of its areas.

To change a secret, click **Replace secret** and type the new values. They replace every saved secret of that connection, and a field you leave empty is removed. Use this when a token is rotated or may have leaked.

Turning a connection off keeps it, and its secrets, but runs no longer get it.

## Test a connection
<!-- permissions: connections.manage -->
Click **Test** to check that Crew Studio can reach the system with the saved settings and secret. Jira, Linear and KM2 issues connections answer with the account the credentials belong to; KM2 issues also checks that the organisation has the tracker entities. For HTTP connections, Crew Studio sends a `GET` to the base URL with the auth header and shows the answer's status, such as "Connected: the server answered HTTP 200." It does not follow redirects, never shows the response body, and does not reach link-local addresses such as cloud metadata services. MCP server connections connect the same way and say how many tools the server offers; **Tools**, then **Discover tools**, keeps them.

Other kinds answer "not supported yet" until their tools arrive. See [Jira, Linear and KM2 issues](/help/trackers) for tracker connections and their people map.

## Delete a connection
<!-- permissions: connections.manage -->
Click **Delete** on its row, then **Confirm?**. The connection and its encrypted secrets are removed, and crews that could use it lose it.

## Letting a crew use a connection

A crew's tools may only use the connections chosen on the crew's **State** tab, under **Connections**. A run then gets only those that are enabled and share a functional area with the crew. See [Crew state](/help/crew-state#connections-a-crew-may-use).
