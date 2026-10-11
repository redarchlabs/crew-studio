import fs from 'node:fs'
import path from 'node:path'
import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { psql } from '../harness/db.ts'
import { DB_NAME, RUNTIME_DIR } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'
import { toast, uid } from '../support/ui.ts'

// A schedule with a watch on a CSV in the Studio workspace: Preview, the first check (everything added), a check
// with no changes (no work order), a follow-up ladder, the work order page and the crew's State tab.

const NAME = uid('E2E Watched courses')
const FILE = `watch/${NAME.replace(/\W+/g, '-').toLowerCase()}.csv`
const CSV_PATH = path.join(RUNTIME_DIR, 'workspace', FILE)
const today = new Date().toISOString().slice(0, 10)
let scheduleId = ''
let crewId = ''

type Check = { outcome: string; work_order_id: string | null; added: number; changed: number; followups_due: number; message: string }
type View = { watch: { last_error: string | null } | null; checks: Check[] }

function writeCsv(rows: string[]): void {
  fs.mkdirSync(path.dirname(CSV_PATH), { recursive: true })
  fs.writeFileSync(CSV_PATH, ['id,due,status', ...rows].join('\n') + '\n')
}

/** Makes the schedule due; the scheduler picks it up within its 20-second tick. */
function makeDue(): void {
  psql(DB_NAME, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE id = '${scheduleId}'`)
}

async function checks(api: APIRequestContext): Promise<Check[]> {
  return (await unwrap<View>(await api.get(`/api/schedules/${scheduleId}/watch`))).checks
}

/** Waits until the watch has recorded `count` checks and returns the newest. */
async function waitForCheck(api: APIRequestContext, count: number): Promise<Check> {
  let latest: Check | undefined
  await expect(async () => {
    const list = await checks(api)
    expect(list.length).toBeGreaterThanOrEqual(count)
    latest = list[0]
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
  crewId = lib.crewId
  writeCsv([`C1,2099-01-10,Open`, `C2,2099-02-01,Open`])
  const api = await apiAs('admin')
  const sc = await unwrap<{ id: string }>(await api.post('/api/schedules', {
    data: { name: NAME, crew_id: crewId, cron: '0 9 * * *', timezone: 'UTC', overlap_policy: 'allow', enabled: true, brief: 'Watched brief', inputs: { topic: LIB.topicDefault } },
  }), 201)
  scheduleId = sc.id
  await api.dispose()
})

test.afterAll(async () => {
  const api = await apiAs('admin')
  await api.delete(`/api/schedules/${scheduleId}`)
  await api.dispose()
  fs.rmSync(CSV_PATH, { force: true })
})

test('add a watch on a workspace file, previewing it first', async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}`)
  await page.getByRole('button', { name: 'Add a watch' }).click()
  await page.getByLabel('Source').selectOption('workspace_file')
  await page.getByLabel('File', { exact: true }).fill(FILE)
  await page.getByLabel('ID field').fill('id')
  await page.getByLabel('Tracked fields').fill('due, status')

  await page.getByRole('button', { name: 'Preview' }).click()
  const preview = page.getByTestId('watch-preview')
  await expect(preview.getByText('would start a run')).toBeVisible()
  await expect(preview.getByText('first check')).toBeVisible()
  await expect(preview.getByText('C1', { exact: true })).toBeVisible()
  await expect(preview.getByText('C2', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Add watch' }).click()
  await expect(toast(page, 'Saved the watch')).toBeVisible()
  await expect(page.getByText('the next check is the first')).toBeVisible()
  await expect(page.getByText('dry run').first()).toBeVisible()
})

test('the first check starts a work order with every item added', async ({ page }) => {
  const api = await apiAs('admin')
  makeDue()
  const first = await waitForCheck(api, 1)
  expect(first.outcome).toBe('started')
  expect(first.added).toBe(2)
  await waitForSuccess(page, first.work_order_id!)
  await page.getByRole('tab', { name: /^Brief/ }).click()
  const found = page.locator('section, div').filter({ has: page.getByText('What the watch found', { exact: true }) }).first()
  await expect(found.getByText('first check')).toBeVisible()
  await expect(found.getByText('dry run', { exact: true })).toBeVisible()
  await expect(found.getByText('C1', { exact: true })).toBeVisible()
  await api.dispose()
})

test('a check with no changes starts nothing and shows in the history', async ({ page }) => {
  const api = await apiAs('admin')
  makeDue()
  const second = await waitForCheck(api, 2)
  expect(second.outcome).toBe('no_changes')
  expect(second.work_order_id).toBeNull()
  await page.goto(`/schedules/${scheduleId}`)
  await expect(page.getByText('no changes', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Runs skipped')).toBeVisible()
  const recent = page.locator('section').filter({ hasText: 'Recent work orders' })
  await expect(recent.getByRole('row').filter({ hasText: NAME })).toHaveCount(1)
  await api.dispose()
})

test('a follow-up ladder makes a due item start a run on its own', async ({ page }) => {
  writeCsv([`C1,${today},Open`, `C2,2099-02-01,Open`])
  await page.goto(`/schedules/${scheduleId}`)
  await page.getByRole('radio', { name: /Changed, or a follow-up is due/ }).click()
  await page.getByRole('switch', { name: /Follow-up ladder/ }).click()
  await page.getByLabel('Date field').fill('due')
  // Make every day a working day so the test does not depend on the weekday
  for (const day of ['Sat', 'Sun']) await page.getByRole('button', { name: day, exact: true }).click()
  await page.getByRole('button', { name: 'Preview' }).click()
  const preview = page.getByTestId('watch-preview')
  await expect(preview.getByText('due_today')).toBeVisible()
  await page.getByRole('button', { name: 'Save watch' }).click()
  await expect(toast(page, 'Saved the watch')).toBeVisible()

  const api = await apiAs('admin')
  makeDue()
  const third = await waitForCheck(api, 3)
  expect(third.outcome).toBe('started')
  expect(third.changed).toBe(1)
  expect(third.followups_due).toBe(1)
  await waitForSuccess(page, third.work_order_id!)
  await page.getByRole('tab', { name: /^Brief/ }).click()
  await expect(page.getByText('Follow-ups due', { exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'due_today' })).toBeVisible()
  await expect(page.getByText(`2099-01-10 → ${today}`, { exact: false })).toBeVisible()
  await api.dispose()
})

test('the crew State tab lists the watch, and the API guards it', async ({ page }) => {
  await page.goto(`/crews/${crewId}?tab=state`)
  const watches = page.locator('section, div').filter({ has: page.getByText('Watches', { exact: true }) }).first()
  await expect(watches.getByRole('link', { name: NAME })).toBeVisible()
  await expect(watches.getByText(/runs? skipped/)).toBeVisible()

  const admin = await apiAs('admin')
  const blocked = await admin.post(`/api/schedules/${scheduleId}/watch/preview`, { data: { source: { kind: 'web_page', url: 'http://127.0.0.1:1/' } } })
  expect(blocked.status()).toBe(422)
  expect(JSON.stringify(await blocked.json())).toContain('source_failed')
  const escape = await admin.put(`/api/schedules/${scheduleId}/watch`, { data: { source: { kind: 'workspace_file', file: '../secrets.csv' }, id_field: 'id', tracked_fields: ['x'] } })
  expect(escape.status()).toBe(422)
  await admin.dispose()

  // Watches follow the schedule's areas (Training): the viewer (Finance, IT) cannot see this one
  const viewer = await apiAs('viewer')
  expect((await viewer.get(`/api/schedules/${scheduleId}/watch`)).status()).toBe(404)
  await viewer.dispose()
})

test('remove the watch', async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}`)
  await page.getByRole('button', { name: 'Remove watch' }).click()
  await page.getByRole('button', { name: 'Confirm?' }).click()
  await expect(toast(page, 'Removed the watch')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add a watch' })).toBeVisible()
})
