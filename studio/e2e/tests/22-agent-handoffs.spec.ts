import { expect, test, type Page } from '@playwright/test'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { llmRequestsSince, llmRequests } from '../support/llmlog.ts'
import { confirm, toast } from '../support/ui.ts'

// Agent chat hand-offs (docs/agent-chat-handoffs-design.md, slice 3b): a builder turns hand-offs on for an agent; a
// person's message makes the lead propose one (E2E_AGENT_HANDOFF makes the fake LLM call Hand off work for the
// specialist), the person confirms the card, keeps chatting while it runs, and gets the result back in the
// conversation. A slow hand-off (E2E_SLOW_HANDOFF puts E2E_SLOW_MARKER in its brief) is cancelled; a confirm that does
// not fit the conversation's budget is refused and the card stays waiting; other people cannot reach a hand-off.

const LEAD = 'E2E Lead'
const SPECIALIST = 'E2E Specialist'
const SPECIALIST_ROLE = 'E2E Specialist reviewer'
const CARD = `Hand off to ${SPECIALIST}: E2E vendor comparison`
let lib: Library
let leadId = ''
let specialistId = ''
let handoffId = ''

test.describe.configure({ mode: 'serial' })

type Settings = Record<string, unknown>

async function chatSettings(): Promise<Settings> {
  const api = await apiAs('admin')
  const out = await unwrap<Settings>(await api.get(`/api/agents/${leadId}/chat`))
  await api.dispose()
  return out
}

async function putChatSettings(change: Settings): Promise<void> {
  const api = await apiAs('admin')
  const current = await unwrap<Settings>(await api.get(`/api/agents/${leadId}/chat`))
  await unwrap(await api.put(`/api/agents/${leadId}/chat`, { data: { ...current, ...change } }), 200)
  await api.dispose()
}

test.beforeAll(async () => {
  lib = await ensureLibrary()
  const api = await apiAs('admin')
  const agents = await unwrap<{ id: string; name: string }[]>(await api.get('/api/agents'))
  const ensure = async (name: string, role: string) =>
    agents.find((a) => a.name === name)?.id ?? (await unwrap<{ id: string }>(await api.post('/api/agents', { data: {
      name, role, goal: 'Answer precisely', backstory: 'Careful.', llm_id: lib.plainLlmId, tools: [], max_iter: 4,
      allow_delegation: false, verbose: false, area_ids: [lib.areaId],
    } }), 201)).id
  leadId = await ensure(LEAD, 'E2E lead adviser')
  specialistId = await ensure(SPECIALIST, SPECIALIST_ROLE)
  // Chat on with the specialist, as the agent chat spec leaves it; hand-offs off until the builder turns them on
  const current = await unwrap<Settings>(await api.get(`/api/agents/${leadId}/chat`))
  await unwrap(await api.put(`/api/agents/${leadId}/chat`, { data: { ...current, enabled: true, llm_id: lib.plainLlmId,
    specialist_ids: [specialistId], connection_ids: [], daily_budget_usd: 1, person_daily_budget_usd: 5, handoffs_enabled: false } }), 200)
  await api.dispose()
})

test.afterAll(async () => {
  await putChatSettings({ handoffs_enabled: false, daily_budget_usd: 1 })
})

async function openAgentChat(page: Page) {
  await page.goto('/chat')
  await expect(page.getByRole('link', { name: 'New conversation' })).toBeVisible()
  const tab = page.getByRole('tab', { name: 'Agents' })
  if (await tab.count()) await tab.click()
  await page.getByLabel('Agent').selectOption({ label: LEAD })
  await expect(page.getByText(`Can ask: ${SPECIALIST}.`)).toBeVisible()
}

/** Starts a conversation with the lead whose reply proposes a hand-off, and returns the card. */
async function proposeHandoff(page: Page, message: string, summary = CARD) {
  await openAgentChat(page)
  await page.getByLabel('Message').fill(message)
  await page.getByLabel('Message').press('Enter')
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
  const card = page.getByRole('region', { name: 'Proposed change' }).filter({ hasText: summary }).first()
  await expect(card.getByText(summary)).toBeVisible({ timeout: 60_000 })
  return card
}

test('a builder turns hand-offs on for the agent', async ({ page }) => {
  await page.goto(`/agents/${leadId}`)
  const panel = page.locator('section').filter({ has: page.getByText('People can chat with this agent') })
  await panel.getByRole('switch', { name: /Hand off longer work/ }).click()
  await panel.getByLabel('Budget per hand-off (USD)').fill('0.5')
  await panel.getByLabel('Time limit (minutes)').fill('10')
  await panel.getByLabel('At once per conversation').fill('2')
  await panel.getByRole('button', { name: 'Save chat settings' }).click()
  await expect(toast(page, 'Saved the chat settings')).toBeVisible()
  const saved = await chatSettings()
  expect(saved).toMatchObject({ handoffs_enabled: true, handoff_budget_usd: 0.5, handoff_timeout_minutes: 10, max_active_handoffs: 2 })
})

test('a confirmed hand-off works in the background and posts its result', async ({ page }) => {
  const before = llmRequests().length
  const card = await proposeHandoff(page, 'Compare Acme, Birch and Cedar for me. E2E_AGENT_HANDOFF')
  await expect(card.getByText('up to $0.50, up to 10 minutes')).toBeVisible()
  // The lead was offered Hand off work beside its coworker tools
  const lead = llmRequestsSince(before).find((r) => r.tool_names?.includes('hand_off_work'))
  expect(lead, 'the lead was offered Hand off work').toBeTruthy()

  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByText('Confirmed', { exact: true })).toBeVisible()
  // Hand-offs do not lock the composer
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeEnabled()
  const result = page.getByRole('listitem').filter({ hasText: `${SPECIALIST} (hand-off result)` })
  await expect(result.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 90_000 })

  // The specialist worked on the brief alone, never offered another hand-off
  const worker = llmRequestsSince(before).find((r) => r.text.includes('Compare the vendors.') && r.text.includes('handed you this work'))
  expect(worker, 'the specialist got the brief').toBeTruthy()
  expect(worker!.tool_names ?? []).not.toContain('hand_off_work')

  const admin = await apiAs('admin')
  const runs = await unwrap<{ id: string; kind: string; crew_name: string; cost_usd: number | null; status: string }[]>(
    await admin.get('/api/runs?kind=handoff'))
  const run = runs.find((r) => r.crew_name === LEAD && r.status === 'succeeded')
  expect(run, 'the hand-off is a run of kind handoff').toBeTruthy()
  expect(run!.cost_usd ?? 0).toBeGreaterThan(0)
  await admin.dispose()
})

test('the person keeps chatting while a slow hand-off runs, then cancels it', async ({ page }) => {
  const card = await proposeHandoff(page, 'Take your time on this one. E2E_SLOW_HANDOFF', `Hand off to ${SPECIALIST}: E2E slow comparison`)
  await card.getByRole('button', { name: 'Confirm' }).click()
  const working = page.getByRole('status', { name: 'Hand-off' })
  await expect(working.getByText(`${SPECIALIST} is working in the background`)).toBeVisible({ timeout: 60_000 })
  const url = page.url()
  const conversationId = url.slice(url.lastIndexOf('/') + 1)

  // Another message gets a reply while the hand-off works
  const replies = await page.getByText('FAKE-LLM-ANSWER').count()
  await page.getByRole('textbox', { name: 'Message' }).fill('Meanwhile, who owns Atlas?')
  await page.getByRole('button', { name: 'Send' }).click()
  await expect(page.getByText('FAKE-LLM-ANSWER')).toHaveCount(replies + 1, { timeout: 60_000 })
  await expect(working.getByText(`${SPECIALIST} is working in the background`)).toBeVisible()

  const admin = await apiAs('admin')
  const view = await unwrap<{ handoffs: { id: string; run_id: string; title: string }[] }>(await admin.get(`/api/chat/conversations/${conversationId}`))
  const handoff = view.handoffs.find((h) => h.title === 'E2E slow comparison')!
  handoffId = handoff.id
  // The conversation cannot be deleted while it runs
  const refused = await admin.delete(`/api/chat/conversations/${conversationId}`)
  expect(refused.status()).toBe(409)
  expect((await refused.json()).error.code).toBe('handoff_active')

  await confirm(working.getByRole('button', { name: 'Cancel' }))
  await expect(page.getByText('Cancelled by you')).toBeVisible({ timeout: 30_000 })
  const run = await unwrap<{ status: string }>(await admin.get(`/api/runs/${handoff.run_id}`))
  expect(run.status).toBe('cancelled')
  await admin.dispose()
})

test('a hand-off that does not fit the conversation budget is refused and its card waits', async ({ page }) => {
  const card = await proposeHandoff(page, 'One more comparison please. E2E_AGENT_HANDOFF')
  // The reply spent some of the day; with the conversation's budget at the minimum nothing usable is left
  await putChatSettings({ daily_budget_usd: 0.01 })
  try {
    await card.getByRole('button', { name: 'Confirm' }).click()
    await expect(toast(page, 'Could not apply the change')).toBeVisible()
    await expect(card.getByText('Waiting for you')).toBeVisible()
  } finally {
    await putChatSettings({ daily_budget_usd: 1 })
  }
})

test('other people cannot reach a hand-off', async () => {
  expect(handoffId).not.toBe('')
  const operator = await apiAs('operator')
  expect((await operator.get(`/api/chat/handoffs/${handoffId}`)).status()).toBe(404)
  expect((await operator.post(`/api/chat/handoffs/${handoffId}/cancel`, { data: {} })).status()).toBe(404)
  const runs = await unwrap<{ id: string }[]>(await operator.get('/api/runs?kind=handoff'))
  expect(runs).toEqual([])
  await operator.dispose()
  const viewer = await apiAs('viewer')
  expect((await viewer.get(`/api/chat/handoffs/${handoffId}`)).status()).toBe(403)
  await viewer.dispose()
})
