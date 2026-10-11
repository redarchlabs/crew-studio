import { createHash, randomBytes } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { psql } from '../harness/db.ts'
import { DB_NAME, FAKE_LLM_URL } from '../harness/paths.ts'
import { SEED, statePath } from '../support/accounts.ts'
import { apiAnon, apiAs, unwrap } from '../support/api.ts'
import { confirm, row, uid } from '../support/ui.ts'

// Connections, the run-state API (as a running crew's tools call it) and the crew page's State tab.
// A finished run of the seeded PE crew (Finance) stands in for a live one: its token is written straight into
// the database, which is what the run manager does when it starts a run.

const FINANCE = '55555555-0000-4000-8000-000000000001'
const SECRET = `e2e-secret-${randomBytes(6).toString('hex')}`
const connName = uid('E2E API')
const token = `csr_e2e${randomBytes(24).toString('hex')}`
let runId = ''

test.describe.configure({ mode: 'serial' })

test.beforeAll(() => {
  runId = psql(DB_NAME, `INSERT INTO runs (crew_id, crew_name, status, budget_usd, spec, area_ids, started_at, finished_at)
    VALUES ('${SEED.peCrewId}', '${SEED.peCrew}', 'succeeded', 1, '{}', ARRAY['${FINANCE}']::uuid[], now(), now()) RETURNING id`).split('\n')[0]
  const hash = createHash('sha256').update(token).digest('hex')
  psql(DB_NAME, `INSERT INTO run_tokens (run_id, token_hash, expires_at) VALUES ('${runId}', '${hash}', now() + interval '1 hour')`)
})

test('an admin adds, tests and re-keys a connection without ever seeing the secret again', async ({ page }) => {
  await page.goto('/connections')
  await page.getByRole('button', { name: 'New connection' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Name').fill(connName)
  await dialog.getByLabel('Kind').selectOption('http')
  await dialog.getByLabel(/^Base URL/).fill(`${FAKE_LLM_URL}/__health`)
  await dialog.getByLabel(/^Auth header name/).fill('X-E2E-Key')
  await dialog.getByLabel(/^Auth header value/).fill(SECRET)
  await dialog.getByRole('option', { name: /Finance/ }).click()
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(dialog).toBeHidden()

  const r = row(page, connName)
  await expect(r.getByText('Auth header value · set')).toBeVisible()
  await r.getByRole('button', { name: 'Test' }).click()
  await expect(r.getByText(/Connected: the server answered HTTP 200/)).toBeVisible()
  expect(await page.content()).not.toContain(SECRET)

  await r.getByRole('button', { name: 'Replace secret' }).click()
  await page.getByRole('dialog').getByLabel(/^Auth header value/).fill(`${SECRET}-2`)
  await page.getByRole('dialog').getByRole('button', { name: 'Replace' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()

  const api = await apiAs('admin')
  const listed = JSON.stringify(await unwrap(await api.get('/api/connections')))
  expect(listed).toContain(connName)
  expect(listed).not.toContain(SECRET)
  await api.dispose()
})

test('a running crew keeps notes and an action log through the run-state API', async () => {
  const runner = await apiAnon(token)
  await unwrap(await runner.put('/api/run-state/notes/weekly-report', { data: { subject: 'project:atlas', content: 'Week 41: on track.', expected_version: 0 } }))
  const conflict = await runner.put('/api/run-state/notes/weekly-report', { data: { subject: 'project:atlas', content: 'stale', expected_version: 0 } })
  expect(conflict.status()).toBe(409)
  // A follow-up of a non-tracker item (Jira and Linear follow-ups are recorded by the tracker comment tool only)
  const rec = { subject: 'item:PRJ-15', action: 'followup:due_soon', details: { to: 'sam' }, idempotency_key: `PRJ-15/${runId}` }
  expect((await runner.post('/api/run-state/log', { data: rec })).status()).toBe(201)
  expect((await runner.post('/api/run-state/log', { data: rec })).status()).toBe(200)
  const check = await unwrap<{ entry: { action: string } | null }>(await runner.get('/api/run-state/log/check?subject=item:PRJ-15&action=followup:due_soon&within=1d'))
  expect(check.entry?.action).toBe('followup:due_soon')
  // The token reaches nothing but the run-state API
  expect((await runner.get('/api/crews')).status()).toBe(401)
  await runner.dispose()

  const admin = await apiAs('admin')
  const journal = await unwrap<{ entries: { kind: string; title: string }[] }>(await admin.get(`/api/runs/${runId}/journal`))
  const lines = journal.entries.filter((e) => e.kind === 'state').map((e) => e.title)
  expect(lines).toContain('Notes: wrote weekly-report v1 (18 bytes)')
  expect(lines.some((l) => l.startsWith('Log: recorded followup:due_soon for item:PRJ-15'))).toBe(true)
  await admin.dispose()
})

test('the State tab shows, edits and rolls back notes, filters the log and links connections', async ({ page }) => {
  await page.goto(`/crews/${SEED.peCrewId}`)
  await page.getByRole('tab', { name: 'State' }).click()
  await expect(page).toHaveURL(/tab=state/)

  const notes = row(page, 'weekly-report')
  await expect(notes.getByText('v1')).toBeVisible()
  await notes.getByRole('button', { name: 'History' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('Week 41: on track.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Edit' }).click()
  await dialog.getByLabel(/saves as v2/).fill('Week 41: one risk.')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog.getByText('Week 41: one risk.')).toBeVisible()
  await dialog.getByRole('button', { name: /^v1/ }).click()
  await dialog.getByRole('button', { name: 'Restore v1' }).click()
  await expect(dialog.getByRole('button', { name: /^v3/ })).toBeVisible()
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(row(page, 'weekly-report').getByText('v3')).toBeVisible()

  await page.getByLabel('Subject filter').fill('PRJ-15')
  await page.getByRole('button', { name: 'Filter' }).click()
  await expect(row(page, 'followup:due_soon').getByText('{"to":"sam"}')).toBeVisible()

  await page.getByRole('button', { name: 'Change' }).click()
  await page.getByRole('option', { name: new RegExp(connName) }).click()
  // Wait for the save itself, not just the picker's label, before reading the run's scope
  const saved = page.waitForResponse((res) => res.request().method() === 'PUT' && res.url().endsWith(`/api/crews/${SEED.peCrewId}/connections`))
  await page.getByRole('button', { name: 'Save connections' }).click()
  expect((await saved).ok()).toBe(true)
  await expect(page.getByText(connName)).toBeVisible()

  const runner = await apiAnon(token)
  const scope = await unwrap<{ connections: { name: string }[] }>(await runner.get('/api/run-state/scope'))
  expect(scope.connections.map((c) => c.name)).toContain(connName)
  await runner.dispose()
})

test('viewers read state without changing it, and other areas cannot see it', async ({ browser }) => {
  const ctx = await browser.newContext({ storageState: statePath('viewer') })
  const page = await ctx.newPage()
  await page.goto(`/crews/${SEED.peCrewId}?tab=state`)
  await expect(row(page, 'weekly-report')).toBeVisible()
  await expect(row(page, 'followup:due_soon')).toBeVisible()
  await expect(page.getByRole('button', { name: /Delete|Change/ })).toHaveCount(0)
  await row(page, 'weekly-report').getByRole('button', { name: 'History' }).click()
  await expect(page.getByRole('dialog').getByText('Week 41: on track.')).toBeVisible()
  await expect(page.getByRole('dialog').getByRole('button', { name: /Edit|Restore|Delete/ })).toHaveCount(0)
  await ctx.close()

  const viewerApi = await apiAs('viewer')
  expect((await viewerApi.put(`/api/crews/${SEED.peCrewId}/state/notes`, { data: { name: 'x', content: 'x' } })).status()).toBe(403)
  expect((await viewerApi.get('/api/connections')).status()).toBe(403)
  await viewerApi.dispose()

  const outsider = await apiAs('outsider')
  expect((await outsider.get(`/api/crews/${SEED.peCrewId}/state/notes`)).status()).toBe(404)
  expect(JSON.stringify(await unwrap(await outsider.get('/api/connections')))).not.toContain(connName)
  await outsider.dispose()
})

test('deleting a log entry and a note is possible for state managers', async ({ page }) => {
  await page.goto(`/crews/${SEED.peCrewId}?tab=state`)
  await confirm(row(page, 'followup:due_soon').getByRole('button', { name: 'Delete' }))
  await expect(page.getByText('Nothing logged yet')).toBeVisible()
  await row(page, 'weekly-report').getByRole('button', { name: 'History' }).click()
  await confirm(page.getByRole('dialog').getByRole('button', { name: 'Delete note' }))
  await expect(page.getByText('No notes yet')).toBeVisible()
})
