---
title: Triggers
category: Automate
order: 20
permissions: triggers.view
summary: How triggers give another system a webhook URL that starts a crew, and how that system calls it.
keywords: webhook, hook, integration, token, payload, callback, signing secret, input map, POST, curl, crew, target, single task, one-task crew, make a crew
---
A trigger gives another system, such as a ticketing tool or an ERP, a URL it can POST JSON to. Each call creates and submits a [work order](/help/work-orders) for the trigger's crew. The trigger can pick input values out of the JSON, add the JSON to the brief, and call the other system back when the run finishes.

Open [Triggers](/triggers) from the **Automate** section of the sidebar to see every trigger in your functional areas.

![The Triggers list showing each trigger's target, status, last fired time, fire count, callback and token prefix.](triggers.png)

## Reading the triggers list

Each row shows the trigger's name and areas, its **Target**, whether it is enabled, when it was **Last fired** and how many times, whether a **Callback** is set, and the start of its **Token** (such as `cst_ab12cd_…`), which identifies the token without revealing it.

Click a trigger to open it. If you can only view triggers, the page says **Read only**. The **Endpoint** panel shows the URL to call, and **Recent work orders** lists what the trigger has created.

## How a call becomes a work order

When another system calls the trigger:

1. Crew Studio checks the token. A wrong or missing token gets `401`.
2. A disabled trigger answers `409` and creates nothing.
3. Input values are read from the JSON body using the trigger's input map, which points each input at a dotted path such as `customer.name` or `items.0.sku`. Objects and lists become JSON text.
4. The brief is the trigger's own brief, followed by the whole JSON payload if **Include the payload** is on.
5. The work order is created and submitted as the person who last saved the trigger, with their permissions and areas. Its title is the trigger name plus the date and time in UTC.

The caller gets `202 Accepted` with the new work order's id, number, run id, status and a status URL. If a required input ends up empty, or the crew cannot run (for example an LLM has no price), the answer is `422` with the reasons.

Limits: the body may be up to 1 MB of JSON, each trigger accepts about 60 calls a minute, and at most 5 of a trigger's work orders can be queued or running at once. Going over a limit returns `429`.

## Calling the webhook

The other system sends a `POST` to the URL in the **Endpoint** panel, with the trigger's token in an `Authorization: Bearer` header (or an `X-CrewStudio-Token` header) and a JSON body. The token is shown only once, when the trigger is set up, alongside a ready-made example like this:

```bash
curl -X POST 'https://studio.example.com/api/hooks/3f6c2a1e-0000-4000-8000-000000000000' \
  -H 'Authorization: Bearer cst_ab12cd_<secret>' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: event-1a2b3c4d' \
  --data '{"customer":{"name":"<company>"}}'
```

The sample body fills every mapped input; here `company` is mapped to `customer.name`.

Send an `Idempotency-Key` header with a value that is unique to the event, such as the other system's event id. If the caller retries with the same key, Crew Studio returns the first work order instead of creating a second one.

## Create a trigger
<!-- permissions: triggers.manage -->
Saving a trigger also needs permission to create work orders, and the crew must be in one of your functional areas. To start a single task, first make a one-task crew from it (see [Run a single task](/help/tasks#run-a-single-task)).

1. On [Triggers](/triggers), click **New trigger**.
2. Enter a **Name** and optional **Description**, then pick a crew under **Target**.
3. Leave **Enabled** on, or turn it off until the caller is ready.
4. Set up **Payload to inputs** (see below).
5. Write the **Brief**. It is placed before the payload in each work order's brief. Leave **Include the payload** on so agents can read the whole JSON body.
6. Optionally set **Budget (USD)** and a **Callback URL**.
7. Click **Create**.

The **Trigger token** window opens with the **Token**, **Hook URL**, a **curl** example and a **Sample JSON payload**, each with a copy button. Copy the token now: it is shown only once. Click **I have saved the token** to close it.

![A trigger's page with the Trigger, Payload to inputs, Brief, and Budget and callback panels, and the Endpoint panel on the right.](trigger-editor.png)

To change a trigger later, edit its fields and click **Save**. Saving makes you the run-as person, and the **Runs as** panel warns you when that will change.

## Map the payload to inputs
<!-- permissions: triggers.manage -->
Each row in **Payload to inputs** maps an input key (left) to a dotted path in the JSON body (right).

- **Add mapping** adds a row, suggesting the next unmapped input of the crew.
- **Map all N target inputs** adds a row for every unmapped input of the crew, using the input key as the path.
- Input keys use lowercase letters, digits and underscores. Paths are names separated by dots; use a number for a list position, as in `items.0.sku`.
- With no mapping, inputs use their defaults and the payload is only added to the brief.

If a required input is left unmapped, the page warns you, and calls answer `422` when that input ends up empty.

## Rotate the token
<!-- permissions: triggers.manage -->
If a token is lost or exposed, click **Rotate token** in the **Endpoint** panel, then **Confirm?**. The old token stops working at once, and the **Trigger token** window shows the new one. Rotating also replaces the callback signing secret, so update both in the calling system.

## Callbacks and the signing secret
<!-- permissions: triggers.manage -->
If you set a **Callback URL**, Crew Studio POSTs the result to it when each run finishes. The URL must be `https` and reach a public address, unless an administrator has allowed the host in the sign-in settings.

Each callback is signed with the trigger's signing secret. The **Callback signing secret** panel lets you **Reveal** and copy the secret, and shows ready-to-use verification code for **Node.js** and **Python**. The receiving system should check the signature and reject timestamps older than 5 minutes. See [Callbacks](/help/api-explorer#callbacks) for the body, the headers and the retry timetable.

## Turn off or delete a trigger
<!-- permissions: triggers.manage -->
To stop calls without losing the setup, turn **Enabled** off and click **Save**; callers then get `409`. To remove the trigger, click **Delete**, then **Confirm?**. Its URL and token stop working, and work orders it already created are kept.
