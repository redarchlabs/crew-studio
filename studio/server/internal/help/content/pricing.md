---
title: Pricing
category: Configure
order: 20
permissions: pricing.view
summary: How model prices per 1M tokens drive cost estimates and budget stops, and why every model needs one.
keywords: price, cost, tokens, per million, input, cached, cache write, prompt caching, output, budget, spend, model pricing
---
The [Pricing](/pricing) page, titled **Model pricing**, holds what each model costs in US dollars per 1 million tokens. Crew Studio uses these prices to estimate the cost of every LLM call, to show costs on runs and work orders, and to stop runs that reach their budget. Prices are shared by everyone; they are not tied to functional areas.

Open it from the **Configure** section of the sidebar.

![The Model pricing table with input, cached input and output prices per 1M tokens, notes and the LLMs using each price.](pricing.png)

## Reading the table

| Column | What it shows |
|---|---|
| Model | The model id the price applies to, such as `gpt-6-luna` or `gemini-2.5-flash`. |
| Input / 1M | Dollars per 1 million tokens sent to the model (prompts, context and tool results). |
| Cached in / 1M | Dollars per 1 million input tokens the provider served from its prompt cache, usually cheaper than normal input. |
| Cache write / 1M | Dollars per 1 million input tokens written to the provider's prompt cache. Anthropic charges 1.25 times the input price for this, and Claude models come set that way. "input" means the normal input price applies (OpenAI and Gemini do not charge extra). |
| Output / 1M | Dollars per 1 million tokens the model writes. |
| Notes | Free text, often the provider's pricing page. A web address becomes a link. |
| Used by | How many LLM configurations use this price; hover to see their names. "unused" means none do. Shown only if you can view LLMs. |

If you can only view prices, a note at the top says the page is read-only.

## Why every model needs a price

Costs in Crew Studio are calculated from this table, not taken from the provider's bill. If a model has no price:

- its calls would count as $0, so cost figures would be wrong;
- a run's budget could never be reached, so it could not stop runaway spend.

To prevent that, a crew will not start if any of its agents, or a hierarchical crew's manager, uses an LLM whose model has no price. The run is refused with a message such as "there is no price for model … add one on the Pricing page". The same applies to the search model in [Settings](/help/settings) when an agent uses the Google Search tool.

On the [LLMs](/help/llms) page, a model without a price shows an amber **No price entry** warning (if you can view prices).

Costs are estimates. Keep spending limits set in your providers' own consoles as well.

## How prices are matched to LLMs

Crew Studio looks for a price in two steps:

1. A price whose model is exactly the LLM's model id, for example `openai/gpt-6-luna`.
2. If there is none, a price for the id without its provider prefix, for example `gpt-6-luna`.

So one price row for `gpt-6-luna` covers any LLM configuration using `openai/gpt-6-luna`, whatever its temperature or reasoning effort. Add an exact `provider/model` row only when the same model needs a different price from one provider.

## Add or change a price
<!-- permissions: pricing.manage -->
1. Click **Add price**. A new row opens at the top of the table.
2. Enter the **Model** id, then the input, cached input and output prices per 1M tokens. Prices must be 0 or more. Leave **Cache write** blank unless the provider charges extra to write to its prompt cache.
3. Optionally add **Notes**, such as the provider's pricing page address.
4. Click the tick to save, or press **Enter**. Click the cross or press **Escape** to cancel.

To change a price, click its pencil icon or double-click the row, edit the values and save the same way. New prices apply to runs that start after you save; costs already recorded do not change.

Each model id can have only one price row.

## Delete a price
<!-- permissions: pricing.manage -->
Click **Delete** in the row, then **Confirm?** within a few seconds. Check the **Used by** column first: any LLM that relied on the price is left without one, and crews using it will not start until you add a price again.
