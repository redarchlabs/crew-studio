---
title: Work orders
category: Console
order: 20
permissions: work_orders.view
summary: How to find work orders, read what each attempt did, and (with the right access) create, re-run, cancel and delete them.
keywords: work order, compact, compact context, context, tokens, WO, request, job, conversation, follow up, ask, feedback, revise, revision, improve, brief, attachments, files, screenshots, paste, inputs, target, crew, crew picker, single task, one-task crew, make a crew, budget, draft, submit, attempt, re-run, retry, journal, workflow, diagram, swimlane, sequence, transcript, output, result, cancel, delete
---
A work order is a request for a [crew](/help/crews) to do something. It holds a written brief, any files the agents should use, the inputs the crew needs, and a budget. Each time a work order is run, Crew Studio records an **attempt** you can inspect afterwards: what each agent did, the files they wrote and the final result.

Work orders come from people in Crew Studio, from schedules, from webhooks sent by other systems, and from the API. Work orders are numbered (WO-1, WO-2, ...) and belong to the functional areas of the crew they run.

Work orders always run a crew. To run a single task, make a one-task crew from it with **Make a crew** on the task's page (see [Run a single task](/help/tasks#run-a-single-task)), then pick that crew.

![The Work orders list with search, filters and a table of work orders.](work-orders.png)

## Finding a work order

Open **Work orders** in the sidebar. The list shows the newest first, 25 per page, with these columns:

- **Work order**: number and title, plus "N attempts" when it has been run more than once.
- **Target**: the crew it runs.
- **Status**: draft, queued, running, succeeded, failed or cancelled.
- **Source**: manual, schedule, webhook or api.
- **Areas**, **Created by**, **Cost** (total so far) and **Updated**.

To narrow the list:

1. Type in **Search title or WO-123…** to match a title or a work order number.
2. Use the **Status**, **Source** and **Crew** filters.

While anything on the page is queued or running, the list refreshes itself every few seconds. On a phone, work orders are shown as cards instead of a table.

## Statuses

| Status | Meaning |
|---|---|
| draft | Saved but never submitted, so it has no attempts. |
| queued | Waiting for a free slot; only a set number of runs execute at once. |
| running | Agents are working on it now. |
| succeeded | The latest attempt finished. |
| failed | The latest attempt stopped with an error, for example at its budget's hard stop. The error is shown on the **Brief** tab. |
| cancelled | The latest attempt was stopped before it finished. |

## A work order's page

Click a work order to open it. The header shows its number, title and status (with a green "live" marker while it is running), the target, where it came from, its areas, who created it and when. For the selected attempt you also see a spend bar against the budget, the **Duration**, **Tokens** and the number of **Attempts**.

![A finished work order showing its header, attempts and the Brief tab.](work-order-detail.png)

### Attempts

When a work order has run more than once, a row of attempts appears under the header ("Attempt 2 of 3"), newest first, each with its status, cost and age. Click one to switch every tab to that attempt. Earlier attempts keep exactly what they ran with.

### Brief

The **Brief** tab shows the brief the selected attempt ran with. If the brief has been changed since, an "edited since this attempt" label appears. Below it you find any error, the **Final output** and each task's output, each with a **Copy** button. The side panel lists the **Input files** and the **Inputs** used by this attempt. A work order started by a watched schedule shows what the watch found instead of raw inputs: the changes, any [roll-ups](/help/watches#roll-ups) of tracked items, and the follow-ups due.

![What the watch found on a work order's Brief tab: the items added, a roll-up of the tracked item and the follow-up due.](work-order-watch.png)

### Conversation

The **Conversation** tab lets you talk with the crew about the work order: ask why it concluded something, where a number came from, or what it would do differently. The crew has the brief and the latest output. Your conversation is yours; other people have their own. Talking needs the Chat with crews and agents permission, and replies spend from the crew's chat budget. When the conversation gets long, click **Compact** to replace it with a summary for later messages (see [Chat](/help/chat)); a revision that includes the conversation gets the summary too.

You can paste screenshots into the message box or attach images and PDFs, as on the [Chat](/help/chat) page.

To have the work redone, write what should change under **Revise with feedback** and click the button. Paste screenshots into the box, drop files on it or click the paperclip to send files with the feedback: they join the work order's files, which the revision gets, and the feedback names them. That starts a new attempt that gets the latest output and your feedback (and, if you leave the box ticked, your conversation) and revises the output rather than starting over. **Feedback given** lists every revision request on the work order. Revising needs permission to create work orders, and waits until the current attempt has finished.

### Workflow

The **Workflow** tab draws how the attempt played out. The **Sequence** view is a swimlane diagram of the journal: one lane per agent plus a Crew lane, with time flowing downward, tasks marked as bands and delegations drawn as red arrows between lanes. Click an entry to see its details in a side panel.

### Journal

The **Journal** tab is a readable log of what each agent did on each task: LLM calls, tool use, searches, delegations and outputs. On the left, **Agents** and **Tasks** list everyone involved, with calls, tokens and cost; click one to show only its entries, and **clear** to undo. You can also search entries and switch the **LLM calls**, **Tools**, **Delegations**, **Tasks** and **Run** groups on and off. Very long journals are cut short; the full record is in the transcript.

Each LLM call of an agent resends everything it has done on its task so far, so calls grow as it works. While an attempt is running, **Compact context** (for people who can cancel work orders) asks every agent to replace its steps so far with a summary before its next LLM call; the task and its latest step are kept. Later calls then resend far fewer tokens. Each compaction shows in the journal with the size before and after (or why it could not be done, in which case nothing changed), and the summary is one extra LLM call of the agent. An agent that has only just started waits until it has done enough to summarise. To have this happen by itself, set a limit under **Compact an agent's context above** in [Settings](/help/settings).

### Files

The **Files** tab lists the work order's **Input files**. Images appear as thumbnails you can click to enlarge, small text files have a **Preview** button, and every file has **Download**.

## Run details
<!-- permissions: runs.view -->
Because you can also view runs, you get more detail on a work order's page:

- The **Workflow** tab has a **Graph** view: the work order, tasks, agents, tools and models as connected boxes, with run order, context, assignments, tool calls, LLM calls and delegations drawn as lines. Click a box for its details.
- The **Files** tab also lists **Files the agents wrote** and the **Transcript and console log**: the complete, untruncated record of every prompt, response, tool call and console line. Both are saved when the attempt finishes.
- A **Debug** tab offers downloads of the full record, plus **LLM calls** (every prompt and response), **Events**, **Console**, the frozen **Spec** and **Callbacks**.
- **Open run** next to the attempt counter opens the attempt on its [run page](/help/runs).

## Creating a work order
<!-- permissions: work_orders.create -->
Click **New work order** on the Work orders page.

![The New work order page with title, target, brief, inputs and budget.](work-order-new.png)

1. Enter a **Title** (up to 200 characters).
2. Under **Target**, pick a crew from the searchable list. Each option lists the crew's tasks, so you can tell similar crews apart. The crew runs all its tasks. To run just one task, first make a one-task crew from it (see [Run a single task](/help/tasks#run-a-single-task)).
3. Write the **Brief**. Markdown works; use **Preview** to check it. The brief can be up to 100,000 characters and is given to every task.
4. Add files (see below).
5. Fill in the **Inputs** panel. Each field matches an input of the crew, for the `{placeholders}` in its tasks and agents. Required inputs have a red asterisk.
6. Optionally set a **Budget (USD)**. Leave it empty to use the crew's budget.
7. Click **Submit** to start straight away, or **Save draft** to finish later.

If you try to leave with unsaved changes, Crew Studio asks before discarding them.

### Adding files and screenshots

- **Paste a screenshot** straight into the brief. It is attached as a file named like `pasted-20261003-142501.png`. Pasting text inserts the text as usual.
- **Drag and drop** files onto the brief, or click **Attach files**.

Accepted: images (PNG, JPEG, GIF, WebP), PDF, text files (such as TXT, Markdown, CSV, JSON, XML, YAML, HTML), and Word, Excel and PowerPoint files. Limits: 25 MB per file, 20 files and 100 MB in total per work order. Files outside these limits are refused with a message saying why.

Files upload once a crew is chosen. The first file you attach saves a draft automatically so it has somewhere to go. Images and PDFs are shown to agents whose model can read them; small text files are quoted; other files are copied into the run's workspace for the agents' tools.

### Editing and re-running

On a finished or draft work order, click **Edit** to change it, then **Save changes**, or **Save & re-run** to start a new attempt. **Re-run** starts another attempt with the current details. Changes affect only future attempts. You cannot edit while an attempt is queued or running.

If a work order's crew has been deleted, the list and header still show the crew's name, but the work order has no target. It cannot run again until you **Edit** it and pick another crew.

## Cancelling a work order
<!-- permissions: work_orders.cancel -->
While an attempt is queued or running, click **Cancel** in the header. A queued attempt is cancelled before it starts; a running one is stopped and marked cancelled. What the agents did before you cancelled stays in the attempt's journal.

## Deleting a work order
<!-- permissions: work_orders.delete -->
Click **Delete** in the header, then click it again (it changes to **Confirm?**) within a few seconds. This removes the work order and all its attempts, events, outputs and files, and cannot be undone. You cannot delete a work order while an attempt is queued or running.
