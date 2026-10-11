# pmo_sim: a simulated project in KM2 for Crew Studio's PMO

`pmo_sim` puts a realistic, moving project into a KM2 organisation so Crew Studio's `km2_tracker` connection (and
the PMO crew on top of it) has something to watch. It installs a small Linear-style tracker schema (`trk_*` custom
entities), seeds an **"ERP ↔ CRM Integration"** project as it stands three weeks in, and then advances it one
simulated working day at a time: work starts and finishes, reviews bounce back, blockers come and go, due dates
slip, bugs and scope changes turn up, epics close, and people answer the Crew Studio bot's comments.

Python 3.12, standard library only. Everything it creates is synthetic (`@example.test` people, a generic "CRM" and
"ERP").

## Setup

1. **Create a sandbox org.** In KM2 **Site Admin -> Organizations**, create an organisation named
   `PMO Sandbox`. Keep it separate from real data: the simulator writes a few hundred records a day.
2. **Mint the keys** in that org (**Admin -> API**):

   | Key | Scopes | Notes |
   |---|---|---|
   | Bootstrap | `config:read`, `config:write` (select them explicitly; wildcards never grant them) | Expire after 1 day. Revoke once the schema is in. |
   | Simulator | `entities:read`, `records:read`, `records:write` | Used by `seed`, `advance`, `status`, `reset`. |
   | Crew Studio tracker | `entities:read`, `records:read`, `records:write` | Goes into the Crew Studio connection, not into this tool. |

3. **Configure.** Copy `.env.example` to `.env` in this folder (it is gitignored) and fill in `KM2_BASE_URL`,
   `KM2_API_KEY` (Simulator) and `KM2_BOOTSTRAP_KEY`. Environment variables override the file.
4. **Install the schema**, then seed:

   ```bash
   cd studio/tools
   python -m pmo_sim bootstrap-schema           # dry run: shows the plan, changes nothing
   python -m pmo_sim bootstrap-schema --apply   # dry run again, then applies (strategy=skip, apply_deletes=false)
   python -m pmo_sim seed --dry-run
   python -m pmo_sim seed
   python -m pmo_sim status
   ```

   Then revoke the Bootstrap key. If you would rather not mint a config key at all, write the bundle to a file and
   import it in KM2 under **Admin -> Migration -> Import** (strategy *skip*):

   ```bash
   python -m pmo_sim bundle --out trk-schema.json
   ```

5. **Connect Crew Studio** only after seeding (so the first watch run takes the seeded project as its baseline):
   add a connection of kind `km2_tracker` with
   - `base_url`: the same KM2 site root,
   - `api_key`: the Crew Studio tracker key,
   - `self_email`: `pmo-bot@example.test` (the seeded bot person "Crew Studio PMO", `is_bot = true`; the simulator
     recognises the bot's comments by that flag and replies to about half of them),
   - `entity_prefix`: `trk_` (default),
   - `activity_field`: `activity_at` (the simulated last-activity time; `updated_at` would be the real write time,
     which makes everything look fresh).

## Commands

Run from `studio/tools` as `python -m pmo_sim <command>`. Global options: `--env-file PATH`, `--today YYYY-MM-DD`
(pretend the real date is different; also `PMO_SIM_TODAY`).

| Command | What it does |
|---|---|
| `bootstrap-schema [--apply]` | POSTs the schema bundle to `/api/v1/config/promotions?dry_run=true`, prints the plan; with `--apply` and a clean dry run, applies it. Re-running is safe (existing entities are skipped). |
| `bundle --out FILE` | Writes the same bundle (KM2 bundle format v2, deterministic uuid5 ids) for a manual import. |
| `seed [--dry-run] [--seed N]` | Creates the people, project, five cycles, ~100 issues, dependency links and a few comments as of day 21. Idempotent: re-running only fills in what is missing. |
| `advance [--days N] [--dry-run]` | Simulates days. Without `--days` it catches up to the real date; with `--days N` it runs exactly N days, even into the future. |
| `status` | Status counts, overdue, blocked, stale and unassigned issues, and the simulator clock. |
| `reset --yes` | Deletes every `trk_*` record (children first), keeps the schema, so you can `seed` again. Without `--yes` it only lists what it would delete. |
| `teardown-records --yes` | Same deletion, then prints the manual teardown steps below. |

## The project

Ten weeks, five two-week cycles, starting on (real date of the seed) - 21 days. Eight people (delivery lead,
solution architect, two integration engineers, CRM administrator, ERP analyst, data engineer, QA) plus the bot.
`ERP-1` is the umbrella issue; 13 epics sit under it (discovery, data mapping, auth and connectivity, middleware,
account, contact, product and price, order, invoice and payment sync, error handling, monitoring, UAT, cutover and
hypercare) with 84 tasks that carry estimates and `blocks` links. On day 21 discovery is done, mapping is nearly
done, auth is in progress and blocked on the ERP vendor's sandbox credentials, three items are overdue, a couple are
stale and a couple are unassigned.

## The clock

- `trk_sim_state` holds `sim_day` (days since the project start), `sim_date` and the `seed`. One simulated day
  is one calendar day; Saturdays and Sundays are quiet. Run `advance` once per working day (by hand or cron) and the
  project moves at real speed; due dates, cycles and "overdue" all use real calendar dates.
- The seed back-dates `activity_at`, so items look stale from the start. Every simulated change sets
  `activity_at` to the simulated time (13:00 UTC plus a few minutes per change); comments carry `sim_posted_at`,
  while their `created_at` is the real write time.
- Each day's randomness comes from hash(seed, day), and the day is planned from the state it started with, so the
  same seed and day always give the same changes. Every change writes a `trk_event` with a deterministic
  `event_id` (`d034.007.status.ERP-41`, `reply.<comment id>` for replies).
- Events are written before the change they describe. If a run stops part-way, the next run reverses that day's
  events in memory, re-plans the day, skips what was already written and finishes it, so nothing is duplicated.
- Daily odds (see `engine.py`): blockers raised ~3% per in-progress issue and cleared ~25% (label `blocked`, a
  comment and a `blocks` link), due dates near or past due slip 2-5 working days with a comment, ~15% chance of a
  new bug or scope ticket, ~2% chance of a reassignment, rework on ~18% of reviews, WIP limits of 2-3 per person,
  and work only starts once its `blocks` dependencies are done. Unfinished work carries over at cycle boundaries.

## Teardown

1. `python -m pmo_sim teardown-records --yes` (deletes all `trk_*` records).
2. In the PMO Sandbox org, **Admin -> API**: revoke the Simulator, Crew Studio tracker and any Bootstrap key; delete
   the `km2_tracker` connection in Crew Studio.
3. **Admin -> Entities**: delete the definitions in this order (dropping a definition drops its table):
   `trk_comment`, `trk_event`, `trk_issue_link`, `trk_sim_state`, `trk_issue`, `trk_cycle`, `trk_project`,
   `trk_person`.
4. **Site Admin -> Organizations**: delete the PMO Sandbox org.

## Tests

```bash
cd studio/tools
../../.venv/bin/python -m pytest pmo_sim/tests -q --cov=pmo_sim --cov-config=pmo_sim/.coveragerc
```

The tests run against `tests/fake_km2.py`, an in-process fake of the KM2 v1 records, entities and promotion
endpoints (filters, sort, cursors, unique and relationship rules, injected 429s and outages). When the KM2
repository is checked out next to this one (`KM2_REPO`, default `~/github/redarchlabs/red-arch-km-2`), the bundle
is also validated with KM2's own pydantic models and diff code. For a manual end-to-end run, start the fake with
`python -m pmo_sim.tests.fake_km2 --port 8765` and point `KM2_BASE_URL` at it with the keys it prints.
