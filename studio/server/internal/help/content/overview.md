---
title: Overview
category: Console
order: 10
permissions: overview.view
summary: What the Overview dashboard shows: 30-day spend, counts of what is set up, and the latest work orders and runs.
keywords: dashboard, home, start page, overview, spend, cost, last 30 days, counts, tiles, recent work orders, recent runs, summary, status
---
The Overview is the first screen you see after signing in. It answers three questions at a glance: what is set up, what ran recently, and what it cost. Open it at any time with **Overview** at the top of the sidebar.

![The Overview with the 30-day spend tile, count tiles, and recent work orders and runs.](overview.png)

## Spend over the last 30 days

The highlighted **Spend · 30 days** tile shows the total estimated cost of runs started in the last 30 days, in US dollars. It counts only runs in your functional areas, so two people can see different totals.

Costs are estimates worked out from your organisation's price list for each model, not figures from the AI providers' invoices.

## Count tiles

Next to the spend tile is a row of tiles, each showing how many of something exist:

| Tile | Counts |
|---|---|
| **Work orders** | Work orders in your areas |
| **Runs** | Runs in your areas |
| **Schedules** | Schedules for crews in your areas |
| **Crews**, **Agents**, **Tasks** | Items in your areas |
| **LLMs** | Language models set up for everyone |
| **Prices** | Model price entries set up for everyone |

You only see a tile when you can open the screen it links to. Click a tile to go to that list.

Because most counts are limited to your [functional areas](/help/access-and-areas#functional-areas), a number lower than you expect usually means some items belong to areas you are not in.

## Recent work orders

The **Recent work orders** panel lists the ten newest work orders you can see, with:

- **Work order**: the number (such as WO-42) and title. Click it to open the work order.
- **Status**: draft, queued, running, succeeded, failed or cancelled.
- **Target**: the crew it runs.
- **Cost** and **Updated**.

Click **All work orders →** to open the full list. See [Work orders](/help/work-orders) for what you can do there.

## Recent runs

The **Recent runs** panel lists the ten newest runs you can see, with the crew, status, how the run was started (for example Manual, Schedule or Work order), who started it, cost, budget, duration and when it started. Click a crew name to open that run, or **All runs →** for the full list. See [Runs](/help/runs).

You see each panel only if you can open the matching list.

## Keeping up to date

While any recent run or work order is queued or running, the Overview refreshes itself every few seconds, so statuses, durations and the spend total stay current. Once everything has finished it stops refreshing; reload the page to check again later.

## Run a crew from the Overview
<!-- permissions: crews.run -->
If you can run crews and can also see the crews list, a **Run a crew** button appears at the top right of the Overview.

![The Run a crew window with a crew chosen, its inputs and a budget override.](run-crew-modal.png)

1. Click **Run a crew**.
2. Pick a crew from the **Crew** list. Only crews in your areas are listed.
3. Fill in the crew's inputs. Required ones are marked with a red asterisk.
4. Optionally enter a **Budget override (USD)**. Leave it empty to use the crew's own budget.
5. Click **Start run**. The new run opens so you can watch it live.

This starts a plain run with inputs only. A [work order](/help/work-orders) can also carry a written brief and files.
