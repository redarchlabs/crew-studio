---
title: LLMs
category: Configure
order: 10
permissions: llms.view
summary: What an LLM configuration is, what each of its settings does, and how to add or change one.
keywords: model, language model, openai, anthropic, claude, gpt, temperature, max tokens, reasoning effort, responses api, vision, litellm
---
An LLM in Crew Studio is a named model configuration, such as "GPT-6 Luna (research)". [Agents](/help/agents) pick one to think with, and hierarchical [crews](/help/crews) pick one for their manager. LLMs are shared by everyone; they are not tied to functional areas.

Open [LLMs](/llms) from the **Configure** section of the sidebar.

![The LLMs list with each configuration's name, model id, reasoning effort, temperature, max tokens and price entry.](llms.png)

## Reading the list

| Column | What it shows |
|---|---|
| Name | The name people pick on agents and crews. |
| Model | The provider and model id, such as `openai/gpt-6-luna`. |
| Effort | The reasoning effort, or "default". A small **responses** tag means the configuration uses OpenAI's Responses API. |
| Temp | The temperature, or "default". |
| Max tokens | The most tokens a single reply may use, or "default". |

If you can only view LLMs, a note at the top says the page is read-only. If you can view history, the **History** link at the top shows every change made to LLMs.

## What each setting means

- **Name**: must be unique. Choose something people will recognise when picking a model for an agent.
- **Model**: a LiteLLM-style id in the form `provider/model`, for example `openai/gpt-6-luna` or `anthropic/claude-opus-5-5`. The provider part decides which service is called.
- **Temperature**: from 0 to 2. Lower values give more predictable answers. Leave it empty to use the provider's default. Claude 5.x models ignore it.
- **Max completion tokens**: a whole number that caps the length of each reply. Leave it empty for the provider's default.
- **Reasoning effort**: for reasoning models only. Choose **none**, **minimal**, **low**, **medium**, **high**, **xhigh** or **max**, or **Model default**. For Claude it sets the effort; for OpenAI it sets `reasoning_effort`. Higher effort usually means better answers, more tokens and higher cost.
- **API**: **Chat Completions** suits most models. **Responses (OpenAI reasoning models)** is only for `openai/` models; `gpt-6-astra` and `gpt-6.1-sol` need it to call tools.
- **Vision: accepts images and PDFs**: turn this on for models that can read images. Screenshots and PDFs attached to a work order then go straight to agents using this model. Agents on other models get the file names instead.

## Price entry badge
<!-- permissions: pricing.view -->
The **Price entry** column shows which row on the [Pricing](/help/pricing) page each LLM's costs come from, with its input and output price per 1M tokens. Hover over it to see whether it is an exact match or was matched after removing the provider prefix (so `openai/gpt-6-luna` can use a `gpt-6-luna` price).

An amber **No price entry** warning means the model has no price. Runs that use it cannot be costed or kept within budget, and crews that use it will not start until a price is added. See [why every model needs a price](/help/pricing#why-every-model-needs-a-price).

## Add an LLM
<!-- permissions: llms.manage -->
1. On [LLMs](/llms), click **New LLM**.
2. Enter a **Name** and the **Model** id.
3. Check the line under **Model**. If you can view prices, it shows the matching price entry or warns **No price entry**, with an **add one** link to the Pricing page.
4. Set **Temperature**, **Max completion tokens**, **Reasoning effort**, **API** and **Vision** as needed. Leave the number fields empty to use the provider's defaults.
5. Click **Create**.

The form checks that temperature is between 0 and 2, that max tokens is a positive whole number, and that the Responses API is used only with `openai/` models.

## Edit or delete an LLM
<!-- permissions: llms.manage -->
To edit, click the LLM's name or its pencil icon. Change the fields in the **Edit** window and click **Save**. Agents and crews using the LLM pick up the change on their next run.

To delete, click **Delete** in the LLM's row, then **Confirm?** within a few seconds. You cannot delete an LLM that an agent or a crew manager still uses; Crew Studio tells you which ones, so you can move them to another LLM first.
