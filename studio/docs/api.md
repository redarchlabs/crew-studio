# Crew Studio API

Base URL: `/api` (the Go service listens on `127.0.0.1:8088`; the Vite dev server proxies `/api` to it).
A machine-readable description of every endpoint is at `GET /api/openapi.yaml`, and the **API explorer** page
in the UI (`/api-docs`, under Developers) renders it with Swagger UI so you can try requests as yourself or with
a service account key. A test keeps the spec in step with the routes.

## Envelope

Every JSON response uses one envelope:

```json
{ "success": true, "data": <payload or null>, "error": null, "meta": { "total": 12, "limit": 50, "offset": 0 } }
```

- `meta` is present only on paginated lists.
- On failure `success` is `false`, `data` is `null`, and `error` is `{ "code": "validation_failed", "message": "...", "details": ["field: problem", ...] }`.
- Status codes: `200` OK, `201` created, `202` accepted (webhook), `204` deleted (no body), `400` bad JSON,
  `401` not signed in (`code: "unauthenticated"`), `403` signed in but missing a permission
  (`code: "forbidden"`, `details: ["needs: crews.edit"]`) or a foreign `Origin` (`code: "forbidden_origin"`),
  `404` not found **or outside your functional areas**, `409` conflict (`code: "conflict"`: name taken, still referenced,
  already running; `code: "last_admin"`: the change would leave nobody holding `admin.users`), `413` file too large, `415` wrong content type, `421` `Host` not allowed (DNS-rebinding guard),
  `422` validation failed, `429` rate limited (`Retry-After` header), `503` too many open run streams, `500`.

Mutating requests (`POST`, `PUT`) must send `Content-Type: application/json`, except file uploads, which are
`multipart/form-data` and must also send the header `X-Requested-With: crewstudio`.

IDs are UUID strings. Timestamps are RFC 3339 strings. JSON bodies may not contain NUL characters
(`\u0000`; `400`).

## Authentication

Every endpoint except `/api/auth/*`, `/api/hooks/*`, `/api/openapi.yaml` and `/healthz` needs a principal:

- **Browser session**: an `HttpOnly`, `SameSite=Lax` cookie `crewstudio_session` set by sign-in. Sessions last
  7 days and end after 12 hours without use.
- **API key**: `Authorization: Bearer csk_<prefix>_<secret>`. A key acts as one account: a service account (keys
  issued by an admin) or a person (personal keys they create themselves, see Account). Keys are shown once, stored
  only as a SHA-256 hash, can expire or be revoked, and each account holds at most 20 active keys. A person's keys
  stop working while they lack `account.api_keys`, and when their account is disabled. API keys can read `/admin`
  but not change it: admin writes and reading a signing secret need a signed-in session (`403` otherwise), so a
  stolen key cannot issue itself new credentials or accounts that outlive its revocation.

`GET /auth/status` (public)

```ts
type AuthStatus = {
  setup_required: boolean        // no people exist yet: show the first-run setup screen
  local_login: boolean           // password sign-in is enabled (administrators only)
  providers: { key: "clerk" | "microsoft"; label: string }[]   // configured SSO providers
}
```

`POST /auth/setup` (public, only while `setup_required`) body `{ token, email, name, password }`. The one-time
`token` is printed in the service log at start-up (`/setup?token=...`). Creates the first person as an
Administrator over all areas, signs them in, and returns `Me`.

`POST /auth/login` body `{ email, password }` returns `Me` and sets the cookie. Only people who hold
`admin.users` may sign in with a password. Wrong details return `401 invalid_credentials`; five failures lock the
account for 15 minutes (the correct password then gets `423 locked`, anything else still `401`, so a lock does not
reveal that an account exists; SSO keeps working); sign-in attempts are rate limited per IP.

`GET /auth/{provider}/login?return_to=/work-orders/123` redirects the browser to Clerk or Microsoft.
`GET /auth/{provider}/callback` finishes the sign-in and redirects to `return_to` (a path on this site), or to
`/login?error=<code>` where code is `not_invited`, `disabled`, `unverified_email`, `expired`, `provider_error`.

SSO accounts are matched to people by email the first time, then by the provider's account id. People must be
invited (created by an admin) first, unless "auto-provision" is on in the access settings. Because of that first
match, only trusted addresses are used: Clerk must mark the email verified; Microsoft accounts must belong to the
configured tenant, and their user principal name is used (the `email` claim only when Microsoft's optional
`xms_edov` claim says its domain is verified).

`POST /auth/logout` ends the session (`204`).

`GET /auth/me`

```ts
type Me = {
  id: string
  kind: "human" | "service"
  name: string
  email: string
  all_areas: boolean
  area_ids: string[]
  permissions: string[]          // see the catalog below
  via: "session" | "api_key"
}
```

## Permissions and functional areas

Permissions are a fixed catalog (`GET /admin/permissions` returns key, feature, label and description):

| Feature | Permissions |
|---|---|
| Overview | `overview.view` |
| Crews | `crews.view`, `crews.create`, `crews.edit`, `crews.delete`, `crews.run`, `crews.chat` (chat with crews and agents that have chat on and confirm what they propose; Administrator, Builder and Operator have it) |
| Crew state | `state.view` (notes, action log, memories and memory settings of crews in your areas), `state.manage` (edit, roll back and delete notes and log entries; add, edit, approve, delete and promote memories). Changing memory settings also needs `crews.edit` |
| Agents | `agents.view`, `agents.create`, `agents.edit`, `agents.delete` |
| Tasks | `tasks.view`, `tasks.create`, `tasks.edit`, `tasks.delete` |
| Work orders | `work_orders.view`, `work_orders.create`, `work_orders.cancel`, `work_orders.delete` |
| Runs | `runs.view`, `runs.cancel`, `runs.delete` |
| Schedules | `schedules.view`, `schedules.manage` |
| Triggers | `triggers.view`, `triggers.manage` |
| LLMs, Pricing, Settings | `llms.view`, `llms.manage`, `pricing.view`, `pricing.manage`, `settings.view`, `settings.manage` |
| Connections | `connections.view` (see connections in your areas, never their secrets), `connections.manage` (create, change, test, delete, replace secrets) |
| History | `history.view` |
| Account | `account.api_keys` (create personal API keys; Builder has it) |
| Admin | `admin.users`, `admin.service_accounts`, `admin.roles`, `admin.areas`, `admin.access`, `admin.audit` |

A person's permissions are the union of their roles' permissions and any granted directly. The built-in
**Administrator** role always holds every permission.

**Functional areas** (Finance, IT, Training, HR, ...) scope crews, agents and tasks; every one belongs to at least
one area. A principal sees an item only when they share an area with it, or have **all areas**. Work orders, runs,
schedules and triggers take their areas from their crew. Items outside your areas answer `404`, and lists
leave them out. When you save an item you can only add or remove your own areas; areas you are not in are kept.
LLMs, prices and settings are shared by everyone (subject to permissions).

`GET /areas` (any signed-in principal) lists every area: `{ id, name, description, created_at, updated_at, usage? }`
(`usage: { crews, agents, tasks, users }` only for `admin.areas`).

## LLMs

`GET /llms`, `POST /llms`, `GET /llms/{id}`, `PUT /llms/{id}`, `DELETE /llms/{id}`

Permissions: read `llms.view`; write `llms.manage`.

```ts
type LLM = {
  id: string
  name: string                          // unique, e.g. "GPT-6 Luna (research)"
  model: string                         // LiteLLM-style id: "openai/gpt-6.1-sol", "anthropic/claude-opus-5-5"
  temperature: number | null            // null = provider default (Claude 5.x models ignore it)
  max_completion_tokens: number | null
  reasoning_effort: "none" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max" | null
                                        // OpenAI reasoning_effort / Claude output_config.effort; null = model default
  api: "completions" | "responses"      // OpenAI only; gpt-6-astra and gpt-6.1-sol need "responses" for tools
  vision: boolean                       // accepts images and PDFs: work order screenshots/PDFs go to agents on it
  created_at: string
  updated_at: string
}
```

Create/update body: every field except `id`, `created_at`, `updated_at`.

## Tools (read-only catalog)

`GET /tools` (`agents.view`)

```ts
type Tool = {
  key: string             // stable id stored on agents, e.g. "google_search"
  name: string
  description: string
  requires_env: string[]  // env vars the tool needs, e.g. ["GEMINI_API_KEY"]
  available: boolean      // false when a required env var is missing on the server
  effect: "reads" | "proposes" | "changes"  // what it does outside the run
  needs: ("crew" | "tracker" | "km2" | "schedule")[]  // what it needs besides keys
}
```

`effect` and `needs` come from `runner/tools_catalog.json`; the server refuses to start when an entry has no effect or
an unknown one. In agent chat an agent keeps only tools whose effect is `reads` and whose needs the chat meets:
`tracker` and `km2` through the agent's listed connections, never `crew` or `schedule`
([design](agent-chat-handoffs-design.md), section 7).

## Pricing

Model prices drive every cost estimate. They are matched to an LLM by `model` (exact match first, then
the part after the provider prefix, so `openai/gpt-6-luna` matches a `gpt-6-luna` price).

`GET /pricing`, `POST /pricing`, `GET /pricing/{id}`, `PUT /pricing/{id}`, `DELETE /pricing/{id}`

Permissions: read `pricing.view`; write `pricing.manage`.

```ts
type ModelPrice = {
  id: string
  model: string                    // unique, e.g. "gpt-6-luna" or "gemini-2.5-flash"
  input_per_mtok: number           // USD per 1M input tokens
  cached_input_per_mtok: number    // tokens read from the provider's prompt cache
  cache_write_per_mtok: number | null   // tokens written to the prompt cache; null means input_per_mtok
  output_per_mtok: number
  notes: string                    // e.g. pricing page URL
  created_at: string
  updated_at: string
}
```

A call's prompt tokens are priced in three parts: those read from the prompt cache at `cached_input_per_mtok`, those
written to it at `cache_write_per_mtok` (Anthropic charges 1.25 times input for its default 5-minute cache, and
CrewAI caches Claude system and task prompts by itself; Claude prices come with that rate), and the rest at
`input_per_mtok`. LLM calls and run usage report `cache_write_tokens` beside `cached_prompt_tokens`; both are part of
`prompt_tokens`.

## Settings

`GET /settings` (`settings.view`), `PUT /settings` (`settings.manage`; body: the full object)

```ts
type Settings = {
  default_budget_usd: number                 // used when a crew has none; the budget of a crew draft from a task
  hard_stop_multiplier: number               // all LLM calls stop at budget x this (>= 1)
  search_model: string                       // Gemini model behind the google_search tool, e.g. "gemini-3.8-flash"
  grounded_search_cost_per_1000: number      // USD per 1,000 grounded search queries
  grounded_searches_free: number             // free grounded queries per period, shared by all runs
  grounding_free_period: "day" | "month"     // Gemini 3: 5,000 per month
  max_concurrent_runs: number                // 1-10
  compact_context_tokens: number             // 0 = never; else 10,000-1,000,000: an agent's context on a task is
                                             // compacted (its steps so far summarised) once it passes this many tokens
}
```

## History

Every create, update and delete is recorded with who made it.

`GET /history?entity_type=agent&entity_id=<uuid>&limit=50&offset=0` (both filters optional, paginated).
Needs `history.view`; admin record types (`area`, `role`, `user`, `api_key`, `access_settings`) also need
`admin.audit`. Revisions of area-scoped items are limited to your areas.

```ts
type Revision = {
  id: number
  entity_type: "llm" | "pricing" | "agent" | "task" | "crew" | "settings" | "work_order" | "schedule" | "trigger"
             | "area" | "role" | "user" | "api_key" | "access_settings"
  entity_id: string
  action: "create" | "update" | "delete"
  snapshot: Record<string, unknown>   // the entity after the change (before it, for delete); secrets never included
  actor_id: string | null             // null for changes made before sign-in existed
  actor_name: string
  created_at: string
}
```

## Agents

`GET /agents`, `POST /agents`, `GET /agents/{id}`, `PUT /agents/{id}`, `DELETE /agents/{id}`

Permissions: `agents.view` / `agents.create` / `agents.edit` / `agents.delete`, within your areas.

```ts
type Agent = {
  id: string
  name: string               // unique, short label shown in the UI
  role: string
  goal: string
  backstory: string
  llm_id: string | null      // required to run (so cost can be tracked); null is allowed while editing
  tools: string[]            // Tool keys
  max_iter: number           // 1-100
  allow_delegation: boolean
  verbose: boolean
  area_ids: string[]         // at least one functional area
  created_at: string
  updated_at: string
}
```

`role`, `goal`, `backstory` may contain `{input_key}` placeholders filled from the run inputs.

## Tasks

`GET /tasks`, `POST /tasks`, `GET /tasks/{id}`, `PUT /tasks/{id}`, `DELETE /tasks/{id}`

Permissions: `tasks.view` / `tasks.create` / `tasks.edit` / `tasks.delete`, within your areas. The agent and
context tasks must be visible to you (or already on the task).

`GET /tasks/{id}/crew-draft` (`tasks.view` and `crews.create`) returns an unsaved `Crew` body that runs just this
task, ready to `POST /crews`: the task plus every task it takes context from (earlier first), their agents, an
input for each `{placeholder}` (label, default and `required` borrowed from a crew that already contains the task,
otherwise required), `process: "sequential"`, the default budget from Settings, and the task's areas that you
belong to. It is named after the task, or `"<name> (task)"` (`"<name> (task 2)"`, ...) if a crew already has that
name, with the description `Runs the task "<name>".`. `404` if the task is not visible to you; `422` with reasons if a crew cannot be made (it takes context
from a task, or uses an agent, outside your areas, or the context forms a loop). Work orders, schedules and triggers
always run a crew, so this is how a single task is run.

```ts
type Task = {
  id: string
  name: string                 // unique
  description: string          // may contain {input_key} placeholders
  expected_output: string      // may contain {input_key} placeholders
  agent_id: string | null      // required for sequential crews
  context_task_ids: string[]   // tasks whose output this task receives
  markdown: boolean
  area_ids: string[]           // at least one functional area
  created_at: string
  updated_at: string
}
```

## Crews

`GET /crews`, `POST /crews`, `GET /crews/{id}`, `PUT /crews/{id}`, `DELETE /crews/{id}`

Permissions: `crews.view` / `crews.create` / `crews.edit` / `crews.delete`, within your areas. Agents and tasks
added to a crew must be visible to you; ones you cannot see that are already on the crew are kept.

```ts
type CrewInput = { key: string; label: string; default: string; required: boolean }

type Crew = {
  id: string
  name: string                                  // unique
  description: string
  process: "sequential" | "hierarchical"
  manager_llm_id: string | null                 // required when process = "hierarchical"
  agent_ids: string[]
  task_ids: string[]                            // execution order
  inputs: CrewInput[]                           // keys match [a-z][a-z0-9_]*
  budget_usd: number                            // default spend cap per run
  area_ids: string[]                            // at least one functional area
  created_at: string
  updated_at: string
}
```

`GET /crews/{id}/validate` returns `{ "valid": boolean, "errors": string[], "warnings": string[] }`: checks a run would start
(every task has an agent in the crew, context tasks come earlier in the crew, placeholders have a matching input, and so on).
`warnings` do not stop a run but it is likely to fail: an LLM the crew uses whose provider key (`OPENAI_API_KEY`,
`ANTHROPIC_API_KEY` or `GEMINI_API_KEY`, by the model's `openai/`, `anthropic/` or `gemini/` prefix) is empty in the
provider env file and the server's environment. Only whether a key is set is checked, never its value.

## Runs

A run is one execution of a crew (a work order's attempts are runs too).

`POST /crews/{id}/runs` (`crews.run`) with body `{ "inputs": { "company": "Acme" }, "budget_usd": 2.0, "callback_url": "https://..." }`
(`budget_usd` optional, defaults to the crew's; `callback_url` only for service accounts, see Callbacks).
Send `Idempotency-Key: <any unique string>` to make retries safe: a repeated key returns the first run with `200`.
Returns `201` with a `Run`, or `422` with the reasons the crew cannot run.

`GET /runs?crew_id=&work_order_id=&status=&trigger_type=&kind=&limit=50&offset=0` (`runs.view`) lists runs in your areas,
newest first (paginated, `task_outputs` omitted). Every run has a `kind`: `crew` for crew runs, `chat` for one turn of
a crew chat (see Crew chat), `handoff` for a hand-off from an agent chat (see [Hand-offs](#hand-offs)); `kind=` picks one
of them. Chat turns and hand-offs hold a person's private conversation, so they are listed and readable (with every
run sub-resource) only for the person who sent the message; for anyone else they are `404`. They still count in
overview spend. `DELETE /runs/{id}` refuses a chat turn or hand-off younger than 24 hours with `409 chat_turn_recent`,
because chat budgets are counted from it.

`GET /runs/{id}` returns one `Run` with `task_outputs` and `spec`.

`POST /runs/{id}/cancel` (`runs.cancel`) stops a queued or running run. Returns the updated `Run`.

`POST /runs/{id}/compact` (`runs.cancel`, or `work_orders.cancel` for a work order's attempt; body `{}`) asks a running
crew run to **compact its agents' context**: before each agent's next LLM call, its conversation on the task so far is
replaced by a summary its own model writes, keeping the system prompt, the task prompt and the latest step. Later
calls then resend fewer tokens. Returns `{ compact_requests }` (how many times it was asked); each agent compacts once
per request made while it was working. The request and each compaction (`context_compacted` events: `agent_role`,
`reason` `asked` or `auto`, `tokens_before`, `tokens_after` (estimates), `messages_summarised`; on failure `failed: true` and `error`, and the context is left as it was) show in the journal; a request made while an agent has too little to summarise stays pending until it has more;
the summary is an ordinary, metered LLM call of the agent. `409 run_not_running` unless the run is running, `409
chat_turn` for a chat turn. Settings' `compact_context_tokens` does the same by itself once an agent's context passes
that size. The runner reads the request count from `GET /run-state/control` (run token) at most every 5 seconds.

`DELETE /runs/{id}` (`runs.delete`) deletes a finished run with its events and files.

`GET /runs/{id}/artifacts` lists the run's stored files: files agents wrote (`kind: "artifact"`) and the full
transcript and console log (`kind: "transcript"`). Download with `GET /attachments/{id}/content`.

`GET /runs/{id}/deliveries` lists callback deliveries for the run (see Callbacks).

```ts
type RunStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled"

type Run = {
  id: string
  crew_id: string | null       // null once the crew is deleted, and for older single-task runs
  crew_name: string            // "Task: <name>" for single-task runs made before 0010_crew_targets_only
  status: RunStatus
  inputs: Record<string, string>
  budget_usd: number
  output: string | null        // final output (markdown)
  error: string | null
  cost_usd: number | null      // estimated spend
  usage: { prompt_tokens: number; completion_tokens: number; cached_prompt_tokens: number; successful_requests: number } | null
  area_ids: string[]
  created_by: string | null
  created_by_name: string
  trigger_type: "manual" | "api" | "schedule" | "webhook" | "work_order"
  kind: "crew" | "chat" | "handoff"     // chat: one turn of a crew chat (its spec has a chat section and no tasks);
                                        // handoff: a hand-off from an agent chat (its chat section has "handoff")
  work_order_id: string | null
  created_at: string
  started_at: string | null
  finished_at: string | null
  task_outputs?: { position: number; task_name: string; agent_role: string; raw: string }[]  // always present on GET /runs/{id}
  spec?: RunSpec               // only on GET /runs/{id}
}

// Everything the run was started with, frozen at start so later edits do not change history
type RunSpec = {
  crew: Crew
  agents: Agent[]
  tasks: Task[]                // in crew order
  llms: LLM[]
  pricing: ModelPrice[]
  settings: Settings
  inputs: Record<string, string>
  budget_usd: number
  work_order?: { id: string; number: number; title: string; brief: string;
                 attachments: { id: string; filename: string; content_type: string; size_bytes: number; path: string }[] }
}
```

### Interactions

`GET /runs/{id}/interactions` summarises who did what in a run, for drawing a graph.

```ts
type InteractionNode = {
  id: string                         // "crew", "task:<name>", "agent:<role>", "tool:<name>", "model:<model>"
  kind: "crew" | "task" | "agent" | "tool" | "model"
  label: string
  status?: "pending" | "running" | "done" | "failed"   // tasks and agents
  stats: { llm_calls: number; tool_calls: number; prompt_tokens: number; completion_tokens: number; cost_usd: number; duration_ms: number }
}

type InteractionEdge = {
  id: string
  source: string
  target: string
  kind: "runs" | "assigned" | "context" | "tool_call" | "llm_call" | "delegation"
  count: number
  cost_usd: number
}

type Interactions = { nodes: InteractionNode[]; edges: InteractionEdge[] }
```

`GET /runs/{id}/llm-calls` lists every LLM call in the run: `{ seq, agent_role, task_name, model, usage, cost_usd, messages, response, created_at }[]`
(`messages` is the chat message array sent; `response` the text returned; strings over 200 000 characters are cut
here, and kept in full in the run transcript).

### Events

`GET /runs/{id}/events?after=0` returns `RunEvent[]` with `seq > after` (up to 1000).

`GET /runs/{id}/stream?after=0` is a Server-Sent Events stream. Each message:

```
id: <seq>
event: run_event
data: <RunEvent JSON>
```

The server first replays stored events after `after` (or the `Last-Event-ID` header), then sends live
ones. When the run reaches a terminal status it sends a `run_status` event and closes the stream.
A `: ping` comment is sent every 15 s; each ping re-checks that the viewer is still signed in and may still see
the run, and ends the stream if not. At most 32 streams may be open at once, 8 per person or key (`503` beyond).

```ts
type RunEvent = { seq: number; type: RunEventType; payload: Record<string, unknown>; created_at: string }

type RunEventType =
  | "run_status"      // { status: RunStatus, error?: string }
  | "crew_started"    // { crew_name }
  | "task_started"    // { task_name, agent_role }
  | "agent_started"   // { agent_role, task_name }
  | "tool_started"    // { agent_role, task_name, tool_name, tool_args }
  | "tool_finished"   // { agent_role, task_name, tool_name, output }   output cut at 4000 chars (full text in the transcript)
  | "tool_error"      // { agent_role, task_name, tool_name, error }
  | "llm_call"        // { agent_role, task_name, model, usage: {prompt_tokens, completion_tokens, cached_prompt_tokens}, cost_usd, spent_usd, call_id }
                      //   the stream omits messages/response; fetch them from /runs/{id}/llm-calls
  | "search"          // { agent_role, task_name, tool_name, query, model, grounded, billable_searches, input_tokens, output_tokens, cost_usd, spent_usd }
  | "delegation"      // { from_agent, to_agent, task_name, tool_name, question }
  | "task_completed"  // { task_name, agent_role, output }
  | "task_failed"     // { task_name, agent_role, error }
  | "crew_completed"  // { output, usage, cost_usd, spend: {...} }
  | "crew_failed"     // { error, budget_stopped: boolean }
  | "budget"          // { spent_usd, budget_usd, state: "searches_stopped" | "hard_stopped" }
  | "artifacts"       // { files: number, bytes: number, error?: string }   files saved to storage after the run
  | "state"           // { tool, message, detail?, agent_role? }   a notes or action log call (see Run state API)
  | "log"             // { line }   raw CrewAI console output (stderr), ANSI codes stripped
```

## Overview

`GET /overview` (`overview.view`), counted within your areas (LLMs and prices are shared)

```ts
type Overview = {
  counts: { work_orders: number; schedules: number; llms: number; pricing: number; agents: number; tasks: number; crews: number; runs: number }
  recent_runs: Run[]               // 10 newest crew runs (chat turns left out), without task_outputs
  recent_work_orders: WorkOrder[]  // 10 newest
  spend_last_30d_usd: number       // every run, chat turns included
  chat_spend_last_30d_usd: number  // the chat turns' and hand-offs' share
}
```

## Work orders

A **crew** is what runs; a **work order** is one request to execute it: a title, a brief (pasted text,
markdown), input files (including images pasted from the clipboard), inputs and budget. Submitting it starts a
run; resubmitting starts another attempt. Every attempt keeps its journal, interaction graph, LLM calls, files
the agents wrote, and a full untruncated transcript.

The target is always a crew (`crew_id`). To run a single task, make a one-task crew from it with
`GET /tasks/{id}/crew-draft` and `POST /crews`, then use its `crew_id`. Sending `task_id`, or a `target_type`
other than `"crew"`, to work orders, schedules or triggers is refused with `422`; the error detail says to make a
one-task crew and use its `crew_id`.

The brief and files are added to every task: images and PDFs are given to the model, small text files are quoted,
other files are copied into the run's workspace under `attachments/`.

Upgrading to `0010_crew_targets_only` turned every task that a work order, schedule or trigger targeted into a crew
of its own (built like a crew draft, with the default budget, the task's areas and a description saying it was made
automatically; one crew per task, shared), and pointed them at it. History shows the crew as created by "Crew Studio
upgrade". Work orders whose task had already been deleted have `crew_id: null`. Earlier attempts (runs) are unchanged.

`GET /work-orders?status=&source=&crew_id=&q=&limit=50&offset=0` (`work_orders.view`; `crew_id` keeps one crew's
work orders; `q` matches the title or `WO-123`) lists work orders in your areas, newest first (paginated).

`POST /work-orders` (`work_orders.create`) body `WorkOrderInput`. With `submit: true` it starts at once; an
`Idempotency-Key` header then makes retries return the first work order. Returns `201` with the `WorkOrder`.

`GET /work-orders/{id}` returns the `WorkOrder` with `attachments` (input files) and `runs` (newest first).

`PUT /work-orders/{id}` (`work_orders.create`) edits it (not while queued or running: `409`). Changes apply to the
next submit.

`POST /work-orders/{id}/submit` (`work_orders.create`) body `{ budget_usd?, callback_url? }` starts a new attempt.
Returns `201` with the `Run`; `409` if one is already queued or running; `422` with reasons it cannot run.

`POST /work-orders/{id}/cancel` (`work_orders.cancel`) stops the current attempt. Returns the `Run`.

`DELETE /work-orders/{id}` (`work_orders.delete`) deletes it with every run and file (`409` while running).

```ts
type WorkOrderInput = {
  title: string                          // 1-200 chars
  brief: string                          // markdown, up to 100 000 chars
  crew_id: string                        // the crew to run (required)
  inputs: Record<string, string>         // values for the crew's inputs
  budget_usd: number | null              // null = the crew's budget
  submit?: boolean                       // create only: start straight away
  callback_url?: string                  // create with submit, service accounts only
}

type WorkOrder = {
  id: string
  number: number                         // shown as WO-<number>
  title: string
  brief: string
  crew_id: string | null                 // null once the crew is deleted (it then has no target)
  target_name: string                    // the crew's name; kept after the crew is deleted
  inputs: Record<string, string>
  budget_usd: number | null
  area_ids: string[]                     // copied from the crew when created or edited
  source: "manual" | "api" | "schedule" | "webhook" | "chat"   // chat: confirmed in a crew chat; source_id is the conversation
  source_id: string | null               // schedule or trigger id
  created_by: string | null
  created_by_name: string
  status: "draft" | RunStatus            // "draft" until first submitted, then the latest attempt's status
  current_run_id: string | null
  cost_usd: number | null                // latest attempt
  run_count: number
  submitted_at: string | null
  finished_at: string | null
  created_at: string
  updated_at: string
  attachments?: Attachment[]             // detail only: input files
  runs?: Run[]                           // detail only: attempts, newest first
}
```

### Attachments

`POST /work-orders/{id}/attachments` (`work_orders.create`): `multipart/form-data` with one or more `file` parts,
plus header `X-Requested-With: crewstudio`. Up to 25 MB per file, 20 files and 100 MB per work order. Allowed:
images (PNG, JPEG, GIF, WebP), PDF, text (plain, markdown, CSV, JSON, XML, YAML, HTML), Word, Excel and
PowerPoint documents. The type is decided from the content, not the name. Returns `201` with `Attachment[]`.
A pasted clipboard image is uploaded the same way (the UI names it `pasted-<time>.png`).

`DELETE /work-orders/{id}/attachments/{attachmentId}` removes an input file. Files cannot be added or removed while
the work order is queued or running (`409`); the limits are enforced atomically, so parallel uploads cannot exceed them.

`GET /attachments/{id}/content` streams a file (input, artifact or transcript) to anyone who can see its work
order or run. Images are served inline; everything else as a download (`?download=1` forces a download).

```ts
type Attachment = {
  id: string
  work_order_id: string | null
  run_id: string | null
  kind: "input" | "artifact" | "transcript"
  filename: string                       // artifacts: path inside the run workspace, e.g. "reports/memo.md"
  content_type: string
  size_bytes: number
  sha256: string
  created_by_name: string
  created_at: string
}
```

Files live in object storage: Azure Blob Storage, Google Cloud Storage, or (locally) the Azurite emulator.

### Journal

`GET /work-orders/{id}/journal?run_id=` (default: the current attempt) and `GET /runs/{id}/journal` turn a run's
events into a readable journal: what each agent did on each task, in order.

```ts
type Journal = {
  run_id: string
  status: RunStatus
  agents: { role: string; tasks: string[]; entries: number; llm_calls: number; tool_calls: number;
            prompt_tokens: number; completion_tokens: number; cost_usd: number; first_at: string | null; last_at: string | null }[]
  tasks: { name: string; agent_role: string; status: "pending" | "running" | "done" | "failed";
           started_at: string | null; finished_at: string | null; output: string | null }[]
  entries: JournalEntry[]                // oldest first, at most 5000 (truncated: true when cut)
  truncated: boolean
}

type JournalEntry = {
  seq: number                            // run event seq; also the LLM call seq for kind "llm_call"
  at: string
  agent_role: string                     // "" for crew-level entries
  task_name: string
  kind: "run" | "task_started" | "task_completed" | "task_failed" | "agent_started" | "llm_call" | "tool_call"
      | "tool_result" | "tool_error" | "search" | "delegation" | "budget" | "artifacts"
  title: string                          // one line, e.g. "Called Google Search", "Delegated to Market Analyst"
  body: string                           // details (markdown/plain), cut at 4000 chars; full text via llm-calls or the transcript
  model: string
  cost_usd: number
  tokens: number
}
```

### Conversation and revising

Each person can talk with a work order's crew about it, and anyone who may create work orders can **revise** it with
feedback.

**Conversation.** A conversation about a work order is a crew conversation (see [Crew chat](#crew-chat)) linked to the
work order: one per person and work order, private to them, needing `crews.chat`. It works whether or not the crew has
chat turned on. The crew's front desk answers when chat is set up; otherwise the agent of the crew's last task (the one
that wrote the final output) does, on its own model. Each turn is given the work order's brief and its latest
attempt's output (cut to 20,000 characters), fenced as data. Budgets are the crew's chat budgets (defaults when chat
was never set up). These conversations are not listed by `GET /chat/conversations`; `/chat/conversations/{id}` and the
proposal calls work on them as on any conversation.

| Call | Does |
|---|---|
| `GET /work-orders/{id}/conversation` | your `ConversationView` about the work order, or `null` before you start one (`work_orders.view`, `crews.chat`) |
| `POST /work-orders/{id}/conversation/messages` | body `{ content }`: sends a message, starting the conversation if need be; `201 ConversationView` (poll it while the reply is written) |
| `POST /work-orders/{id}/conversation/compact` | body `{}`: compacts your conversation (see Compacting under [Crew chat](#crew-chat)); `404` before you start one |
| `GET /work-orders/{id}/feedback` | the work order's feedback, oldest first (`work_orders.view`): `WorkOrderFeedback[]` |
| `POST /work-orders/{id}/revise` | files to go with the feedback are uploaded first as the work order's input files (`POST /work-orders/{id}/attachments`), which every attempt is given; the web app names them at the end of the feedback. Body `{ feedback, include_conversation? }` (`work_orders.create`): starts a new attempt that revises the latest one; `201 { run, feedback }`. `409 not_submitted` before the first attempt, `409` while an attempt is queued or running, `422` for empty feedback (at most 8,000 characters) |

```ts
type WorkOrderFeedback = {
  id: string; work_order_id: string
  run_id: string | null            // the attempt the feedback is about
  revision_run_id: string | null   // the attempt it started
  feedback: string
  with_conversation: boolean       // your conversation about the work order went to the revision
  created_by_name: string; created_at: string
}
```

**Revising.** The new attempt runs the crew as a re-run does, and every task also gets a "Revision of attempt N"
section: the latest attempt's output (cut to 20,000 characters), the feedback, and, with `include_conversation`, your
latest 20 messages with the crew about the work order. Its spec carries the same as `spec.work_order.revision`
(`{ attempt, previous_output, feedback, feedback_by, conversation? }`). A plain re-run (`/submit`) has no revision.

## Schedules

A schedule creates a work order for a crew on a cron schedule. Its work orders run with the permissions
and areas of the person who last saved it (`run_as`); if that person loses access, the schedule pauses itself and
records why in `last_error`.

`GET /schedules`, `GET /schedules/{id}` (`schedules.view`); `POST /schedules`, `PUT /schedules/{id}`,
`DELETE /schedules/{id}`, `POST /schedules/{id}/run-now` (`schedules.manage` and `work_orders.create`, within your
areas). `run-now` returns the new `WorkOrder` (`201`).

`POST /schedules/preview` body `{ cron, timezone }` returns `{ next: string[] }` (the next 5 times) or `422`.

```ts
type Schedule = {
  id: string
  name: string
  description: string
  crew_id: string             // the crew to run (required on create and update)
  target_name: string
  cron: string                // 5 fields "min hour day month weekday", or @hourly / @daily / @weekly / @monthly
  timezone: string            // IANA name, e.g. "Europe/London"; default "UTC"
  brief: string               // becomes each work order's brief
  inputs: Record<string, string>
  budget_usd: number | null
  enabled: boolean
  overlap_policy: "skip" | "allow"   // skip: do not start while the previous work order is still running
  run_as: string | null
  run_as_name: string
  area_ids: string[]          // the target's areas
  next_run_at: string | null
  last_run_at: string | null
  last_work_order_id: string | null
  last_error: string | null
  created_at: string
  updated_at: string
}
```

Schedules may not fire more often than every 5 minutes. A schedule missed while the service was down fires once
when it comes back.

`inputs.run_tasks` (any work order may carry it; crews need not declare it) narrows the run to some of the crew's
tasks: task ids or names (ignoring case), separated by commas or new lines, run in the crew's order. A name the crew
does not have is a `422` when the work order is submitted. The PMO's schedules use it to run one job each.

### Report destinations

`GET /schedules/{id}/report-destinations` (`schedules.view`) and `PUT` (`schedules.manage`) with
`{ destinations: [{ kind, connection_id, recipients?, folder? }] }`. Where the reports of the schedule's runs go
besides the work order (design section 12; `docs/delivery.md`). `kind` is `slack`, `teams`, `email` or `km2`; the
connection must be of that kind, visible to you, one the crew may use (crew State tab) and in an area of the schedule.
Email destinations list 1 to 50 bare addresses (no display names, no line breaks); KM2 destinations may name a folder
id the connection allows (empty: its default folder). At most 10 destinations. The answer is
`{ destinations: [ReportDestination], connections: [{ id, name, kind }], kinds }`; `connections` (what may be chosen) is
only filled for people who can manage the schedule. A connection a destination uses cannot be deleted (`409`).
Audited as `report_destinations_set`.

```ts
type ReportDestination = { id; schedule_id; position; kind: "slack" | "teams" | "email" | "km2"; connection_id; connection_name; recipients: string[]; folder: string; created_at }
```

### Watches

A schedule can have one **watch** (design: `docs/crew-state-and-pmo-design.md`, sections 5 and 6). On every tick the
watch fetches its source and compares it with its snapshot (code, no AI). With nothing to do, no work order is
created and the check is recorded as `no_changes`. Otherwise the work order gets three extra inputs, which crews need
not declare: `changes` (JSON, below), `followups_due` (JSON, only with a ladder) and `dry_run` (`"true"`/`"false"`).
Only work orders a schedule created may carry these undeclared; anywhere else they are refused like any undeclared
input.
The snapshot is saved only after the work order exists. Overlap `skip` still applies (the check is recorded as `busy`
and the source is not read). A failed check sets the watch's and the schedule's `last_error` without pausing it.
`POST /schedules/{id}/run-now` on a watched schedule always starts a work order, with the current changes.

| Call | Permission | Does |
|---|---|---|
| `GET /schedules/{id}/watch` | `schedules.view` | `{ watch: Watch \| null, stats, checks, source_kinds, connections }`; `checks` are the 20 newest, `stats` cover 30 days, `connections` (the crew's usable HTTP connections) only for `schedules.manage` |
| `PUT /schedules/{id}/watch` | `schedules.manage` | body `WatchInput`; creates or replaces the watch and answers like `GET`. Another source, id field or tracked fields drops the snapshot |
| `DELETE /schedules/{id}/watch` | `schedules.manage` | removes the watch, its snapshot and its checks (`204`) |
| `POST /schedules/{id}/watch/preview` | `schedules.manage` | body `WatchInput`; fetches now and answers `{ changes, followups_due, would_run, reason, item_count, sample }`. Saves and starts nothing. `422 source_failed` when the source cannot be read. 10 a minute per person, burst 5 |
| `DELETE /schedules/{id}/watch/snapshot` | `schedules.manage` + `state.manage` | forgets the snapshot; the next check is a first check |
| `GET /crews/{id}/state/watches` | `state.view` | the watches on the crew's schedules, each with `stats` |

Saving, removing and clearing are audited (`watch_saved`, `watch_deleted`, `watch_snapshot_cleared`).

```ts
type WatchInput = {
  connection_id: string | null      // http_json only: an enabled http connection linked to the crew, sharing an area with it
  source:
    | { kind: "http_json"; path: string; items_path?: string }        // path such as "/issues?state=open", added to the base URL
    | { kind: "web_page"; url: string; selector?: string }             // public http(s) pages only; CSS selector optional
    | { kind: "workspace_file"; file: string; format?: "csv" | "json"; items_path?: string }  // in the workspace's watch/ folder
  id_field: string                  // dotted path (or CSV column) identifying each item; ignored for web pages
  tracked_fields: string[]          // 1-30 dotted paths or columns to compare; ignored for web pages ("text")
  run_when?: "changed" | "always" | "changed_or_followups"            // default "changed"
  followups?: FollowupLadder | null
  dry_run?: boolean                 // default true
  max_items?: number                // default 2000, at most 10000; more items fail the check
}
type FollowupLadder = {
  date_field: string                // one of tracked_fields
  skip_when?: Record<string, string[]>   // tracked field -> values (ignoring case)
  steps: { name: string; offset: string; notify: string; repeat?: string }[]  // offset "-3d" | "0d" | "+2d" | "1w"; repeat "7d"
  quiet_days?: ("Mon" | "Tue" | "Wed" | "Thu" | "Fri" | "Sat" | "Sun")[]
  timezone?: string                 // default: the schedule's
  max_per_tick?: number             // default 25, at most 200
}
type Changes = {                    // the "changes" input
  source: string; checked_at: string
  previous_check_at: string | null  // null on the first check, when every item is "added"
  added: { id: string; fields?: Record<string, unknown> }[]
  changed: { id: string; fields: Record<string, { old: unknown; new: unknown }> }[]
  removed: { id: string; reason: string }[]
  unchanged_count: number
  added_fields_omitted?: boolean    // the added items' fields were dropped to stay under 120,000 characters
}
type FollowupsDue = {               // the "followups_due" input
  due: { item: string; subject: string; step: string; action: string   // action "followup:<step>"
         notify: string; notify_value?: unknown; date: string; step_date: string; days_late: number
         repeat?: string; last_sent_at: string | null; fields: Record<string, unknown> }[]
  more_waiting: number; undated_items: number; dry_run: boolean; how_to_record: string
}
```

Text longer than 300 characters (and nested values whose JSON is) is compared by SHA-256 and shown as
`"(long text, N characters; compared by hash)"`. A step is due when its date (the item's date plus the offset, moved off
quiet days) has passed and the action log of the schedule has no `followup:<step>` entry for the item's subject
recorded on or after that date (or none within `repeat`, counted in days in the ladder's time zone); only the latest passed step of an item can be due, nothing is due on a quiet day, and at most
`max_per_tick` go to one run. When the watch is not in dry run, dry-run log entries do not count. The crew marks a
follow-up as sent with `POST /run-state/log` (the Record Action tool).

**Dry run.** A schedule's run whose `dry_run` input is `"true"` gets `dry_run: true` in `GET /run-state/scope`; its log
entries are recorded with `dry_run: true`, and its log checks also see dry-run entries (other runs ignore them). A real
entry with the same idempotency key as a dry-run one replaces it. Text from the source reaches the prompt inside a
JSON block marked as data, with backticks escaped so it cannot close the block. The runner tells
every task of a dry run not to post or send anything and to list what it would have sent under a "Would post" heading
in its output, giving each message's destination, mentions and full text.

## Triggers (webhooks)

A trigger gives another system a URL that creates (and submits) a work order when it POSTs JSON to it.

`GET /triggers`, `GET /triggers/{id}` (`triggers.view`); `POST /triggers`, `PUT /triggers/{id}`,
`DELETE /triggers/{id}`, `POST /triggers/{id}/rotate` (`triggers.manage` and `work_orders.create`, within your
areas). Create and rotate return the trigger with `token` set: it is shown once. `signing_secret` is returned only
to principals with `triggers.manage`.

```ts
type Trigger = {
  id: string
  name: string
  description: string
  crew_id: string                       // the crew to run (required on create and update)
  target_name: string
  enabled: boolean
  token_prefix: string                  // identifies the token without revealing it
  input_map: Record<string, string>     // input key -> dotted path in the payload, e.g. { "company": "customer.name" }
  brief: string                         // text placed before the payload in each work order's brief
  include_payload: boolean              // append the JSON payload to the brief
  budget_usd: number | null
  callback_url: string | null           // POSTed when each work order's run finishes
  signing_secret?: string
  run_as: string | null
  run_as_name: string
  area_ids: string[]
  last_fired_at: string | null
  fire_count: number
  created_at: string
  updated_at: string
  token?: string                        // create and rotate only
}
```

### Calling a trigger

`POST /hooks/{triggerId}` with `Authorization: Bearer cst_<prefix>_<secret>` (or header `X-CrewStudio-Token`),
`Content-Type: application/json`, any JSON body up to 1 MB. Optional `Idempotency-Key` header. Returns `202`:

```json
{ "success": true, "data": { "work_order_id": "...", "number": 42, "run_id": "...", "status": "queued",
  "status_url": "/api/work-orders/..." } }
```

Wrong or missing token: `401`. Disabled trigger: `409`. The target cannot run (missing input, no price, ...): `422`.
Rate limited per trigger, and at most 5 of a trigger's work orders may be queued or running at once (`429`).

## Callbacks

When a run started with a `callback_url` (by a trigger, or by a service account) finishes, Crew Studio POSTs:

```json
{ "event": "run.finished", "delivered_at": "...",
  "run": { "id", "status", "crew_name", "output", "error", "cost_usd", "usage", "started_at", "finished_at", "trigger_type" },
  "work_order": { "id", "number", "title" } | null }
```

Headers: `X-CrewStudio-Event: run.finished`, `X-CrewStudio-Delivery: <id>`, `X-CrewStudio-Timestamp: <unix seconds>`,
`X-CrewStudio-Signature: sha256=<hex HMAC-SHA256 of "<timestamp>.<body>" with the signing secret>`. The secret is
the trigger's `signing_secret`, or the service account's (admin screen). Reject requests whose timestamp is more
than 5 minutes old.

Any `2xx` within 10 seconds counts as delivered; otherwise it is retried after 30 s, 2 min, 10 min, 30 min and 2 h,
then marked failed. Callback URLs must be `https` and resolve to public addresses, unless the host is on the
access settings' `callback_allowed_hosts` list (for internal systems; then `http` and private addresses are
allowed). Redirects are not followed.

## Account

Personal API keys act as the signed-in person, with their permissions and areas.

- `GET /account/api-keys` lists your keys as a page of `AccountAPIKey` (see Key lists). Anyone signed in may
  call it.
- `POST /account/api-keys` body `{ name, expires_at? }` returns `{ key: APIKey, secret: "csk_..." }`; the secret is
  shown once. Needs `account.api_keys` and a browser session: a request made with an API key gets `403`, so a key
  cannot create more keys. `409` when you already hold 20 active keys.
- `DELETE /account/api-keys/{keyId}` revokes one of your keys (`404` for anyone else's).

Personal keys cannot request run callbacks; those are for service accounts.

## Admin

All under `/admin`. Every change is recorded in History (admin types).

### People and service accounts

`GET /admin/users?kind=human|service`, `POST /admin/users`, `GET /admin/users/{id}`, `PUT /admin/users/{id}`,
`DELETE /admin/users/{id}`. People need `admin.users`; service accounts need `admin.service_accounts`.
You cannot remove the last active person holding `admin.users` (`409`, `code: "last_admin"`). You can only change, disable, delete, set
passwords for, sign out, or manage the keys and secrets of accounts whose permissions and areas you hold yourself
(`403` otherwise), so a less privileged admin cannot take over a more privileged account. The email of a person
who already signs in with SSO cannot be changed.

```ts
type UserInput = {
  kind: "human" | "service"
  email: string               // people only; how SSO sign-ins are matched
  name: string
  status: "active" | "disabled"   // disabling signs them out everywhere
  all_areas: boolean
  area_ids: string[]
  role_ids: string[]
  permissions: string[]       // granted directly, on top of roles
}

type User = UserInput & {
  id: string
  email: string | null
  effective_permissions: string[]
  has_password: boolean
  identities: { provider: string; email: string; last_login_at: string }[]
  last_login_at: string | null
  created_at: string
  updated_at: string
}
```

`PUT /admin/users/{id}/password` body `{ password }` (12-72 characters) sets a local password; `DELETE` removes it.
Only for people who hold `admin.users` (local sign-in is for administrators). Needs `admin.users`.

`POST /admin/users/{id}/sessions/revoke` signs someone out everywhere: `{ ended: number }`.

API keys of an account (`admin.users` for people, `admin.service_accounts` for service accounts):
`GET /admin/users/{id}/api-keys` returns a page of keys; `DELETE /admin/users/{id}/api-keys/{keyId}` revokes (you
must hold everything the account holds). `POST` body `{ name, expires_at? }` returns
`{ key: APIKey, secret: "csk_..." }` (the secret is shown once) and works only for service accounts: admins never
issue keys that act as a person.

`GET /admin/api-keys` lists every key: people's keys with `admin.users`, service accounts' with
`admin.service_accounts`.

**Key lists** (`/account/api-keys`, `/admin/users/{id}/api-keys`, `/admin/api-keys`) are pages (`meta.total`),
active keys first and each group newest first. They take `limit` (default 50, at most 200), `offset`, `q` (matches
the key name, account name or prefix), `kind` (`human` or `service`) and `status` (`active`, `expired` or
`revoked`); other values answer `422`.

```ts
type AccountAPIKey = APIKey & { user_name: string; user_kind: "human" | "service"; created_by_name: string | null;
                                can_revoke: boolean }   // false when the owner holds something you do not
```

`GET /admin/users/{id}/signing-secret` returns `{ signing_secret }`; `POST .../signing-secret/rotate` replaces it.

```ts
type APIKey = { id: string; user_id: string; name: string; prefix: string; created_at: string;
                expires_at: string | null; last_used_at: string | null; revoked_at: string | null }
```

### Roles

`GET /admin/roles` (`admin.roles` or `admin.users`), `POST`, `GET/PUT/DELETE /admin/roles/{id}` (`admin.roles`).
`GET /admin/permissions` returns the catalog `{ key, feature, label, description }[]`.

```ts
type Role = { id: string; name: string; description: string; builtin: boolean; permissions: string[];
              user_count: number; created_at: string; updated_at: string }
```

The built-in Administrator role cannot be changed or deleted.

### Functional areas

`POST /areas`, `PUT /areas/{id}`, `DELETE /areas/{id}` (`admin.areas`). An area still holding crews, agents or
tasks cannot be deleted (`409`).

### Access settings

`GET /admin/access`, `PUT /admin/access` (`admin.access`)

```ts
type AccessSettings = {
  sso_auto_provision: boolean            // create an account for unknown SSO sign-ins (no areas)
  auto_provision_role_id: string | null  // role they get
  callback_allowed_hosts: string[]       // hosts callbacks may reach on private networks
}
```

### Audit log

`GET /admin/audit?limit=50&offset=0` (`admin.audit`): sign-ins, sign-outs and failures, and API keys created,
revoked or refused. Key events carry `key_id`, `key_name`, `prefix` and the account (`account_id`,
`account_name`, `account_kind`); a refused key carries the `path` and, when it is shaped like a key, its `prefix`.
The same refused key from the same address is recorded at most once every 10 minutes, and revoking a key that is
already revoked records nothing. Secrets are never recorded.

```ts
type AuditEvent = { id: number; action: "login" | "login_failed" | "logout" | "setup" | "sessions_revoked"
                      | "api_key_created" | "api_key_revoked" | "api_key_rejected";
                    actor_id: string | null; actor_name: string; ip: string; details: Record<string, unknown>; created_at: string }
```

## Connections

Credentials for outside systems. A connection belongs to functional areas like a crew does, and its secrets are
encrypted at rest (AES-256-GCM with the server's `STUDIO_SECRETS_KEY`, bound to the connection's id). Secrets are
write-only: no endpoint returns them. Without `STUDIO_SECRETS_KEY` the server answers `409 secrets_key_missing` to
creates and secret replacements; once connections exist, the server refuses to start without the key (or with a key
that does not open their secrets). To change the key, stop the service and run `crewstudio rotate-secrets-key` (or `-rotate-secrets-key`) with
`STUDIO_SECRETS_KEY_OLD` (default `STUDIO_SECRETS_KEY`) and `STUDIO_SECRETS_KEY_NEW` set: it re-encrypts every
connection secret in one transaction (nothing changes if any secret does not open), then set `STUDIO_SECRETS_KEY` to
the new key.

`GET /connections`, `GET /connections/{id}`, `GET /connections/kinds` (`connections.view`; only connections sharing an
area with you). `POST /connections`, `PUT /connections/{id}`, `PUT /connections/{id}/secret`,
`POST /connections/{id}/test`, `DELETE /connections/{id}` (`connections.manage`; you can only add or remove your own
areas, and changing, re-keying, testing or deleting a connection needs every one of its areas, else `403`). Every change is written to the audit log (`connection_created`, `connection_updated`,
`connection_secret_replaced`, `connection_deleted`) without secret values.

```ts
type Connection = {
  id: string
  name: string                       // unique
  kind: "jira" | "linear" | "km2_tracker" | "km2" | "http" | "slack" | "teams" | "email"
  config: Record<string, string>     // non-secret settings; GET /connections/kinds lists them per kind
  secret_keys: string[]              // which secret fields are set (never their values)
  missing_secrets: string[]          // required secrets not set yet, e.g. ["api_token"] or ["webhook_url or bot_token"]
  secret_updated_at: string | null
  area_ids: string[]
  enabled: boolean                   // runs only get enabled connections
  crew_count: number                 // crews that may use it
  created_by: string | null
  created_at: string
  updated_at: string
}
// POST body: { name, kind, config, area_ids, enabled?, secrets?: Record<string, string> }
// PUT body: the same without secrets (sending secrets is a 422) and the kind cannot change; leaving out enabled keeps
//   it. Changing a URL, host, port or auth_header clears the saved secrets (audited with secrets_cleared: true).
// PUT /connections/{id}/secret body: { secrets: Record<string, string> } replaces every secret field
```

| Kind | `config` | `secrets` |
|---|---|---|
| `jira` | `site_url` (https `*.atlassian.net` or a site in `STUDIO_JIRA_SITES`), `email`, `cloud_id`, `issue_type`, `allow_create` (all optional but the first two) | `api_token` |
| `linear` | `allow_create` (optional) | `api_key` |
| `km2_tracker` | `base_url` (https, or http to this machine; no path; only origins in `STUDIO_KM2_TRACKER_SITES` when set), `self_email` (the Crew Studio `trk_person`), `entity_prefix` (default `trk_`), `activity_field` (default `updated_at`), `issue_url_template` (optional, with `{id}` or `{key}`), `allow_create` (optional) | `api_key` (an org-wide `km2_` key with `entities:read`, `records:read`, `records:write`; see `docs/trackers.md`, "KM2 issues") |
| `km2` | `base_url`, `default_folder_id` (a KM2 folder id; empty turns saving off), `write_folder_ids` (optional, comma-separated folder ids, at most 50) | `api_key` (a `km2_` key bound to a non-admin KM2 profile; one connection per area) |
| `http` | `base_url`, `auth_header` (optional) | `auth_value` (optional) |
| `slack` | `channel` (needed with a bot token: a channel ID such as `C0123456789`, or `#name`) | `webhook_url` (`https://hooks.slack.com/services/...`) or `bot_token` (`xoxb-...` with `chat:write`) |
| `teams` | | `post_url`: the URL of a Teams workflow made from a "When a Teams webhook request is received" template, allowing Anyone to trigger it; it must be on a `*.powerplatform.com` host (Office 365 connector and old `logic.azure.com` URLs are refused when sending) |
| `email` | `host`, `port` (25, 465, 587 or 2525 unless the host is this machine), `security` (`starttls`, the default; `tls`; or `none`, only to a relay on this machine), `from_address`, `username` (optional), `allowed_domains` (optional, comma-separated: reports may only go to these domains) | `password` (optional) |

Webhook URLs (`webhook_url`, `post_url`) must be https; plain http is accepted only to this machine and is refused when
sending unless its origin is listed in `STUDIO_DELIVERY_ORIGINS` (comma-separated origins; meant for tests). Changing an
email connection's `security` clears its password, like a new host or port. `STUDIO_SLACK_API_URL` replaces
`https://slack.com/api` (tests only). See `docs/delivery.md` for the verified Slack and Teams facts.

`GET /connections/kinds` returns `{ kinds: [{ kind, label, fields: [{ key, label, input, secret, required, hint,
options?, prefix? }], any_secret?, testable }], secrets_ready }` (`prefix`: the value must start with it, such as
`km2_`). Values are single lines; URLs must be http(s) without a user name or
password.

`POST /connections/{id}/test` returns `{ supported, ok, message, status_code?, duration_ms }`. Jira, Linear and
`km2_tracker` connections answer with the account their credentials belong to; `km2_tracker` also checks the
organisation's tracker entities and lists what is missing (see `docs/trackers.md`, which also documents the
people map, the tracking list, tracker watch sources and the run-state tracker tools). For `http`: a `GET` to `base_url` with the auth header, 10-second timeout, redirects reported but not followed,
no proxy, link-local, multicast and unspecified addresses refused, and the body never returned. Connection failures
are reported only as "could not connect" or "timed out". URL settings may not contain a query string or fragment. KM2
connections list the folders their key can see (KM2's `GET /api/v1/knowledge/folders`, which needs `knowledge:read`) and
add `{ folders: [{ id, name, path, parent_id }] (the first 200), folder_count, warnings }`; warnings name save folders the
key cannot see (see "KM2 tools" below). Slack and Teams connections post a test message to their channel, and email
connections send one to the person testing (or, when they have no email, the from address), never to anyone else; the
answer adds `sent_to` for email. Test messages are limited to 6 a minute per person. `mcp` connections connect to the
server and count its tools without keeping them (see "MCP server connections" below). Other kinds answer
`supported: false`.

### MCP server connections

A connection of kind `mcp` points at an outside MCP server (Streamable HTTP; settings `url` and `auth_header`, secret
`auth_value`), whose reading tools agents may use in chat (slice 3e of the
[hand-offs design](agent-chat-handoffs-design.md)). It is not Studio's own `POST /mcp` endpoint. `url` must be https, or
plain http to this machine (`422`). Studio calls the server itself with the saved secret: 25-second limit, no proxy, no
redirects, link-local and cloud metadata addresses refused, at most 4 MB per response. Changing `url` clears the secret
(as for every destination setting) and forgets the connection's tools.

- `GET /connections/{id}/tools` (`connections.view`) lists the tools Discover tools last found:
  `[{ connection_id, name, description, input_schema, effect, enabled, discovered_at, updated_by_name, updated_at }]`.
  `name`, `description` and `input_schema` are the server's words: untrusted, show them as text. Other kinds: `422
  not_mcp`.
- `POST /connections/{id}/tools/discover` (`connections.manage`, all of the connection's areas) calls `tools/list` and
  keeps the result: `{ tools, found, skipped: [string], review: [string] }`. A tool whose description and input schema
  are unchanged keeps its `effect` and `enabled`. On the first discovery a tool takes `reads` when the server's
  `readOnlyHint` says so, `changes` otherwise; later, new and changed tools start as `changes` and are named in `review`
  (the hint is the server's own claim). Tools no longer listed are removed. Names outside `[A-Za-z0-9_.-]{1,128}`, schemas over 16 KB and tools past
  200 are skipped; descriptions are cut to 4,000 characters. 10 a minute per person, shared with Test (`429`). A failure
  is `502 mcp_failed` with the reason (and the server's own error text, untrusted, in `details`).
- `PUT /connections/{id}/tools` (`connections.manage`, all of the connection's areas) body `{ tools: [{ name, effect:
  "reads" | "changes", enabled }] }` sets the named tools; an unknown name refuses the whole change (`422`). Audited as
  `connection_tools_updated` (discovery as `connection_tools_discovered`).

`GET /connections/{id}/km2/folders` lists, for folder pickers, every folder a `km2` connection's saved key can see:
`{ ok, message, status_code?, folders: [{ id, name, path, parent_id }], folder_count, allowed: { default_folder_id,
write_folder_ids }, warnings }`. It needs the connection in your areas and `connections.view` or `schedules.manage`
(report destinations are chosen on schedules; the PMO wizard needs `connections.view`). It counts against the same
per-person budget as testing a KM2 connection (`429` past it). A key without `knowledge:read`, or a KM2 that fails,
answers `200` with `ok: false` and a `message` to show; another kind of connection is `422 not_km2`. The key is never
returned. `GET /crews/{id}/memory/promote/folders` (`state.manage`) answers the same for the KM2 connection promoting
the crew's memories saves through (`409 no_km2` or `km2_ambiguous` as for promoting). Saves are still checked on the
server against the allowed folders.
`select` settings are one of the field's `options`. `toggle` settings are
`"true"` or left out; `uuid` settings are one KM2 folder id and `uuid_list` settings a comma-separated list of them.

## Crew state

A crew's working memory between runs: versioned **notes** and an **action log** (see
`docs/crew-state-and-pmo-design.md`). State belongs to the crew and, for runs started by a schedule, to that schedule;
other runs use the crew-wide state (`schedule_id` null). Everything here needs the crew to be in your areas.

- `GET /crews/{id}/state/notes?schedule_id=&subject=` (`state.view`) returns the latest version of each note without
  content: `[{ schedule_id, schedule_name, subject, name, version, bytes, written_by, run_id, updated_at }]`.
  `schedule_id` empty lists every schedule, `none` only crew-wide notes.
- `GET /crews/{id}/state/notes/versions?schedule_id=&subject=&name=` returns up to 100 `NoteVersion`s, newest first.
  Here `schedule_id` empty or `none` means crew-wide.
- `PUT /crews/{id}/state/notes` (`state.manage`) body `{ schedule_id, subject, name, content, expected_version }` saves
  a new version written by you. `POST /crews/{id}/state/notes/rollback` body
  `{ schedule_id, subject, name, version, expected_version }` saves an old version's content as the newest.
  A stale `expected_version` answers `409 version_conflict`.
- `DELETE /crews/{id}/state/notes?schedule_id=&subject=&name=` (`state.manage`) removes a note and its versions.
- `GET /crews/{id}/state/log?schedule_id=&subject=&action=&limit=&offset=` (`state.view`) pages the action log newest
  first; `subject` and `action` match anywhere, ignoring case. `DELETE /crews/{id}/state/log/{entryId}`
  (`state.manage`). Deletions are audited (`note_deleted`, `action_log_entry_deleted`).
- `GET /crews/{id}/connections` (`connections.view`) lists the connections the crew's tools may use, among those in
  your areas. `PUT /crews/{id}/connections` (`crews.edit` and `connections.view`) body `{ connection_ids }` sets
  them; links to connections outside your areas are kept. A run only gets the linked connections that are enabled
  and share an area with the run.

```ts
type NoteVersion = {
  id: string; crew_id: string; schedule_id: string | null; subject: string; name: string
  version: number; content: string           // at most 64 KB
  run_id: string | null; written_by: string  // "run", or the person's name
  created_at: string
}
type ActionEntry = {
  id: string; crew_id: string; schedule_id: string | null; schedule_name: string
  subject: string                             // such as "jira:PRJ-15"; at most 200 characters
  action: string                              // such as "followup:due_soon"; letters, digits, . _ : / -
  run_id: string | null; dry_run: boolean; details: object; idempotency_key: string | null; at: string
}
```

Note names use letters, digits, `.`, `_`, `:` and `-` (at most 100). Subjects are free text of at most 200
characters without control characters; `""` is the crew (or schedule) itself.

### Memory (the Memory tab)

Soft lessons a crew learns between runs, recalled by similarity before each task (design section 19). Memories belong
to the crew; a **scope** is a path relative to the crew (`/`, `/people/sam`): lowercase parts of `[a-z0-9_-]` (1 to 64
characters each, at most 8 parts and 200 characters), never `.` or `..`; a leading `/crew/<this crew's id>` is removed.
Scope filters match a scope and everything below it, by whole parts. A memory's `content` is 1 to 2,000 characters
(no control characters apart from newlines and tabs), with up to 10 `categories` (`[a-z0-9 _-]{1,40}`, lowercased),
flat `metadata` (at most 20 keys and 4 KB) and an `importance` from 0 to 1 (default 0.5). Studio embeds every memory
with its own embedder (`STUDIO_MEMORY_EMBEDDINGS`); without one memory is unavailable and can only be off. Everything
here needs the crew in your areas (`404` otherwise).

| Call | Permission | Does |
|---|---|---|
| `GET /crews/{id}/memory/settings` | `state.view` | `{ settings: MemorySettings, available, unavailable_reason, embed_model, suggested_llm_id, reembed_pending }`; a crew nobody set up has memory off with the defaults. `reembed_pending` counts the crew's memories still embedded with an older model: a background worker re-embeds 200 a minute across Studio, and until then recall cannot find them |
| `PUT /crews/{id}/memory/settings` | `crews.edit` + `state.manage` | body `MemorySettings`; answers like `GET`. Mode `off` always works; other modes answer `409 memory_unavailable` without an embedder. `422` for bad values, an unknown or unpriced `llm_id`, or (automatic mode or deep recall with `llm_id` null) when no agent of the crew has a priced model. Audited as `memory_settings_changed` |
| `GET /crews/{id}/memory?q=&scope=&status=&pinned=&run_id=&limit=&offset=` | `state.view` | a page of `MemoryRecord` (with `meta`), newest first. With `q`, best matches by meaning first, keeping memories whose text contains `q`; without an embedder (or when it fails) a text match only. `status` is `active` or `pending` (omitted: both) |
| `GET /crews/{id}/memory/scopes` | `state.view` | `[{ scope, count }]` |
| `POST /crews/{id}/memory` | `state.manage` | body `{ content, scope?, categories?, importance?, pinned?, metadata? }`; `201 MemoryRecord`, active at once (never held by review or the pending cap), `source` `user:<name>`. `409 memory_exists` when the scope already holds the same text (ignoring case and spacing), `409 memory_unavailable` without an embedder, `429 memory_crew_full` at 5,000 memories |
| `PATCH /crews/{id}/memory/{mid}` | `state.manage` | body any of `{ content, scope, categories, importance, pinned, metadata, status: "active" }`; `status: "active"` approves a pending memory. A changed content is embedded again. `200 MemoryRecord` |
| `DELETE /crews/{id}/memory/{mid}` | `state.manage` | `204` |
| `POST /crews/{id}/memory/delete` | `state.manage` | body `{ scope_prefix?, contains?, ids?, dry_run }` (at least one filter; they combine). Deletes the matching memories, pinned ones included; `dry_run` only counts. `{ matched, deleted }` (`deleted` 0 in a dry run). Use it to reset a scope or remove everything about a person |
| `POST /crews/{id}/memory/promote` | `state.manage` | body `{ memory_ids (1 to 50), title, folder?, external_ref? }`; see below. `{ outcome, document_id, external_ref, folder_id }` |

```ts
type MemorySettings = {
  mode: "off" | "recall_only" | "explicit" | "automatic"
  llm_id: string | null          // extraction and deep recall; null = the crew's cheapest priced agent model at launch
  recall_depth: "shallow" | "deep"
  recall_limit: number           // 1-20, default 5
  saves_per_run: number          // 0-100, default 20
  retention_days: number | null  // 1-3650, default 365; null keeps memories (pinned ones are always kept)
  review_run_saves: boolean      // default true: every memory a run saves waits as pending until a person approves it
  recency_weight: number; semantic_weight: number; importance_weight: number  // 0-1, defaults 0.3 / 0.5 / 0.2
  recency_half_life_days: number // 1-3650, default 30
  crew_id: string; updated_by_name: string; updated_at: string | null        // read-only
}
type MemoryRecord = {
  id: string; content: string; scope: string; categories: string[]; metadata: object; importance: number
  source: string                 // "run:<run id>" or "user:<name>"
  private: false; pinned: boolean; status: "active" | "pending"
  run_id: string | null; agent_role: string; task_name: string; written_by: string  // "run" or the person's name
  created_at: string; last_accessed: string; updated_at: string; km2_document_id: string | null
}
```

Modes: **recall only** (runs read memories and save none), **explicit** (agents get a Remember tool; one embedding per
save, no LLM call), **automatic** (CrewAI extracts memories from each task's output, one LLM call per task on the
memory model; CrewAI's own "Save to memory" tool is not offered). While `review_run_saves` is on (the default), every
memory a run saves, with the Remember tool or by automatic extraction, is `pending`: runs do not recall it until a
person approves it, and unapproved ones are deleted after 30 days. It is on by default because text a crew reads
(tickets, emails, web pages) can plant instructions that a run saves as a lesson. Memories people add are active at
once. A crew holds at most 500 pending memories (`429 memory_pending_full` for runs past that; people can still add).
Once a person approves or pins a memory, runs cannot change or delete it. Housekeeping deletes unpinned memories
older than the crew's `retention_days` (365 without settings) once an hour. Memory settings are frozen into each run's
spec when it starts (`RunSpec.memory`, left out when memory is off or unavailable, and for chat turns); a memory model
without a price stops the run, like any unpriced model.

**Promote to KM2** turns memories into one KM2 document (a bullet per memory with its scope, date and writer) through
the crew's only enabled KM2 connection, which must be in your areas (`409 no_km2`, `409 km2_ambiguous`). It uses the
`km_save_document` save path: the connection's folders, a `km2:save` action log entry with no run (`details.tool`
`memory_promote`, `details.promoted_by`), 200 saves per connection a day (`429 km2_day_cap`) and 20 promotions per
person a day (`429 km2_promote_cap`). `external_ref` defaults to `crew-memory-<first 8 of the crew id>-<hash of the ids>`;
the same promotion again answers `outcome: "already_saved"`. Pending memories cannot be promoted (`422`,
`memory_ids: approve these memories first`). Field errors are keyed `memory_ids`, `title`, `folder`
or `external_ref`; KM2's refusals answer as for the KM2 tools. The memories get `km2_document_id`. Audited as
`memories_promoted`; changes on the tab as `memory_created`, `memory_updated`, `memory_deleted` and `memories_deleted`
(ids, scopes and counts, never the text).

### Run state API

`/api/run-state/...` is for the runner's state tools, not for people or API keys. When a run starts, Crew Studio
creates a random token for it (`csr_...`), stores only its SHA-256, and passes it to the runner on stdin as
`state_api: { url, token }` (never in the environment). The token works until the run ends plus 2 minutes, and only
for that run's crew and schedule. Calls send `Authorization: Bearer csr_...` and optionally `X-Crew-Agent` (the calling
agent's role, percent-encoded). A missing, unknown or expired token answers `401`; a run whose crew was deleted
answers `409 crew_deleted`, and one started by a schedule that has since been deleted `409 schedule_deleted`. Each run
may make 20 calls a second (burst 60) and write at most 1000 note versions and log entries (`429` after that). Each
note keeps its newest 100 versions.

| Call | Does |
|---|---|
| `GET /run-state/scope` | `{ run_id, crew_id, crew_name, schedule_id, schedule_name, area_ids, connections: [{ id, name, kind }], expires_at, dry_run }` |
| `GET /run-state/notes?subject=` | the run's notes (as the crew list above, without content) |
| `GET /run-state/notes/{name}?subject=` | the latest `NoteVersion`, or `404` |
| `PUT /run-state/notes/{name}` | body `{ subject?, content, expected_version? }`; `409 version_conflict` when stale, `422` over 64 KB |
| `GET /run-state/log/check?subject=&action=&within=` | `{ entry: ActionEntry \| null }`; `within` such as `30m`, `24h`, `7d`, `2w` |
| `POST /run-state/log` | body `{ subject, action, details?, idempotency_key? }`; `201 { entry, duplicate: false }`, or `200 { entry, duplicate: true }` when the key was already recorded |

Every call, including refused ones, adds a `state` event to the run (`{ tool, message, detail?, agent_role? }`, such as
"Notes: read weekly-report v12"), which shows in its events, live stream and journal (entry kind `state`).

A chat turn (a run of kind `chat`) only reads: `PUT /run-state/notes/{name}` and `POST /run-state/log` answer `403`.
It proposes changes instead, and only chat turns may (`403` for every other run):

| Call | Does |
|---|---|
| `POST /run-state/proposals` | body `{ type: "start_work_order" \| "notes_put" \| "tracking_add" \| "tracking_remove" \| "start_handoff" \| "tracker_comment" \| "tracker_suggestion" \| "tracker_issue" \| "km2_save", payload }` (`start_handoff`: see [Hand-offs](#hand-offs); the tracker and KM2 cards: see [Changes agents propose](#changes-agents-propose); a hand-off's own proposals are messages of their own, at most 5 per hand-off (`409 too_many_proposals`), and `start_handoff` from a hand-off is `403 no_chains`) (tracking payloads: see `docs/trackers.md`); attaches a pending proposal to the reply the run is writing and answers `201 { message_id, proposal, state }`. At most one per reply (`409 proposal_exists`); only while the turn is running (`409 turn_finished`); `403` when the turn was not given the tool that makes it; `422` for an unknown type or a bad payload, and for a `start_work_order` whose `inputs` leave out a required input that has no default or name a key the crew does not have (the details name the keys, such as `inputs.company: is required`, so the agent can retry) |

### Memory during a run

The runner's `StudioMemoryStorage` (CrewAI's `StorageBackend`) and `StudioEmbedder` call these with the run token;
scopes and records follow the rules of the Memory tab above. The run's frozen memory settings decide what it may do:
every call answers `403 memory_off` when memory is off for the run (always for chat turns), writes answer
`403 memory_read_only` in recall-only mode, and anything that embeds answers `503 memory_unavailable` when the server
has no embedder or the provider failed. Searches, saves, changes, deletes and refusals add a journal line (such as
"Memory: recalled 4 of 37"); embeds, reads, counts and touches do not.

| Call | Does |
|---|---|
| `POST /run-state/memory/embed` | body `{ texts (1 to 32), kind: "query" \| "document" }`; `{ model, dimensions, vectors }`. At most 300 texts per run go to the provider (`429 memory_embed_cap`); repeats come from a per-run cache (dropped when the run ends) and are free, and texts the provider failed on are not counted |
| `POST /run-state/memory/search` | body `{ embedding, scope_prefix?, categories?, metadata_filter?, limit? (1-50, default 10), min_score? }`; `{ matches: [{ record: MemoryRecord, score }] }`, best first, `score` the cosine similarity clamped to 0..1. Only active memories and those this run saved, embedded with the current model, scoring at least the larger of `min_score` and the embedder's relevance floor (0.28 for OpenAI text-embedding-3-small, 0.5 for Gemini, 0.3 otherwise). The runner asks for 4 times the recall limit (at most 50) and lets CrewAI's ranking weights pick the top ones |
| `POST /run-state/memory/records` | body `{ records: [{ id?, content, scope?, categories?, metadata?, importance? }] (1 to 20) }`; `201 { saved: [{ id, duplicate, status, skipped? }], skipped, remaining_saves }`, one `saved` entry per record in order. Studio embeds the content (cache first) and keeps a sent UUID when it is free. Duplicates are looked for only among memories this run may see (active ones and its own): the same text in the same scope, or a near-duplicate in the same scope (cosine 0.95 or more for OpenAI, 0.98 for Gemini, 0.97 otherwise), is touched instead (`duplicate: true`). A skipped record has `id` and `status` empty and `skipped` set to `save_cap` (past the crew's `saves_per_run`), `crew_full` (5,000 memories), `pending_full` (500 waiting for review) or `not_saved` (the same text as a memory the run may not see; nothing more is said); `skipped` counts them. When no record could be saved for a cap: `429 memory_save_cap`, `429 memory_crew_full` or `429 memory_pending_full`. `status` is `pending` while the crew's `review_run_saves` is on, otherwise `active`. `metadata.agent` and `metadata.task` fill `agent_role` and `task_name`. Content and metadata strings may not hold control, line or paragraph separator or format characters (bidi controls, zero-width characters): `422` |
| `GET /run-state/memory/records?scope_prefix=&limit=&offset=` | `{ records }`, newest first (limit up to 200) |
| `GET /run-state/memory/records/{id}` | a `MemoryRecord` the run may see, or `404` |
| `PATCH /run-state/memory/records/{id}` | body any of `{ content, scope, categories, metadata, importance }`; only memories this run saved (`403 memory_not_owned`), and never one a person pinned or (while review is on) approved (`403 memory_locked`); a changed content is embedded again |
| `GET /run-state/memory/scopes?parent=` | `{ scopes }`: the scopes one level below `parent` |
| `GET /run-state/memory/scope-info?scope=` | `{ path, record_count, categories, oldest_record, newest_record, child_scopes }` |
| `GET /run-state/memory/categories?scope_prefix=` | `{ categories: { name: count } }` |
| `GET /run-state/memory/count?scope_prefix=` | `{ count }` |
| `POST /run-state/memory/delete` | body `{ record_ids?, scope_prefix?, categories?, older_than?, metadata_filter? }`; deletes only unpinned memories this run saved, and while review is on only those still pending (also CrewAI's `reset`): `{ deleted }` |
| `POST /run-state/memory/touch` | body `{ record_ids }` (at most 200); `{ touched }` |

Runs see active memories and their own pending ones; another run's pending memories stay hidden until approved.

### KM2 tools

The run-state API also carries the KM2 tools (design section 9). Crew Studio calls KM2's public v1 API itself with the
connection's key (`Authorization: Bearer km2_...`), so the key never reaches a run. The run's KM2 connection is the one
`km2` connection in `GET /run-state/scope`'s `connections`: with none the calls answer `409 no_km2`, with more than one
`409 km2_ambiguous`. Agents never choose a key.

| Call | Does |
|---|---|
| `POST /run-state/km2/search` | body `{ query, top_k? }` (1 to 20, default 5); KM2 `POST /api/v1/search`; `{ connection, query, hits: [{ document_id, document_title, text, score, chunk_order }], total }` |
| `POST /run-state/km2/ask` | body `{ question }`; KM2 `POST /api/v1/search/chat`; `{ connection, question, answer, sources }` (sources keep their scalar fields only, cut short) |
| `POST /run-state/mcp/{connection_id}/tools/{name}` | agent chat only (see "Agent chat" below): body `{ arguments }` (a JSON object, at most 32 KB); `{ connection, tool, content, is_error, truncated }` with the tool's text cut to 64 KB |
| `POST /run-state/km2/documents` | body `{ title, content, external_ref, folder?, metadata?, source_file? }`; KM2 `POST /api/v1/knowledge/documents` (JSON); `201` when KM2 created the document, `200` for `updated`, `unchanged`, `metadata_updated` or `reprocessed`: `{ saved, outcome, document_id, processing_status, external_ref, folder_id, logged, message }` |

Saves: `folder` is empty (the connection's `default_folder_id`), a folder id, or a folder name or path (such as
`Projects/Atlas`) among the connection's save folders. An id outside them is refused before KM2 is called; a name is
looked up in KM2's folder list (which needs `knowledge:read`; without it `400 km2_folder_names`) and matched among them
only. Refusals: `403 km2_folder_not_allowed`, `400 km2_folder_ambiguous`, and `km2_no_folder` (`409` when the connection
has no save folders, `400` when it has no default and none was named). `external_ref` is 1 to 190 characters of letters,
digits, `.`, `_`, `:` and `-`, starting with a letter or digit; content is up to 1 MB of text; `metadata` is flat
(strings, numbers, booleans, null or lists of those, at most 50 keys and 8 KiB, without KM2's reserved keys). Crew Studio
adds `source: "crew-studio"`, `crew`, `run_id` and `schedule`.

Every save is reserved in the action log as `km2:save` (subject `km2:<external_ref>`, outcome `sending`) before anything
is sent, under a short per-run lock, and finished with KM2's outcome and document id, plus the connection, folder, title,
size and content hash. A save KM2 refused is removed; one whose answer never came (KM2 failed or timed out after it was
sent) is kept with outcome `unknown`, counts, and may be sent again in the same run. In a dry run nothing is saved:
`200 { dry_run: true, would_save, logged }` with a dry-run entry (naming a folder by name still reads KM2's folder list).
Limits: 10 saves per run (`429 km2_save_cap`), 200 real saves per connection in 24 hours (`429 km2_day_cap`), and saves
count toward the run's 1,000 state writes. The same document twice in one run is sent once: `200 { saved, repeated: true,
outcome, document_id, logged }` (or `409 km2_in_flight` while the first call is still sending). Agents cannot record
`km2:` actions or idempotency keys with `POST /run-state/log` (`422`).

KM2's refusals keep their status (`403 km2_forbidden`, `404 km2_not_found`, `409 km2_conflict`, `413 too_large`,
`422 km2_invalid`, `429 km2_rate_limited`; anything else, including a redirect, `502 km2_failed`), with KM2's reason in
`details` and never the key (anything that looks like a KM2 key is removed from KM2's text). Rate limits answered with
`Retry-After` of up to 20 seconds are waited out (twice at most). Reads are limited per run: 30 searches a minute, 6
questions a minute and 2 questions open at once (`429`), besides a per-connection limit for all runs. Chat turns may call
search and ask only when given `km_search` / `km_ask`, and never save (`403`).

### Report delivery (deliver_report)

`POST /run-state/reports` body `{ project, markdown, cadence? }` (Markdown up to 64 KB; chat turns get `403`). `cadence`
is `weekly` (the default) or `daily`; anything else is `400 bad_cadence`. A weekly report covers the ISO week and is
titled `<project> status, <YYYY>-W<ww>`; a daily report covers the local date in the schedule's time zone and is titled
`<project> daily report, <YYYY-MM-DD>`. Crew Studio keeps the report as the project's `weekly-report` note, or a daily
report as its `daily-report` note (subject `project:<slug>`, in the run's scope; a daily report never touches the
weekly note), and delivers it to the
work order output and to every destination of the run's schedule (none for a run no schedule started), several at a
time, each bounded to 30 seconds and finished even if the caller gives up. Answer `201`:

```ts
{ report_id, project, week /* ISO week in the schedule's time zone, e.g. 2026-W41 */, cadence /* weekly | daily */,
  period /* the ISO week, or a daily report's date such as 2026-10-06 */, dry_run,
  deliveries: [{ destination: "work_order" | "slack" | "teams" | "email" | "km2", target, status: "sent" | "would_send" | "failed", message }], message }
```

`<slug>` is the project name in lower-case ASCII letters and digits joined by dashes (`Project Atlas` → `project-atlas`).
Each delivery is recorded in `report_deliveries` with where it went and, once sent, in the action log as
`report:<destination>` (subject `project:<slug>`, idempotency key `report:<report id>:<position>`). KM2 gets the report
through the `km_save_document` save path (the connection's allowed folder, a `km2:save` reservation with its caps of 10
per run and 200 per connection a day, the crew's only KM2 connection) as a document with `external_ref`
`<slug>-weekly-<YYYY>-W<ww>` (daily: `<slug>-daily-<YYYY-MM-DD>`) and metadata `{ source, crew, run_id, schedule, kind:
weekly-report | daily-report, project, week, period, report_id }` (daily reports also `date`). Slack, Teams and email
messages carry the report's title, so a daily one says "daily report". Dry run: nothing is sent; every destination is
`would_send` with a dry-run action log entry. The same report again in a run answers `200` and sends nothing; other text
for the same project and cadence is `409 report_exists` (a run may deliver one weekly and one daily report per project). Caps: 20 reports and 60 sends per run (`429 report_cap`); reports count
toward the run's state writes. A delivery whose connection was deleted, turned off, unlinked from the crew or moved out
of its areas fails with that reason. Writing the report back to the tracker is not built (design section 12).

`markdown` is the report's **narrative** (on track or not, the critical path, risks, asks); the tool's description asks
the model not to re-list items. Before delivering, Crew Studio reads the project's tracked items itself (the crew's
tracking items for that project and their children, through the watch's tracking-list reader, at most 20 seconds) and
stores them with the report as `snapshot`. The deliveries are built from it: the email is `multipart/alternative` with
the data-plus-narrative Markdown as text and a self-contained HTML page (inline CSS and SVG, no scripts or remote
assets) as HTML; Slack and Teams get a summary (counts with deltas, the five most overdue items, the narrative); KM2 gets
the data as Markdown tables with the narrative. Links in the narrative that are not http(s), and images, keep only their
text. When the tracker cannot be read the report is stored with `snapshot: null` and sent as written. Either way each
delivery's action log entry has `details.snapshot`: `"included"` or `"left out: <why>"`.

```ts
type Snapshot = {
  version: 1, generated_at, timezone, today /* local date in the schedule's time zone */, cadence, project,
  previous_report_id?, truncated /* items were dropped to fit 512 KB; counts still cover all */,
  items: [{ key, title, url?, assignee?, status, status_category, due_date?, labels, blocked, parent?, epic?, updated_at? }] /* open items, ≤ 400 */,
  kpis: { open, overdue, due_today, due_7, blocked, unassigned_soon, done_since_prev /* null on the first report */ },
  deltas: Kpis | null /* against the previous report of the same project and cadence */,
  sections: { overdue: [{ key, days_late }], due_today: [key], due_next_7: [key], blocked: [key], unassigned_soon: [key],
              by_person: [{ name, open, overdue, due_7 }] } /* ≤ 100 rows each, ≤ 50 people */,
  changes: { newly_overdue: [key], finished: [{ key, title }], slipped: [{ key, title?, from, to }],
             reassigned: [{ key, title?, from, to }], new: [key] } | null,
  timeline: [{ date, count }] /* the next 14 days */, status_mix: { to_do, in_progress, done, canceled },
  trend: [{ date } & Kpis] /* up to 14 reports, oldest first */,
}
```

`GET /work-orders/{id}/reports` (`work_orders.view`) lists the work order's reports with their deliveries
(`{ reports: [Report & { retryable }] }`; each report has `cadence`, `period` and `snapshot` besides `week`), and `POST /work-orders/{id}/reports/retry` (`work_orders.create`; 6 a minute per
person) sends again only the deliveries that failed (or were left `pending` for over 10 minutes), never a dry-run
report: `{ reports, retried, sent }`. A delivery already in the action log is marked sent without sending. Audited as
`report_deliveries_retried`.

`GET /work-orders/{id}/reports/{reportId}/html` (`work_orders.view`, with the same work order access check as the list,
otherwise `404`) downloads one report as the self-contained HTML page (a report without a snapshot shows its Markdown),
as an attachment named `<slug>-<cadence>-<period>.html` with `Content-Security-Policy: default-src 'none'; style-src
'unsafe-inline'; img-src data:; sandbox`, `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`.

### PMO setup

`GET /pmo/setup` and `POST /pmo/setup` need `crews.create`, `crews.edit`, `agents.create`, `tasks.create`, `schedules.manage`,
`work_orders.create` and `connections.view`. `GET` offers the enabled tracker connections (Jira, Linear, `km2_tracker`: `trackers`) and
slack, teams, email and km2 connections (`destinations`) you can see, every LLM, and the template agents' LLMs as
suggestions (`409 pmo_template_missing` without the PMO template from migration 0016). `POST` body
`{ name?, area_ids, tracker_connection_id, timezone, coordinator_llm_id?, reporter_llm_id?, destinations?, enabled? }`
creates, in one transaction: copies of the template's two agents and tasks (named after `name`, default `Team PMO`), the
crew with the chosen connections and chat on (the coordinator as front desk, $1 a day per conversation), and two
schedules running as you in `timezone` with `inputs.run_tasks` set to their task: `<name>: daily sweep`
(`0 7 * * 1-5`; a `tracking_list` watch with new comments, `changed_or_followups`, the default ladder below and dry run on)
and `<name>: weekly report` (`0 8 * * 1`; run `always`, dry run on, the destinations). Ladder: `due_date`; skip
`status_category` done or canceled; `due_soon -3d owner`, `due_today 0d owner`, `overdue_escalate +2d lead`,
`overdue_weekly +9d lead repeat 7d`; quiet days Sat and Sun, in `timezone`. Answer `201 { crew, agents, tasks, schedules }`.
A name that is already taken by a crew, agent, task or schedule the install would create (plain `PMO` names its
coordinator "PMO Coordinator", like the template) is `422` with the detail
`name: an agent named "PMO Coordinator" already exists; choose another name`. In `GET`, an LLM whose provider key is
not set carries a `warning`.
Audited as `pmo_installed`.

## Crew chat

People talk to a crew instead of filling in forms. A crew with chat on names one of its agents as the **front
desk**; each message a person sends is a short single-agent run of kind `chat` (journal, cost and budget as for any
run, as the person). The front desk gets only these tools, whatever its agent is set up with, and of them only the
ones the person chatting could use themselves: Read Note, List Notes and Check Action Log (`state.view`), Start Work
Order (`work_orders.create`) and Propose Note Change (`state.view` and `state.manage`); the crew's notes are given to
a turn only for people with `state.view`. A chat turn that calls a read it was not given gets `403`. Each turn is given the conversation's latest 20 messages
and the crew's crew-wide notes (each cut to 3,000 characters, 12,000 in all), not the whole history. Everything needs
`crews.chat` and a person (service accounts get `403`); conversations belong to one person and one crew, and nobody
else can see or use them (others get `404`), not even through the runs API.

`GET /crews/{id}/chat` (`crews.view`) and `PUT /crews/{id}/chat` (`crews.edit`):

```ts
type ChatSettings = {
  crew_id: string
  enabled: boolean
  front_desk_agent_id: string | null   // one of the crew's agents; required when enabled
  llm_id: string | null                // a cheaper model for chat; null keeps the agent's own
  daily_budget_usd: number             // what one conversation may spend in any 24 hours; default 1.0, at most 100
  person_daily_budget_usd: number      // what one person may spend on the crew's chat in any 24 hours over all their
                                       // conversations (deleted ones too); a turn still queued or running counts
                                       // at its whole budget; default 5.0, at most 1000
  updated_by_name: string
  updated_at: string | null
}
```

| Call | Does |
|---|---|
| `GET /chat/crews` | crews in your areas you can chat with: `[{ id, name, description, front_desk_name, front_desk_role }]` |
| `GET /chat/conversations?crew_id=&agent_id=` | your conversations with crews and agents, most recently used first (at most 100): `[{ id, crew_id, crew_name, agent_id, agent_name, user_id, title, created_at, updated_at }]` (`crew_id` is null for a conversation with an agent; see [Agent chat](#agent-chat)) |
| `POST /chat/conversations` | body `{ crew_id, content }` (or `{ agent_id, content }` for [Agent chat](#agent-chat)): starts a conversation with its first message; `201 ConversationView` |
| `GET /chat/conversations/{id}` | `ConversationView` |
| `POST /chat/conversations/{id}/messages` | body `{ content }` (at most 4,000 characters); `201 ConversationView`; `409 chat_turn_active` while the last reply is being written, `409 chat_budget_used` once the conversation's daily budget is spent, `409 chat_person_budget_used` once the person's, `422` when chat is off |
| `POST /chat/conversations/{id}/compact` | body `{}`: compacts the conversation (below); `201 ConversationView` with the summary message, written like a reply; `409 nothing_to_compact` with fewer than two messages since the last summary, `409 conversation_changed` if a message arrived while it started, and the same `409`s as sending a message |
| `DELETE /chat/conversations/{id}` | deletes it (`409 chat_turn_active` while a reply is being written, `409 handoff_active` while a hand-off runs); its chat and hand-off runs are kept and still count towards the person's budget |
| `POST /chat/messages/{id}/confirm` | applies the reply's pending proposal; `200 ChatMessage` |
| `POST /chat/messages/{id}/decline` | declines it |
| `POST /chat/messages/{id}/undo` | undoes a confirmed change that can be undone |

```ts
type ConversationView = {
  conversation: Conversation
  messages: ChatMessage[]          // the latest 200, oldest first
  budget: { daily_budget_usd: number; spent_last_24h_usd: number; person_daily_budget_usd: number;
            person_spent_last_24h_usd: number; remaining_usd: number }   // remaining: the smaller of the two
  chat_enabled: boolean            // false once chat is turned off: readable, not continued
  handoffs: Handoff[]              // agent chat: the latest 20 hand-offs, newest first (see Hand-offs); [] with a crew
  questions: HumanQuestion[]       // agent chat: the open questions its hand-offs wait on you to answer, soonest due
                                   // first (see Questions during a hand-off); [] with a crew
}
type ChatMessage = {
  id: string; conversation_id: string; role: 'user' | 'assistant' | 'summary'   // summary: a compaction (below)
  content: string                  // a reply is empty until its run succeeds; poll while run_status is queued or running
  proposal: { type: 'start_work_order' | 'notes_put' | 'tracking_add' | 'tracking_remove' | 'start_handoff' | 'tracker_comment' |
              'tracker_suggestion' | 'tracker_issue' | 'km2_save'; summary: string; payload: object } | null
  proposal_state: 'pending' | 'confirmed' | 'declined' | 'expired' | null   // expired 24 hours after proposed_at
  proposed_at: string | null
  proposal_result: object | null   // { work_order_id, work_order_number, run_id }, { name, subject, version, previous_version, undoable },
                                   // for start_handoff { handoff_id, run_id, message_id }, for the tracker cards
                                   // { connection, issue, comment_id?, url }, for km2_save { connection, document_id,
                                   // outcome, external_ref, folder_id }
  decided_by_name: string; decided_at: string | null; undone_at: string | null
  run_id: string | null; run_status: RunStatus | null; run_error?: string; cost_usd: number | null
  run_kind?: 'chat' | 'handoff'    // the kind of run writing an assistant message; left out without a run
  handoff?: { id, title, worker_name, worker_role, deadline_at, budget_usd, timeout_minutes, started_at,
              open_question_id }   // on a hand-off's messages; open_question_id: the question its run waits on you for, or null
  handoff_proposal?: boolean       // a card a hand-off proposed while it worked (no run; content is the card's summary),
                                   // shown before its result
  created_at: string
}
```

**Files with a message.** `POST /chat/conversations`, `POST /chat/conversations/{id}/messages` and `POST
/work-orders/{id}/conversation/messages` also take `multipart/form-data` (with the `X-Requested-With` header, as for
uploads): a `content` field (and `crew_id` or `agent_id` to start a conversation) and up to 4 `file` parts: PNG, JPEG, GIF, WebP
or PDF, at most 10 MB each and 20 MB together (`415 unsupported_file`, `413 too_large`, `422` for more files). A
conversation holds at most 40 files and 200 MB (`409 chat_files_full`). The text may be empty when files are sent; it
is then stored as "(See the attached files: names.)". The files belong to the message (`attachments` on `ChatMessage`: `{ id,
filename, content_type, size_bytes }[]`), only its owner can open them with `GET /attachments/{id}/content`, and they
go when the conversation is deleted. The turn's model is given them when it can see images (the LLM's vision
setting); otherwise it is told it cannot look at them. Later turns get the message's text, not its files.

**Compacting.** Each turn resends the conversation (its latest 20 messages). Compacting runs one short turn (a chat
run with no tools, on the chat model, counted against the chat budgets) that summarises the conversation since the
last summary, folding that summary in. The summary is a message of role `summary`; from then on each turn is given the
latest summary and up to 20 messages after it instead of the earlier messages, and so is a revision that includes the
conversation. A failed compaction changes nothing: turns keep getting the messages as before.

**Confirming.** Only the conversation's owner, signed in to Crew Studio, can confirm, decline or undo (`403` with an
API key, so a script acting for someone cannot approve what the AI proposed); another person's message is `404`. A
proposal expires 24 hours after it was made (`409 proposal_expired`; reading the conversation shows it as `expired`).
Deciding twice is `409 proposal_decided`; a message without one `409 no_proposal`; undoing what cannot be undone
`409 not_undoable`, twice `409 already_undone`. `start_work_order` (payload `{ title, brief, inputs? }`, `inputs` by
key) creates and submits a work order of the crew as that person (`source: "chat"`, `source_id` = the conversation; needs `work_orders.create`). `notes_put`
(payload `{ name, subject, content, base_version }`) saves a crew-wide note with the person's name, refused with
`409 version_conflict` if the note changed since the front desk read it (confirming and undoing it need
`state.view` and `state.manage`, as editing notes on the State tab does); undo restores the version before it (or
removes a note the change created), refused if the note changed again. A failed confirm leaves the proposal pending.

## Agent chat

People can also talk to one **agent** directly ([design](agent-chat-design.md)). The agent may bring in its
**specialists** (other agents) inside the same turn, and propose a work order on one of the **crews it may start**.
Each message is one chat run (kind `chat`, `crew_id` null, named after the agent) whose spec holds the agent first and
its specialists after it (`RunSpec.chat.agent`: `{ agent_id, specialists: [{ agent_id, name, role }], crews: [{ id,
name, description, inputs: [{ key, label, required }] }] }`). With specialists the agent gets CrewAI's Ask question to
coworker and Delegate work to coworker tools; specialists never hand work on and get at most 3 steps (`max_iter`) in a
turn. Every agent keeps only those of its own tools whose catalog `effect` is `reads` and whose `needs` the turn meets
(see [Tools](#tools-read-only-catalog)): `tracker` and `km2` are met when the agent's `connection_ids` hold an enabled
connection of that kind (Jira, Linear or KM2 issues; KM2) that shares an area with the person and the agent. The agent
also gets Start Crew Work Order when it has crews and the person holds `work_orders.create`. Only specialists, crews and
connections the person can see are included; the connections an agent has a tool for are frozen into
`RunSpec.chat.connections` (`[{ id, name, kind }]`). Specialists use their own models and need a price for them (`422`
otherwise); their cost counts towards the turn's budget. A turn has no notes. Its run token may call only `POST
/run-state/proposals`, `GET /run-state/control`, and the tracker and KM2 reads (`GET /run-state/trackers`,
`/trackers/projects`, `/trackers/issues`, `/trackers/issues/{ref}`, `/trackers/issues/{ref}/children`; `POST
/run-state/km2/search`, `/km2/ask`) when one of its agents was given the tool; those reach only the frozen connections
that are still enabled, that the person can still see and that still share an area with the agent (`409 no_tracker` /
`no_km2` otherwise), and `POST /run-state/mcp/{connection_id}/tools/{name}` (below). A path with encoded characters is
`403`, and so is everything else. These reads need only
`crews.chat`, not `state.view` as in crew chat. In a turn where any agent has a tracker or KM2 read, no agent gets
`scrape_website` (tracker and KM2 text could otherwise be sent to a URL it names); `google_search` stays. A crew chat turn carries its crew's inputs the same way, in `RunSpec.chat.crew_inputs` (the frozen crew has
none, as a turn is not a run of it).

**MCP read tools** (slice 3e): an `mcp` connection in `connection_ids` that is enabled and shares an area with the person
and the agent brings its tools that are enabled and marked `reads` (at most 40 per turn), frozen into
`RunSpec.chat.mcp_tools` (`[{ connection_id, connection_name, name, description, input_schema }]`) with the connection
in `RunSpec.chat.connections`. The runner gives them to the lead only (in turns, and in hand-offs it works itself), and
no agent of such a turn gets `scrape_website`. `POST /run-state/mcp/{connection_id}/tools/{name}` body `{ arguments }`
needs the tool in the frozen list (`403` otherwise), the connection still enabled, visible to the person and shared with
the agent (`409 no_mcp`), and the tool still enabled and `reads` (`403 mcp_tool_off`). Studio calls the server with the
saved secret, which never reaches the run, and answers `{ connection, tool, content, is_error, truncated }` (content cut
to 64 KB, the server's words); a failure is `502 mcp_failed`. Every call is journalled with its argument names and size,
never their values. 30 calls a minute per run, 120 a minute and 4 at once per connection (`429`). The runner gives the
model only the structure of each input schema (property names of plain characters, descriptions cut to 120 characters,
no titles, examples or defaults). The allowlist matches only a lower-case uuid and a tool name of `[A-Za-z0-9_.-]{1,128}`.

`GET /agents/{id}/chat` (`agents.view`) and `PUT /agents/{id}/chat` (`agents.edit`):

```ts
type AgentChatSettings = {
  agent_id: string
  enabled: boolean
  llm_id: string | null                // a cheaper model for the agent in chat; specialists keep their own
  specialist_ids: string[]             // other agents it may ask, at most 8; each with a role of its own (CrewAI picks
                                       // coworkers by role), and only agents in your areas may be added
  crew_ids: string[]                   // crews it may propose a work order on, at most 10, in your areas
  connection_ids: string[]             // connections its tracker, KM2 and MCP reads may use, at most 10, in your areas;
                                       // only jira, linear, km2_tracker, km2 and mcp, at most one km2 (`422` otherwise); adding
                                       // one needs `connections.view` (`403`), keeping saved ones does not
  daily_budget_usd: number             // as for crews: default 1.0, at most 100
  person_daily_budget_usd: number      // what one person may spend chatting with this agent in 24 hours; default 5.0
  handoffs_enabled: boolean            // the agent may propose hand-offs (below); default false
  handoff_budget_usd: number           // what one hand-off may spend; default 0.5, at most 100
  handoff_timeout_minutes: number      // how long one may run; default 20, 1 to 180
  max_active_handoffs: number          // hand-offs queued or running at once per conversation; default 2, 1 to 5
  questions_enabled: boolean           // during a hand-off the lead may ask the person (below); default false
  question_rules: string               // when to ask, plain language, at most 4,000 characters; required when enabled
  question_timeout_minutes: number     // how long a question waits; default 30, 1 to 180 (never past the hand-off's
                                       // deadline less a minute)
  question_on_timeout: 'proceed' | 'fail'   // default proceed: carry on and state the assumption; fail: stop the hand-off
  max_questions: number                // questions to the person per hand-off; default 3, 1 to 20
  specialists_may_propose: boolean     // its specialists get the proposing counterparts of their tracker and KM2 writes
                                       // too (the agent always does); default false
  updated_by_name: string
  updated_at: string | null
  hidden_specialists: number           // read only: specialists outside your areas, left out of specialist_ids
  hidden_crews: number                 // read only: crews outside your areas, left out of crew_ids
  hidden_connections: number           // read only: connections outside your areas, left out of connection_ids
}
```

`specialist_ids`, `crew_ids` and `connection_ids` list only the agents, crews and connections in your areas. Saving keeps the ones outside them
where they were, so you can save what you see without dropping them.

| Call | Does |
|---|---|
| `GET /chat/agents` | agents in your areas with chat on: `[{ id, name, role, goal, specialists: [{ id, name, role }], crews: [{ id, name }] }]` (only those you can see) |
| `POST /chat/conversations` | body `{ agent_id, content }`; `422` with both `agent_id` and `crew_id` |

The other conversation and proposal calls are the crew chat ones. A `start_work_order` proposal from agent chat names
its crew: the tool sends `{ crew, title, brief, inputs? }` with the crew's name, and Studio stores `{ crew_id,
crew_name, title, brief, inputs? }` (`422` for a crew not offered to the turn; a crew chat may not name a crew). In
both kinds of chat the inputs are checked against the crew's current inputs when proposed, as for a work order: a
required input without a default must be given, and every key must be one of the crew's (`422` naming the keys).
Confirming checks again that chat with the agent is on (`409 chat_off`) and the crew still on its list (`409
crew_not_offered`) and in your areas. The per-person budget counts the runs (`runs.chat_agent_id`), so deleting a
conversation does not reset it, and a turn still queued or running counts at its whole budget. A turn is refused
(`422`) when a specialist has no priced model, a kept tool needs a key the server lacks, the search model has no
price, or the provider key of the agent's or a specialist's model (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY` or
`GEMINI_API_KEY`) is not set (the same check that only warns for a crew run). Deleting an agent that is a specialist, or a crew on an agent's list, is `409` naming the agent.

### Hand-offs

With `handoffs_enabled`, the agent of a turn also gets **Hand off work** (tool key `start_handoff`), which proposes
handing a piece of work too long for one reply to a background run ([design](agent-chat-handoffs-design.md), slice
3b). The tool sends `{ worker, title, brief }`: the worker is the agent itself or one of the turn's specialists, named by
role, name or id (`422` otherwise, naming the roles). Studio stores `{ worker_agent_id, worker_name, worker_role, title,
brief, budget_usd, timeout_minutes }` (the last two are the agent's limits when proposed, for the card); the summary is
`Hand off to <worker>: <title>`. Nothing runs until the person confirms.

Confirming (`POST /chat/messages/{id}/confirm`, the owner, signed in) waits for the reply carrying the card to finish
(`409 chat_turn_active` before: until then it holds the budget the hand-off needs), and checks again that chat and hand-offs are on for the
agent (`409 chat_off`, `409 handoffs_off`) and that the worker is still the agent or on its specialist list and in your
areas (`409 worker_not_offered`). It then freezes a fresh turn of the agent with the conversation so far and starts a
run of kind `handoff` (`crew_id` null, `chat_agent_id` the agent, `created_by` you) whose spec carries
`chat.handoff: { id, title, brief, worker_agent_id, worker_name, worker_role, deadline_at, timeout_minutes,
request_message_id }` and an empty `message`; the agent loses Hand off work in it, and each agent gets its own steps up
to 25. At the same time it stores the hand-off and an empty **result message** (an assistant message with the run's
`run_id`, `run_kind: "handoff"` and `handoff`), filled from the run's output when it succeeds, as a reply is. The run's
budget is the smallest of `handoff_budget_usd` and what is left of the conversation's and the person's daily budgets
less $0.01, so a turn can still run beside it (`409 chat_budget_used` / `chat_person_budget_used` when less than $0.02
is left); while queued or running it counts at that whole budget in both. At most `max_active_handoffs` run at once per conversation, and 5 per person and agent (`409
handoffs_busy`). A refused confirm leaves the card pending. The result is `{ handoff_id, run_id, message_id }`.

A running hand-off does not block the next message. Each turn's spec lists the conversation's hand-offs still running
(`chat.pending_handoffs: [{ title, worker_name, worker_role, started_at, deadline_at }]`), and a finished result is in
later turns' history with `kind: "handoff_result"`, `title` and `worker`. A conversation cannot be deleted (`409
handoff_active`) or compacted (`409 handoff_active`) while one of its hand-offs runs. The run stops at `deadline_at`,
`timeout_minutes` after the run starts (waiting for a slot does not count; the runner refuses further LLM calls and the
run fails with "The hand-off ran past its N-minute limit"); the server kills a stuck process two minutes later. A
hand-off compacts its agents' context by itself; `POST /runs/{id}/compact` is `409 chat_turn` for it, as for a chat
turn. Deleting a hand-off's run (after a day) keeps the hand-off and its result message (`run_id` and `run_status`
become null), as a chat reply is kept. Each card a hand-off proposes is an assistant message of its own (`handoff` set,
`handoff_proposal: true`, no run, `content` the card's summary), at most 5 per hand-off (`409 too_many_proposals`), and
says it was proposed during the hand-off; the page shows them just before the hand-off's result. Later turns get them in
their history with `kind: "handoff_proposal"`. A hand-off run's token has the same allowlist as an agent chat turn (which
includes `POST /run-state/questions` and `GET /run-state/questions/{id}`, refused unless the run is a hand-off with
questions on), and `start_handoff` from it is `403 no_chains`.

```ts
type Handoff = {
  id: string; conversation_id: string; request_message_id: string | null; result_message_id: string
  worker_agent_id: string | null; worker_name: string; worker_role: string
  title: string; brief: string; deadline_at: string; created_at: string
  budget_usd: number; timeout_minutes: number
  run_id: string | null; run_status: RunStatus | null   // null once the run was deleted
  run_error?: string; cost_usd: number | null; started_at: string | null; finished_at: string | null
  open_question_id: string | null      // the question its run waits on you to answer, if any
}
```

| Call | Does |
|---|---|
| `GET /chat/handoffs/{id}` | one `Handoff` of your own conversation (`404` for anyone else's) |
| `POST /chat/handoffs/{id}/cancel` | body `{}`: cancels its run as the Runs page does; `200 Handoff`; only the conversation's owner, signed in (`403` with an API key); `409 handoff_finished` once it is over. People with `runs.cancel` can also cancel it from the Runs page |
| `GET /chat/conversations/{id}/questions` | the questions your conversation's hand-offs asked you, open ones first (soonest due first), then the others newest first; at most 200 (`404` for anyone else's) |

### Changes agents propose

In agent chat no agent posts to a tracker or saves to KM2 itself ([design](agent-chat-handoffs-design.md), slice 3d).
When the agent has `tracker_comment`, `tracker_suggest_change`, `tracker_create_issue` or `km_save_document`, its turn
gets the proposing counterpart instead (`propose_tracker_comment`, `propose_tracker_suggestion`, `propose_tracker_issue`,
`propose_km_save`), when a listed connection of that kind is frozen into the turn and the person holds `crews.run`; its
specialists get theirs only with `specialists_may_propose`. Each makes a card through `POST /run-state/proposals`:

| Type | The tool sends | Summary |
|---|---|---|
| `tracker_comment` | `{ connection, issue, text }` | `Comment on PRJ-10` |
| `tracker_suggestion` | `{ connection, issue, field, value, reason }` (fields as for `tracker_suggest_change`) | `Suggest a change on PRJ-10: due date to 2026-11-01` |
| `tracker_issue` | `{ connection, project, title, description, parent }`; `409 create_off` unless the connection lets crews create issues | `Create an issue in PRJ: <title>` |
| `km2_save` | `{ connection, title, external_ref, content, folder }`; content at most 100,000 bytes; the external ref is stored under `agent-chat-<12 hex characters of the conversation id>-`, so a card can only replace a document an earlier card of the same conversation saved (confirming refuses any other) | `Save to KM2: <title>` |

`connection` is the name or id of one of the turn's frozen connections of that kind, or empty when there is only one
(`422` naming them otherwise; `403` when the turn has none). The calling agent is the one whose role the runner sends in
`X-Crew-Agent`; it must be an agent of the turn that was given that counterpart (`403` otherwise, also when the role is
missing or matches no agent). Studio stores `connection_id`, `connection_name`, `proposed_by` and `proposed_by_agent_id`
instead; a payload that sends them itself is `422`. Text fields are cleaned and capped as for the tools they stand for, leaving room for the
attribution; hidden characters (Unicode format characters such as U+202E or U+200B, and tag characters) are dropped. On a
Linear connection a text with a profile link (which Linear turns into a mention) is `422` when proposed.

Confirming (`POST /chat/messages/{id}/confirm`, the owner, signed in) needs `crews.run` (`403`), and checks again that
chat with the agent is on (`409 chat_off`), that a specialist's card may still be confirmed (`409 proposals_off` once
`specialists_may_propose` is off), that the agent that proposed it is still the lead or one of its specialists you can see,
and still has the tool whose change the card makes (`409 proposer_not_offered`), and that the connection is still on the agent's list, of that kind, in your areas and
sharing an area with the agent (`409 connection_not_offered`), still enabled (`409 connection_off`) and, for a new issue,
still allowed to create issues (`409 create_off`). It then posts or saves through the connection's account: a comment,
suggestion or new issue ends with `(Posted by <you> from a Crew Studio agent chat; drafted by <agent>.)`; a KM2 document
goes to the default folder or a listed one, with `{ source, kind: "agent-chat", saved_by, drafted_by, conversation_id }`
in its metadata. There is no action log entry (an agent has none) and no Undo. A tracker or KM2 refusal answers with its
reason (`422 tracker_check_failed`, `502 tracker_failed` "The tracker did not accept the change.", the KM2 codes; on Linear,
`422` for a text with a profile link, which would mention someone) and leaves the card pending. Only such a clear refusal
(a 4xx or a rate limit: nothing was written) leaves the card pending. Any other failure of the post or save (a 2xx Studio
could not read, a 5xx, no answer in time, the service unreachable) may have been made, so the card is confirmed with
`{ outcome: "unknown", message, … }` instead of being offered again, and cannot post twice. While pending, the card says
what Studio will add (the attribution line, or the KM2 metadata).

### Questions during a hand-off

Every specialist of an agent-chat turn or hand-off gets **Ask the lead** (tool key `ask_lead`, listed in its frozen
tools). The runner answers it with a one-shot call of the lead (its persona and chat model, at most 3 steps) given the
conversation, the work (the hand-off's brief, or the person's latest message in a turn) and the question, each fenced
as data; its answer goes back to the specialist fenced too. The lead may be asked 5 times per hand-off and 2 times per
turn. Each consultation adds `consultation` events (`{ from_agent, to_agent, question, outcome, answer }`, outcome
`asked`, `answered`, `needs_person`, `failed`, `declined` or `stopped`) shown in the journal.

With `questions_enabled`, confirming a hand-off freezes the agent's question settings into its spec as `escalation`
(`{ rules, timeout_minutes, on_timeout, max_questions }`, the shape a crew's escalation has); a chat turn never gets one.
The lead (when it works the hand-off, or when a specialist asks it) then has **Ask a human** and may ask the
conversation's owner through `POST /run-state/questions` with `asked_for` naming the specialist. The question is
addressed to the run's creator, the conversation's owner, and shows in the conversation (`questions`, the hand-off's
`open_question_id`) and in `GET /questions` with `conversation_id`, `conversation_title`, `handoff_id` and
`handoff_title`. Only the owner reads and answers it, signed in: with an API key `GET /questions`, the question stream,
the conversation view and `GET /chat/conversations/{id}/questions` leave hand-off questions out, and
`POST /questions/{id}/answer` is `403`; for anyone but the owner it is `404`, as for a question that does not exist. A
hand-off whose owner is disabled or gone has nobody to ask: `POST /run-state/questions` is `403 escalation_off` at once.
The lead desk may ask the person at most once per consultation (each question counts towards `max_questions`). A
hand-off's run, and so its journal with the question and answer, is visible only to its owner. The hand-off's deadline does not pause while it waits: a question is due at
`min(now + question_timeout_minutes, deadline_at - 1 minute)`, and with less than a minute left before then
`POST /run-state/questions` is `409 handoff_out_of_time`. On timeout the hand-off carries on with an assumption
(`proceed`) or stops (`fail`, keeping what was written).

## Human input

A crew's **escalation rules** say when its agents should stop and ask a person. With escalation on, every agent of
the crew gets the rules in its instructions and an **Ask a human** tool. A question goes to the person who started
the run, and the run waits for the answer. Waiting has no run status of its own: a run with an open question is
`running`, and the open question is what "waiting for you" means. A run whose creator is not a person (service
account, trigger, schedule) asks nobody in particular; anyone with `work_orders.cancel` in its areas may answer.
The settings are frozen into the run spec (`spec.escalation`) when the run starts, so editing them later does not
change running work. Chat turns never get escalation (a hand-off gets its agent's question settings; see [Questions during a hand-off](#questions-during-a-hand-off)), and in a hierarchical crew only the worker agents can ask
(not the manager CrewAI builds).

`GET /crews/{id}/escalation` (`crews.view`) and `PUT /crews/{id}/escalation` (`crews.edit`):

```ts
type EscalationSettings = {
  crew_id: string
  enabled: boolean
  rules: string                        // plain language, at most 4,000 characters; required when enabled
  timeout_minutes: number              // how long a question waits for an answer, 1 to 180 (default 30)
  on_timeout: 'proceed' | 'fail'       // proceed: the agent carries on and states its assumption; fail: the run stops
  max_questions: number                // questions one run may ask, 1 to 20 (default 5)
  updated_by_name: string
  updated_at: string | null
}
```

| Call | Does |
|---|---|
| `GET /questions` | the open questions waiting on you (at most 100, soonest deadline first): `HumanQuestion[]` |
| `GET /questions/stream` | Server-Sent Events: a `questions` event with that same list when you connect and whenever a question is asked, answered or times out |
| `POST /questions/{id}/answer` | body `{ answer }` (1 to 4,000 characters); `200 HumanQuestion`. `403 not_your_question` when it waits on someone else; `409 question_answered`, `409 question_timed_out` or `409 question_closed` once it is no longer open |
| `GET /work-orders/{id}/questions` | every question the work order's attempts asked, newest first (`work_orders.view`) |

```ts
type HumanQuestion = {
  id: string; run_id: string
  work_order_id: string | null; work_order_number: number | null; work_order_title: string; crew_name: string
  assignee_id: string | null       // the person asked; null when the run was not started by a person
  agent_role: string; task_name: string
  asked_for: string                // the agent it was asked for: a hand-off's lead asks for a specialist; "" otherwise
  question: string; context: string
  status: 'open' | 'answered' | 'timed_out' | 'closed'   // closed: the run ended before anyone answered
  answer: string; answered_by_name: string
  asked_at: string; deadline_at: string; answered_at: string | null
  conversation_id: string | null; conversation_title: string   // set for a question from an agent chat's hand-off
  handoff_id: string | null; handoff_title: string
}
```

**During a run.** The tool calls the run-state API with the run's token: `POST /run-state/questions`
(`{ question, context?, task_name?, asked_for? }`, `201 { id, status, answer, answered_by, deadline_at, on_timeout }`;
`403 escalation_off` when the run has no escalation or is a chat turn, `429` past `max_questions`, `409
handoff_out_of_time` for a hand-off with too little time left), then
`GET /run-state/questions/{id}?wait=25`, a long-poll that Studio answers the moment the person replies, the question
times out, or after `wait` seconds (at most 25). Asking and answering each add an `ask_human` line to the run's
journal. With `on_timeout: "fail"`, an unanswered question stops the run: further LLM calls are blocked and it fails
with "No answer from a person within N minutes; the rules for asking say to stop."

## Help

Open to every signed-in principal; every answer is filtered by the caller's permissions. An article needs any one
of its permissions, and a section may need more. Hidden articles answer `404`, the same as missing ones.

- `GET /help` returns the table of contents: `{ categories: [{ title, articles: [{ slug, title, summary, headings: [{ id, title }] }] }] }`.
- `GET /help/articles/{slug}` returns `{ slug, title, category, summary, sections: [{ id, title, markdown }] }`. The
  intro section has id `intro` and an empty title. Screenshots in the Markdown point at `/help/images/{name}`, and
  links to articles the caller cannot open are turned into plain text.
- `GET /help/search?q=...&limit=8` takes `q` of 2–200 characters and a `limit` of 1–20, and returns
  `{ query, mode: "semantic" | "keyword", results: [{ slug, article_title, category, section_id, section_title, snippet, score }] }`.
  Results are ranked by keyword (BM25) and, in `semantic` mode, by embeddings as well (`STUDIO_HELP_SEARCH`).
- `GET /help/images/{name}` returns PNG bytes (`Cache-Control: private`), only for a screenshot used in a section the
  caller can see.

## MCP

`POST /mcp` (outside `/api`) is a Model Context Protocol server (Streamable HTTP, stateless, JSON responses) for
clients such as Claude Code:

```
claude mcp add --transport http crew-studio https://your-studio/mcp --header "Authorization: Bearer csk_…"
```

It accepts API keys only; a session cookie is refused with `401`. Each request passes the same Origin check, per-address
and per-principal rate limits as `/api`, and the key is checked again on every request, so revoking it cuts off an
open client at once. JSON-RPC batches are refused with `400`, and every tool call also spends one request from the
caller's per-principal limit. Tools run the `/api` handlers in-process as the key's owner, so permissions, functional areas,
validation and history behave exactly as they do over REST. Every non-GET tool call is also written to the audit log
as `mcp_change` with `method`, `path`, `status` and `key_id`.

| Tool | Does |
|---|---|
| `whoami` | `GET /auth/me` |
| `describe_kind` | fields of a kind, from the model types (type, read-only, allowed values) |
| `list_items`, `get_item` | list (main fields only) or read agents, tasks, crews, LLMs, pricing, schedules, connections and watches; areas and agent tools are list-only; watches are not listed (read one by its schedule's id) |
| `create_item`, `delete_item` | `POST` / `DELETE` on the kind's collection; a watch is created with `PUT /schedules/{id}/watch` (`id` is the schedule's) and deleted with `DELETE` there |
| `update_item` | reads the item, replaces only the named fields, `PUT`s the result and returns each changed field before and after; `expected_updated_at` refuses a stale write, `dry_run` previews |
| `get_settings`, `update_settings` | the same, for settings |
| `validate_crew`, `start_run`, `get_run` | `GET /crews/{id}/validate`, `POST /crews/{id}/runs` (its `idempotency_key` argument is sent as `Idempotency-Key`), `GET /runs/{id}` without `spec` |
| `list_history` | `GET /history`, newest first, at most 100 |
| `test_connection` | `POST /connections/{id}/test` |
| `get_people_map`, `sync_people_map`, `link_person` | `GET /connections/{id}/people`, `POST /connections/{id}/people/sync`, `PUT /connections/{id}/people/{accountId}` with `{ user_id }` |
| `list_crew_connections`, `set_crew_connections` | `GET` / `PUT /crews/{id}/connections` |
| `preview_watch` | `POST /schedules/{id}/watch/preview` with the saved watch, plus `changes` when given (nothing is saved) |
| `run_schedule_now` | `POST /schedules/{id}/run-now` |
| `get_pmo_setup_options`, `setup_pmo` | `GET` / `POST /pmo/setup` |

Kinds: `agent`, `task`, `crew`, `llm`, `pricing`, `schedule`, `connection` and `watch` are writable; `area` and `tool`
are read-only. A `watch` is addressed by its schedule's id (`GET`/`PUT`/`DELETE /schedules/{id}/watch`; `get_item`
returns the `watch` part of the view). `describe_kind "connection"` lists the allowed `kind` values and, under
`extra.connection_kinds`, each kind's config settings (key, label, input, required, hint, options) and the names of its
secrets.

**Connection secrets are UI-only.** MCP never sets, replaces or shows a secret: a `create_item` or `update_item` on a
connection whose fields carry `secrets`, or any key that a connection kind declares secret (`api_key`, `api_token`,
`auth_value`, `webhook_url`, `bot_token`, `post_url`, `password`) at any depth, including inside `config`, is refused
before anything is sent, with "set secrets in the Crew Studio UI (Connections → Replace secret)". Reads return what
REST returns: the names of the secrets that are set (`secret_keys`) or missing (`missing_secrets`), never values. A
change of a connection's URL, host, port, header or entity prefix clears its saved secrets, as over REST.

Unknown field names, server-set fields (`id`, `created_at`, `updated_at`, and a schedule's computed fields) and `null`
for a field whose type does not allow it are refused before anything is sent. Ids must be UUIDs. The
`expected_updated_at` check runs just before the `PUT`, not inside the store's transaction, so two writes landing in
the same instant can still overwrite each other.

## Rate limits

Per connecting address, before credentials are checked: 200 requests/second, burst 800 (so guessing keys or
tokens is throttled). Per principal (or per IP when signed out): 50 requests/second, burst 200. Starting runs and submitting work
orders: 20 per minute per principal, burst 5. Sign-in: 10 attempts per minute per IP. Webhooks: 60 per minute per
trigger. Help searches: 60 per minute per principal, burst 20. Run-state calls: 20 per second per run, burst 60. Chat messages: 30 per minute per principal, burst 10. Watch previews: 10 per minute per principal, burst 5. Connection test
messages (Slack, Teams, email): 6 per minute per principal, burst 3. Report retries: 6 per minute per principal, burst 3. Exceeding a limit returns `429` with `Retry-After`.
