import { createHash, randomBytes } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { expect, test, type APIRequestContext, type Locator, type Page } from '@playwright/test'
import { psql } from '../harness/db.ts'
import { DB_NAME, FAKE_DELIVERY_URL, FAKE_KM2_URL, FAKE_LLM_URL, FAKE_SMTP_PORT, FAKE_TRACKER_URL, STUDIO_DIR } from '../harness/paths.ts'
import { LIB, ROLE_USERS } from '../support/accounts.ts'
import { apiAnon, apiAs, findByName, unwrap } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'

/**
 * Captures the screenshots used by the in-app help (server/internal/help/content/images). It runs last so
 * the earlier specs have filled the screens with runs, work orders, schedules and triggers, and creates
 * anything still missing. Off by default because it writes into the source tree:
 *
 *   HELP_SCREENSHOTS=1 npx playwright test
 *
 * The PMO screens (connections, crew state, watches, reports, chat) use an "Atlas PMO" built from the fakes:
 * fake_tracker.py (Jira project PRJ), fake_km2.py, fake_delivery.py (Slack and SMTP) and fake_llm.py. Agent chat uses
 * a "Delivery Adviser" with two specialists that may start the Atlas PMO or the E2E crew.
 */
const OUT = path.join(STUDIO_DIR, 'server', 'internal', 'help', 'content', 'images')
const RUN_TIMEOUT = 120_000
const SETUP_TIMEOUT = 480_000

test.skip(process.env.HELP_SCREENSHOTS !== '1', 'set HELP_SCREENSHOTS=1 to refresh the help screenshots')
// Not serial: one broken screen must not stop the rest being captured
test.use({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 })

type Item = { id: string; name?: string; title?: string; status?: string; email?: string }
type Ids = { crew: string; agent: string; task: string; run: string; workOrder: string; schedule: string; trigger: string; person: string; role: string }
const ids = {} as Ids

async function list(api: APIRequestContext, url: string): Promise<Item[]> {
  const data = await unwrap<Item[] | { items: Item[] }>(await api.get(url))
  return Array.isArray(data) ? data : data.items
}

/** A finished work order (and its run) to photograph; submits one through the fake LLM when none exists. */
async function finishedWorkOrder(api: APIRequestContext, crewId: string): Promise<{ id: string; runId: string }> {
  type WO = Item & { current_run_id: string | null }
  const title = 'Quarterly market scan'
  const done = (await list(api, '/api/work-orders?limit=100')).find((w) => w.title === title && w.status === 'succeeded') as WO | undefined
  if (done?.current_run_id) return { id: done.id, runId: done.current_run_id }
  const wo = await unwrap<WO>(
    await api.post('/api/work-orders', {
      data: { title, brief: 'Summarise the main changes this quarter.', target_type: 'crew', crew_id: crewId, inputs: { topic: 'battery storage' }, submit: true },
    }),
    201,
  )
  await expect
    .poll(async () => (await unwrap<WO>(await api.get(`/api/work-orders/${wo.id}`))).status, { timeout: RUN_TIMEOUT })
    .toBe('succeeded')
  const fresh = await unwrap<WO>(await api.get(`/api/work-orders/${wo.id}`))
  return { id: wo.id, runId: fresh.current_run_id! }
}

/** The item with this name (or title), created from body when it does not exist yet. */
async function namedOrCreate(api: APIRequestContext, url: string, name: string, body: Record<string, unknown>): Promise<string> {
  const hit = (await list(api, url)).find((x) => (x.name ?? x.title) === name)
  if (hit) return hit.id
  return (await unwrap<Item>(await api.post(url, { data: body }), 201)).id
}

/** Hides the pointer, focus ring and toasts, lets animations settle and saves the viewport. */
async function capture(page: Page, name: string): Promise<void> {
  // A leftover "Saved" toast is noise in a help picture; the style goes away with the next navigation
  await page.addStyleTag({ content: '[data-sonner-toaster] { visibility: hidden !important; }' })
  await page.mouse.move(0, 0)
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
  await page.waitForTimeout(500)
  await page.screenshot({ path: path.join(OUT, name), animations: 'disabled' })
}

/** Waits until nothing on the page says Loading. */
async function settled(page: Page): Promise<void> {
  // Live run pages keep a stream open, so network idle is a best effort
  await page.waitForLoadState('networkidle', { timeout: 8_000 }).catch(() => undefined)
  await expect(page.getByRole('status').filter({ hasText: /Loading/ })).toHaveCount(0, { timeout: 15_000 })
}

/** Opens a screen, waits until it has settled and saves the viewport. */
async function shot(page: Page, name: string, url: string, ready?: (page: Page) => Promise<void>): Promise<void> {
  await page.goto(url)
  await settled(page)
  if (ready) await ready(page)
  await capture(page, name)
}

/** The panel (a <section> with an eyebrow title) whose title matches. */
function panel(page: Page, title: RegExp): Locator {
  return page.locator('div.eyebrow').filter({ hasText: title }).first().locator('xpath=ancestor::section[1]')
}

/** Scrolls the page's main area so the element sits near the top of the viewport, below any sticky page header. */
async function scrollToTop(target: Locator, margin = 16): Promise<void> {
  await target.scrollIntoViewIfNeeded()
  await target.evaluate((el, m) => {
    const main = document.querySelector('main')
    if (!main) return el.scrollIntoView({ block: 'start' })
    const header = main.querySelector('header.sticky')
    const covered = header ? header.getBoundingClientRect().height : 0
    const top = el.getBoundingClientRect().top - main.getBoundingClientRect().top + main.scrollTop - covered - m
    main.scrollTo({ top: Math.max(0, top) })
  }, margin)
}

/**
 * Replaces the fake LLM's placeholder answer ("FAKE-LLM-ANSWER") with a realistic result in the e2e database, so
 * help pictures of a finished run read naturally. The last task's output is also the run's final output.
 */
function tidyRunOutput(runId: string, tasks: string[]): void {
  const quote = (text: string) => `$out$${text}$out$`
  const last = tasks.length - 1
  psql(DB_NAME, [
    `UPDATE runs SET output = ${quote(tasks[last])} WHERE id = '${runId}';`,
    ...tasks.map((text, i) => `UPDATE run_task_outputs SET raw = ${quote(text)} WHERE run_id = '${runId}' AND position = ${i};`),
    `UPDATE run_task_outputs SET raw = ${quote(tasks[last])} WHERE run_id = '${runId}' AND position > ${last};`,
  ].join('\n'))
}

const MARKET_SCAN = [
  '- Grid-scale battery deployments rose about 40% on last quarter.\n- Lithium iron phosphate cell prices fell for the fifth quarter in a row.\n- Two new long-duration pilots started, both iron-air.\n- Interconnection queues remain the main cause of delay.\n- Three utilities opened tenders for 4-hour storage.',
  'Battery storage grew strongly this quarter, helped by falling cell prices and new utility tenders. Grid connection delays are still the biggest risk to the pipeline.',
]
const ATLAS_SWEEP = [
  '## Atlas daily sweep\n\n**PRJ-10 Atlas rollout** is in progress; 1 of 3 children is done.\n\n- **PRJ-11 Vendor access** is 3 days overdue and blocked. Escalated to the lead, Dana Ruiz.\n- **PRJ-13** has no owner and is due after the rollout itself; suggested moving it into the rollout window.\n\n### Would post\n\n- PRJ-11: a follow-up comment asking for a new date (to the lead).\n- PRJ-11: a suggested due date two weeks out.',
]

test.beforeAll(async () => {
  fs.mkdirSync(OUT, { recursive: true })
  const lib = await ensureLibrary()
  const api = await apiAs('admin')
  try {
    const wo = await finishedWorkOrder(api, lib.crewId)
    tidyRunOutput(wo.runId, MARKET_SCAN)
    if ((await list(api, '/api/account/api-keys')).length === 0) {
      await unwrap(await api.post('/api/account/api-keys', { data: { name: 'Nightly report script', expires_at: null } }), 201)
    }
    Object.assign(ids, {
      crew: lib.crewId,
      agent: lib.agentId,
      task: lib.task2Id,
      workOrder: wo.id,
      run: wo.runId,
      schedule: await namedOrCreate(api, '/api/schedules', 'Weekday market scan', {
        name: 'Weekday market scan', description: 'Every weekday morning', target_type: 'crew', crew_id: lib.crewId, cron: '0 9 * * 1-5',
        timezone: 'Europe/London', brief: '', inputs: { topic: 'battery storage' }, budget_usd: null, enabled: false, overlap_policy: 'skip',
      }),
      trigger: await namedOrCreate(api, '/api/triggers', 'CRM new account', {
        name: 'CRM new account', description: 'Fired by the CRM', target_type: 'crew', crew_id: lib.crewId, enabled: true,
        input_map: { topic: 'account.industry' }, brief: 'Research the new account.', include_payload: true, budget_usd: null, callback_url: null,
      }),
      person: (await list(api, '/api/admin/users')).find((u) => u.email === ROLE_USERS.builder.email)!.id,
      role: (await findByName<{ id: string; name: string }>(api, '/api/admin/roles', 'Builder')).id,
    })
  } finally {
    await api.dispose()
  }
})

test('sign-in page', async ({ browser }) => {
  // Signed out: the project's admin storage state must not come along
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, storageState: { cookies: [], origins: [] } })
  const page = await ctx.newPage()
  await shot(page, 'login.png', '/login', async (p) => {
    await expect(p.getByRole('button', { name: /sign in/i }).first()).toBeVisible()
  })
  await ctx.close()
})

// ---------------------------------------------------------------------------------------------------------------
// The PMO screens. Their data is made once (and found again when a failed test restarts the worker), before the
// console screens below are taken, so the overview, runs and work order lists include it.
// ---------------------------------------------------------------------------------------------------------------

const PMO = 'Atlas PMO'
const PMO_AREA = 'IT'
const PMO_LLM = 'GPT-6 Luna (fast)'
const KM2_KEY = 'km2_e2e-key' // the fake KM2's only key
const KM2_PROJECTS = '6a1f0000-0000-4000-8000-000000000001'
const KM2_ATLAS = '6a1f0000-0000-4000-8000-000000000002'
const CONN = { jira: 'Acme Jira', km2: 'Acme KM2 (IT)', slack: 'PMO Slack', email: 'PMO email', http: 'Birch status API' } as const
const RECIPIENTS = ['sam.lee@example.com', 'dana.ruiz@example.com']

type Check = { outcome: string; work_order_id: string | null }
type WorkOrder = Item & { current_run_id: string | null; inputs: Record<string, string> }
type Pmo = {
  areaId: string; llmId: string; conns: Record<keyof typeof CONN, string>; crewId: string; sweepId: string; weeklyId: string; sweepWO: string; weeklyWO: string
  adviserId: string
}
const pmo = {} as Pmo

async function connection(api: APIRequestContext, name: string, kind: string, config: Record<string, string>, secrets: Record<string, string>, areaId: string): Promise<string> {
  const hit = (await list(api, '/api/connections')).find((c) => c.name === name)
  if (hit) return hit.id
  return (await unwrap<Item>(await api.post('/api/connections', { data: { name, kind, area_ids: [areaId], config, secrets, enabled: true } }), 201)).id
}

/** Makes a schedule due; the scheduler picks it up within its 20-second tick. */
function makeDue(scheduleId: string): void {
  psql(DB_NAME, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = '${scheduleId}'`)
}

async function checks(api: APIRequestContext, scheduleId: string): Promise<Check[]> {
  return (await unwrap<{ checks: Check[] }>(await api.get(`/api/schedules/${scheduleId}/watch`))).checks
}

async function waitForChecks(api: APIRequestContext, scheduleId: string, count: number): Promise<Check[]> {
  let found: Check[] = []
  await expect(async () => {
    found = await checks(api, scheduleId)
    expect(found.length).toBeGreaterThanOrEqual(count)
  }).toPass({ timeout: 90_000, intervals: [2000] })
  return found
}

async function succeeded(api: APIRequestContext, workOrderId: string): Promise<WorkOrder> {
  await expect
    .poll(async () => (await unwrap<WorkOrder>(await api.get(`/api/work-orders/${workOrderId}`))).status, { timeout: RUN_TIMEOUT })
    .toBe('succeeded')
  return unwrap<WorkOrder>(await api.get(`/api/work-orders/${workOrderId}`))
}

/** A run-state client for a run, as the run's own tools would call the API (the run manager writes this token). */
async function runClient(runId: string): Promise<APIRequestContext> {
  const token = `csr_help${randomBytes(24).toString('hex')}`
  const hash = createHash('sha256').update(token).digest('hex')
  // One token per run: a finished run's own token is replaced
  psql(DB_NAME, `INSERT INTO run_tokens (run_id, token_hash, expires_at) VALUES ('${runId}', '${hash}', now() + interval '1 hour')
    ON CONFLICT (run_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at`)
  return apiAnon(token)
}

async function deliveryFake(urlPath: string): Promise<void> {
  const res = await fetch(`${FAKE_DELIVERY_URL}${urlPath}`, { method: 'POST' })
  expect(res.ok).toBe(true)
}

/** The PMO itself: connections, the wizard's crew and schedules, and one tracked project. */
async function installPmo(api: APIRequestContext): Promise<void> {
  pmo.areaId = (await list(api, '/api/areas')).find((a) => a.name === PMO_AREA)!.id
  pmo.llmId = await namedOrCreate(api, '/api/llms', PMO_LLM, {
    name: PMO_LLM, model: LIB.model, temperature: null, max_completion_tokens: 4096, reasoning_effort: null, api: 'completions', vision: false,
  })
  const a = pmo.areaId
  pmo.conns = {
    jira: await connection(api, CONN.jira, 'jira', { site_url: FAKE_TRACKER_URL, email: 'e2e-bot@e2e.example' }, { api_token: 'e2e-jira-token' }, a),
    km2: await connection(api, CONN.km2, 'km2', { base_url: FAKE_KM2_URL, default_folder_id: KM2_ATLAS, write_folder_ids: KM2_PROJECTS }, { api_key: KM2_KEY }, a),
    slack: await connection(api, CONN.slack, 'slack', {}, { webhook_url: `${FAKE_DELIVERY_URL}/slack/hook/T0/B0/atlas` }, a),
    email: await connection(api, CONN.email, 'email',
      { host: '127.0.0.1', port: String(FAKE_SMTP_PORT), security: 'none', from_address: 'pmo@example.com' }, { password: 'unused' }, a),
    http: await connection(api, CONN.http, 'http', { base_url: `${FAKE_LLM_URL}/__health`, auth_header: 'X-Api-Key' }, { auth_value: randomBytes(12).toString('hex') }, a),
  }
  let crew = (await list(api, '/api/crews')).find((c) => c.name === PMO)
  if (!crew) {
    const out = await unwrap<{ crew: Item }>(await api.post('/api/pmo/setup', { data: {
      name: PMO, area_ids: [a], tracker_connection_id: pmo.conns.jira, timezone: 'Europe/London', coordinator_llm_id: pmo.llmId, reporter_llm_id: pmo.llmId,
      enabled: true,
      destinations: [
        { kind: 'slack', connection_id: pmo.conns.slack, recipients: [], folder: '' },
        { kind: 'email', connection_id: pmo.conns.email, recipients: RECIPIENTS, folder: '' },
        { kind: 'km2', connection_id: pmo.conns.km2, recipients: [], folder: '' },
      ],
    } }))
    crew = out.crew
  }
  pmo.crewId = crew.id
  // Read the tracker's people first, so roll-ups and follow-ups name the lead (Dana Ruiz) instead of her account id
  await unwrap(await api.post(`/api/connections/${pmo.conns.jira}/people/sync`, { data: {} }))
  const schedules = await list(api, '/api/schedules')
  pmo.sweepId = schedules.find((s) => s.name === `${PMO}: daily sweep`)!.id
  pmo.weeklyId = schedules.find((s) => s.name === `${PMO}: weekly report`)!.id
  const tracked = await unwrap<{ items: { item_ref: string }[] }>(await api.get(`/api/crews/${pmo.crewId}/state/tracking`))
  if (!tracked.items.some((i) => i.item_ref === 'PRJ-10')) {
    await unwrap(await api.post(`/api/crews/${pmo.crewId}/state/tracking`, { data: {
      connection_id: pmo.conns.jira, item_ref: 'PRJ-10', project: 'Atlas', include_children: 'all', note: 'Phase 2 rollout', lead_account_id: 'acc-dana', schedule_id: null,
    } }), 201)
  }
}

/** A day counted from today as an ISO date, like the fake tracker's dates: dayISO(-3) is three days ago. */
function dayISO(offset: number): string {
  return new Date(Date.now() + offset * 86_400_000).toISOString().slice(0, 10)
}

/** A day counted from today as people write it in prose, such as "2 October". */
function dayText(offset: number): string {
  return new Date(Date.now() + offset * 86_400_000).toLocaleDateString('en-GB', { day: 'numeric', month: 'long', timeZone: 'UTC' })
}

/**
 * Runs fn with the sweep's follow-up ladder due every day of the week. The PMO's ladder keeps Saturday and Sunday
 * quiet, and nothing is due on a quiet day, so a refresh run at the weekend would find no follow-up to post (and the
 * Action log picture would have none). The ladder is put back afterwards, so its picture still shows the quiet days.
 */
async function withEveryDayLadder(api: APIRequestContext, scheduleId: string, fn: () => Promise<void>): Promise<void> {
  type Watch = Record<string, unknown> & { followups: Record<string, unknown> | null }
  const saved = (await unwrap<{ watch: Watch }>(await api.get(`/api/schedules/${scheduleId}/watch`))).watch
  if (!saved.followups) throw new Error('the daily sweep has no follow-up ladder')
  await unwrap(await api.put(`/api/schedules/${scheduleId}/watch`, { data: { ...saved, followups: { ...saved.followups, quiet_days: [] } } }))
  try {
    await fn()
  } finally {
    await unwrap(await api.put(`/api/schedules/${scheduleId}/watch`, { data: saved }))
  }
}

/**
 * Two checks of the daily sweep: the first starts a dry-run work order (everything added, one follow-up due), the
 * sweep then "posts" that follow-up as a dry-run comment, so the second check finds nothing to do.
 */
async function sweepChecks(api: APIRequestContext): Promise<void> {
  await withEveryDayLadder(api, pmo.sweepId, () => sweepTwice(api))
}

async function sweepTwice(api: APIRequestContext): Promise<void> {
  let found = await checks(api, pmo.sweepId)
  if (!found.some((c) => c.outcome === 'started')) {
    makeDue(pmo.sweepId)
    found = await waitForChecks(api, pmo.sweepId, 1)
  }
  pmo.sweepWO = found.find((c) => c.outcome === 'started')!.work_order_id!
  const wo = await succeeded(api, pmo.sweepWO)
  tidyRunOutput(wo.current_run_id!, ATLAS_SWEEP)
  if (found.length >= 2) return
  type Due = { item: string; step: string }
  const due = (JSON.parse(wo.inputs.followups_due ?? '{"due":[]}') as { due: Due[] }).due
  const run = await runClient(wo.current_run_id!)
  try {
    for (const d of due) {
      const ref = d.item.replace(/^(jira|linear):/, '')
      await unwrap(await run.post(`/api/run-state/trackers/issues/${ref}/comments`, { data: {
        text: 'Vendor access is now past its due date and still blocked. Can we agree a new date this week?', mentions: [], followup_step: d.step,
      } }))
    }
    await unwrap(await run.post('/api/run-state/trackers/issues/PRJ-11/suggestions', { data: {
      field: 'due_date', value: dayISO(14), reason: `Blocked on vendor access since ${dayText(-8)}.`, mentions: [],
    } }))
  } finally {
    await run.dispose()
  }
  makeDue(pmo.sweepId)
  await waitForChecks(api, pmo.sweepId, 2)
}

/** The weekly report, delivered for real (not a dry run): Slack fails once, email and KM2 go through. */
async function weeklyReport(api: APIRequestContext): Promise<void> {
  const done = (await list(api, '/api/work-orders?limit=100')).find((w) => w.title?.startsWith(`${PMO}: weekly report`) && w.status === 'succeeded')
  if (done) {
    pmo.weeklyWO = done.id
    return
  }
  const view = await unwrap<{ watch: Record<string, unknown> }>(await api.get(`/api/schedules/${pmo.weeklyId}/watch`))
  await unwrap(await api.put(`/api/schedules/${pmo.weeklyId}/watch`, { data: { ...view.watch, dry_run: false } }))
  const sc = await unwrap<Record<string, unknown>>(await api.get(`/api/schedules/${pmo.weeklyId}`))
  // The fake LLM calls deliver_report when the brief holds this marker; the brief is put back afterwards
  await unwrap(await api.put(`/api/schedules/${pmo.weeklyId}`, { data: { ...sc, brief: `${String(sc.brief ?? '')}\n\nE2E_DELIVER_REPORT` } }))
  await deliveryFake('/__fail?status=502')
  try {
    const wo = await unwrap<Item>(await api.post(`/api/schedules/${pmo.weeklyId}/run-now`, { data: {} }), 201)
    pmo.weeklyWO = wo.id
    await succeeded(api, wo.id)
  } finally {
    await deliveryFake('/__ok')
    const now = await unwrap<Record<string, unknown>>(await api.get(`/api/schedules/${pmo.weeklyId}`))
    await unwrap(await api.put(`/api/schedules/${pmo.weeklyId}`, { data: { ...now, brief: sc.brief } }))
  }
}

/** Crew-wide notes and log entries, written as an earlier run of the crew would, plus one edit by a person. */
async function crewMemory(api: APIRequestContext): Promise<void> {
  const notes = await unwrap<{ name: string }[]>(await api.get(`/api/crews/${pmo.crewId}/state/notes`))
  if (notes.some((n) => n.name === 'risk-register')) return
  const runId = psql(DB_NAME, `INSERT INTO runs (crew_id, crew_name, status, budget_usd, spec, area_ids, started_at, finished_at)
    VALUES ('${pmo.crewId}', '${PMO}', 'succeeded', 1, '{}', ARRAY['${pmo.areaId}']::uuid[], now() - interval '26 hours', now() - interval '26 hours' + interval '48 seconds')
    RETURNING id`).split('\n')[0]
  const run = await runClient(runId)
  const put = async (name: string, subject: string, content: string, version: number) =>
    unwrap(await run.put(`/api/run-state/notes/${name}`, { data: { subject, content, expected_version: version } }))
  try {
    await put('risk-register', 'project:atlas', '# Atlas risks\n\n1. Vendor access (PRJ-11) is late. Owner: Sam Lee.', 0)
    await put('risk-register', 'project:atlas', '# Atlas risks\n\n1. Vendor access (PRJ-11) is late and blocked. Owner: Sam Lee.\n2. Sub-task PRJ-13 has no owner.', 1)
    await put('stakeholders', 'project:birch', '# Birch stakeholders\n\n- Sponsor: Dana Ruiz\n- Weekly report goes to the steering group.', 0)
    const log = async (subject: string, action: string, details: Record<string, unknown>, key: string) =>
      unwrap(await run.post('/api/run-state/log', { data: { subject, action, details, idempotency_key: key } }), 201)
    await log('project:atlas', 'risk:raised', { risk: 'Vendor access is late', owner: 'Sam Lee' }, `atlas/risk/vendor/${runId}`)
    await log('project:birch', 'kickoff:scheduled', { date: dayISO(2) }, `birch/kickoff/${runId}`)
  } finally {
    await run.dispose()
  }
  await unwrap(await api.put(`/api/crews/${pmo.crewId}/state/notes`, { data: {
    name: 'risk-register', subject: 'project:atlas', expected_version: 2,
    content: `# Atlas risks\n\n1. Vendor access (PRJ-11) is late and blocked. Owner: Sam Lee. New date agreed for ${dayText(14)}.\n2. Sub-task PRJ-13 has no owner.`,
  } }))
}

/**
 * The PMO crew's memory for the Memory tab pictures: Automatic mode with review on, two memories people added (one
 * pinned) and two an earlier automatic run saved, which wait for review (review covers every run save). The run's saves go through the run-state
 * API, so the fake LLM's /v1/embeddings embeds them like a real run's.
 */
async function crewMemories(api: APIRequestContext): Promise<void> {
  const settings = { mode: 'automatic', llm_id: null, recall_depth: 'shallow', recall_limit: 5, saves_per_run: 20, retention_days: 365,
    review_run_saves: true, recency_weight: 0.3, semantic_weight: 0.5, importance_weight: 0.2, recency_half_life_days: 30 }
  await unwrap(await api.put(`/api/crews/${pmo.crewId}/memory/settings`, { data: settings }))
  const existing = await unwrap<{ items: Item[] } | Item[]>(await api.get(`/api/crews/${pmo.crewId}/memory?limit=50`))
  if ((Array.isArray(existing) ? existing : existing.items).length > 0) return
  const add = async (content: string, scope: string, categories: string[], importance: number, pinned: boolean) =>
    unwrap(await api.post(`/api/crews/${pmo.crewId}/memory`, { data: { content, scope, categories, importance, pinned } }), 201)
  await add('Dana Ruiz wants the Birch weekly report before Monday 09:00, not later in the day.', '/people/dana-ruiz', ['people', 'reporting'], 0.8, true)
  await add('Sam Lee usually replies late on Fridays; chase PRJ-11 on Thursday afternoons instead.', '/people/sam-lee', ['people'], 0.6, false)
  const spec = JSON.stringify({ memory: { ...settings, llm_id: pmo.llmId, embed_model: 'text-embedding-3-small:768' } })
  const runId = psql(DB_NAME, `INSERT INTO runs (crew_id, crew_name, status, budget_usd, spec, area_ids, started_at, finished_at)
    VALUES ('${pmo.crewId}', '${PMO}', 'succeeded', 1, $spec$${spec}$spec$, ARRAY['${pmo.areaId}']::uuid[], now() - interval '3 hours', now() - interval '3 hours' + interval '52 seconds')
    RETURNING id`).split('\n')[0]
  const run = await runClient(runId)
  try {
    const meta = { agent: 'PMO Coordinator', task: 'Daily sweep' }
    await unwrap(await run.post('/api/run-state/memory/records', { data: { records: [
      { content: 'PRJ-15 dates have slipped three times; ask the owner for the reason, not just a new date.', scope: '/projects/atlas',
        categories: ['deadlines'], importance: 0.7, metadata: meta },
      { content: 'The vendor status page lags real outages by about an hour; check the vendor Slack channel first.', scope: '/vendors/acme',
        categories: ['vendor'], importance: 0.5, metadata: meta },
    ] } }), 201)
  } finally {
    await run.dispose()
  }
}

const ADVISER = 'Delivery Adviser'

/**
 * Agent chat: an adviser in the PMO's area that may ask two specialists, start the Atlas PMO or the E2E crew, and read
 * the PMO's Jira and KM2 connections. It
 * exists before the crew chat pictures are taken, so the Chat page shows its Crews and Agents switch there too.
 */
async function agentChat(api: APIRequestContext): Promise<void> {
  const agent = (name: string, role: string, goal: string) => namedOrCreate(api, '/api/agents', name, {
    name, role, goal, backstory: 'Has run delivery for large IT programmes.', llm_id: pmo.llmId, tools: [], max_iter: 4,
    allow_delegation: false, verbose: false, area_ids: [pmo.areaId],
  })
  pmo.adviserId = await agent(ADVISER, 'Delivery adviser', 'Answer questions about how projects are going and what to do next')
  const specialists = [
    await agent('Risk Analyst', 'Delivery risk analyst', 'Find and rank the risks to a project'),
    await agent('Vendor Researcher', 'Vendor researcher', 'Check what is known about a vendor and its track record'),
  ]
  const libCrew = (await list(api, '/api/crews')).find((c) => c.name === LIB.crew)!.id
  await unwrap(await api.put(`/api/agents/${pmo.adviserId}/chat`, { data: {
    enabled: true, llm_id: pmo.llmId, specialist_ids: specialists, crew_ids: [pmo.crewId, libCrew], connection_ids: [pmo.conns.jira, pmo.conns.km2],
    daily_budget_usd: 1, person_daily_budget_usd: 5,
  } }))
}

test.describe('PMO screens', () => {
  test.beforeAll(async () => {
    test.setTimeout(SETUP_TIMEOUT)
    const api = await apiAs('admin')
    try {
      await installPmo(api)
      await sweepChecks(api)
      await weeklyReport(api)
      await crewMemory(api)
      await agentChat(api)
    } finally {
      await api.dispose()
    }
  })

  test('connections', async ({ page }) => {
    await shot(page, 'connections.png', '/connections', async (p) => {
      const jira = p.getByRole('row').filter({ hasText: CONN.jira })
      await jira.getByRole('button', { name: 'Test' }).click()
      await expect(jira.getByText('Connected as Studio Bot.')).toBeVisible()
    })

    const km2 = page.getByRole('row').filter({ hasText: CONN.km2 })
    await km2.getByRole('button', { name: 'Test' }).click()
    await expect(km2.getByText('Connected: the key sees 3 folders.')).toBeVisible()
    await km2.getByText('Folders the key can see (3)').click()
    await expect(km2.getByText('default for saves')).toBeVisible()
    await capture(page, 'connection-km2-test.png')

    await page.reload()
    await settled(page)
    await page.getByRole('row').filter({ hasText: CONN.jira }).getByRole('button', { name: 'People map' }).click()
    const map = page.getByRole('dialog')
    await map.getByRole('button', { name: 'Read people from the tracker' }).click()
    await expect(map.getByText('Dana Ruiz')).toBeVisible()
    await expect(map.getByText('Sam Lee')).toBeVisible()
    await capture(page, 'connection-people-map.png')
    await map.getByRole('button', { name: 'Close' }).last().click()
    await expect(map).toBeHidden()

    // A new connection, filled in but never saved; the secret field stays empty
    await page.getByRole('button', { name: 'New connection' }).first().click()
    const editor = page.getByRole('dialog')
    await editor.getByLabel('Name').fill('Birch Jira')
    await editor.getByLabel('Kind').selectOption('jira')
    await editor.getByLabel('Site URL').fill('https://birch-projects.atlassian.net')
    await editor.getByLabel('Account email').fill('studio-bot@example.com')
    await editor.getByRole('option', { name: new RegExp(`^${PMO_AREA}\\b`) }).click()
    // Picking the area scrolls the window down; show its top half (name, kind and the Jira settings)
    await editor.getByLabel('Name').scrollIntoViewIfNeeded()
    await capture(page, 'connection-editor.png')
    await page.keyboard.press('Escape')
  })

  test('Set up a PMO wizard', async ({ page }) => {
    await shot(page, 'pmo-setup.png', '/pmo/setup', async (p) => {
      await p.getByLabel('Name').fill('Birch PMO')
      await p.getByRole('option', { name: new RegExp(`^${PMO_AREA}\\b`) }).click()
      await p.getByLabel('Time zone').selectOption('Europe/London')
      await p.getByRole('button', { name: 'Next' }).click()
      await p.getByLabel('Tracker connection').selectOption(pmo.conns.jira)
      await p.getByRole('button', { name: 'Next' }).click()
      await p.getByRole('button', { name: 'Add destination' }).click()
      const first = p.getByTestId('destination-row').nth(0)
      await first.getByLabel('Destination', { exact: true }).selectOption('slack')
      await first.getByLabel('Connection').selectOption(pmo.conns.slack)
      await p.getByRole('button', { name: 'Add destination' }).click()
      const second = p.getByTestId('destination-row').nth(1)
      await second.getByLabel('Destination', { exact: true }).selectOption('email')
      await second.getByLabel('Connection').selectOption(pmo.conns.email)
      await second.getByLabel('Recipients').fill(RECIPIENTS.join(', '))
    })
    await page.getByRole('button', { name: 'Next' }).click()
    await page.getByLabel('PMO Coordinator model').selectOption(pmo.llmId)
    await page.getByLabel('Status Reporter model').selectOption(pmo.llmId)
    await expect(page.getByTestId('pmo-summary')).toContainText('Slack, Email')
    await capture(page, 'pmo-setup-review.png')
  })

  test('crew State tab', async ({ page }) => {
    await page.goto(`/crews/${pmo.crewId}?tab=state`)
    await settled(page)
    await expect(page.getByText('PRJ-10', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: `${PMO}: daily sweep` })).toBeVisible()

    // Only the Connections panel: its help section is for people who can see connections, not crew state
    const connections = panel(page, /^Connections$/)
    await expect(connections.getByText(CONN.jira)).toBeVisible()
    await page.mouse.move(0, 0)
    await connections.screenshot({ path: path.join(OUT, 'crew-state-connections.png'), animations: 'disabled' })

    await scrollToTop(panel(page, /Watches/))
    await capture(page, 'crew-state.png')

    const tracking = panel(page, /^Tracking list$/)
    await tracking.getByRole('button', { name: 'Track an item' }).click()
    await tracking.getByRole('textbox', { name: 'Item' }).fill('PRJ-11')
    await tracking.getByLabel('Project (optional)').fill('Atlas vendor')
    await tracking.getByLabel('Note (optional)').fill('Vendor onboarding')
    await tracking.getByRole('button', { name: 'Look up' }).click()
    await expect(tracking.getByTestId('tracking-check').getByText('Vendor access')).toBeVisible()
    await scrollToTop(tracking)
    await capture(page, 'crew-state-track-item.png')
    await tracking.getByTestId('tracking-check').getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('row').filter({ hasText: 'risk-register' }).getByRole('button', { name: 'History' }).click()
    const history = page.getByRole('dialog')
    await expect(history.getByRole('button', { name: /^v3/ })).toBeVisible()
    await history.getByRole('button', { name: /^v2/ }).click()
    await expect(history.getByText('Sub-task PRJ-13 has no owner.')).toBeVisible()
    await capture(page, 'crew-state-note-history.png')
    await history.getByRole('button', { name: 'Close' }).last().click()
    await expect(history).toBeHidden()

    // The page ends with Recent KM2 saves and the Action log, so one picture serves both help sections
    await expect(page.getByRole('table', { name: 'Recent KM2 saves' }).getByText(/atlas-weekly-/)).toBeVisible()
    const log = panel(page, /^Action log$/)
    await expect(log.getByText(/^followup:/).first()).toBeVisible()
    await scrollToTop(log)
    await capture(page, 'crew-state-action-log.png')
  })

  test('crew Memory tab', async ({ page }) => {
    const api = await apiAs('admin')
    try {
      await crewMemories(api)
    } finally {
      await api.dispose()
    }
    await page.goto(`/crews/${pmo.crewId}?tab=memory`)
    await settled(page)
    const settings = panel(page, /^Memory settings$/)
    await expect(settings.getByLabel('Memory', { exact: true })).toHaveValue('automatic')
    await expect(settings.getByRole('switch', { name: /Review memories that runs save/ })).toBeVisible()
    await scrollToTop(settings)
    await capture(page, 'memory-settings.png')

    const rows = page.getByTestId('memory-row')
    await expect(rows).toHaveCount(4)
    await expect(rows.filter({ hasText: 'waiting for review' })).toHaveCount(2)
    await scrollToTop(page.getByRole('button', { name: 'Add a memory' }), 24)
    await capture(page, 'memory.png')
  })

  test('schedule Watch panel', async ({ page }) => {
    await page.goto(`/schedules/${pmo.sweepId}`)
    await settled(page)
    const watch = panel(page, /^Watch$/)
    await expect(watch.getByText(/items in the snapshot/)).toBeVisible()
    await scrollToTop(watch)
    await capture(page, 'watch-panel.png')

    await scrollToTop(page.getByRole('switch', { name: /Follow-up ladder/ }), 40)
    await capture(page, 'watch-ladder.png')

    await watch.getByRole('button', { name: 'Preview' }).click()
    const preview = page.getByTestId('watch-preview')
    await expect(preview.getByText(/would (start a run|not run)/)).toBeVisible({ timeout: 30_000 })
    await scrollToTop(preview, 60)
    await capture(page, 'watch-preview.png')

    await page.reload()
    await settled(page)
    const recent = page.getByText('Recent checks', { exact: true })
    await expect(page.getByText('no changes', { exact: true }).first()).toBeVisible()
    await scrollToTop(recent, 220)
    await capture(page, 'watch-checks.png')
  })

  test('report destinations on a schedule', async ({ page }) => {
    await page.goto(`/schedules/${pmo.weeklyId}`)
    await settled(page)
    const destinations = panel(page, /Report destinations/)
    await expect(destinations.getByTestId('destination-row')).toHaveCount(3)
    await scrollToTop(destinations)
    await capture(page, 'schedule-destinations.png')
  })

  test('work order: what the watch found, and report deliveries', async ({ page }) => {
    await page.goto(`/work-orders/${pmo.sweepWO}`)
    await settled(page)
    const found = panel(page, /^What the watch found$/)
    await expect(found.getByTestId('rollup').first()).toBeVisible()
    await scrollToTop(found)
    await capture(page, 'work-order-watch.png')

    await page.goto(`/work-orders/${pmo.weeklyWO}?tab=reports`)
    await settled(page)
    const card = page.getByTestId('report-card')
    await expect(card.getByTestId('delivery-row').filter({ hasText: 'Slack:' }).getByText('failed', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Retry failed deliveries' })).toBeVisible()
    // The designed report: Crew Studio's counts and charts around the reporter's narrative
    await card.getByRole('button', { name: 'Show report' }).click()
    await expect(card.getByTestId('due-report').getByTestId('kpi-overdue')).toBeVisible()
    await capture(page, 'work-order-reports.png')
  })

  test('chat', async ({ page }) => {
    test.setTimeout(180_000)
    await page.goto(`/chat?crew=${pmo.crewId}`)
    await settled(page)
    await page.getByRole('textbox', { name: 'Message' }).fill('What is slipping in Atlas this week?')
    await page.getByRole('textbox', { name: 'Message' }).press('Enter')
    await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
    const conversation = page.url().split('/').pop()!
    await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 60_000 })
    // The fake LLM proposes a work order when it sees this marker; the stored texts are tidied below
    await page.getByRole('textbox', { name: 'Message' }).fill('Please run a sweep of Atlas now. E2E_CHAT_PROPOSE')
    await page.getByRole('textbox', { name: 'Message' }).press('Enter')
    const card = page.getByRole('region', { name: 'Proposed change' })
    await expect(card.getByText('Waiting for you')).toBeVisible({ timeout: 60_000 })
    await expect(page.getByText('FAKE-LLM-ANSWER')).toHaveCount(2, { timeout: 60_000 })

    const replies = [
      'Two things are slipping in **Atlas**:\n\n- **PRJ-11 Vendor access** is 3 days overdue and blocked; Sam Lee owns it.\n- **PRJ-13** has no owner and is due after the rollout itself.\n\nEverything else under PRJ-10 is on track.',
      'I can start a sweep of Atlas now. It reads the tracking list and lists what slipped; confirm below to start it.',
    ]
    psql(DB_NAME, `
      UPDATE conversation_messages SET content = replace(content, ' E2E_CHAT_PROPOSE', '') WHERE conversation_id = '${conversation}' AND role = 'user';
      UPDATE conversation_messages SET proposal = jsonb_set(jsonb_set(proposal, '{payload,title}', '"Atlas sweep"'), '{summary}', '"Start a work order: Atlas sweep"')
        WHERE conversation_id = '${conversation}' AND proposal IS NOT NULL;
      WITH r AS (SELECT id, row_number() OVER (ORDER BY created_at) AS n FROM conversation_messages WHERE conversation_id = '${conversation}' AND role = 'assistant')
      UPDATE conversation_messages m SET content = CASE r.n WHEN 1 THEN $a$${replies[0]}$a$ ELSE $b$${replies[1]}$b$ END FROM r WHERE m.id = r.id;
      UPDATE conversations SET title = 'What is slipping in Atlas this week?' WHERE id = '${conversation}';`)
    await page.reload()
    await settled(page)
    await expect(card.getByText('Start a work order: Atlas sweep')).toBeVisible()
    await capture(page, 'chat.png')

    await card.getByRole('button', { name: 'Confirm' }).click()
    await expect(card.getByText('Confirmed', { exact: true })).toBeVisible()
    const opened = card.getByRole('link', { name: /Open WO-\d+/ })
    await expect(opened).toBeVisible()
    await capture(page, 'chat-proposal-confirmed.png')
    // Let the work order it started finish, so the overview and lists taken next show no half-done run
    const api = await apiAs('admin')
    try {
      await succeeded(api, (await opened.getAttribute('href'))!.split('/').pop()!)
    } finally {
      await api.dispose()
    }
  })

  test('agent chat', async ({ page }) => {
    test.setTimeout(180_000)
    await page.goto(`/chat?agent=${pmo.adviserId}`)
    await settled(page)
    await expect(page.getByRole('tab', { name: 'Agents' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('Can ask: Risk Analyst, Vendor Researcher.')).toBeVisible()
    await page.getByRole('textbox', { name: 'Message' }).fill('Which vendors could hold up the Atlas rollout?')
    await page.getByRole('textbox', { name: 'Message' }).press('Enter')
    await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
    const conversation = page.url().split('/').pop()!
    await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 60_000 })
    // The fake LLM proposes a run of the E2E crew when it sees this marker; the card is pointed at the Atlas PMO below
    await page.getByRole('textbox', { name: 'Message' }).fill('Can you get the PMO to review the vendor risk this week? E2E_AGENT_PROPOSE')
    await page.getByRole('textbox', { name: 'Message' }).press('Enter')
    const card = page.getByRole('region', { name: 'Proposed change' })
    await expect(card.getByText('Waiting for you')).toBeVisible({ timeout: 60_000 })
    await expect(page.getByText('FAKE-LLM-ANSWER')).toHaveCount(2, { timeout: 60_000 })

    const replies = [
      'I asked the **Vendor Researcher**. One vendor matters for Atlas:\n\n- **Acme Identity** owes the vendor access (PRJ-11), which is 3 days overdue and blocked. Its last two projects also slipped on access.\n\nThe **Risk Analyst** rates it the biggest risk to the rollout date.',
      'The Atlas PMO can review it. Confirm below to start a work order on it.',
    ]
    const payload = JSON.stringify({ crew_id: pmo.crewId, crew_name: PMO, title: 'Atlas vendor risk review',
      brief: 'Review the vendor risk on Atlas (PRJ-11, Acme Identity) and suggest what to do before the rollout.' })
    psql(DB_NAME, `
      UPDATE conversation_messages SET content = replace(content, ' E2E_AGENT_PROPOSE', '') WHERE conversation_id = '${conversation}' AND role = 'user';
      UPDATE conversation_messages SET proposal = jsonb_set(jsonb_set(proposal, '{payload}', $p$${payload}$p$::jsonb), '{summary}', '"Start a work order on ${PMO}: Atlas vendor risk review"')
        WHERE conversation_id = '${conversation}' AND proposal IS NOT NULL;
      WITH r AS (SELECT id, row_number() OVER (ORDER BY created_at) AS n FROM conversation_messages WHERE conversation_id = '${conversation}' AND role = 'assistant')
      UPDATE conversation_messages m SET content = CASE r.n WHEN 1 THEN $a$${replies[0]}$a$ ELSE $b$${replies[1]}$b$ END FROM r WHERE m.id = r.id;
      UPDATE conversations SET title = 'Which vendors could hold up the Atlas rollout?' WHERE id = '${conversation}';`)
    await page.reload()
    await settled(page)
    await expect(card.getByText(`Start a work order on ${PMO}: Atlas vendor risk review`)).toBeVisible()
    await capture(page, 'chat-agents.png')
  })

  test('chat settings on an agent', async ({ page }) => {
    await page.goto(`/agents/${pmo.adviserId}`)
    await settled(page)
    const chat = panel(page, /^Chat$/)
    await expect(chat.getByRole('switch', { name: /People can chat with this agent/ })).toBeChecked()
    // Scroll the specialists list to the chosen ones; they sort among every other agent
    await chat.getByRole('option', { name: /^Risk Analyst/ }).scrollIntoViewIfNeeded()
    // From the specialists down, so the crews and the connections it may read fit too
    await expect(chat.getByRole('option', { name: new RegExp(`^${CONN.jira}`) })).toHaveAttribute('aria-selected', 'true')
    await scrollToTop(chat.getByText('Specialists it may ask', { exact: true }))
    await capture(page, 'agent-chat-settings.png')
  })

  test('chat settings in the crew builder', async ({ page }) => {
    await page.goto(`/crews/${pmo.crewId}`)
    await settled(page)
    const chat = panel(page, /^Chat$/)
    await expect(chat.getByLabel('Front desk agent')).toBeVisible()
    await scrollToTop(chat)
    await capture(page, 'chat-settings.png')
  })
})

test('console screens', async ({ page }) => {
  await shot(page, 'overview.png', '/')
  await shot(page, 'work-orders.png', '/work-orders')
  await shot(page, 'work-order-new.png', '/work-orders/new')
  await shot(page, 'work-order-detail.png', `/work-orders/${ids.workOrder}`)
  await shot(page, 'runs.png', '/runs')
  await shot(page, 'run-detail.png', `/runs/${ids.run}`, async (p) => {
    await expect(p.locator('.react-flow__node').first()).toBeVisible({ timeout: 15_000 })
  })
})

test('build screens', async ({ page }) => {
  await shot(page, 'crews.png', '/crews')
  await shot(page, 'crew-builder.png', `/crews/${ids.crew}`, async (p) => {
    await expect(p.locator('.react-flow__node').first()).toBeVisible({ timeout: 15_000 })
  })
  await page.getByRole('main').getByRole('button', { name: 'Run', exact: true }).first().click()
  await expect(page.getByRole('dialog').getByRole('heading', { name: new RegExp(`Run ${LIB.crew}`) })).toBeVisible()
  await page.waitForTimeout(300)
  await page.screenshot({ path: path.join(OUT, 'run-crew-modal.png'), animations: 'disabled' })
  await page.keyboard.press('Escape')
  await shot(page, 'agents.png', '/agents')
  await shot(page, 'agent-editor.png', `/agents/${ids.agent}`)
  await shot(page, 'tasks.png', '/tasks')
  await shot(page, 'task-editor.png', `/tasks/${ids.task}`)
})

test('automation and configuration screens', async ({ page }) => {
  await shot(page, 'schedules.png', '/schedules')
  await shot(page, 'schedule-editor.png', `/schedules/${ids.schedule}`)
  await shot(page, 'triggers.png', '/triggers')
  await shot(page, 'trigger-editor.png', `/triggers/${ids.trigger}`)
  await shot(page, 'llms.png', '/llms')
  await shot(page, 'pricing.png', '/pricing')
  await shot(page, 'settings.png', '/settings')
  await shot(page, 'history.png', '/history')
  await shot(page, 'account-api-keys.png', '/account/api-keys')
})

test('API explorer', async ({ page }) => {
  await shot(page, 'api-explorer.png', '/api-docs', async (p) => {
    await expect(p.locator('.swagger-ui .opblock').first()).toBeVisible({ timeout: 20_000 })
  })
})

test('admin screens', async ({ page }) => {
  await shot(page, 'admin-people.png', '/admin/users')
  await shot(page, 'admin-person-editor.png', `/admin/users/${ids.person}`)
  await shot(page, 'admin-service-accounts.png', '/admin/service-accounts')
  await shot(page, 'admin-api-keys.png', '/admin/api-keys')
  await shot(page, 'admin-roles.png', '/admin/roles')
  await shot(page, 'admin-role-editor.png', `/admin/roles/${ids.role}`)
  await shot(page, 'admin-areas.png', '/admin/areas')
  await shot(page, 'admin-sign-in.png', '/admin/access')
  await shot(page, 'admin-audit.png', '/admin/audit')
})

test('help page', async ({ page }) => {
  await shot(page, 'help.png', '/help', async (p) => {
    await expect(p.getByRole('navigation', { name: 'Help contents' })).toBeVisible()
  })
})
