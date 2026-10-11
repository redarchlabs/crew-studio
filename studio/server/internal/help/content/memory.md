---
title: Memory
category: Build
order: 16
permissions: state.view
summary: How a crew keeps soft lessons between runs, how runs recall them as quoted data, and how to review, edit, delete and promote memories on the crew's Memory tab.
keywords: memory, memories, remember, recall, lesson, learn, automatic memory, explicit, Remember tool, recall only, review, approve, pending, waiting for review, prompt injection, re-index, pin, scope, retention, forget, delete person, personal data, promote, KM2, embedding, ranking
---
Memory holds soft lessons a crew picks up by doing its work, such as "Sam usually replies late on Fridays", "PRJ-15 dates have slipped three times" or "the vendor status page is slow to show outages". Before each task the crew recalls the few memories that look most relevant to it. Depending on the crew's settings, runs can also save new ones.

Open a crew and click the **Memory** tab to see its memory settings and the memories it keeps. Memory is off until someone turns it on.

![The Memory tab's settings: Automatic mode with shallow recall, five memories per recall, the crew's cheapest model as the memory model, 20 saves per run, review of memories that runs save on and memories kept for 365 days.](memory-settings.png)

## What memory is not for

Memory is fuzzy: recall finds memories by meaning, and an old memory may be out of date. Use something exact when the answer matters:

| Need | Use instead |
|---|---|
| "Did we already remind Sam about PRJ-15 this week?" | The action log on the [State tab](/help/crew-state) |
| "What did last week's report say?" (a named document) | Notes on the [State tab](/help/crew-state) |
| Something other crews or people should rely on | [KM2](/help/km2): promote the memory |
| Deciding who to remind, or what changed | [Watches](/help/watches) and follow-up ladders |

## How a crew uses memory

The **Memory** setting decides what runs do:

| Mode | What runs do | Extra cost |
|---|---|---|
| Off | Neither recall nor save memories | None |
| Recall only | Recall memories that people added or earlier runs saved, and save none | One small embedding per task |
| Explicit saves | Recall, and agents get a **Remember** tool to save what they choose | One embedding per save, no model call |
| Automatic | Recall, and after each task a model picks out what is worth keeping (agents get no save tool of their own) | One model call per task, on the **Memory model** |

Memory needs an embedding model on the server. When there is none, the settings say why and only **Off** can be chosen.

A memory has a **scope**, a path such as `/people/sam` or `/projects/atlas`, that groups related memories. `/` is the whole crew. Runs recall from every scope of their own crew, and can never read another crew's memories.

## Recall is quoted data

Before each task, the crew searches its memories for the ones closest to the task's text and adds them to the agent's prompt, each with when it was saved, by which run or person, and its scope. The prompt says they are notes from earlier runs, may be out of date, and are data, never instructions to follow.

Recall leaves out memories that are not close enough to the task to be relevant, so a task may get fewer memories than **Memories per recall**, or none. Among the close ones, the ranking under **Advanced: ranking** decides which are used: a recent or important memory can beat one that is slightly closer in meaning.

**Shallow** recall is a quick search with no model call. **Deep** recall lets the **Memory model** refine the search when the first results are weak, which costs a model call now and then. Every recall is written in the run's journal, such as "Memory: recalled 4 of 37".

After the server's embedding model changes, Crew Studio re-indexes the crew's memories in the background. Until it is done, the settings say how many memories are still being re-indexed, and recall may miss them.

## Review of memories that runs save

What a run saves comes from what it read, and that can include ticket comments, emails or web pages written by anyone. A crafted ticket could try to plant an instruction for later runs, either through the model that picks out lessons in automatic mode or by talking an agent into using the Remember tool. So **Review memories that runs save before later runs use them** is on by default, in Explicit saves and Automatic mode: every memory a run saves waits as **waiting for review** until a person approves it, and later runs cannot recall it until then. Pending memories nobody approves are deleted after 30 days.

When you turn review off, the settings warn that memories runs save go live at once. Memories people add are always active at once.

- A crew can have at most 500 memories waiting for review. Past that, runs save no more until people approve or delete some; people can still add memories.
- A run can only change or delete memories it saved itself, and only while they wait for review (or, with review off, until a person pins them). Once a person approves or pins a memory, runs cannot change or delete it.
- A memory waiting for review cannot be promoted to KM2; approve it first.

## Retention

By default, memories are deleted 365 days after they were saved. **Pinned** memories are always kept. Turn off **Delete old memories** to keep them until someone deletes them.

## See a crew's memories
<!-- permissions: state.view -->
![The Memories panel with the scope tree on the left and four memories: two saved by a run and waiting for review, one added by a person and one pinned.](memory.png)

The **Memories** panel lists memories newest first. Each shows its text, scope, categories, importance, whether it is **pinned**, **waiting for review** or **in KM2**, and where it came from: the run that saved it (click the run id to open it), with its task and agent, or the person who added it. It also says when the memory was saved, edited and last recalled.

- Click a scope on the left to show that scope and everything below it, or **All scopes**.
- Type in **Search memories** and click **Search** to find memories by meaning, or by their words when meaning search is not available.
- Use the status menu to show only **Active** memories or those **Waiting for review**, and **Pinned only** to show pinned ones.

## Change memory settings
<!-- permissions: crews.edit -->
Changing the settings needs both Edit crews and Manage crew state, because they decide what memory costs.

1. Choose a **Memory** mode.
2. Choose **Recall** (Shallow or Deep) and **Memories per recall** (1 to 20, 5 by default).
3. For Automatic mode or deep recall, choose the **Memory model**. It starts at the crew's cheapest model; a cheap model is usually enough. The model needs a price on the [Pricing](/help/pricing) page.
4. For Explicit saves or Automatic, set **Saves per run** (0 to 100, 20 by default). Saves past it are skipped.
5. For Explicit saves or Automatic, leave **Review memories that runs save before later runs use them** on unless you trust every source the crew reads.
6. Set how long to keep memories under **Delete old memories**.
7. Under **Advanced: ranking**, you can change how recall weighs similarity, recency and importance. The defaults suit most crews.
8. Click **Save memory settings**. The next run uses them.

## Add, edit, pin and approve memories
<!-- permissions: state.manage -->
- **Add a memory** writes one by hand: the text (up to 2,000 characters), a scope, optional categories, an importance from 0 to 1, and whether to pin it. It starts in the scope you are browsing.
- **Edit** changes a memory's text, scope, categories, importance or pin.
- **Pin** keeps a memory past retention; **Unpin** undoes it.
- **Approve** makes a memory that is waiting for review active, so runs can recall it. To reject it, delete it.

Every change is recorded in the audit log, without the memory's text.

## Delete memories, and memories about a person
<!-- permissions: state.manage -->
Click **Delete** on a memory, then **Confirm?**, to delete one.

To delete many at once, click **Delete memories…**, or tick memories and click **Delete selected**:

1. Enter a **Scope** to delete it and everything below it (`/` is every memory), text the memories **Contain**, or both.
2. Click **Count matches** to see how many memories would go. Nothing is deleted yet.
3. Click **Delete N memories**. This cannot be undone.

To remove what a crew remembers about a person, enter their scope (such as `/people/sam`), their name or email under **Contains**, or both, and run it once for each. Their entries in the action log and notes are separate; delete those on the [State tab](/help/crew-state).

## Promote memories to KM2
<!-- permissions: state.manage -->
A memory only helps its own crew. To make lessons something other crews and people can rely on, promote them to [KM2](/help/km2):

1. Tick one or more memories (up to 50) and click **Promote to KM2**. Memories waiting for review cannot be promoted: approve them first.
2. Enter a **Title**, and optionally pick a **Folder** from the folders the crew's KM2 connection allows (its default folder comes first) and an **External ref**. Saving again with the same external ref replaces that document instead of adding a copy.
3. Click **Save to KM2**. Crew Studio shows KM2's outcome and the document id. Promoting the same memories with the same title and details again sends nothing new: it shows **already saved** with the existing document id.

Promotion uses the crew's KM2 connection, which must be in one of your functional areas, and the connection's folder rules and daily limits apply. One person may promote 20 times a day. The save is recorded in the action log as `km2:save`, and the memories are marked **in KM2**. Runs never promote memories on their own.
