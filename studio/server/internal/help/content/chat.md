---
title: Chat with a crew or agent
category: Console
order: 35
permissions: crews.chat, crews.edit
summary: How to talk to a crew's front desk or straight to an agent, confirm or decline the changes they propose, and turn chat on for a crew or an agent.
keywords: chat, conversation, agent chat, specialists may propose, propose tracker comment, comment card, km2 save card, hand-off, handoff, hand off work, background, cancel hand-off, ask the lead, ask a human, question, waiting for you, answer, specialist, specialists, connections it may read, tracker in chat, km2 in chat, reads, changes, delegate, coworker, paste, screenshot, image, attach, file, pdf, compact, compaction, summary, context, tokens, talk, message, front desk, propose, proposal, confirm, decline, undo, card, start work order, note, budget, daily budget, cheaper model, chat model, crews.chat
---
Instead of filling in a form, you can talk to a crew: "What is slipping in Atlas?", "Do a sweep of Atlas now", "Add the vendor delay to the risk register". The crew's **front desk** agent answers. It can read the crew's notes and action log, and when it wants to change something it shows you a card. Nothing changes until you click **Confirm**.

## Talking to a crew
<!-- permissions: crews.chat -->
Open **Chat** in the menu. Pick a crew (only crews in your areas with chat turned on are listed), type a message and press **Enter** (**Shift+Enter** adds a line). The reply appears when it is ready; while the front desk is working the page says **Thinking…**, or **Waiting for a free slot…** when other runs are using every slot.

Your conversations are listed on the left. Each one belongs to you and that crew; nobody else can open it. Click **New conversation** to start another, and **Delete** to remove one (not while a reply is being written).

![The Chat page with the Crews and Agents switch, the crew and conversation list on the left, and a conversation whose last reply proposes a work order waiting for you.](chat.png)

Each message is a short run of its own, with one agent and no tasks. If you can view runs, it shows on the [Runs](/help/runs) page as a **chat** turn with its cost and journal. Only you see your chat turns there; other people only see their cost in the overall spend. The front desk is given your latest 20 messages and the crew's crew-wide notes, not the whole conversation, so keep each request self-contained when a conversation gets long.

## Talking to an agent
<!-- permissions: crews.chat -->
You can also talk to one agent directly, without a crew. On the **Chat** page, click **Agents** above the list (it shows when there are agents you can chat with) and pick one. Under its name you see who it **Can ask** (its specialists) and which crews it **Can start**.

Ask it anything. When a part of the job needs a specialist, the agent asks one during the same reply: the specialist works with its own model and web search, and the agent writes its reply from what it learnt. The run's journal on the [Runs](/help/runs) page shows each hand-off and what the specialist did. A specialist does not see your conversation, only what the agent asks it. Because you are waiting, a specialist gets at most 3 steps in a reply and is asked to search at most three times and answer briefly. If a specialist fails, the agent says so in its reply.

When your request matches one of the crews the agent may start, it shows a **Start a work order on** card naming the crew. The agent fills in the crew's inputs (such as the company to research) from what you said, and asks you first when a required one is missing; check them on the card. Nothing runs until you click **Confirm**; the card links to the work order. If the crew has been taken off the agent's list since, confirming is refused.

![The Chat page switched to Agents: the chosen agent with who it can ask and which crews it can start, and a conversation whose last reply proposes a work order on one of those crews.](chat-agents.png)

An agent in chat can use those of its own tools that only read, ask its specialists and propose crew runs; it cannot change anything itself. You only reach specialists, crews and connections in your own functional areas. Files, compacting and budgets work as for crews; the budgets are the agent's. A message is refused, saying which, when the model of the agent or of one of its specialists needs a provider key (such as `ANTHROPIC_API_KEY`) the server does not have.

## Hand-offs
<!-- permissions: crews.chat -->
Some work is too long for one reply: comparing several vendors, deep research, a long write-up. When the agent's builder has turned on **Hand off longer work**, the agent can hand such work to a **background run**, done by the agent itself or by one of its specialists, while you keep chatting.

1. The agent answers briefly and shows a card: **Hand off to** the worker, with the title, the brief and its limits (*up to $0.50, up to 20 minutes*). Nothing runs yet.
2. Click **Confirm** (once the reply has finished). The card says **Confirmed** and a new entry appears in the conversation: **Ann is working in the background**, with when it started, its limits, a **Cancel** button and a *run* link. While it waits for a free slot it says it will start when one is free.
3. The message box stays open: ask the agent something else and it replies as usual. It knows which hand-offs are still running, so it says so instead of starting the same one again.
4. When the work is done, the entry becomes a message headed **Ann (hand-off result)** with the result, its cost and a *run* link. Later replies are given the result like any earlier message, so you can ask follow-ups.

If the hand-off fails, runs out of time or you cancel it, the entry says **The hand-off did not finish** and why (**Cancelled by you** after **Cancel**), with what it spent. Only you can cancel your hand-offs here, and only while signed in to Crew Studio; people who may cancel runs can also cancel it on the [Runs](/help/runs) page.

The worker sees the conversation so far and the brief, with the same read-only tools as in a reply; a working agent can still bring in its specialists. It gets more steps than in a reply (up to 25), since nobody is waiting on each one. It cannot change anything itself: anything it proposes (a crew run, or a tracker comment or KM2 document, see [Changes agents propose](#changes-agents-propose)) appears as a card of its own just above the hand-off's entry, with one line saying who proposed it during which hand-off, and a reminder that it was proposed while nobody was watching, so check it before you confirm. A hand-off may propose at most 5 such cards. It can never start another hand-off.

Limits, set by the agent's builder:

- Each hand-off may spend its **budget per hand-off**, but never more than is left of the conversation's and your daily budgets, and it always leaves a little for your next messages. While it runs it counts at that whole amount, so several hand-offs cannot each take the last dollar.
- It is stopped at its **time limit**, counted from when it starts (waiting for a free slot does not count); the entry then says it ran past it.
- A conversation may have only so many hand-offs running at once (**At once per conversation**, at most 5), and you at most 5 with one agent across your conversations.

When confirming is refused (the budget is used up, too many are running, hand-offs were turned off or the specialist was taken off the agent's list), the card stays waiting and says why. A conversation cannot be deleted, nor compacted, while one of its hand-offs runs; cancel it or wait for it first. Hand-offs show on the [Runs](/help/runs) page as **hand-off** runs that only you see, and like chat turns they cannot be deleted there until they are a day old; deleting one later keeps its result in the conversation.

## Questions during a hand-off
<!-- permissions: crews.chat -->
A specialist that needs a decision it cannot make itself (list prices or negotiated prices?) asks the lead with **Ask the lead**. The lead answers from what it knows of your conversation and the work, without stopping the specialist's run. In a reply, when only you can answer, the lead says so instead, and its reply asks you; you answer in your next message.

During a hand-off, when the agent's builder has turned on **The lead may ask the person during a hand-off**, the lead can bring the question to you:

1. A **Waiting for you** card appears under the hand-off's entry, saying who asked (*Asked by Market analyst through Research lead*), the question, why it is needed and by when. The **Chat** entry in the menu shows a badge, and the Chat page lists the conversations waiting for you at the top.
2. Type your answer and click **Send answer**. The hand-off carries on with it at once; the card moves under *Earlier questions*.
3. If nobody answers in time, the hand-off follows the agent's rule: it carries on and states its assumption, or it stops and keeps what it wrote.

The hand-off's time limit keeps running while it waits for you, so a question is always due a minute before the hand-off must stop; with less time left than that, the lead does not ask and decides itself. Each hand-off may ask you at most the agent's **Questions per hand-off**, and the lead may be asked at most 5 times in a hand-off (2 in a reply). Only you can answer the questions in your conversations, and only while signed in to Crew Studio. Everything the agents write in a question is shown as plain text, and each card reminds you it was written by an AI agent: never put a password or other secret in an answer. The run's journal shows *asked the lead*, the lead's answer, the question to you and your answer.

## What an agent can use in chat

The agent and each specialist keep only the tools on their own list that the [Agents](/help/agents#tools) page marks **Reads**, and only when the chat can give them what the tool needs:

| Tool | In agent chat |
|---|---|
| Google Search | Yes |
| Scrape Website | Yes, except in a chat where the agent or one of its specialists can read a tracker, KM2 or an MCP server: then no agent in that chat gets it |
| Read Workspace File | Yes; it reads only the files sent with your message |
| List Tracker Projects, Find Tracker Issues, Get Tracker Issue, Get Tracker Children | Yes, through a Jira, Linear or KM2 issues connection on the agent's **Connections it may read** list that is enabled and in an area you share with the agent |
| Search Knowledge (KM2), Ask Knowledge (KM2) | Yes, through the KM2 connection on that list (an agent lists at most one), on the same terms |
| Tools of an MCP server connection on that list | The agent itself (not its specialists): the tools marked **Reads** and enabled on the connection, on the same terms. See [MCP server connections](/help/mcp-connections) |
| Ask question / Delegate work to coworker | The agent, when it has specialists |
| Ask the lead | Each specialist |
| Ask a human | During a hand-off whose agent allows questions: the lead (when it does the work, or when a specialist asks it). Never in a reply |
| Start Crew Work Order (a card) | The agent, when it has crews and you hold **Create work orders** |
| Hand off work (a card) | The agent, when its hand-offs are on; never inside a hand-off |
| Comment on Tracker Issue, Suggest Tracker Change, Create Tracker Issue, Save to Knowledge (KM2) | Never themselves. They become **Propose Tracker Comment**, **Propose Tracker Suggestion**, **Propose Tracker Issue** and **Propose Knowledge Save (KM2)** (cards): for the agent, and for its specialists when **Specialists may propose changes** is on, through a listed connection of that kind, when you hold **Run crews**. See [Changes agents propose](#changes-agents-propose) |
| Other tools marked **Changes** (writing files, generating video, delivering reports) | Never |
| Crew notes, the action log, the tracking list | Never: an agent has no crew |

You need only **Chat with crews and agents** and an area shared with the connection for these reads; crew chat's tracker and KM2 reads also need **View crew state**. If a connection is disabled, or you or the agent lose its area, the agent stops reading through it, even during a reply. Everything a tracker or KM2 returns is treated as information, not instructions. Because issue text and documents are written by other people, a chat that can read them cannot also open web pages: text asking the agent to "fetch" an address with your data in it finds no tool to do it with. Google Search stays, since its searches go to Google, not to an address the text chooses.

## Sending screenshots and files

Paste a screenshot into the message box, drop images or PDFs onto it, or click the paperclip. Up to 4 files go with a message (10 MB each), and a conversation holds up to 40. The front desk looks at them if its model can see images (the LLM's **Vision** setting); otherwise it tells you it could not. Files show under your message; only you can open them, and they are deleted with the conversation. Later messages are given the text of earlier ones, not their files, so describe anything the crew should remember.

## Compacting a long conversation

Every message resends the conversation so far, so a long conversation costs more with each message. The line under the messages says roughly how many tokens each message resends, and when older messages no longer fit in the 20 that are sent.

Click **Compact** to have the front desk summarise the conversation. The summary appears in the conversation as **Conversation compacted**; click **Show the summary** to read it. From then on each message is given the summary and the messages after it instead of everything before, so it costs less and the gist of older messages is kept. Compacting is one short turn of its own: it spends a little from the chat budget, and you wait for it like a reply. You can compact again later; the new summary includes the earlier one. If compacting fails, nothing changes.

## What the front desk can do
<!-- permissions: crews.chat -->
| Tool | What it does | You need |
|---|---|---|
| Read Note, List Notes | Reads the crew's notes, such as last week's report | View crew state |
| Check Action Log | Says exactly whether the crew already did something | View crew state |
| Start Work Order | Proposes a full run of the crew with a brief and its inputs | Create work orders |
| Propose Note Change | Proposes new text for one of the crew's notes | View and Manage crew state |

It gets exactly these tools, whatever its agent is otherwise set up with, and only those you could use yourself: chat runs as you. Without **View crew state** it is not given the crew's notes either. It cannot write notes or the action log itself, and text it reads (notes, earlier messages, tool results) cannot give it more. Scheduled and work order runs cannot make proposals at all.

## Confirm, decline and undo
<!-- permissions: crews.chat -->
A proposal appears as a card under the reply, with what would happen: the work order's title, brief and inputs, or the note's name and its whole new text. A work order proposal must fit the crew: Studio refuses one that leaves out a required input without a default, or names an input the crew does not have, and the front desk or agent tries again.

- **Confirm** applies it as you. A work order starts straight away (this needs **Create work orders**) and the card links to it; the work order shows **chat** as its source. A note is saved as a new version with your name (this needs **Manage crew state**).
- **Decline** leaves everything as it was.
- **Undo** on a confirmed note change puts the note back as it was (a note the change created is removed). It is refused if the note changed again since; use the crew's **State** tab then. A started work order is cancelled from its own page instead.

![A confirmed proposal card, showing who confirmed it and when, with a link to the work order it started.](chat-proposal-confirmed.png)

Only you can confirm your conversation's proposals, and only while signed in to Crew Studio; an API key cannot. A proposal expires 24 hours after it was made and then shows as **Expired**. If confirming fails, for example because the note changed since the front desk read it, the card stays waiting and says why; ask the crew again.

## What chat costs
<!-- permissions: crews.chat -->
Each conversation may spend the crew's **daily budget per conversation** in any 24 hours (default $1.00), and you may spend its **daily budget per person** on the crew's chat over all your conversations (default $5.00; deleting a conversation or starting a new one does not reset it). A reply still being written counts at the most it may spend until it finishes, so replies in several conversations at once cannot go over your budget. Under the messages you see both; once either is used, sending waits until older turns fall out of the 24 hours. Hand-offs count against the same budgets (see [Hand-offs](#hand-offs)). Chat turns and hand-offs cannot be deleted from the Runs page until they are a day old, since the budgets count them. Chat spend is part of the **Spend · 30 days** figure on the [Overview](/help/overview).

## Changes agents propose

An agent never posts to a tracker or saves to KM2 itself in chat. When it (or, if its builder allows it, one of its specialists) has such a tool, it gets one that proposes the same change instead, and you see a card:

- **Comment on PRJ-10**: the connection, the issue and the whole comment.
- **Suggest a change on PRJ-10**: the field, the suggested value and the reason. It is posted as a *suggested change* comment; no field is ever changed.
- **Create an issue in PRJ**: the project, title, parent and description. Only through a connection where an administrator turned on **Let crews create issues**.
- **Save to KM2**: the folder, title, document id and the whole document. Its id always starts with *agent-chat-* and a part of the conversation's own id, so a card can only replace a document an earlier card of the same conversation saved.

Each card also says which agent drafted it, and what Studio itself will add (the *Posted by* line, or the details saved with a KM2 document). Invisible characters, such as text-direction overrides and zero-width spaces, are removed, so what you read is what is sent. Nothing is posted or saved until you click **Confirm**. Studio then checks again, as you, that you still hold **Run crews**, that the agent's chat is still on (and, for a specialist's card, that its specialists may still propose and it is still on the agent's list), that the agent that drafted it still has the tool, and that the connection is still enabled, still on the agent's list, and in an area you and the agent share; if not, the card stays waiting and says why. The change is made through the connection's own account, in your name: a tracker comment or new issue ends with *Posted by* you *from a Crew Studio agent chat; drafted by* the agent, and a KM2 document records you and the agent in its details. If the tracker or KM2 refuses (an issue that does not exist, say), the card stays waiting with its answer. If anything else goes wrong once the change was sent (no answer, an answer Studio cannot read, an error on their side), it may have been made: the card then says so and is not offered again, so nothing is posted twice; check the issue or document before asking the agent again. These cards cannot be undone from chat; the card links to the comment or issue. A reply makes at most one card; a hand-off at most 5. What the agent wrote on a card is shown as plain text.

## Turning chat on for an agent
<!-- permissions: agents.edit -->
Open the agent and use the **Chat** panel below its settings: turn chat on, and pick its chat model, its specialists, the crews it may start, the connections it may read, its daily budgets, whether its specialists may propose changes and whether it may hand off longer work. See [Agents](/help/agents).

## Turning chat on for a crew
<!-- permissions: crews.edit -->
Open the crew and find the **Chat** panel on its **Design** tab (save a new crew first):

- **People can chat with this crew** turns chat on.
- **Front desk agent** is the crew agent that answers. It must be in the crew; if it is removed from the crew later, chat stops until you pick another.
- **Chat model** replaces the agent's own model in chat. A cheaper model is usually fine. A model used here cannot be deleted.
- **Daily budget per conversation** caps what one conversation may spend in 24 hours (up to $100).
- **Daily budget per person** caps what one person may spend on this crew's chat in 24 hours, over all their conversations (up to $1,000).

Click **Save chat settings**. People who hold **Chat with crews and agents** and share an area with the crew can then chat with it. Turning chat off keeps conversations readable but stops new messages. A message is refused when the front desk's model (or the chat model) needs a provider key the server does not have.

![The Chat panel in the crew builder, with chat turned on, the front desk agent, the chat model and both daily budgets.](chat-settings.png)
