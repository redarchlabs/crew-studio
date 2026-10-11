---
title: Crews
category: Build
order: 10
permissions: crews.view
summary: How crews bring agents and tasks together, how to read the crew page, and how to build, delete and run a crew.
keywords: crew, crew builder, process, sequential, hierarchical, manager llm, budget, run inputs, placeholders, validation, graph, task order, pipeline, make a crew, one-task crew, single task, crew draft
---
A crew is a team of [agents](/help/agents) working through a list of [tasks](/help/tasks). When a crew runs, its tasks are carried out in the order you set, each by its assigned agent. Crews are also what [work orders](/help/work-orders), [schedules](/help/schedules) and [triggers](/help/triggers) run; to run a single task, make a one-task crew from it (see [Run a single task](/help/tasks#run-a-single-task)).

The **Crews** page lists every crew in your functional areas. Each row shows the crew's name and description, its **Areas**, its **Process**, its **Pipeline** (the number of agents, then the tasks in order), the **Inputs** it asks for, its **Budget** per run in US dollars, and when it was last **Updated**. Type in **Filter crews…** to search by name or description, and use the area menu (it starts at **All areas**) to show one functional area. Click a crew's name to open it.

![The Crews page, listing each crew with its areas, process, pipeline of tasks, inputs and budget.](crews.png)

## The crew page

The left side holds the crew's settings in three panels: **Crew**, **Work** and **Run inputs**. The right side shows the **Crew graph** and the **Validation** panel. If you can only view crews, the title has a **read-only** label and the fields are locked.

![A crew's page, with the settings on the left and the crew graph and validation panel on the right.](crew-builder.png)

### Crew panel

- **Name** (unique) and **Description**.
- **Functional areas**: who can see the crew. See [Access and areas](/help/access-and-areas).
- **Process**: **Sequential** runs the tasks one after another in the listed order, and every task needs an agent. **Hierarchical** adds a manager that plans the work and hands tasks to the crew's agents; it needs a **Manager LLM**, and its tasks may be left without an agent so the manager decides.
- **Budget per run (USD)**: the default spending limit for each run, more than 0 and at most 1,000. See [Runs](/help/runs) for how the limit is enforced.

### Work panel

**Tasks (execution order)** lists the tasks, numbered in the order they run, each with its agent. A "not in crew" warning means the task's agent is missing from **Agents**, the list of agents that belong to the crew.

### Run inputs

Agents and tasks can contain placeholders in curly braces, such as `{company}`. When the crew runs, each one is replaced with a value given by the person starting the run. Each input has:

- **Key**: the placeholder name without braces. It starts with a lowercase letter and uses only lowercase letters, digits and `_`. Each key appears once.
- **Label**: the name shown on the run form (the key, if left empty).
- **Default**: the value the run form starts with.
- **Req.**: whether a value must be given.

Inputs that no task or agent in the crew uses are listed under **Unused keys**.

### Crew graph

The graph is a live picture of the crew, including unsaved changes: its tasks in order, the agent on each task, context links between tasks, and each agent's tools and LLM. The legend explains the line styles. Items with a problem carry a warning. Click a task or agent to open it, an LLM to open the [LLMs](/llms) page, or a tool to see what it does and what it is missing.

### Validation panel

The **Validation** panel lists everything that would stop the saved crew from running, or shows **Ready to run**. Click the refresh button (**Re-validate**) to check again after fixing something elsewhere. It reports, for example:

- no tasks, or no agents;
- a hierarchical crew without a manager LLM;
- an agent using the "CrewAI default" model instead of a chosen LLM, so the cost cannot be tracked;
- an LLM, or the Google Search model, with no price on the [Pricing](/help/pricing) page;
- an agent using a tool that is unknown or unavailable;
- a task with no agent in a sequential crew, or whose agent is not in the crew;
- a task that uses the output of a task that is not in the crew or comes later;
- a `{placeholder}` with no matching run input;
- a task, agent or LLM that no longer exists.

Under **Draft checks** it also warns about unsaved changes as you make them.

## Create and edit crews
<!-- permissions: crews.create, crews.edit -->
To create a crew, click **New crew** on the Crews page (or **Build a crew** if there are none yet). To change one, open it. To build a crew around one task, click **Make a crew** on the task's page instead: the builder opens already filled in, and you review it and click **Create** (see [Make a crew from a task](/help/tasks#make-a-crew-from-a-task)).

1. Enter a **Name**, an optional **Description**, and at least one functional area. You can only add or remove areas you belong to; others stay checked and locked. If you belong to exactly one area, a new crew starts in it.
2. Choose a **Process**. For **Hierarchical**, pick a **Manager LLM**.
3. Set **Budget per run (USD)**. A new crew starts at $1.
4. In **Work**, pick a task from **Choose a task to add…** and click **Add**. Adding a task also adds its agent to the crew.
5. Use the up and down arrows to reorder tasks, and the **X** to remove one. A task that uses another task's output must come after it.
6. Tick any extra **Agents**. If a task shows "not in crew — add", click it to add that agent.
7. In **Run inputs**, click **Detect from placeholders**. Studio adds a required input for each `{placeholder}` in the crew's tasks and agents that has none yet. Click **Add** to add one by hand, or the bin icon to remove one.
8. Click **Create** (new crew) or **Save**, then check the **Validation** panel.

While you have unsaved changes, the title shows an **unsaved** tag and Studio asks before you leave the page.

## Chat with a crew

A crew can also answer questions in [Chat](/help/chat): turn it on, and pick the agent that answers, in the crew's **Chat** panel on its Design tab.

## What a crew keeps between runs

A saved crew's page has a **State** tab with its notes, action log and connections (see [Crew state](/help/crew-state)), and a **Memory** tab with the lessons it has learned (see [Memory](/help/memory)).

## Delete a crew
<!-- permissions: crews.delete -->
Open the crew and click **Delete** in the header, then **Confirm?** within a few seconds. A crew still used by a [schedule](/help/schedules) or [trigger](/help/triggers) cannot be deleted; the error names them. Deleting a crew does not delete its agents or tasks. Work orders that ran it are kept and still show its name, but have no target until someone edits them and picks another crew.

## Run a crew
<!-- permissions: crews.run -->
Click **Run** at the end of the crew's row on the Crews page, or **Run** in the header of the crew's page. On the crew's page the button stays disabled until the crew is saved with no unsaved changes.

![The run dialog, with a field for each run input and an optional budget override.](run-crew-modal.png)

1. Fill in each input. Fields show the label and `{key}`, start with the default value, and required ones have a red asterisk.
2. Optionally enter a **Budget override (USD)** for this run only. Leave it empty to use the crew's budget, shown in the hint.
3. Click **Start run**, or **Cancel**.

If a required input is empty, or the crew is not ready (anything the Validation panel would list), the dialog shows the problems and the run does not start. Otherwise Studio opens the new run's page, where you can follow it live. See [Runs](/help/runs).

## See past versions
<!-- permissions: history.view -->
The **History** link on the Crews page lists every change made to crews, and the one on a crew's page lists that crew's changes, with a full copy of each version. See [History](/help/history).
