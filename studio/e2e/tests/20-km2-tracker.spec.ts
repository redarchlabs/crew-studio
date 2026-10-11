import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { psql } from '../harness/db.ts'
import { DB_NAME, FAKE_KM2_URL } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'
import { toast, uid } from '../support/ui.ts'

// KM2 issues as a tracker, through the fake KM2 organisation (harness/fake_km2_tracker.py): a km2_tracker connection
// made in the UI, refused for a plain-http address and tested against the trk_* entities, its people map, a watch on
// project ERP whose first run posts a comment as the Crew Studio person (the fake LLM calls Comment on Tracker Issue
// when the task holds E2E_KM2_TRACKER_COMMENT), and a second check that reports a changed issue and a person's new
// comment but not Studio's own. Then a reporting crew tracking ERP-1 delivers a daily report (E2E_DELIVER_DAILY): Crew
// Studio reads the tracked items itself and the Reports tab shows the designed report, tiles and charts, with the text
// and an HTML download behind it. No real KM2 is contacted.

const CONN = uid('E2E KM2 Issues')
const KEY = 'km2_e2e-tracker-key'
const BOT = '9e000000-0000-4000-8000-000000000001'
const SAM = '9e000000-0000-4000-8000-000000000003'
const names = { agent: uid('E2E Tracker Follow-up'), task: uid('E2E ERP follow-up'), crew: uid('E2E ERP Crew'), schedule: uid('E2E ERP sweep') }
const ids = { agent: '', task: '', crew: '', schedule: '', connection: '', reporter: '', reportTask: '', reportCrew: '' }

type Check = { outcome: string; work_order_id: string | null; added: number; changed: number }
type TrackerComment = { issue_key: string; author: string | null; author_name: string; body: string; mentions: string | null }
type Changes = { changed: { id: string }[]; new_comments: { id: string; author: string; excerpt: string }[] }

async function fakeTracker<T>(path: string, body?: Record<string, string>): Promise<T> {
  const res = await fetch(`${FAKE_KM2_URL}${path}`, body ? { method: 'POST', body: JSON.stringify(body), headers: { 'content-type': 'application/json' } } : {})
  return (await res.json()) as T
}

/** Makes the schedule due; the scheduler picks it up within its 20-second tick. */
function makeDue(): void {
  psql(DB_NAME, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = '${ids.schedule}'`)
}

async function waitForCheck(api: APIRequestContext, count: number): Promise<Check> {
  let latest: Check | undefined
  await expect(async () => {
    const view = await unwrap<{ checks: Check[] }>(await api.get(`/api/schedules/${ids.schedule}/watch`))
    expect(view.checks.length).toBeGreaterThanOrEqual(count)
    latest = view.checks[0]
  }).toPass({ timeout: 75_000, intervals: [2000] })
  return latest!
}

async function waitForSuccess(page: Page, workOrderId: string): Promise<void> {
  await page.goto(`/work-orders/${workOrderId}`)
  await expect(page.getByRole('main').locator('header').getByText('succeeded', { exact: true }).first()).toBeVisible({ timeout: 120_000 })
}

test.describe.configure({ mode: 'serial' })

test.beforeAll(async () => {
  const lib = await ensureLibrary()
  const api = await apiAs('admin')
  ids.agent = (await unwrap<{ id: string }>(await api.post('/api/agents', { data: {
    name: names.agent, role: 'Tracker follow-up', goal: 'Chase blocked ERP work', backstory: 'A tidy project coordinator.', llm_id: lib.plainLlmId,
    tools: ['tracker_get_issue', 'tracker_comment'], max_iter: 3, allow_delegation: false, verbose: false, area_ids: [lib.areaId],
  } }), 201)).id
  ids.task = (await unwrap<{ id: string }>(await api.post('/api/tasks', { data: {
    name: names.task, description: 'Follow up on the blocked ERP issue. E2E_KM2_TRACKER_COMMENT', expected_output: 'A short note.',
    agent_id: ids.agent, context_task_ids: [], markdown: true, area_ids: [lib.areaId],
  } }), 201)).id
  ids.crew = (await unwrap<{ id: string }>(await api.post('/api/crews', { data: {
    name: names.crew, description: 'Follows ERP issues in KM2', process: 'sequential', manager_llm_id: null, agent_ids: [ids.agent], task_ids: [ids.task],
    inputs: [], budget_usd: 0.5, area_ids: [lib.areaId],
  } }), 201)).id
  ids.schedule = (await unwrap<{ id: string }>(await api.post('/api/schedules', { data: {
    name: names.schedule, crew_id: ids.crew, cron: '0 9 * * *', timezone: 'UTC', overlap_policy: 'allow', enabled: true, brief: 'ERP sweep', inputs: {},
  } }), 201)).id
  await api.dispose()
})

test.afterAll(async () => {
  const api = await apiAs('admin')
  if (ids.schedule) await api.delete(`/api/schedules/${ids.schedule}`)
  for (const crew of [ids.crew, ids.reportCrew]) if (crew) await api.put(`/api/crews/${crew}/connections`, { data: { connection_ids: [] } })
  for (const [path, id] of [['crews', ids.reportCrew], ['tasks', ids.reportTask], ['agents', ids.reporter]] as const) if (id) await api.delete(`/api/${path}/${id}`)
  if (ids.connection) await api.delete(`/api/connections/${ids.connection}`)
  for (const [path, id] of [['crews', ids.crew], ['tasks', ids.task], ['agents', ids.agent]] as const) if (id) await api.delete(`/api/${path}/${id}`)
  await api.dispose()
})

test('a KM2 issues connection is added in the UI, refuses plain http, and checks the organisation', async ({ page }) => {
  await page.goto('/connections')
  await page.getByRole('button', { name: 'New connection' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Name').fill(CONN)
  await dialog.getByLabel('Kind').selectOption('km2_tracker')
  await dialog.getByLabel('Base URL').fill('http://km2.example.com')
  await dialog.getByLabel(/person email/).fill('crew-studio@e2e.example')
  await dialog.getByLabel('Org API key').fill(KEY)
  await dialog.getByRole('option', { name: new RegExp(`^${LIB.area}`) }).click()
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(dialog.getByText(/must be https/)).toBeVisible()
  await dialog.getByLabel('Base URL').fill(FAKE_KM2_URL)
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(toast(page, `Saved ${CONN}`)).toBeVisible()

  const api = await apiAs('admin')
  const list = await unwrap<{ id: string; name: string; kind: string }[]>(await api.get('/api/connections'))
  const conn = list.find((c) => c.name === CONN)!
  expect(conn.kind).toBe('km2_tracker')
  ids.connection = conn.id
  await unwrap(await api.put(`/api/crews/${ids.crew}/connections`, { data: { connection_ids: [ids.connection] } }))
  await api.dispose()

  await page.reload()
  const row = page.getByRole('row').filter({ hasText: CONN })
  await row.getByRole('button', { name: 'Test' }).click()
  await expect(row.getByText('Connected as Crew Studio. The organisation has the tracker entities.')).toBeVisible()

  // The people map reads the trk_person records, without the Crew Studio person itself
  await row.getByRole('button', { name: 'People map' }).click()
  const people = page.getByRole('dialog')
  await people.getByRole('button', { name: 'Read people from the tracker' }).click()
  await expect(toast(page, /Read 2 people from/)).toBeVisible()
  await expect(people.getByText('Priya Patel')).toBeVisible()
  await expect(people.getByText('Sam Lee')).toBeVisible()
  await people.getByRole('button', { name: 'Close' }).last().click()
})

test('a watch on project ERP is previewed and saved, posting for real', async ({ page }) => {
  await page.goto(`/schedules/${ids.schedule}`)
  await page.getByRole('button', { name: 'Add a watch' }).click()
  await page.getByLabel('Source').selectOption('tracker')
  await page.getByLabel('Connection', { exact: true }).selectOption(ids.connection)
  await expect(page.getByLabel(/Extra JQL/)).toHaveCount(0)
  await page.getByLabel('Project').fill('ERP')
  await page.getByRole('button', { name: 'Preview' }).click()
  const preview = page.getByTestId('watch-preview')
  await expect(preview.getByText('would start a run')).toBeVisible()
  await expect(preview.getByText('km2_tracker:ERP-2', { exact: true })).toBeVisible()
  await expect(preview.getByText('km2_tracker:ERP-3', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Add watch' }).click()
  await expect(toast(page, 'Saved the watch')).toBeVisible()

  const api = await apiAs('admin')
  const view = await unwrap<{ watch: Record<string, unknown> }>(await api.get(`/api/schedules/${ids.schedule}/watch`))
  await unwrap(await api.put(`/api/schedules/${ids.schedule}/watch`, { data: { ...view.watch, dry_run: false } }))
  await api.dispose()
})

test('the first check runs the crew, which comments on ERP-2 as the Crew Studio person', async ({ page }) => {
  const api = await apiAs('admin')
  makeDue()
  const first = await waitForCheck(api, 1)
  expect(first.outcome).toBe('started')
  expect(first.added).toBe(3)
  await waitForSuccess(page, first.work_order_id!)
  const comments = await fakeTracker<TrackerComment[]>('/__tracker/comments')
  const ours = comments.filter((c) => c.author === BOT)
  expect(ours).toHaveLength(1)
  expect(ours[0]).toMatchObject({ issue_key: 'ERP-2', author_name: 'Crew Studio', body: '@Sam Lee\n\nIs the vendor sandbox unblocked yet?', mentions: SAM })
  await api.dispose()
})

test('the next check reports a changed issue and a new comment, leaving out Studio\'s own', async ({ page }) => {
  const api = await apiAs('admin')
  await fakeTracker('/__tracker/comment', { issue: 'ERP-2', author: 'sam', body: 'Sandbox credentials arrive Thursday.' })
  await fakeTracker('/__tracker/issue', { issue: 'ERP-4', status: 'In Progress', status_category: 'in_progress' })
  makeDue()
  const second = await waitForCheck(api, 2)
  expect(second.outcome).toBe('started')
  expect(second.changed).toBe(1)
  const wo = await unwrap<{ inputs: Record<string, string> }>(await api.get(`/api/work-orders/${second.work_order_id}`))
  const changes = JSON.parse(wo.inputs.changes) as Changes
  expect(changes.changed.map((c) => c.id)).toEqual(['km2_tracker:ERP-4'])
  expect(changes.new_comments).toEqual([expect.objectContaining({ id: 'km2_tracker:ERP-2', author: 'Sam Lee', excerpt: 'Sandbox credentials arrive Thursday.' })])
  await waitForSuccess(page, second.work_order_id!)
  await page.getByRole('tab', { name: /^Brief/ }).click()
  await expect(page.getByText('Sandbox credentials arrive Thursday.').first()).toBeVisible()
  await api.dispose()
})

type DueReport = { kpis: { overdue: number; blocked: number; open: number }; project: string; cadence: string; items: { key: string; assignee?: string }[] }
type ReportsView = { reports: { id: string; cadence: string; snapshot: DueReport | null }[] }

test('a daily report carries Crew Studio\'s own reading of ERP and shows tiles and charts', async ({ page }) => {
  const lib = await ensureLibrary()
  const api = await apiAs('admin')
  ids.reporter = (await unwrap<{ id: string }>(await api.post('/api/agents', { data: {
    name: uid('E2E ERP Reporter'), role: 'Status reporter', goal: 'Report on ERP every day', backstory: 'Writes short status notes.', llm_id: lib.plainLlmId,
    tools: ['deliver_report'], max_iter: 3, allow_delegation: false, verbose: false, area_ids: [lib.areaId],
  } }), 201)).id
  ids.reportTask = (await unwrap<{ id: string }>(await api.post('/api/tasks', { data: {
    name: uid('E2E ERP daily report'), description: 'Deliver the daily ERP report. E2E_DELIVER_DAILY', expected_output: 'A short note.',
    agent_id: ids.reporter, context_task_ids: [], markdown: true, area_ids: [lib.areaId],
  } }), 201)).id
  ids.reportCrew = (await unwrap<{ id: string }>(await api.post('/api/crews', { data: {
    name: uid('E2E ERP Reporting'), description: 'Reports on ERP', process: 'sequential', manager_llm_id: null, agent_ids: [ids.reporter],
    task_ids: [ids.reportTask], inputs: [], budget_usd: 0.5, area_ids: [lib.areaId],
  } }), 201)).id
  await unwrap(await api.put(`/api/crews/${ids.reportCrew}/connections`, { data: { connection_ids: [ids.connection] } }))
  await unwrap(await api.post(`/api/crews/${ids.reportCrew}/state/tracking`, { data: { connection_id: ids.connection, item_ref: 'ERP-1', project: 'ERP' } }), 201)
  // ERP-4 (unassigned) comes due in two days, so the timeline and the unassigned section have something to show
  const inTwoDays = new Date(Date.now() + 2 * 86_400_000).toISOString().slice(0, 10)
  await fakeTracker('/__tracker/issue', { issue: 'ERP-4', due_date: inTwoDays })
  const wo = await unwrap<{ id: string }>(await api.post('/api/work-orders', { data: {
    title: 'ERP daily report', brief: 'Report on ERP.', crew_id: ids.reportCrew, inputs: {}, budget_usd: null,
  } }), 201)
  await unwrap(await api.post(`/api/work-orders/${wo.id}/submit`, { data: {} }))
  await waitForSuccess(page, wo.id)

  const view = await unwrap<ReportsView>(await api.get(`/api/work-orders/${wo.id}/reports`))
  expect(view.reports).toHaveLength(1)
  const snap = view.reports[0].snapshot!
  // ERP-2 is Sam's, three days late and blocked; ERP-3 is done
  expect(snap).toMatchObject({ project: 'ERP', cadence: 'daily', kpis: { overdue: 1, blocked: 1 } })
  expect(snap.items.find((i) => i.key === 'ERP-2')?.assignee).toBe('Sam Lee')
  expect(snap.items.find((i) => i.key === 'ERP-3')).toBeUndefined()

  await page.getByRole('tab', { name: 'Reports' }).click()
  const card = page.getByTestId('report-card')
  await expect(card.getByText(/^ERP daily report, \d{4}-\d{2}-\d{2}$/)).toBeVisible()
  await card.getByRole('button', { name: 'Show report' }).click()
  const report = card.getByTestId('due-report')
  await expect(report.getByTestId('kpi-overdue')).toContainText('1')
  await expect(report.getByTestId('kpi-blocked')).toContainText('1')
  await expect(report.getByTestId('kpi-overdue').getByText('first report')).toBeVisible()
  await expect(report.getByRole('complementary', { name: 'Summary' }).getByText('At risk.')).toBeVisible()
  await expect(report.getByRole('img', { name: 'Sam Lee: 1 overdue' })).toBeVisible()
  await expect(report.getByTestId('chart-timeline').getByText('Today')).toBeVisible()
  await expect(report.getByTestId('kpi-unassigned_soon')).toContainText('1')
  await expect(report.getByTestId('section-unassigned').getByText('ERP-4', { exact: true })).toBeVisible()
  await expect(report.getByTestId('chart-status').getByRole('list', { name: 'Legend' })).toContainText('Done 1')
  const late = report.getByTestId('section-overdue')
  await expect(late.getByText('3 days late')).toBeVisible()
  // The connection has no issue link template, so the key is not a link
  await expect(late.getByText('ERP-2', { exact: true })).toBeVisible()
  await expect(late.getByRole('link')).toHaveCount(0)

  // The HTML download is a page that runs nothing; the Text view is the crew's own words
  const href = await card.getByRole('link', { name: 'Download HTML' }).getAttribute('href')
  const res = await page.request.get(href!)
  expect(res.status()).toBe(200)
  expect(res.headers()['content-security-policy']).toContain("default-src 'none'")
  const html = await res.text()
  expect(html).toContain('Workload by person')
  expect(html.toLowerCase()).not.toContain('<script')
  await card.getByRole('tab', { name: 'Text' }).click()
  await expect(card.getByTestId('due-report')).toHaveCount(0)
  await expect(card.getByText(/Vendor sandbox access is the critical path/)).toBeVisible()
  await api.dispose()
})
