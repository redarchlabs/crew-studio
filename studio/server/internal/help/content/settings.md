---
title: Settings
category: Configure
order: 30
permissions: settings.view
summary: The global defaults for budgets, grounded search pricing and how many runs may execute at once.
keywords: default budget, make a crew, one-task crew, hard stop, multiplier, spend limit, search model, gemini, google search, grounding, free allowance, concurrency, concurrent runs, queue, compact, compaction, context, tokens
---
[Settings](/settings) holds a few global defaults that apply to every run in Crew Studio: budgets, the cost of Google Search grounding, and how many runs may execute at the same time. There is one set of settings for everyone; they are not tied to functional areas.

Open it from the **Configure** section of the sidebar. If you can only view settings, a note at the top says the page is read-only and the fields are locked.

![The Settings page with the Budgets, Grounded search and Execution panels.](settings.png)

## Budgets

Every run has a budget in US dollars. Crew Studio estimates the cost of each LLM call and search from the [Pricing](/help/pricing) table as the run goes.

- **Default budget (USD)**: used when a crew has no budget of its own, and as the starting budget of a crew made from a task with **Make a crew** (see [Make a crew from a task](/help/tasks#make-a-crew-from-a-task)). A work order, schedule or trigger can still set its own budget, which takes priority. Must be greater than 0.
- **Hard-stop multiplier**: sets the absolute limit as a multiple of the budget. Use a value from 1 to 5.

What happens as a run spends:

1. When it reaches its **budget**, searches are refused so the agents finish with what they already have.
2. When it reaches **budget × hard-stop multiplier**, every further LLM call is blocked and the run fails. Outputs from tasks that already finished are kept.

For example, with a $2.00 budget and a multiplier of 1.5, searches stop at $2.00 and all LLM calls stop at $3.00.

## Grounded search

Agents with the Google Search tool use Gemini's grounding with Google Search. These settings describe that service and its cost.

- **Search model**: the Gemini model behind the `google_search` tool, for example `gemini-3.8-flash`. It needs its own price on the Pricing page, or crews with search-enabled agents will not start.
- **Cost per 1,000 search queries (USD)**: what Google charges for grounded queries beyond the free allowance.
- **Free search queries**: how many grounded queries are free each period. The allowance is shared across all runs, so once it is used up, every run pays for its searches.
- **Free allowance resets**: **Monthly (Gemini 3)** or **Daily (Gemini 2.5)**, matching how your Gemini model's free tier works.

Check Google's current pricing for your search model and keep these values in step with it, so search costs and budget stops stay accurate.

## Execution

- **Max concurrent runs**: how many runs may execute at the same time, from 1 to 10. Runs beyond this wait in the queue with the status "queued" and start as others finish.

Raising it gets work done sooner but means more simultaneous calls to your model providers, which can hit their rate limits.

- **Compact an agent's context above (tokens)**: every LLM call of an agent resends everything it has done on its task so far, so long tasks get expensive. Above this size, the agent's steps so far are replaced by a summary its own model writes (the task and the latest step are kept), and later calls resend far less. 0, the default, never does this by itself; otherwise 10,000 to 1,000,000. 60,000 is a reasonable start. Summaries can drop detail an agent needed, so watch the journal after turning it on. Applies to runs that start after you save. You can also compact a running attempt by hand from its journal.

## Change the settings
<!-- permissions: settings.manage -->
1. Change the values you need. Crew Studio checks each one: the default budget must be above 0, the multiplier from 1 to 5, the free queries a whole number of 0 or more, concurrent runs a whole number from 1 to 10, and the compaction limit 0 or a whole number from 10,000 to 1,000,000. **Search model** cannot be empty.
2. Click **Save settings**. The button is available only when something has changed.
3. To undo your edits before saving, click **Discard changes**.

If you try to leave the page with unsaved changes, Crew Studio asks you to confirm first. Every save is recorded in [History](/help/history) with who made it.

A new concurrency limit is used the next time Crew Studio starts a queued run. Budget and search settings are used by runs that start after you save.
