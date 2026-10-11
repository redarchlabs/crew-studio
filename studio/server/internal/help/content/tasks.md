---
title: Tasks
category: Build
order: 30
permissions: tasks.view
summary: What a task is, what each task setting means, how placeholders become run inputs, how to run a single task by making a one-task crew, and how to create, change and delete tasks.
keywords: task, description, expected output, agent, context, context tasks, markdown, placeholders, inputs, order, make a crew, one-task crew, run a task, single task, crew draft
---
A task is one piece of work: what to do, what a good result looks like, and which [agent](/help/agents) does it. A [crew](/help/crews) runs a list of tasks in order. A task can also read the results of earlier tasks, so later work builds on earlier work. Tasks always run as part of a crew; to run a single task, make a one-task crew from it (see [Run a single task](/help/tasks#run-a-single-task)).

The **Tasks** page lists every task in your functional areas. Each row shows the task's name and the start of its description, its **Areas**, its **Agent** ("Unassigned" if none, "Not visible" if outside what you can see), its **Context** tasks, the **Inputs** (placeholders) it uses, and the **Crews** that include it. Type in **Filter tasks…** to search by name or description, and use the area menu (it starts at **All areas**) to show one functional area.

![The Tasks page, listing each task with its agent, context tasks, inputs and crews.](tasks.png)

## The task page

A task's page has a **Task** panel and an **Execution** panel on the left, and a **Used by** panel on the right. If you can only view tasks, the title has a **read-only** label and the fields are locked.

![A task's page, with the Task and Execution panels and the Used by panel.](task-editor.png)

### Task panel

- **Name**: a short label for the task. Task names must be unique.
- **Functional areas**: who can see the task. See [Access and areas](/help/access-and-areas).
- **Description**: the instructions for the agent: what to do, what to look at and anything to avoid. Required.
- **Expected output**: what the finished result should be, for example "A markdown report with an overview, risks and a recommendation". Being specific here gives more consistent results. Required.

### Execution panel

- **Agent**: the agent who does the task. A sequential crew needs an agent on every task. **Unassigned (manager decides in hierarchical crews)** suits only tasks in hierarchical crews, where the crew's manager picks an agent. If the agent is outside your functional areas, it shows as "Assigned agent (not visible to you)" and is kept as it is. The **Open agent** link opens the chosen agent.
- **Context tasks (their outputs are passed to this task)**: other tasks whose results this task receives as background. For example, a "write report" task can take the results of a "research company" task. A task cannot be its own context.
- **Markdown output**: asks the agent to format its answer as Markdown (headings, lists, tables). It is on for new tasks.

### Used by

This panel lists the crews that include the task, with links to each.

## Placeholders become run inputs

Description and expected output can contain placeholders in curly braces, such as `{company}` or `{location}`. When a crew runs, each placeholder is replaced with the value given for the matching run input. This lets one task work on a different subject each time.

Use lowercase letters, digits and `_` in placeholder names, starting with a letter. Placeholders are highlighted as you type, and the keys found are listed under the field. If the task is already in a crew, a dashed warning tag marks a key that none of its crews has an input for yet.

Each placeholder needs a matching input on every crew that uses the task, or the crew will not run. The crew page's **Validation** panel lists any that are missing. See [The crew page](/help/crews#the-crew-page).

## Context and order

Context only works in one direction: a task can read results from tasks that run before it in the same crew. In each crew that uses the task:

- every context task must also be in that crew;
- every context task must come earlier in the crew's task order.

The crew page flags both problems, in its graph and in its Validation panel, and a crew will not run until they are fixed.

## Run a single task

[Work orders](/help/work-orders), [schedules](/help/schedules) and [triggers](/help/triggers) always run a crew. To run one task on its own, it needs a crew of its own: a one-task crew. Such a crew holds the task, any tasks it takes context from (earlier ones first, so their results are ready), and the agents of those tasks. Once it is saved, pick it like any other crew.

If you cannot create crews, ask someone who can to make one for you.

When Crew Studio was upgraded to crew-only targets, every task that a work order, schedule or trigger used to run became a one-task crew automatically, and those work orders, schedules and triggers now run that crew. Each such crew is named after its task and described as "Runs the task …", and its creation shows in [History](/help/history) as made by "Crew Studio upgrade". Earlier attempts of a work order keep exactly what they ran with.

## Make a crew from a task
<!-- permissions: crews.create -->
On a task's page, click **Make a crew**. The crew builder opens with a draft crew that is not saved yet:

- the task, plus every task it takes context from, earlier tasks first;
- the agents of those tasks;
- a run input for each `{placeholder}` they use (a crew that already includes the task lends its labels and defaults);
- the task's name as the crew name, or "<name> (task)" if a crew already has that name;
- the default budget from [Settings](/help/settings) and the task's functional areas that you belong to.

Review it as you would any crew, change anything you like, and click **Create**. See [Create and edit crews](/help/crews#create-and-edit-crews).

If the crew cannot be made, a message says why. This happens when the task takes context from a task, or uses an agent, outside your functional areas, or when its context tasks form a loop.

## Create and edit tasks
<!-- permissions: tasks.create, tasks.edit -->
To create a task, click **New task** on the Tasks page (or **Create a task** if there are none yet). To change one, open it from the list.

1. Enter a **Name** and choose at least one functional area. You can only add or remove areas you belong to; areas you are not in stay checked and locked. If you belong to exactly one area, a new task starts in it.
2. Write the **Description** and **Expected output**. Add `{placeholders}` where a value should come from the crew's run inputs.
3. Choose the **Agent**.
4. Tick any **Context tasks** whose results this task needs. The list can be filtered when there are many tasks.
5. Turn **Markdown output** on or off.
6. Click **Create** (for a new task) or **Save**.

While you have unsaved changes, the title shows an **unsaved** tag, and Studio asks before you leave the page.

Changes apply to every crew that uses the task, from their next run. If you add a placeholder, each crew that uses the task needs a matching run input; see [Crews](/help/crews). If you add a context task, make sure it comes earlier in those crews.

## Delete a task
<!-- permissions: tasks.delete -->
Open the task and click **Delete** in the header, then **Confirm?** within a few seconds. A task cannot be deleted while a crew or another task (as context) still uses it; the error names each one. Remove the task from those first. If it is in a one-task crew, delete that crew first (see [Delete a crew](/help/crews#delete-a-crew)).

## See past versions
<!-- permissions: history.view -->
The **History** link on the Tasks page lists every change made to tasks, and the one on a task's page lists that task's changes, with a full copy of each version. See [History](/help/history).
