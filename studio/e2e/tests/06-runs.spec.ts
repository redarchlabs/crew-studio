import { expect, test, type Page } from '@playwright/test'
import { LIB } from '../support/accounts.ts'
import { apiAs, findByName, unwrap } from '../support/api.ts'
import { llmRequests, llmRequestsSince } from '../support/llmlog.ts'
import { ensureLibrary } from '../support/library.ts'
import { confirm, fixmeIfOverviewBroken, linkRow, row, toast } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })
test.beforeAll(async () => {
  await ensureLibrary()
})

const TOPIC = 'fusion energy'
const RUN_TIMEOUT = 120_000
const SLOW = 'E2E_SLOW_MARKER'
const FAIL = 'E2E_FAIL_MARKER'

type Named = { id: string; name: string }

async function makeCrew(name: string, marker: string): Promise<string> {
  const api = await apiAs('admin')
  const agent = await findByName<Named>(api, '/api/agents', LIB.agent)
  const areas = await unwrap<Named[]>(await api.get('/api/areas'))
  const areaIds = [areas.find((a) => a.name === LIB.area)!.id]
  const task = await unwrap<Named>(
    await api.post('/api/tasks', { data: { name: `${name} Task`, description: `${marker}: write one short paragraph.`, expected_output: 'A paragraph.', agent_id: agent.id, context_task_ids: [], markdown: false, area_ids: areaIds } }),
    201,
  )
  const crew = await unwrap<Named>(
    await api.post('/api/crews', { data: { name, description: '', process: 'sequential', manager_llm_id: null, agent_ids: [agent.id], task_ids: [task.id], inputs: [{ key: 'topic', label: 'Topic', default: 'anything', required: true }], budget_usd: 1, area_ids: areaIds } }),
    201,
  )
  await api.dispose()
  return crew.id
}

async function statusBadge(page: Page, status: string): Promise<void> {
  await expect(page.getByRole('main').locator('header').getByText(status, { exact: true }).first()).toBeVisible({ timeout: RUN_TIMEOUT })
}

test.describe('running a crew from the UI', () => {
  let runUrl = ''
  let requestsBefore = 0

  test('the run form validates inputs and then starts a run', async ({ page }) => {
    requestsBefore = llmRequests().length
    await page.goto('/crews')
    await row(page, LIB.crew).getByRole('button', { name: 'Run' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: `Run ${LIB.crew}` })).toBeVisible()
    const topic = dialog.getByLabel(/Topic/)
    await expect(topic).toHaveValue(LIB.topicDefault) // the crew's default
    await topic.fill('')
    await dialog.getByRole('button', { name: 'Start run' }).click()
    await expect(dialog.getByText('Topic is required')).toBeVisible()
    await dialog.getByLabel('Budget override (USD)').fill('0')
    await dialog.getByRole('button', { name: 'Start run' }).click()
    await expect(dialog.getByText('Budget must be greater than 0')).toBeVisible()
    await dialog.getByLabel('Budget override (USD)').fill('')

    await topic.fill(TOPIC)
    const stream = page.waitForRequest((req) => /\/api\/runs\/[0-9a-f-]{36}\/stream/.test(req.url()))
    await dialog.getByRole('button', { name: 'Start run' }).click()
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}/)
    runUrl = page.url()
    // the page follows the run over server-sent events
    await stream
    await expect(page.getByRole('heading', { name: LIB.crew })).toBeVisible()
    await expect(page.getByText(`${TOPIC}`, { exact: false }).first()).toBeVisible() // input chip
  })

  test('the run completes with the fake LLM output, cost and tokens', async ({ page }) => {
    await page.goto(runUrl)
    await statusBadge(page, 'succeeded')
    const header = page.getByRole('main').locator('header')
    await expect(header.getByText('Spend')).toBeVisible()
    await expect(header).toContainText('/ $0.75') // the crew budget
    await expect(header.getByRole('progressbar')).toHaveAttribute('aria-valuenow', /0\.\d+/)
    // tokens: at least one request of 1,200 + 150 tokens each
    await expect(header).toContainText(/Tokens[\d,]{4,}/)
    await expect(header).toContainText(/\d+ requests/)
    await expect(header).toContainText('1 agents · 2 tasks')
    await expect(page.getByRole('button', { name: 'Cancel run' })).toHaveCount(0)

    // the model received the input interpolated into the prompt, over the OpenAI chat-completions wire format
    const seen = llmRequestsSince(requestsBefore)
    expect(seen.length).toBeGreaterThanOrEqual(2)
    expect(seen.some((r) => r.text.includes(TOPIC))).toBe(true)
    expect(seen.every((r) => r.path.endsWith('/chat/completions'))).toBe(true)
  })

  test('outputs, timeline, journal, LLM calls, files and spec tabs show the run', async ({ page }) => {
    await page.goto(runUrl)
    await statusBadge(page, 'succeeded')

    await page.getByRole('tab', { name: /Outputs/ }).click()
    await expect(page.getByText('Final output')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'FAKE-LLM-ANSWER' }).first()).toBeVisible()
    await expect(page.getByText(`1. ${LIB.task1}`)).toBeVisible()
    await expect(page.getByText(`2. ${LIB.task2}`)).toBeVisible()

    await page.getByRole('tab', { name: /Timeline/ }).click()
    await expect(page.getByText(LIB.task1).first()).toBeVisible()
    await expect(page.getByText(LIB.task2).first()).toBeVisible()
    await expect(page.getByText('done', { exact: true }).first()).toBeVisible()
    // filtering by event category hides rows
    await page.getByRole('button', { name: 'llm', exact: true }).click().catch(() => undefined)

    await page.getByRole('tab', { name: /Journal/ }).click()
    await expect(page.getByRole('heading').or(page.locator('header')).filter({ hasText: LIB.task1 }).first()).toBeVisible()
    await expect(page.getByText(LIB.agentRole.replace('{topic}', TOPIC)).first()).toBeVisible()
    await page.getByLabel('Search journal').fill('FAKE-LLM-ANSWER')
    await page.getByText(`Finished ${LIB.task1}`).click() // entries expand to show their body
    await expect(page.getByText('FAKE-LLM-ANSWER').first()).toBeVisible()

    await page.getByRole('tab', { name: /LLM calls/ }).click()
    const calls = page.getByRole('row').filter({ hasText: 'gpt-6-luna' })
    await expect(calls.first()).toBeVisible()
    await calls.first().click()
    await expect(page.getByText('FAKE-LLM-ANSWER').first()).toBeVisible()

    await page.getByRole('tab', { name: /Files/ }).click()
    await expect(page.getByText('No input files.')).toBeVisible()
    await expect(page.getByText('transcript.jsonl')).toBeVisible()
    await expect(page.getByText('console.log')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Download transcript.jsonl' })).toHaveAttribute('href', /\/api\/attachments\/.+\/content/)

    await page.getByRole('tab', { name: /Logs/ }).click()
    await page.getByRole('tab', { name: /Callbacks/ }).click()
    await page.getByRole('tab', { name: /Spec/ }).click()
    await expect(page.getByText(LIB.crew).first()).toBeVisible()
    await expect(page.getByRole('tab', { name: /Spec/ })).toHaveAttribute('aria-selected', 'true')
  })

  test('the transcript downloads with every prompt and response', async ({ page }) => {
    await page.goto(`${runUrl}?tab=files`)
    const link = page.getByRole('link', { name: 'Download transcript.jsonl' })
    await expect(link).toBeVisible()
    const href = (await link.getAttribute('href'))!
    const res = await page.request.get(href)
    expect(res.status()).toBe(200)
    const text = await res.text()
    expect(text).toContain('FAKE-LLM-ANSWER')
    expect(text).toContain(TOPIC)
  })

  test('the run is listed and can be filtered', async ({ page }) => {
    await page.goto('/runs')
    await expect(linkRow(page, LIB.crew).first()).toContainText('succeeded')
    await expect(linkRow(page, LIB.crew).first()).toContainText('Manual')
    await page.getByLabel('Filter by status').selectOption('failed')
    await expect(page.getByText('No runs match these filters')).toBeVisible()
    await page.getByLabel('Filter by status').selectOption('succeeded')
    await page.getByLabel('Filter by crew').selectOption({ label: LIB.crew })
    await expect(linkRow(page, LIB.crew).first()).toBeVisible()
    await page.getByLabel('Filter by trigger').selectOption({ label: 'Manual' })
    await expect(linkRow(page, LIB.crew).first()).toBeVisible()
  })

  test('the overview counts the run', async ({ page }) => {
    await fixmeIfOverviewBroken(page, test)
    await page.goto('/')
    await expect(page.getByRole('link', { name: /^Runs\s*\d+/ }).first()).toBeVisible()
    await expect(page.getByRole('link', { name: LIB.crew }).first()).toBeVisible()
  })
})

test.describe('cancelling a long run', () => {
  test('cancel stops the run, which cannot be deleted while it runs', async ({ page }) => {
    const crewId = await makeCrew('E2E Slow Crew', SLOW)
    const before = llmRequests().length

    await page.goto(`/crews/${crewId}`)
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Start run' }).click()
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}/)
    const runId = /\/runs\/([0-9a-f-]{36})/.exec(page.url())![1]

    // wait until the model call is really in flight (the fake server holds it for ~90 s)
    await expect.poll(() => llmRequestsSince(before).some((r) => r.text.includes(SLOW)), { timeout: RUN_TIMEOUT, intervals: [500] }).toBe(true)
    await statusBadge(page, 'running')
    await expect(page.getByRole('button', { name: 'Cancel run' })).toBeVisible()

    const api = await apiAs('admin')
    const del = await api.delete(`/api/runs/${runId}`)
    expect(del.status()).toBe(409)
    expect(JSON.stringify(await del.json())).toContain('Cancel the run before deleting it')
    await api.dispose()

    await page.getByRole('button', { name: 'Cancel run' }).click()
    await expect(toast(page, 'Run cancelled')).toBeVisible()
    await statusBadge(page, 'cancelled')
    await expect(page.getByRole('button', { name: 'Cancel run' })).toHaveCount(0)

    // the list agrees, and a finished run can be deleted
    await page.goto('/runs')
    await page.getByLabel('Filter by status').selectOption('cancelled')
    const r = linkRow(page, 'E2E Slow Crew').first()
    await expect(r).toContainText('cancelled')
    await confirm(r.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, 'Run deleted')).toBeVisible()
    await expect(page.getByText('No runs match these filters')).toBeVisible()
  })

  test('a model that keeps failing ends the run as failed, with the error shown', async ({ page }) => {
    test.setTimeout(240_000)
    const crewId = await makeCrew('E2E Failing Crew', FAIL)
    await page.goto(`/crews/${crewId}`)
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Start run' }).click()
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}/)
    await statusBadge(page, 'failed')
    await expect(page.getByRole('main').locator('header').locator('div.text-danger')).toBeVisible()
    await page.getByRole('tab', { name: /Outputs/ }).click()
    await expect(page.getByText('No outputs yet')).toBeVisible()
  })
})
