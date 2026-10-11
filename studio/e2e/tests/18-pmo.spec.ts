import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { FAKE_DELIVERY_URL, FAKE_SMTP_PORT, FAKE_TRACKER_URL } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'
import { llmRequestsSince, llmRequests } from '../support/llmlog.ts'
import { toast, uid } from '../support/ui.ts'

// The PMO (phase 5): Slack and email connections tested in the UI, the Set up a PMO wizard, a dry-run daily sweep
// that runs only the sweep task, and a weekly report the Status Reporter delivers with deliver_report (the fake LLM
// calls it when the brief holds E2E_DELIVER_REPORT) to a fake Slack and a fake SMTP server, with a failed Slack
// delivery retried from the work order. Nothing real is contacted (harness/fake_delivery.py, fake_tracker.py).

const RUN_TIMEOUT = 90_000
const PMO = uid('E2E PMO')
const RECIPIENTS = ['sam@e2e.example', 'dana@e2e.example']
const conns: Record<string, string> = {}
const names: Record<string, string> = { jira: uid('E2E PMO Jira'), slack: uid('E2E PMO Slack'), email: uid('E2E PMO Mail') }
let crewId = ''
let sweepId = ''
let weeklyId = ''
let plainLlmId = ''

type Received = { path: string; body: { text?: string } }
type Mail = { from: string; to: string[]; data: string }

async function fake<T>(path: string, method = 'GET'): Promise<T> {
  const res = await fetch(`${FAKE_DELIVERY_URL}${path}`, { method })
  return (await res.json()) as T
}

async function connection(api: APIRequestContext, name: string, kind: string, config: Record<string, string>, secrets: Record<string, string>, areaId: string): Promise<string> {
  const c = await unwrap<{ id: string }>(await api.post('/api/connections', { data: { name, kind, area_ids: [areaId], config, secrets } }), 201)
  return c.id
}

async function statusBadge(page: Page, status: string): Promise<void> {
  await expect(page.getByRole('main').locator('header').getByText(status, { exact: true }).first()).toBeVisible({ timeout: RUN_TIMEOUT })
}

test.describe.configure({ mode: 'serial' })

test.beforeAll(async () => {
  const lib = await ensureLibrary()
  plainLlmId = lib.plainLlmId
  const api = await apiAs('admin')
  conns.jira = await connection(api, names.jira, 'jira', { site_url: FAKE_TRACKER_URL, email: 'e2e-bot@e2e.example' }, { api_token: 'e2e-jira-token' }, lib.areaId)
  conns.slack = await connection(api, names.slack, 'slack', {}, { webhook_url: `${FAKE_DELIVERY_URL}/slack/hook/T0/B0/e2e` }, lib.areaId)
  conns.email = await connection(api, names.email, 'email',
    { host: '127.0.0.1', port: String(FAKE_SMTP_PORT), security: 'none', from_address: 'pmo@e2e.example' }, { password: 'unused' }, lib.areaId)
  await api.dispose()
})

test('Slack and email connections send a test message', async ({ page }) => {
  const before = (await fake<Received[]>('/__received')).length
  const mailBefore = (await fake<Mail[]>('/__mail')).length
  await page.goto('/connections')
  for (const [key, want] of [['slack', 'Sent a test message. Check that it arrived.'], ['email', /Sent a test email to admin@e2e\.test/]] as const) {
    const row = page.getByRole('row').filter({ hasText: names[key] })
    await row.getByRole('button', { name: 'Test' }).click()
    await expect(row.getByText(want)).toBeVisible()
  }
  const received = await fake<Received[]>('/__received')
  expect(received.length).toBe(before + 1)
  expect(received.at(-1)?.body.text).toContain('Crew Studio test message')
  expect((await fake<Mail[]>('/__mail')).length).toBe(mailBefore + 1)
})

test('the wizard sets up a PMO with its schedules, watches, chat and destinations', async ({ page }) => {
  await page.goto('/crews')
  await page.getByRole('link', { name: 'Set up a PMO' }).click()
  await expect(page.getByRole('heading', { name: 'Set up a PMO' })).toBeVisible()
  await page.getByLabel('Name').fill(PMO)
  await page.getByRole('option', { name: new RegExp(`^${LIB.area}`) }).click()
  await page.getByLabel('Time zone').selectOption('UTC')
  await page.getByRole('button', { name: 'Next' }).click()
  const api = await apiAs('admin')
  await page.getByLabel('Tracker connection').selectOption(conns.jira)
  await page.getByRole('button', { name: 'Next' }).click()
  await page.getByRole('button', { name: 'Add destination' }).click()
  const first = page.getByTestId('destination-row').nth(0)
  await first.getByLabel('Destination', { exact: true }).selectOption('slack')
  await first.getByLabel('Connection').selectOption(conns.slack)
  await page.getByRole('button', { name: 'Add destination' }).click()
  const second = page.getByTestId('destination-row').nth(1)
  await second.getByLabel('Destination', { exact: true }).selectOption('email')
  await second.getByLabel('Connection').selectOption(conns.email)
  await second.getByLabel('Recipients').fill(RECIPIENTS.join(', '))
  await page.getByRole('button', { name: 'Next' }).click()
  // The E2E LLM speaks the fake's chat completions, so the scripted deliver_report call can happen
  await page.getByLabel('PMO Coordinator model').selectOption(plainLlmId)
  await page.getByLabel('Status Reporter model').selectOption(plainLlmId)
  await expect(page.getByTestId('pmo-summary')).toContainText('to the work order, Slack, Email')
  await page.getByRole('button', { name: 'Set up the PMO' }).click()
  await expect(toast(page, `${PMO} is set up; tell it what to track in Chat`)).toBeVisible()
  await expect(page).toHaveURL(/\/crews\/[0-9a-f-]{36}$/)
  crewId = page.url().split('/').pop()!

  const schedules = await unwrap<{ id: string; name: string; cron: string; inputs: Record<string, string> }[]>(await api.get('/api/schedules'))
  const sweep = schedules.find((s) => s.name === `${PMO}: daily sweep`)!
  const weekly = schedules.find((s) => s.name === `${PMO}: weekly report`)!
  expect(sweep.cron).toBe('0 7 * * 1-5')
  expect(weekly.cron).toBe('0 8 * * 1')
  sweepId = sweep.id
  weeklyId = weekly.id
  const chat = await unwrap<{ enabled: boolean }>(await api.get(`/api/crews/${crewId}/chat`))
  expect(chat.enabled).toBe(true)
  // Something to follow: PRJ-10 and its children in the fake Jira
  await unwrap(await api.post(`/api/crews/${crewId}/state/tracking`, { data: { connection_id: conns.jira, item_ref: 'PRJ-10', project: 'Atlas' } }), 201)
  await api.dispose()

  await page.goto(`/schedules/${weeklyId}`)
  const panel = page.locator('section').filter({ has: page.getByText('Report destinations', { exact: true }) })
  await expect(panel.getByTestId('destination-row')).toHaveCount(2)
  await expect(page.getByTestId('run-tasks-note')).toBeVisible()
})

test('the daily sweep runs only its own task, in dry run, and posts nothing', async ({ page }) => {
  const api = await apiAs('admin')
  const posted = async () => ((await (await fetch(`${FAKE_TRACKER_URL}/__posted`)).json()) as unknown[]).length
  const postedBefore = await posted()
  const wo = await unwrap<{ id: string; inputs: Record<string, string> }>(await api.post(`/api/schedules/${sweepId}/run-now`, { data: {} }), 201)
  expect(wo.inputs.dry_run).toBe('true')
  expect(JSON.parse(wo.inputs.rollups).items[0].item).toBe('PRJ-10')
  await page.goto(`/work-orders/${wo.id}`)
  await statusBadge(page, 'succeeded')
  const detail = await unwrap<{ current_run_id: string }>(await api.get(`/api/work-orders/${wo.id}`))
  const run = await unwrap<{ spec: { tasks: { name: string }[] } }>(await api.get(`/api/runs/${detail.current_run_id}`))
  expect(run.spec.tasks.map((t) => t.name)).toEqual([`${PMO}: daily follow-up sweep`])
  expect(await posted()).toBe(postedBefore)
  await api.dispose()
})

test('the weekly report is delivered to Slack and email, and a failed delivery is retried', async ({ page }) => {
  const api = await apiAs('admin')
  // Turn dry run off for the weekly report, and have the reporter call deliver_report
  const view = await unwrap<{ watch: Record<string, unknown> }>(await api.get(`/api/schedules/${weeklyId}/watch`))
  await unwrap(await api.put(`/api/schedules/${weeklyId}/watch`, { data: { ...view.watch, dry_run: false } }))
  const sc = await unwrap<Record<string, unknown>>(await api.get(`/api/schedules/${weeklyId}`))
  await unwrap(await api.put(`/api/schedules/${weeklyId}`, { data: { ...sc, brief: 'Write and deliver the weekly report. E2E_DELIVER_REPORT' } }))
  await fake('/__fail?status=502', 'POST')
  const mailBefore = (await fake<Mail[]>('/__mail')).length
  const before = llmRequests().length

  const wo = await unwrap<{ id: string; inputs: Record<string, string> }>(await api.post(`/api/schedules/${weeklyId}/run-now`, { data: {} }), 201)
  expect(wo.inputs.dry_run).toBe('false')
  await page.goto(`/work-orders/${wo.id}`)
  await statusBadge(page, 'succeeded')
  expect(llmRequestsSince(before).find((r) => r.has_tools)?.tool_names).toContain('deliver_report')

  const mails = (await fake<Mail[]>('/__mail')).slice(mailBefore)
  expect(mails).toHaveLength(1)
  expect(mails[0].to).toEqual(RECIPIENTS)
  expect(mails[0].data).toMatch(/Subject: Atlas status, \d{4}-W\d{2}/)
  // Crew Studio read PRJ-10 itself: the email has the designed page as its HTML part next to the text
  expect(mails[0].data).toContain('multipart/alternative')
  expect(mails[0].data).toMatch(/Content-Type: text\/html; charset="?utf-8"?/i)

  await page.getByRole('tab', { name: 'Reports' }).click()
  const card = page.getByTestId('report-card')
  await expect(card.getByText(/^Atlas status, \d{4}-W\d{2}$/)).toBeVisible()
  const slackRow = card.getByTestId('delivery-row').filter({ hasText: 'Slack:' })
  await expect(slackRow.getByText('failed', { exact: true })).toBeVisible()
  await expect(card.getByTestId('delivery-row').filter({ hasText: 'Email:' }).getByText('sent', { exact: true })).toBeVisible()

  await fake('/__ok', 'POST')
  const hooksBefore = (await fake<Received[]>('/__received')).length
  await page.getByRole('button', { name: 'Retry failed deliveries' }).click()
  await expect(toast(page, 'Retried 1 delivery; 1 sent')).toBeVisible()
  await expect(slackRow.getByText('sent', { exact: true })).toBeVisible()
  const received = (await fake<Received[]>('/__received')).slice(hooksBefore)
  expect(received).toHaveLength(1)
  expect(received[0].body.text).toContain('Atlas weekly status')
  // The email was not sent again
  expect((await fake<Mail[]>('/__mail')).length).toBe(mailBefore + 1)
  await api.dispose()
})
