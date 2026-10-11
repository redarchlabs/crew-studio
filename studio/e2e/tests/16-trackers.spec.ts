import { expect, test } from '@playwright/test'
import { FAKE_TRACKER_URL } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'
import { llmRequests, llmRequestsSince } from '../support/llmlog.ts'
import { confirm, toast, uid } from '../support/ui.ts'

// Jira through the fake tracker (harness/fake_tracker.py): a Jira connection made in the UI and tested, its people
// map, the crew's tracking list (look up, confirm, stop tracking), a tracking-list watch whose preview rolls the
// children up, and the roll-ups on a work order's Brief tab. No real tracker is contacted.

const CONN = uid('E2E Jira')
const SCHEDULE = uid('E2E Atlas sweep')
let crewId = ''
let areaId = ''
let scheduleId = ''
let connectionId = ''

test.describe.configure({ mode: 'serial' })

test.beforeAll(async () => {
  const lib = await ensureLibrary()
  crewId = lib.crewId
  areaId = lib.areaId
  const api = await apiAs('admin')
  const sc = await unwrap<{ id: string }>(await api.post('/api/schedules', {
    data: { name: SCHEDULE, crew_id: crewId, cron: '0 9 * * *', timezone: 'UTC', overlap_policy: 'allow', enabled: true, brief: 'Atlas sweep', inputs: { topic: LIB.topicDefault } },
  }), 201)
  scheduleId = sc.id
  await api.dispose()
})

test.afterAll(async () => {
  const api = await apiAs('admin')
  await api.delete(`/api/schedules/${scheduleId}`)
  if (connectionId) {
    const view = await unwrap<{ items: { id: string }[] }>(await api.get(`/api/crews/${crewId}/state/tracking`))
    for (const item of view.items) await api.delete(`/api/crews/${crewId}/state/tracking/${item.id}`)
    await api.put(`/api/crews/${crewId}/connections`, { data: { connection_ids: [] } })
    await api.delete(`/api/connections/${connectionId}`)
  }
  await api.dispose()
})

test('a Jira connection is added in the UI, tested, and refuses other sites', async ({ page }) => {
  await page.goto('/connections')
  await page.getByRole('button', { name: 'New connection' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Name').fill(CONN)
  await dialog.getByLabel('Kind').selectOption('jira')
  await dialog.getByLabel('Site URL').fill('https://evil.example.com')
  await dialog.getByLabel('Account email').fill('e2e-bot@e2e.example')
  await dialog.getByLabel('API token').fill('e2e-jira-token')
  await dialog.getByRole('option', { name: new RegExp(`^${LIB.area}`) }).click()
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(dialog.getByText(/the Jira site must be/)).toBeVisible()
  await dialog.getByLabel('Site URL').fill(FAKE_TRACKER_URL)
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(toast(page, `Saved ${CONN}`)).toBeVisible()

  const api = await apiAs('admin')
  const list = await unwrap<{ id: string; name: string; area_ids: string[] }[]>(await api.get('/api/connections'))
  const conn = list.find((c) => c.name === CONN)!
  connectionId = conn.id
  expect(conn.area_ids).toEqual([areaId])
  await unwrap(await api.put(`/api/crews/${crewId}/connections`, { data: { connection_ids: [connectionId] } }))
  await api.dispose()

  await page.reload()
  const row = page.getByRole('row').filter({ hasText: CONN })
  await row.getByRole('button', { name: 'Test' }).click()
  await expect(row.getByText('Connected as Studio Bot.')).toBeVisible()
})

test('the people map reads the tracker and links people by email where it can', async ({ page }) => {
  await page.goto('/connections')
  await page.getByRole('row').filter({ hasText: CONN }).getByRole('button', { name: 'People map' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('No people yet.', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: 'Read people from the tracker' }).click()
  await expect(toast(page, /Read 3 people from/)).toBeVisible()
  await expect(dialog.getByText('Sam Lee')).toBeVisible()
  await expect(dialog.getByText('Dana Ruiz')).toBeVisible()
  await expect(dialog.getByText('hidden by the tracker').first()).toBeVisible()
  await dialog.getByRole('button', { name: 'Close' }).last().click()
})

test('an item is tracked only after looking it up and confirming', async ({ page }) => {
  await page.goto(`/crews/${crewId}?tab=state`)
  const panel = page.locator('section').filter({ has: page.getByText('Tracking list', { exact: true }) }).first()
  await expect(panel.getByText('Nothing is tracked yet.')).toBeVisible()
  await panel.getByRole('button', { name: 'Track an item' }).click()
  await panel.getByRole('textbox', { name: 'Item' }).fill('PRJ-99')
  await panel.getByRole('button', { name: 'Look up' }).click()
  await expect(panel.getByText(/PRJ-99 was not found in/)).toBeVisible()
  await panel.getByRole('textbox', { name: 'Item' }).fill('PRJ-10')
  await panel.getByLabel('Project (optional)').fill('Atlas')
  await panel.getByLabel('Lead (optional)').fill('acc-dana')
  await panel.getByRole('button', { name: 'Look up' }).click()
  const found = panel.getByTestId('tracking-check')
  await expect(found.getByText('Atlas rollout')).toBeVisible()
  await expect(found.getByText(/2 direct children/)).toBeVisible()
  await found.getByRole('button', { name: /Confirm and track/ }).click()
  await expect(toast(page, 'Now tracking PRJ-10 (Atlas)')).toBeVisible()
  await expect(panel.getByText('PRJ-10', { exact: true })).toBeVisible()
  await expect(panel.getByText(/added by E2E Admin/)).toBeVisible()
  // The people map (read above) names the lead
  await expect(panel.getByText('Dana Ruiz', { exact: true })).toBeVisible()
})

test('a tracking-list watch previews roll-ups of the children', async ({ page }) => {
  await page.goto(`/schedules/${scheduleId}`)
  await page.getByRole('button', { name: 'Add a watch' }).click()
  await page.getByLabel('Source').selectOption('tracking_list')
  await page.getByRole('button', { name: 'Preview' }).click()
  const preview = page.getByTestId('watch-preview')
  await expect(preview.getByText('would start a run')).toBeVisible()
  await expect(preview.getByText('jira:PRJ-11', { exact: true })).toBeVisible()
  const rollup = preview.getByTestId('rollup')
  await expect(rollup.getByText('PRJ-10', { exact: true })).toBeVisible()
  await expect(rollup.getByText('overdue 1')).toBeVisible()
  await expect(rollup.getByText('blocked 1')).toBeVisible()
  await expect(rollup.getByText(/3 children: 1 done/)).toBeVisible()
  await page.getByRole('button', { name: 'Add watch' }).click()
  await expect(toast(page, 'Saved the watch')).toBeVisible()
})

test('a work order started from the watch shows the roll-ups on its Brief tab', async ({ page }) => {
  const api = await apiAs('admin')
  const wo = await unwrap<{ id: string; inputs: Record<string, string> }>(await api.post(`/api/schedules/${scheduleId}/run-now`, { data: {} }), 201)
  expect(JSON.parse(wo.inputs.rollups).items[0].item).toBe('PRJ-10')
  expect(wo.inputs.dry_run).toBe('true')
  await api.dispose()
  await page.goto(`/work-orders/${wo.id}`)
  await page.getByRole('tab', { name: /^Brief/ }).click()
  const rollup = page.getByTestId('rollup').first()
  await expect(rollup.getByText('PRJ-10', { exact: true })).toBeVisible()
  await expect(rollup.getByText(/due after the parent 1/)).toBeVisible()
})

test('in chat, the front desk proposes tracking an item and the person confirms and undoes it', async ({ page }) => {
  const lib = await ensureLibrary()
  const api = await apiAs('admin')
  const settings = await unwrap<{ enabled: boolean }>(await api.get(`/api/crews/${crewId}/chat`))
  if (!settings.enabled) {
    await unwrap(await api.put(`/api/crews/${crewId}/chat`, { data: { enabled: true, front_desk_agent_id: lib.agentId, llm_id: lib.plainLlmId, daily_budget_usd: 0.5 } }))
  }
  const before = llmRequests().length
  await page.goto(`/chat?crew=${crewId}`)
  await page.getByLabel('Message').fill('Track the vendor ticket too. E2E_CHAT_TRACK')
  await page.getByRole('button', { name: 'Send' }).click()
  const card = page.getByRole('region', { name: 'Proposed change' }).last()
  await expect(card.getByText('Track PRJ-11 (Atlas vendor), without its children')).toBeVisible({ timeout: 45_000 })
  const calls = llmRequestsSince(before).filter((r) => r.has_tools)
  expect(calls[0].tool_names).toEqual(expect.arrayContaining(['read_tracking_list', 'get_tracker_issue', 'find_tracker_issues', 'propose_tracking_item', 'propose_stop_tracking']))
  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByRole('link', { name: 'Tracking PRJ-11' })).toBeVisible()
  const tracked = async () => (await unwrap<{ items: { item_ref: string; added_by_name: string }[] }>(await api.get(`/api/crews/${crewId}/state/tracking`))).items
  expect((await tracked()).find((i) => i.item_ref === 'PRJ-11')?.added_by_name).toBe('E2E Admin')
  await card.getByRole('button', { name: 'Undo' }).click()
  await expect(card.getByText('Undone')).toBeVisible()
  expect((await tracked()).some((i) => i.item_ref === 'PRJ-11')).toBe(false)
  await api.dispose()
})

test('stopping tracking keeps the item in the history', async ({ page }) => {
  await page.goto(`/crews/${crewId}?tab=state`)
  const panel = page.locator('section').filter({ has: page.getByText('Tracking list', { exact: true }) }).first()
  await confirm(panel.getByRole('button', { name: 'Stop tracking' }))
  await expect(toast(page, 'Stopped tracking PRJ-10')).toBeVisible()
  await expect(panel.getByText('Nothing is tracked yet.')).toBeVisible()
  await panel.getByRole('switch', { name: 'Show removed items' }).click()
  await expect(panel.getByText('removed', { exact: true }).first()).toBeVisible()
})
