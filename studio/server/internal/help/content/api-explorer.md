---
title: API explorer
category: Developers
order: 10
summary: How to browse and try the Crew Studio API, and how responses, retries and callbacks work.
keywords: api, rest, swagger, openapi, endpoint, integration, developer, try it out, authorize, bearer, envelope, idempotency, callback, webhook, service account key, crew_id, task_id, crew-draft, one-task crew
---
The [API explorer](/api-docs) lists every endpoint other systems can call, grouped by feature (Work orders, Runs, Crews, Schedules, Triggers, Configuration and so on), and lets you send real requests from the page. It reads the API's own description at `/api/openapi.yaml`, so it always matches the running version.

Open it from the **Developers** section of the sidebar. Everyone who is signed in can use it.

![The API explorer showing endpoint groups, with the notes about who requests run as and that calls are real.](api-explorer.png)

## Try an endpoint

1. Click a group to see its endpoints, then click an endpoint to open it. You see its parameters, the request body it accepts and the responses it can return.
2. Click **Try it out**.
3. Fill in the parameters and edit the example body.
4. Click **Execute**. The explorer shows the request it sent, the status code, the response body and how long it took.

These are real calls. Starting a run or submitting a work order runs agents and spends money against the budget, and creating or deleting something really creates or deletes it.

## Who the requests run as

By default, requests run **as you**, using your signed-in session. They have exactly your permissions and functional areas, so an endpoint you cannot use in the app answers `403` here too, and items outside your areas answer `404`.

To test the way an integration will see the API, click **Authorize** and paste:

- a service account key (`csk_…`), to act as that service account; or
- a trigger token (`cst_…`), for the `/hooks` endpoint that triggers use (see [Triggers](/help/triggers)).

Keys you paste are kept only for this page visit and are forgotten when you leave or reload. Service account keys are created by an administrator; see [Service accounts](/help/admin-service-accounts).

## The response envelope

Every JSON response has the same shape:

```json
{ "success": true, "data": { … }, "error": null, "meta": { "total": 12, "limit": 50, "offset": 0 } }
```

- `data` holds the result. `meta` appears only on lists that are split into pages; pass `limit` and `offset` to move through them.
- On failure, `success` is `false`, `data` is `null`, and `error` has a `code`, a readable `message` and a list of `details`, such as `"inputs.company: is required"` or `"needs: crews.edit"`.

Common status codes: `200` OK, `201` created, `202` accepted (webhooks), `204` deleted, `400` bad JSON, `401` not signed in, `403` missing a permission, `404` not found or outside your areas, `409` conflict (for example a name already taken or something still in use), `422` validation failed, `429` too many requests.

Work orders, schedules and triggers always run a crew, given by its `crew_id`. A request that sends a `task_id` (or a `target_type` other than `crew`) is refused with `422`, and the message explains what to do instead: make a one-task crew and use its `crew_id`. `GET /tasks/{id}/crew-draft` returns such a crew, unsaved and ready to send to `POST /crews`.

When you get `429`, wait for the number of seconds in the `Retry-After` header before trying again. Starting runs and submitting work orders is limited to about 20 a minute for each person or service account.

## Safe retries with Idempotency-Key

Network errors can leave a caller unsure whether a request worked. To retry safely, send an `Idempotency-Key` header with a value unique to that piece of work (up to 200 characters), such as your own order or event id.

If Crew Studio has already seen the key, it returns the first result instead of starting more work. This works when:

- creating a work order with `submit: true` (`POST /work-orders`);
- submitting a work order again (`POST /work-orders/{id}/submit`);
- starting a crew run (`POST /crews/{id}/runs`);
- calling a trigger (`POST /hooks/{id}`).

## Callbacks

Instead of checking a run's status over and over, a caller can ask Crew Studio to POST the result to a `callback_url` when the run finishes. Only requests made with a **service account key** may pass `callback_url`; a request made as yourself is refused with `422`. Triggers have their own callback URL setting.

The callback body has an `event` of `run.finished`, the run (status, output, error, cost, usage and timings) and, when there is one, the work order's id, number and title.

Each callback is signed. The `X-CrewStudio-Signature` header holds `sha256=` followed by an HMAC-SHA256 of `<timestamp>.<body>`, using the service account's or trigger's signing secret, and `X-CrewStudio-Timestamp` holds the time in Unix seconds. The receiver should recompute the signature over the raw body, compare it in constant time, and reject timestamps more than 5 minutes old.

Any `2xx` answer within 10 seconds counts as delivered. Otherwise Crew Studio retries after 30 seconds, 2 minutes, 10 minutes, 30 minutes and 2 hours, then gives up. Callback URLs must use `https` and reach a public address, unless an administrator has allowed the host.

## Personal API keys
<!-- permissions: account.api_keys -->
To call the API from your own scripts and tools as **you**, create a personal key on [My API keys](/account/api-keys) (**Developers > My API keys**). Paste it under **Authorize** in the explorer to try requests with it. See [My API keys](/help/account-api-keys) for how keys work.
