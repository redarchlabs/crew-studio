---
title: Runs
category: Console
order: 30
permissions: runs.view
summary: How to find runs and read a run's page: the live interaction graph, timeline, LLM prompts and responses, outputs, logs, frozen spec and budget.
keywords: run, runs, execution, history, status, live, resume, resume from task, run again, retry, failed run, reuse, reused, interaction graph, timeline, events, LLM calls, prompt, response, tokens, cost, spend, budget, hard stop, searches stopped, outputs, logs, console, spec, callbacks, transcript, cancel, delete, stop
---
A run is one execution of a crew. A run can be started by a person, by a work order (each attempt is a run), by a schedule, by a webhook or through the API. Crew Studio keeps everything about each run: every event, every LLM prompt and response, tool calls, delegations, costs and the exact configuration it ran with.

## Finding a run

Open **Runs** in the sidebar. Runs are listed newest first, 25 per page.

![The Runs list with crew, status and trigger filters.](runs.png)

Each row shows:

- **Crew**: the crew name and the start of the run's id. Older runs of a single task (from before work orders always ran a crew) are named "Task: " followed by the task name. Click the name to open the run.
- **Status**: queued, running, succeeded, failed or cancelled.
- **Trigger**: how it started (Manual, API, Schedule, Webhook or Work order). A Work order label links to that work order.
- **Started by**, **Cost**, **Budget**, **Duration** and **Started**.

Use the filters at the top right to narrow the list: a crew (shown if you can see crews), **Any status**, **Any trigger**, and **Runs and chat** to show only crew runs or only [chat](/help/chat) turns (marked **chat**). While a run on the page is queued or running, the list refreshes every few seconds.

You only see runs in your [functional areas](/help/access-and-areas#functional-areas).

## The run page

Click a run to open it. The header shows the crew (click it to open the crew), the status, and a connection marker: "live" while events are streaming in, or "reconnecting…" if the connection drops. Below that:

- The **Spend** bar (see [Budget and hard stop](#budget-and-hard-stop)).
- **Duration**, **Tokens** (with the number of requests) and **Process** (with the number of agents and tasks).
- The inputs the run was given, and any error it ended with.
- For a work order attempt, a link back to the work order.

![A finished run showing the header, spend bar and interaction graph.](run-detail.png)

Everything updates live while the run is going; you do not need to reload.

### Interactions

The default tab draws the run as a graph: the crew, its tasks, the agents doing them, and the tools and models they used. Lines show assignments, context passed between tasks, tool calls, LLM calls and delegations (in red). Each box shows its calls, tokens, cost and time. Click a box to see just its events in a **Timeline for selection** panel underneath; **Open in timeline** takes you to the full timeline with that focus kept.

### Timeline

Every event in order. Switch event types on and off with the chips (crew, task, agent, llm, tool, search, delegation, budget), and pick an agent to see only its events. Expand an LLM call to read its prompt and response.

### Journal

A readable, per-task account of what each agent did, with agents and tasks on the left as filters, a search box, and chips to show or hide LLM calls, tools, delegations, tasks and run entries.

### LLM calls

A table of every call to a model: number, agent, task, model, prompt and completion tokens (cached prompt tokens are shown with a "c"), cost and time. The total count and cost are at the top. Click a row to see the full **Messages** sent (system, user and assistant) and the **Response**, with a **Copy** button. Use **Refresh** while a run is in progress.

### Outputs

The **Final output** and each task's output, as formatted text. Each has **Copy** and a **.md** download. Each task shows the agent that did it and what it cost (its LLM calls, searches and videos). In a resumed run, a task it reused says **reused from run** with a link to the run that did the work: it cost $0 in this run, and the card shows what it cost when it ran. If that run was deleted, or is not one you may open, the card says so instead of linking to it.

Each task's output is saved the moment the task finishes, so a run that fails, is cancelled, times out or is stopped when Crew Studio restarts keeps the tasks it finished.

### Files

The work order's input files (if any), **Files the agents wrote**, and the **Transcript and console log**: the complete record of every prompt, response, tool call and console line. Agent files and the transcript are saved when the run finishes.

### Logs

The raw console output. It follows new lines as they arrive; click **Pause** to stop scrolling and **Resume** to catch up. Use **Filter lines…** to search and **Copy all** to copy.

### Callbacks

If the run reports its result to another system when it finishes, each delivery is listed with its URL, status (pending, delivered or failed), attempts, last response and next retry.

### Spec

The frozen configuration the run started with: crew, inputs and budget, agents, tasks, LLMs, prices and settings. Later edits to agents, tasks or prices do not change it, so you can always see exactly what ran.

## Budget and hard stop

Every run has a budget in US dollars: the crew's budget unless a different one was set for the run or its work order. Costs are estimates from your organisation's price list.

The **Spend** bar shows spend against the budget. It turns amber at 80% and red at 100%. The bar extends past the budget to the **hard stop**, which is the budget multiplied by the hard-stop multiplier in Settings; a thin line marks the budget itself.

- **At the budget**, web searches are refused so agents finish from what they already have. The bar shows "searches stopped".
- **At the hard stop**, every further LLM call is blocked and the run fails. The bar shows "hard stop". Outputs of tasks that had already finished are kept.

## Cancelling a run
<!-- permissions: runs.cancel -->
To stop a run that is queued or running, click **Cancel run** in the run's header, or **Cancel** on its row in the Runs list. A queued run is cancelled before it starts; a running one is stopped and marked cancelled. Its events and logs up to that point stay available.

## Resuming a failed run
<!-- permissions: crews.run, work_orders.create -->
When a run fails or is cancelled part-way, for example because a provider ran out of credits, you do not have to pay for the finished tasks again. A finished run that can pick up where it stopped shows **Resume from task N** in its header, where N is the first task that did not finish.

1. Fix what stopped the run, if you can (add credits, raise the budget, or change the model on the agent).
2. Click **Resume from task N**.
3. Choose the options:
   - **Use the crew's current settings**: off, the remaining tasks run with the agents, tasks and LLMs the run started with. On, they run with the crew as it is now, for example after you switched an agent's model. The finished tasks must still be the crew's first tasks, in the same order, and neither they nor their agents may have been edited since, apart from an agent's LLM.
   - **Budget (USD)**: what the new run may spend on its own work. Leave it empty to use the earlier run's budget. Each resume has a budget of its own. If the run stopped because it spent its budget, the dialog says so; raise the budget, or the new run may stop the same way.
4. Click **Resume from task N**. The new run opens.

The new run copies the finished tasks' outputs (at no cost) and hands them to the later tasks as context, exactly as if those tasks had just run. Files the earlier run's agents wrote are put back in its workspace; its journal starts with what it reused and names any file that could not be put back. Its header says **Resumed from run** with a link, and the earlier run's header links to the runs that resumed it.

Resuming as it was is refused when something the run would use was taken away since it started: an LLM that was deleted, or a tool that is no longer available. Resume with the crew's current settings instead, or run it again. The crew must also still exist and be in your areas, as for starting a run.

The task that failed runs again from the start. The dialog says what it had already spent. Anything it already did before it stopped, such as a note it saved or a comment it posted, may happen a second time. If an LLM the run used now lacks its provider key, the dialog warns that the remaining tasks are likely to fail as they were.

A run can be resumed when at least one task finished and at least one is left. While a resume of a run is queued or running, that run cannot be resumed again. Chat turns and hand-offs are not resumed. You need **Run crews** (`crews.run`); for a work order's attempt, **Create work orders** (`work_orders.create`), and only its latest attempt can be resumed, from the work order's page.

## Running a crew again
<!-- permissions: crews.run -->
On a finished run of a crew, **Run again** opens the run dialog with the same inputs and budget filled in, to start a fresh run of the crew as it is now. For a work order's attempt, use **Re-run** on the work order instead.

## Deleting a run
<!-- permissions: runs.delete -->
Finished runs have a **Delete** button on their row in the Runs list. Click it, then click **Confirm?** within a few seconds. This permanently removes the run with its events, outputs and files. A run that is still queued or running must be cancelled first.

If the run was an attempt of a work order, the work order stays and its other attempts are kept.
