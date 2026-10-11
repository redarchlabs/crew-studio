---
title: Schedules
category: Automate
order: 10
permissions: schedules.view
summary: How schedules create work orders for a crew at set times, and who they run as.
keywords: cron, timer, recurring, repeat, automation, time zone, run as, paused, next run, overlap, crew, target, single task, one-task crew, make a crew
---
A schedule creates and submits a [work order](/help/work-orders) for a crew at the times you choose, for example every weekday at 09:00. Each work order gets the schedule's brief, inputs and budget, and its title is the schedule name followed by the date and time it fired.

Open [Schedules](/schedules) from the **Automate** section of the sidebar to see every schedule in your functional areas.

![The Schedules list showing each schedule's target, timing, next and last run, run-as person and enabled state.](schedules.png)

## Reading the schedules list

Each row shows:

| Column | What it tells you |
|---|---|
| Schedule | The name and description, the functional areas it belongs to, and any recent problem in amber. |
| Target | The crew it runs. |
| When | The timing in words (such as "Weekdays at 09:00"), with the cron expression and time zone underneath. |
| Next run | How soon it fires next, and the exact time in its own time zone. Paused schedules show "paused". |
| Last run | When it last fired, with a **Last work order** link to the work order it created. |
| Run as | The person whose permissions the work orders use. |
| Enabled | Whether the schedule is on or paused. |

Click a schedule's name to open it. If you can only view schedules, the page says **Read only** and every field is locked. The **Recent work orders** panel at the bottom lists what the schedule has created. The **History** link shows every change made to schedules (see [History](/help/history)).

## When a schedule fires

The timing is a five-field cron expression: minute, hour, day of month, month and weekday. For example, `0 9 * * 1-5` means 09:00 Monday to Friday. The shortcuts `@hourly`, `@daily`, `@weekly` and `@monthly` also work.

- **Time zone**: the expression is read in the schedule's own time zone, so "09:00 Europe/London" stays at 09:00 local time through daylight-saving changes.
- **Minimum interval**: a schedule may not fire more often than every 5 minutes.
- **Missed runs**: if Crew Studio was stopped when a schedule was due, the schedule fires once when the service comes back, not once for every time it missed.
- **Overlap**: each schedule says what happens if its previous work order is still running. **Skip** does not start a new one and records that the run was skipped. **Allow** starts a new work order anyway.

## Only run when something changed

Give a schedule a [watch](/help/watches) to check a source first: an API, a web page or a file. It then starts a work order only when something changed or a [follow-up](/help/follow-up-ladders) is due, and passes the changes to the crew. Checks that start nothing are listed in the watch's **Recent checks**.

## Who a schedule runs as

A schedule acts as the person who last saved it. Its work orders use that person's permissions and functional areas, and the **Run as** column and panel show who that is.

If that person loses access, the schedule pauses itself and records why. This happens when the person is disabled or deleted, loses a permission the work order needs, or no longer shares an area with the crew. The reason appears as **Last problem** at the top of the schedule and in amber in the list, starting with "Paused:". Temporary problems, such as a brief database error, are recorded without pausing the schedule.

## Create or edit a schedule
<!-- permissions: schedules.manage -->
To save a schedule you also need permission to create work orders, and the crew must be in one of your functional areas. To schedule a single task, first make a one-task crew from it (see [Run a single task](/help/tasks#run-a-single-task)).

1. On [Schedules](/schedules), click **New schedule**.
2. In the **Schedule** panel, enter a **Name** and optional **Description**.
3. Under **Target**, pick a crew from the searchable list.
4. In the **When** panel, pick a **Repeats** preset (such as **Weekdays at 09:00**) or **Custom…**, and choose a **Time zone**. You can also type straight into **Cron expression**.
5. Check the preview under the expression. It describes the timing in words and lists the **Next 5 runs**, also shown in your own time zone if it differs. If the expression is invalid or fires more often than every 5 minutes, the preview explains why.
6. Under **If the previous work order is still running**, choose **Skip** or **Allow**.
7. Write the **Brief**. It becomes each work order's brief.
8. Fill in the **Inputs** for the crew. Required inputs must have a value, because nobody is there to fill them in when the schedule fires.
9. Optionally set **Budget (USD)**. Leave it empty to use the crew's budget.
10. Click **Create**.

![A schedule's page with the Schedule, When, Brief, Inputs, Budget and Runs as panels.](schedule-editor.png)

To change a schedule, open it, edit the fields and click **Save**. Saving makes you the run-as person, and the **Runs as** panel warns you when that will change. Saving also clears the last problem. If you change only the brief, inputs or budget, the next run time stays where it was.

## Pause, run now or delete
<!-- permissions: schedules.manage -->
- **Pause or resume**: use the **Enabled** switch in the list, or the **Enabled** toggle on the schedule's page followed by **Save**. Paused schedules keep their settings but do not fire. Both ways save the schedule, so you become its run-as person.
- **Restart a schedule that paused itself**: read the **Last problem**, fix the cause (for example, add a price for an LLM the crew's agents use), turn **Enabled** on and click **Save**. It then runs as you.
- **Run now**: on a saved schedule, click **Run now** to create and submit one work order straight away. It runs as you, not as the run-as person, and opens the new work order. Save any changes first; the button is disabled while there are unsaved edits.
- **Delete**: click **Delete**, then **Confirm?** within a few seconds. Work orders the schedule already created are kept.
