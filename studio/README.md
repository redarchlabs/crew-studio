# Crew Studio

Design, run and inspect CrewAI crews from a browser instead of writing Python.

Everything lives in Postgres: LLM configs, model prices, budget and search settings, agents, tasks, crews,
work orders, schedules, triggers, people, roles and functional areas, every run (with a frozen copy of the exact
configuration it ran with), every event, every LLM prompt and response, tool call, delegation and cost, and an
edit history of every change with who made it. Files (work order attachments, files agents write, and each run's
complete transcript) live in object storage: Azure Blob Storage or Google Cloud Storage, or the Azurite emulator
locally.

```
 Browser (React)  ── session cookie ──┐                                      Postgres (Docker, :55432)
 Other systems    ── API key / webhook ┤   Go service (:8088)            ┌─▶ config, people, roles, areas,
 Clerk / Microsoft ◀── OIDC sign-in ──┤   auth + RBAC + area scoping    │   work orders, runs, events,
                                      └─▶ REST API, SSE, OpenAPI ───────┤   llm_calls, revisions, audit
                                          run queue, scheduler,          └─▶ Azure Blob / GCS / Azurite:
                                          callbacks, housekeeping             attachments, artifacts, transcripts
                                                 │ stdin: frozen RunSpec + work order
                                                 ▼ stdout: JSON-line events (+ full transcript file)
                                          Python runner (runner/run_crew.py), one private workspace per run:
                                          builds the CrewAI crew, gives agents the brief and files,
                                          prices every call, enforces the budget, sandboxes tools
```

How the parts work together, the life of a run and where data lives: [docs/architecture.md](docs/architecture.md).

## Quick start

Needs Docker, Go 1.24+, Node 20+, and the project's Python venv (`../.venv` with `crewai` and `crewai-files`
installed, see [../QUICKSTART.md](../QUICKSTART.md)). Provider keys stay in `crewaiagents/.env` (`OPENAI_API_KEY`,
`GEMINI_API_KEY`); Crew Studio only passes them to the runner and never stores or shows them.

From the `crewaiagents` folder:

```bash
./run-stack.sh          # Postgres + API + built UI at http://127.0.0.1:8088
./run-stack.sh --dev    # same, plus the Vite dev server with hot reload at http://127.0.0.1:5174
./run-stack.sh --stop   # stop the Postgres container (data is kept)
```

The script creates `studio/.env` (random DB password) on first run, starts Postgres and Azurite, builds the Go
service and UI, and runs migrations on start. **On the very first start the service prints a one-time link**
(`/setup?token=...`) to create the first administrator; after that, sign in with that account. Ctrl+C stops the API and dev server. The `make` targets in `studio/` do the same
steps individually (`make setup`, `make start`, `make dev-api` + `make dev-web`).

**Forgot the administrator password?** Run `make reset-admin-password` in `studio/` (add `EMAIL=you@example.com`
when there is more than one administrator). It asks for the new password twice without showing it, clears any
lockout and signs that administrator out everywhere. Passwords are stored only as hashes, so they cannot be read back.

The first migration imports two crews that were first hand-written in Python (**PE Due Diligence Crew** and
**AI Research Crew**), along with their LLM settings and the prices from `spend_tracker.py`. The PE crew's company and location are
now run inputs (`{company}`, `{location}`), asked for when you press **Run**.

## Using it

- **LLMs**: model id in LiteLLM form (`openai/gpt-6-luna`, `anthropic/claude-opus-5-5`), optional temperature,
  max completion tokens and **reasoning effort**, and for OpenAI the API (`responses` is required for tool calling on
  `gpt-6-astra` and `gpt-6.1-sol`). Seeded: GPT-6 Luna, GPT-6 Astra and GPT-6.1 Sol (high reasoning), and Claude
  Opus 5.5, Fable 5.1, Sonnet 5.5 (high effort) and Haiku 4.5. Claude needs `ANTHROPIC_API_KEY` in the provider `.env`.
- **Pricing**: USD per 1M input / cached input / output tokens. Every LLM (and the search model, if an agent
  uses Google Search) needs a price, or the crew will not run; that keeps the budget cap honest.
- **Settings**: default budget, hard-stop multiplier, search model (`gemini-3.8-flash`), grounding price and free
  allowance (Gemini 3: 5,000 queries a month, then $14 per 1,000, counted per query), and how many runs may execute
  at once (extra runs queue).
- **Agents / Tasks / Crews**: edit in forms; the crew page draws the crew as a graph and lists anything that
  would stop it running. `{placeholders}` in agent and task text become run inputs.
- **Runs**: the run page shows a live interaction graph (crew → tasks → agents → tools/models, context flow
  and delegations, with calls, tokens, cost and time on every node), a timeline, every LLM call with its full
  prompt and response, task outputs, raw logs, and the frozen spec.
- **History**: every create, update and delete, with the full snapshot.

## Sign-in, roles and functional areas

- **Sign-in**: Microsoft Entra ID and Clerk, both through OpenID Connect handled by the Go service (authorization
  code + PKCE + nonce; the ID token is verified against the provider's keys). Configure them in `studio/.env`
  (see `.env.example`) and register `<STUDIO_PUBLIC_URL>/api/auth/microsoft/callback` /
  `<STUDIO_PUBLIC_URL>/api/auth/clerk/callback` with the provider. People must be invited (Admin > People) by
  email unless auto-provisioning is turned on (Admin > Sign-in settings). Administrators can also sign in with a
  password (for first-run setup and when SSO is down); turn that off with `STUDIO_LOCAL_LOGIN=false`.
- **Permissions** are a fixed catalog grouped by feature (Crews, Agents, Tasks, Work orders, Runs, Schedules,
  Triggers, LLMs, Pricing, Settings, History, Admin). **Roles** bundle permissions (Administrator, Builder,
  Operator and Viewer are seeded); people get roles plus individual permissions. Nobody can grant a permission,
  role or area they do not hold, and the last person able to manage users cannot be removed.
- **Functional areas** (Finance, IT, Training, HR, ...) are managed in Admin. Every crew, agent and task belongs to
  at least one; people see and act on items only in their areas (or everything, with "all areas"). Work orders,
  runs, schedules and triggers follow their crew. The imported PE crew is in Finance, the AI research crew
  in IT.

## Work orders

A **crew** is what runs; a **work order** is one execution of it. Write a brief, paste text or
screenshots straight from the clipboard, attach files (images, PDF, text, CSV, JSON, Word, Excel, PowerPoint;
25 MB each), fill in the inputs, and submit. Every attempt keeps:

- a **journal**: what each agent did on each task, in order (LLM calls, tools, searches, delegations, outputs);
- a **workflow diagram** of the agents, tasks, tools and models and how they interacted, plus a swimlane view;
- every LLM prompt and response, the raw event timeline and console log;
- the files the agents wrote, and the **complete, untruncated transcript** of the run, in object storage.

Work orders, schedules and triggers always target a crew. To run a single task, click **Make a crew** on the
task's page: the crew builder opens with a draft one-task crew (the task, the tasks it takes context from, their
agents and an input per placeholder) to review and save (`GET /api/tasks/{id}/crew-draft` in the API). Migration
0010 turned every task an existing work order, schedule or trigger targeted into such a crew.

Images and PDFs go to agents whose LLM has **Vision** turned on (set on the LLMs page; on by default for GPT-6,
Claude and Gemini); other agents are told the file names. Each run gets its own workspace folder, so runs cannot read each other's files.

## Automation and the integration API

- **Schedules** create a work order for a crew on a cron schedule in a time zone (at most every 5 minutes),
  acting as the person who last saved the schedule. If that person loses access, the schedule pauses itself.
- **Triggers** give another system a webhook URL with its own token: `POST /api/hooks/{id}` with a JSON payload;
  an input map picks values out of the payload, and the payload is added to the brief.
- **Service accounts** (Admin) get API keys for the whole API. `Idempotency-Key` makes retries safe, and runs can
  report back to a `callback_url` with an HMAC-signed POST (retried with backoff; private addresses only for hosts
  on the allow-list). The **API explorer** (Developers > API explorer) is a Swagger UI over `/api/openapi.yaml`
  for trying any endpoint as yourself or with a service account key; the full contract is [docs/api.md](docs/api.md).
- **Personal API keys** (Developers > My API keys) let people holding `account.api_keys` call the API as
  themselves from scripts. Admin > API keys lists every key in the system, people's and service accounts', for
  review and revocation. Keys are shown once and stored only as one-way hashes. Any key can be limited when it is
  created: to the REST API, MCP or both, and to some of its account's permissions and functional areas (never more
  than the account holds, checked on every request).

## In-app help

**Support > Help** has an article for every screen, with screenshots, a table of contents and search. Everything is
filtered on the server by the reader's permissions: an article needs any one of its `permissions`, and a section can
add its own (`<!-- permissions: crews.run -->` on the line after its `##` heading), so someone without `crews.run`
never sees how to run a crew. The contents, articles, screenshots, search results and even links between articles
follow the same rule; a hidden article answers 404 like a missing one.

- **Articles** are Markdown in `server/internal/help/content/` (front matter: `title`, `category`, `order`,
  `permissions`, `summary`, `keywords`), compiled into the binary. `TestEmbeddedContent` checks that every link,
  anchor and screenshot exists.
- **Screenshots** live in `content/images/` and are only served to people who can see a section that uses them.
  Refresh them all from the e2e suite: `cd e2e && HELP_SCREENSHOTS=1 npx playwright test 99-help-screenshots`.
- **Search** blends keyword ranking (BM25) with vector search, merged by reciprocal rank. `STUDIO_HELP_SEARCH`
  picks the embeddings: `openai` (default, `text-embedding-3-small` at 768 dimensions, `OPENAI_API_KEY`), `gemini`
  (`gemini-embedding-2`, `GEMINI_API_KEY`; the key's API restrictions must allow the embedContent methods) or
  `keyword` (no external calls). Keys come from the provider `.env`. Each section is embedded once and cached in the
  `help_embeddings` table, so after that only search queries call the provider. Those calls are limited to 60 a
  minute per person and 10 a second overall. A query unrelated to every article returns nothing. If the provider
  fails, search switches to keywords for 30 seconds, and the page shows which mode answered.

## Crew memory

Crews can keep soft lessons between runs (the crew's **Memory** tab; off until someone turns it on; see
[docs/architecture.md](docs/architecture.md) section 5). Studio embeds memories itself, so runs never hold an
embedding key. `STUDIO_MEMORY_EMBEDDINGS` picks the embeddings: `openai` (`text-embedding-3-small` at 768 dimensions,
`OPENAI_API_KEY`, and `OPENAI_BASE_URL` when the provider `.env` sets one), `gemini` (`gemini-embedding-2`,
`GEMINI_API_KEY`) or `off`. By default it follows `STUDIO_HELP_SEARCH` (and is `off` when help search is `keyword`);
when both use the same provider they share one embedder. Without an embedder, or without its key, memory cannot be
turned on and the Memory tab says why. Changing the provider later re-embeds stored memories in the background.

## File storage

`STUDIO_STORAGE=azure` (default) uses `STUDIO_AZURE_STORAGE_CONNECTION_STRING`; locally that is the Azurite
container (`UseDevelopmentStorage=true`), and in Azure it is a storage account connection string. `STUDIO_STORAGE=gcs`
uses `STUDIO_GCS_BUCKET` with a service account key file or Application Default Credentials. Containers are
created automatically; GCS buckets must already exist. Files are always served through the API (with permission
and area checks), never by public URL.

## Budget

Each run has a budget (crew default, overridable per run). At the budget, searches are refused so agents
finish from what they have; at budget × hard-stop multiplier every further LLM call is blocked and the run
fails with its finished task outputs kept. Costs are estimates from your Pricing table, so keep spend
limits set in the OpenAI and Google Cloud consoles too.

## Tools agents can use

Defined in [runner/tools_catalog.json](runner/tools_catalog.json) and implemented in
[runner/studio_runner/tools.py](runner/studio_runner/tools.py):

| Key | What it does |
|---|---|
| `google_search` | Gemini Google Search grounding (needs `GEMINI_API_KEY`); priced per search query |
| `scrape_website` | Fetches a public page as text; private, loopback and link-local addresses are blocked on every redirect, and the connection goes to the address that was checked |
| `workspace_read` / `workspace_write` | Read and write files inside `studio/workspace/` only; symlinks out are refused |
| `generate_video` | Makes a 3-10 second clip with sound from a prompt (optionally starting from a workspace image) with Gemini Omni Flash and saves it as an MP4 in the workspace, so it is kept with the run's files. Billed by tokens at the `gemini-omni-1.1-flash` price (about $0.10 a second at 720p, 3x at 4k); a clip that would not fit in the run's remaining budget is refused before it is made. Needs `GEMINI_API_KEY` to be allowed to use the Gemini API (Generative Language API): a Vertex-only key gets HTTP 403 |

## Reasoning models

- **OpenAI** (`gpt-6-astra`, `gpt-6.1-sol`): `reasoning_effort` is passed to CrewAI, and `api: responses` switches
  CrewAI to the Responses API, which these models need for tool calling.
- **Anthropic** (`claude-opus-5-5`, `claude-fable-5-1`, `claude-sonnet-5-5`): the runner wraps CrewAI's Claude client
  ([runner/studio_runner/llm_compat.py](runner/studio_runner/llm_compat.py)) to send `output_config.effort`, to turn
  CrewAI's forced single-tool `tool_choice` into `auto` (these models reject forced tool use), and to drop sampling
  parameters they reject. Thinking tokens bill as output, so high effort costs more per call.

## Security notes

- Every endpoint needs a signed-in person or an API key, and checks a permission and the item's functional areas
  (items outside your areas answer 404). Sessions are `HttpOnly`, `SameSite=Lax` cookies (`Secure` over https),
  stored only as hashes, and end after 12 idle hours or 7 days. API keys, trigger tokens and session tokens are
  stored as SHA-256 hashes; passwords with bcrypt; five wrong passwords lock an account for 15 minutes.
- The service refuses to start on a non-loopback address unless `STUDIO_ALLOW_REMOTE=true`, and then needs an
  https `STUDIO_PUBLIC_URL` (list extra host names in `STUDIO_ALLOWED_HOSTS`).
- Uploaded files are typed from their content, not their name; only plain images are shown inline and everything
  is served with a sandboxing Content-Security-Policy, so an uploaded HTML or SVG file cannot run script.
- Callback URLs must be https and resolve to public addresses (checked when connecting, so DNS cannot redirect them
  inside the network); redirects are not followed.
- Requests whose `Host` is not loopback are refused (stops DNS rebinding), browser requests from other origins are
  refused, and writes must be `application/json`, which stops cross-site form posts. The UI is served with a strict
  Content-Security-Policy.
- The runner process gets an allow-listed environment (no database URL or password, no unrelated credentials)
  and loads provider keys from the provider `.env` itself. Provider key patterns are redacted from stored logs.
- The service refuses to start if the agent workspace would contain a `.env` file.
- Each run is capped at 100,000 events / 256 MB of output and a maximum duration (`STUDIO_MAX_RUN_MINUTES`, 240).

## Database migrations

Migrations are [goose](https://github.com/pressly/goose) SQL files in `server/internal/db/migrations`, embedded in
the binary and applied on every start (each in its own transaction, one process at a time). goose records them in
the `goose_db_version` table. A database migrated before goose (with a `schema_migrations` table) is adopted on the
first start: its applied migrations are recorded for goose and the old table is dropped, so nothing runs twice.

```bash
make migrate-status              # every migration and when it was applied
make migrate-down VERSION=6      # list what rolling back to 6 would undo (0 means everything)
make migrate-down VERSION=6 YES=1   # stop the service and back up first: this does it
```

Rolling back runs each migration's `-- +goose Down` section and **deletes the data it added** (rolling back 0005, for
example, deletes work orders and leaves their files in object storage). A rollback that would remove something
still in use, such as a seeded agent a task uses or an imported crew that has runs, fails and changes nothing.

To add a migration, create the next number (`0008_short_name.sql`) with both sections, each wrapped in one
`-- +goose StatementBegin` / `-- +goose StatementEnd` block (goose cannot split statements containing `$s$` quoted
text itself). Never edit a migration once it has been applied anywhere: goose does not notice, so the change never
runs. The tests check every Down section by rolling back to each version and re-applying.

## Layout

```
studio/
├── docker-compose.yml        Postgres 17 on 127.0.0.1:55432
├── docs/api.md               API contract (envelope, endpoints, event types)
├── server/                   Go service
│   ├── cmd/crewstudio        entrypoint (flags: -studio-dir, -migrate, -migrate-status, -migrate-down-to, -yes)
│   └── internal/
│       ├── api               HTTP handlers (auth, admin, work orders, files, automation), SSE, OpenAPI
│       ├── auth              permission catalog, principal, passwords and tokens, OIDC sign-in
│       ├── launch            starts runs and work orders (permission, area and run checks in one place)
│       ├── scheduler         fires schedules
│       ├── callbacks         signed run callbacks (SSRF-guarded) and storage/session housekeeping
│       ├── journal           run events → readable per-agent journal
│       ├── storage           Azure Blob (Shared Key) and GCS clients
│       ├── store             Postgres repositories + revision history
│       ├── db/migrations     goose migrations: schema and seed (embedded, applied on start)
│       ├── runner            run queue, Python subprocess supervisor, event broker
│       ├── runspec           "can this crew run?" checks, spec freezing, price matching
│       ├── interactions      run events → interaction graph
│       ├── help              in-app help: articles + screenshots (content/), permission filtering, search
│       ├── model             domain types and validation
│       └── config            settings from the environment and studio/.env
├── runner/                   Python: run_crew.py + studio_runner/ (build, listeners, meter, tools) + tests
├── tools/pmo_sim/            Python: simulated ERP ↔ CRM project in a KM2 org for km2_tracker (see its README)
├── web/                      React UI (see web/README.md)
└── workspace/                files agents may read and write
```

## Tests

```bash
make test        # Go (race detector, needs Postgres), Python runner, web
```

The Go integration tests use the `crewstudio_test` database (one schema per package). The runner's end-to-end
test runs `run_crew.py` against a fake OpenAI-compatible server, so no test spends money.

Browser end-to-end tests (Playwright, Chromium) live in `e2e/`:

```bash
make test-e2e    # builds the app, recreates the crewstudio_e2e database, serves it on 127.0.0.1:8091 with a fake LLM
```

They never touch the dev stack on 8088/5174 or the `crewstudio` database. See `e2e/README.md`.

## License

Copyright 2026 Red Arch Labs. Licensed under the Apache License, Version 2.0; see [LICENSE](../LICENSE) and
[NOTICE](../NOTICE). Third-party components and their licenses are listed in
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).
