---
title: MCP server connections
category: Configure
order: 38
permissions: connections.view, agents.edit
summary: Connect an outside MCP server, discover its tools, mark which ones only read, and let agents use those tools in chat. Studio calls the server itself; its secret never reaches a run.
keywords: mcp server connection, outside mcp, model context protocol, streamable http, tools, discover tools, reads, changes, readOnlyHint, agent chat, connections it may read, external tools
---
An **MCP server connection** points at an outside server that speaks MCP (the Model Context Protocol) over Streamable HTTP, such as a company's notes, ticket or search service. Crew Studio asks it which tools it offers, an administrator says what each tool does, and agents in [chat](/help/chat#what-an-agent-can-use-in-chat) may then use the tools that only read.

This is not the same as [Connect Claude (MCP)](/help/mcp), which is Crew Studio's own MCP endpoint for Claude Code. An MCP server connection goes the other way: Crew Studio is the client, and the server is someone else's.

## Add an MCP server connection
<!-- permissions: connections.manage -->
1. On [Connections](/connections), click **New connection** and pick the kind **MCP server (tools for agent chat)**.
2. Enter the **Server URL**, the server's Streamable HTTP address, such as `https://mcp.example.com/mcp`. It must be https; plain http works only to a server on the same machine as Crew Studio.
3. If the server needs a key, enter the **Auth header name** (such as `Authorization`) and, under **Secrets**, the **Auth header value** (such as `Bearer …`). The value is encrypted and never shown again.
4. Pick the **Functional areas** and click **Create**.
5. Click **Test**: Crew Studio connects and says how many tools the server offers.

Changing the server URL removes the saved secret and the tools found so far, since they belong to the old server. Replace the secret and discover the tools again.

## Discover tools and say what each one does
<!-- permissions: connections.manage -->
Click **Tools** on the connection's row, then **Discover tools**. Crew Studio asks the server for its tool list and keeps each tool with its name, description and the arguments it takes. For every tool, choose:

- **Effect**: **Reads** for a tool that only returns information, **Changes** for anything that writes, sends, deletes or spends. On the first discovery a tool starts as Reads when the server says it only reads (its `readOnlyHint`) and as Changes otherwise. That is the server's own claim, so check each one.
- **Enabled**: turn a tool off to keep it out of chat whatever its effect.

Click **Save**. The **Agent chat** column shows which tools agents may use: those that are enabled and marked Reads.

Discovering again keeps what you chose for every tool whose description and arguments are unchanged. A tool that is new, or now describes itself differently (it may do something else now), starts as Changes whatever the server says, and the window lists it so you can check it and mark it Reads. Tools the server no longer offers are removed. Save or undo your changes before discovering again. Tools with names other than letters, digits, `_`, `.` and `-`, with argument schemas over 16 KB, or past the first 200, are left out, and the window lists them.

Anyone with **View connections** can open **Tools** to see the list; only **Manage connections** can discover or change it.

## Let an agent use the tools in chat
<!-- permissions: agents.edit -->
On the agent's page, under **Chat with the agent**, add the MCP connection to **Connections it may read** (see [Agents](/help/agents#chat-with-the-agent)). In each reply, and in hand-offs the agent works itself, the agent gets the connection's tools that are enabled and marked Reads, at most 40 over all its MCP connections. Its specialists do not get them.

A chat uses an MCP connection only while it is enabled and in an area shared by the person chatting and the agent. Every call is checked again when it is made: if you mark a tool Changes, turn it or the connection off, or the person or agent leaves its area, the next call is refused, even during a reply.

## What keeps this safe

- **The secret stays in Crew Studio.** Runs never get it: the agent asks Crew Studio, which calls the server with the saved auth header and returns only the result.
- **Only reading tools, never a card.** Tools marked Changes are not offered in chat at all, and there is no card to propose an MCP call.
- **Results are information, not instructions.** Tool descriptions and results are the server's words. The agent gets only the structure of a tool's arguments with short descriptions, never the server's titles or examples. The agent sees them marked as such, a result is cut to 64 KB, and a chat with MCP tools gets no Scrape Website, so text from the server cannot send your data to an address it chooses. The web pages show them as plain text.
- **Limits.** Each call has 25 seconds, no redirects are followed, link-local and cloud metadata addresses are refused, a chat may make 30 calls a minute (120 a minute per connection over all chats), and at most 4 calls wait on one server at once.
- **A record of every call.** Each call appears in the run's journal with the tool, the connection, the names of its arguments (not their values) and the size of the result.

Arguments the agent sends go to the server, so only connect servers you trust with what the agent may read in the same chat.
