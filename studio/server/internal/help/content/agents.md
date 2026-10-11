---
title: Agents
category: Build
order: 20
permissions: agents.view
summary: What an agent is, what each agent setting means, which tools agents can use, and how to create, change and delete agents.
keywords: agent, role, goal, backstory, questions, ask the lead, ask the person, when to ask the person, llm, model, tools, reads, changes, connections it may read, google search, scrape website, workspace, delegation, max iterations, verbose, placeholders, chat, agent chat, specialists, specialist, hand-off, hand off longer work, background run
---
An agent is the worker in a crew. You describe who it is (a role, a goal and a backstory), choose the language model it thinks with, and give it tools it may use. [Tasks](/help/tasks) are assigned to agents, and [crews](/help/crews) bring agents and tasks together.

The **Agents** page shows one card per agent in your functional areas, with its name and role, the start of its goal, its LLM ("CrewAI default" if none is chosen), its tools, its areas, and where it is used ("2 crews · 3 tasks", or "Not used yet"). A **delegates** tag means it may hand work to other agents. Tools in a warning colour are unavailable or unknown; hover over one to see why. Type in **Filter agents…** to search by name, role or goal, and use the area menu (it starts at **All areas**) to show one functional area. Click a card to open the agent.

![The Agents page, showing a card for each agent with its role, goal, LLM, tools and where it is used.](agents.png)

## The agent page

An agent's page has three panels on the left and a **Used by** panel on the right. If you can only view agents, the title has a **read-only** label and the fields are locked.

![An agent's page, with the Identity, Model & tools and Behaviour panels and the Used by panel.](agent-editor.png)

### Identity

- **Name**: a short label shown across Studio. Names must be unique.
- **Functional areas**: who can see the agent. See [Access and areas](/help/access-and-areas).
- **Role**: the agent's job title or specialty, for example "Senior research analyst". Required.
- **Goal**: what the agent is trying to achieve. Required.
- **Backstory**: background that shapes how it works, such as its experience and standards. Required.

### Model & tools

- **LLM**: the language model the agent uses, from the [LLMs](/help/llms) page. Under the field, Studio shows the matching price, or a "No price entry" warning. A crew will not run while any of its agents uses **CrewAI default model** or an LLM without a price on the [Pricing](/help/pricing) page, because the cost could not be tracked.
- **Tools**: the tools the agent may use while working. See [Tools](/help/agents#tools) below.

### Behaviour

- **Max iterations** (1 to 100, 20 for a new agent): the most steps the agent may take on one task, such as thinking, calling a tool and reading the result, before it must give its best answer. Lower numbers keep runs shorter and cheaper.
- **Allow delegation**: lets this agent hand work to, or ask questions of, other agents in the crew.
- **Verbose**: writes more detailed CrewAI logs during a run, useful when working out why an agent behaved as it did.

### Used by

This panel lists the crews and tasks that use the agent, with links to each. If the agent is in at least one crew, **Inputs available** lists the run input keys those crews define, which you can use as placeholders.

## Placeholders

Role, goal and backstory can contain placeholders in curly braces, such as `{company}` or `{industry}`. When a crew runs, each placeholder is replaced with the value given for the matching run input, so one agent can be reused for different subjects.

Use lowercase letters, digits and `_` in placeholder names, starting with a letter. Placeholders are highlighted as you type, and the keys found are listed under the field. If the agent is already in a crew, a dashed warning tag marks a key that none of its crews has an input for yet.

A crew will not run while any of its agents uses a placeholder the crew has no input for. The crew's **Validation** panel lists these; see [The crew page](/help/crews#the-crew-page).

## Tools

Tools let an agent do more than write text. A tool that needs a setting the server does not have is dimmed and cannot be ticked, with a note saying what is missing; an administrator adds it to the server's configuration. A crew will not run if one of its agents uses an unavailable tool.

| Tool | What it does |
|---|---|
| **Google Search** | Searches Google through Gemini and gives the agent a summarised answer with links to the source pages. Each agent may run at most 10 searches in a run; after that it is told to read the pages it has found or write its answer. Needs a Gemini API key on the server. Each search is costed from the search model's price and the search rate in [Settings](/help/settings). Once a run reaches its budget, further searches are refused. |
| **Scrape Website** | Opens a public web page and gives the agent its readable text and a list of the links on it, so the agent can open the next page without searching for it. Addresses on local or private networks are blocked. |
| **Read Workspace File** | Reads a text file from the run's workspace folder, such as one another agent wrote earlier in the run. Nothing outside that folder can be read. |
| **Write Workspace File** | Writes a text file into the run's workspace folder, and nowhere else. |
| **Generate Video** | Makes a 3 to 10 second video clip with sound from a description, optionally starting from an image in the workspace, and saves it there as an MP4. It uses Google's Gemini and is costed from the [Pricing](/help/pricing) table, at roughly $0.10 per second of 720p video. A clip that would not fit in what is left of the run's budget is refused. Needs a Gemini API key on the server. |

Each run has its own workspace folder, so one run cannot read another run's files.

Each tool in the **Tools** list is marked **Reads** (it only returns information) or **Changes** (it writes, posts, sends or spends outside the run). Hover over the mark to see what happens to the tool in [agent chat](/help/chat#what-an-agent-can-use-in-chat): there an agent keeps only tools marked **Reads**, and tracker and KM2 reads only through the connections listed in its **Chat** panel. Its tracker and KM2 writes become cards the person confirms (see **Specialists may propose changes** below). Tools that need a crew's state, such as crew notes and the action log, are never used in agent chat.

## Create and edit agents
<!-- permissions: agents.create, agents.edit -->
To create an agent, click **New agent** on the Agents page (or **Create an agent** if there are none yet). To change one, click its card.

1. Enter a **Name** and choose at least one functional area. You can only add or remove areas you belong to; areas you are not in stay checked and locked. If you belong to exactly one area, a new agent starts in it.
2. Write the **Role**, **Goal** and **Backstory**. Add `{placeholders}` where a value should come from the crew's run inputs.
3. Choose an **LLM**. If you can view LLMs, the **Manage LLMs** link next to the field opens the LLMs page.
4. Tick the **Tools** the agent should have.
5. Adjust **Max iterations**, **Allow delegation** and **Verbose** if needed.
6. Click **Create** (for a new agent) or **Save**.

While you have unsaved changes, the title shows an **unsaved** tag, and Studio asks before you leave the page.

Changes apply to every crew and task that uses the agent, from their next run. If you add a placeholder, each crew that uses the agent needs a matching run input; see [Crews](/help/crews).

## Chat with the agent
<!-- permissions: agents.edit -->
A saved agent's page has a **Chat** panel. With it, people can talk to the agent directly on the [Chat](/help/chat) page, and the agent can bring in other agents for parts of the work:

- **People can chat with this agent** turns it on.
- **Chat model** replaces the agent's own model in chat. A cheaper model is usually fine. Specialists keep their own models.
- **Specialists it may ask** are up to 8 other agents. In a chat turn the agent can ask one a question or hand it a piece of work; the specialist answers with its own model and its own read tools, and its cost counts towards the turn. In chat a specialist gets at most 3 steps (whatever its **Max iterations**) and is asked to keep its searches and answer short. Each specialist needs an LLM and a role of its own, different from the agent's and from the other specialists'.
- **Crews it may start** are up to 10 crews. When a request fits one, the agent shows the person a card to start a work order on it; nothing runs until they confirm.
- **Connections it may read** are up to 10 Jira, Linear, KM2 issues, KM2 or MCP server connections. The tracker and KM2 read tools of the agent and its specialists use them in chat; anything written through them is a card the person confirms first. An MCP server connection gives the agent itself the server's tools that are marked **Reads** and enabled on the connection (see [MCP server connections](/help/mcp-connections)). A chat uses only the ones that are enabled and in an area shared by the person chatting and the agent. At most one of them may be a KM2 connection, since KM2 reads cannot choose between two. You need **View connections** to see this list or add to it; saving without it keeps the connections already listed.
- **Daily budget per conversation** and **Daily budget per person** cap what chat with this agent may spend in 24 hours.
- **Specialists may propose changes**: in chat no agent posts to a tracker or saves to KM2 itself. The agent's own **Comment on Tracker Issue**, **Suggest Tracker Change**, **Create Tracker Issue** and **Save to Knowledge (KM2)** always become cards the person confirms (see [Changes agents propose](/help/chat#changes-agents-propose)); with this on, its specialists' do too, in a reply or a hand-off (at most 5 cards per hand-off). The note under it lists which tools of the agent and of each specialist become cards. Confirming needs **Run crews** and a connection of that kind on the list above.
- **Hand off longer work** lets the agent propose handing work too long for one reply to a background run, done by itself or a specialist; each starts only when the person confirms its card, and the result comes back to the conversation (see [Hand-offs](/help/chat#hand-offs)). With it on:
  - **Budget per hand-off** caps what one hand-off spends (default $0.50, up to $100); the daily budgets above still apply.
  - **Time limit** stops a hand-off after this many minutes (default 20, 1 to 180).
  - **At once per conversation** caps the hand-offs running at once in one conversation (default 2, 1 to 5).
  - **The lead may ask the person during a hand-off**: specialists can always ask the agent with **Ask the lead**; with this on, when only the person can answer, the agent asks them in the conversation and the hand-off waits (see [Questions during a hand-off](/help/chat#questions-during-a-hand-off)). With it on:
    - **When to ask the person** is required: plain language, like a crew's rules for asking ("ask before choosing between price models").
    - **Wait for an answer** (default 30 minutes, 1 to 180), never past a minute before the hand-off's own time limit, which keeps running while it waits.
    - **If nobody answers**: carry on and state the assumption, or stop the hand-off.
    - **Questions per hand-off** (default 3, 1 to 20).

![The Chat panel on an agent's page, with the specialists it may ask (two chosen), the two crews it may start and the Jira and KM2 connections it may read.](agent-chat-settings.png)

Click **Save chat settings**. You can only add specialists, crews and connections in your functional areas. Ones outside them that someone else added are not listed; the panel says how many there are, and saving keeps them. A person chatting only gets the specialists, crews and connections they can see themselves. Chat with the agent stops with a message when one of its tools needs a key the server lacks, a specialist has no priced LLM, or the provider key of its or a specialist's model is not set. While an agent is a specialist of another, or a crew is on an agent's list, it cannot be deleted; take it off the list first.

In chat, an agent and its specialists keep only their tools marked **Reads**: web search and scraping, reading the files sent with the message, and tracker and KM2 reads through the listed connections. When any of them can read a tracker or KM2, none of them gets **Scrape Website** in that chat, so text from a tracker cannot be sent out to a web address; Google Search stays. Tools that change things are left out, except the tracker and KM2 writes, which become cards; tools that need a crew and tools from MCP connections are left out. See [what an agent can use in chat](/help/chat#what-an-agent-can-use-in-chat).

## Delete an agent
<!-- permissions: agents.delete -->
Open the agent and click **Delete** in the header, then **Confirm?** within a few seconds. An agent still assigned to a task or still in a crew cannot be deleted; the error names them, and the **Used by** panel shows them too. Reassign or remove the agent there first.

## See past versions
<!-- permissions: history.view -->
The **History** link on the Agents page lists every change made to agents, and the one on an agent's page lists that agent's changes, with a full copy of each version. See [History](/help/history).
