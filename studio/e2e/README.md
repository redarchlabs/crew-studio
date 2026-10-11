# Crew Studio browser tests

Playwright Test (`@playwright/test` 1.63, Chromium build 1243 from `~/.cache/ms-playwright`) driving the real UI,
the real Go server and the real Python runner. No test calls a real LLM: the runner is pointed at a local fake.

## Run

```bash
make test-e2e                      # from studio/ (installs node_modules on first use)
cd e2e && npx playwright test      # same thing
E2E_SKIP_BUILD=1 npx playwright test tests/07-work-orders.spec.ts   # reuse the previous build, one spec
npx playwright show-report out/report
```

Needs Docker (Postgres `crewstudio-postgres-1` and Azurite), `studio/.env` (read in memory for the database
password, never printed or written), and `../.venv` with crewai installed. A full run takes about 4 minutes.

## Environment overrides

`E2E_OUT_DIR` (default `e2e/out`), `E2E_PORT` (8091), `E2E_FAKE_LLM_PORT` (8092), `E2E_FAKE_TRACKER_PORT` (8094),
`E2E_FAKE_KM2_PORT` (8095), `E2E_FAKE_DELIVERY_PORT` (8096) and `E2E_FAKE_SMTP_PORT` (8097) for the fake Slack, Teams
and SMTP server, `E2E_FAKE_MCP_PORT` (8098) for the fake outside MCP server (`harness/fake_mcp.py`), and
`E2E_DB_NAME` (`crewstudio_e2e`; the Azure container name follows it) let a second suite run side by side. `E2E_SKIP_BUILD=1` reuses the previous build.

## What the harness does (`harness/`)

1. `docker compose --env-file .env up -d --wait` in `studio/`.
2. Drops and recreates the database `crewstudio_e2e` (never the real `crewstudio`).
3. Builds the Go server to `out/bin/crewstudio` and the web app to `out/web-dist` (so `server/bin` and `web/dist`
   used by the dev stack are untouched). If `npm run build` fails (for example a type error in a unit test someone is
   editing) it falls back to `vite build`.
4. Starts `fake_llm.py` (127.0.0.1:8092) and the server (127.0.0.1:8091) with a strict environment allow-list and
   `STUDIO_PROVIDER_ENV_FILE` pointing at a generated file holding fake keys and the fake base URLs.
5. Reads the first-run setup token from `out/logs/server.log`. Logs: `out/logs/`.
6. Teardown stops both process groups.

`fake_llm.py` speaks OpenAI chat completions, the Responses API and Anthropic messages, answers with
`Thought: I now know the final answer` / `Final Answer: ...` plus token usage, and appends every request to
`out/logs/fake-llm-requests.jsonl` (`support/llmlog.ts` reads it, e.g. to assert a pasted image arrived as a base64 image
part). A prompt containing `E2E_SLOW_MARKER` is held for 90 s (cancel tests); `E2E_FAIL_MARKER` gets HTTP 500.

## Projects and fixtures

`first-run` (01, creates the admin through `/setup`) -> `seed` (`seed.setup.ts`: builder, operator, viewer and
out-of-area users via the admin API, with a `storageState` file each in `out/auth/`) -> `chromium` (02-11, one worker, file order).
Password sign-in is administrators-only, so non-admin sessions are created by inserting a session row in the e2e database.
Specs 06-09 call `ensureLibrary()` so they also run alone.

## Specs

01 first run, 02 local login and sign-in settings, 03 admin, 04 RBAC and areas, 05 library CRUD, 06 runs,
07 work orders, 08 schedules, 09 triggers and API keys, 10 API explorer, 11 settings and pricing, 14 schedule watches
(a workspace-file watch: preview, the first check, a check that starts nothing, a follow-up ladder), 16 Jira and
Linear trackers (a Jira connection made and tested in the UI, its people map, the tracking list, a tracking-list watch
with roll-ups). 18 the PMO (Slack and email connection tests, the Set up a PMO wizard, a dry-run daily sweep that runs only its task, and a
weekly report delivered with deliver_report to `harness/fake_delivery.py`, a stdlib fake Slack, Teams and SMTP server, with
a failed Slack delivery retried from the work order). `harness/fake_tracker.py` is a stdlib fake Jira (and Linear `viewer`) the server reaches through
`STUDIO_JIRA_SITES` and `STUDIO_LINEAR_API_URL`; no real tracker is contacted. 20 KM2 issues as a tracker (a
`km2_tracker` connection made and tested in the UI, its people map, a watch on project ERP whose run comments as the
Crew Studio person, and a check that reports a changed issue and a person's comment); `harness/fake_km2_tracker.py`,
served by `fake_km2.py`, is the KM2 organisation with the `trk_*` entities (v1 catalog and records API).

## Notes

* The Admin > Sign-in settings page has no switch for password sign-in; it is the server setting
  `STUDIO_LOCAL_LOGIN`, so spec 02 starts a second server on :8093 with it off.
* The test runner gives new API request contexts the project's admin `storageState`; `support/api.ts` empties it for anonymous clients.
* Service-account API key UI is being reworked by another change (personal/admin API keys); the key steps are in 03 and 09.
