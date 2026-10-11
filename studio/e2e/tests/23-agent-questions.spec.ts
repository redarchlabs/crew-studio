import { expect, test, type Page } from '@playwright/test'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { llmRequests, llmRequestsSince } from '../support/llmlog.ts'
import { toast } from '../support/ui.ts'

// Questions up the line (docs/agent-chat-handoffs-design.md, slice 3c): a builder lets the lead ask the person during a
// hand-off. A person's message makes the lead hand work to the specialist (E2E_AGENT_QUESTION), whose brief makes it ask
// the lead (E2E_HANDOFF_ASK_LEAD); its question makes the lead desk ask the person (E2E_LEAD_ASK_PERSON). The question
// waits in the conversation as a Waiting for you card with a badge on Chat; the person answers and the hand-off
// finishes. The journal records each step, and nobody else can list or answer the question.

const LEAD = 'E2E Lead'
const LEAD_ROLE = 'E2E lead adviser'
const SPECIALIST = 'E2E Specialist'
const SPECIALIST_ROLE = 'E2E Specialist reviewer'
const CARD = `Hand off to ${SPECIALIST}: E2E price comparison`
const QUESTION = 'Which prices should the comparison use?'
let lib: Library
let leadId = ''
let specialistId = ''
let conversationId = ''
let questionId = ''
let runId = ''

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
  leadId = await ensure(LEAD, LEAD_ROLE)
  specialistId = await ensure(SPECIALIST, SPECIALIST_ROLE)
  // Chat and hand-offs on with the specialist; questions off until the builder turns them on
  const current = await unwrap<Settings>(await api.get(`/api/agents/${leadId}/chat`))
  await unwrap(await api.put(`/api/agents/${leadId}/chat`, { data: { ...current, enabled: true, llm_id: lib.plainLlmId,
    specialist_ids: [specialistId], connection_ids: [], daily_budget_usd: 1, person_daily_budget_usd: 5, handoffs_enabled: true,
    handoff_budget_usd: 0.5, handoff_timeout_minutes: 10, max_active_handoffs: 2, questions_enabled: false } }), 200)
  await api.dispose()
})

test.afterAll(async () => {
  await putChatSettings({ handoffs_enabled: false, questions_enabled: false })
})

async function openAgentChat(page: Page) {
  await page.goto('/chat')
  await expect(page.getByRole('link', { name: 'New conversation' })).toBeVisible()
  const tab = page.getByRole('tab', { name: 'Agents' })
  if (await tab.count()) await tab.click()
  await page.getByLabel('Agent').selectOption({ label: LEAD })
  await expect(page.getByText(`Can ask: ${SPECIALIST}.`)).toBeVisible()
}

test('a builder lets the lead ask the person during a hand-off', async ({ page }) => {
  await page.goto(`/agents/${leadId}`)
  const panel = page.locator('section').filter({ has: page.getByText('People can chat with this agent') })
  await panel.getByRole('switch', { name: /The lead may ask the person during a hand-off/ }).click()
  await panel.getByLabel('When to ask the person').fill('Ask before choosing between price models')
  await panel.getByLabel('Wait for an answer (minutes)').fill('5')
  await panel.getByLabel('Questions per hand-off').fill('2')
  await panel.getByRole('button', { name: 'Save chat settings' }).click()
  await expect(toast(page, 'Saved the chat settings')).toBeVisible()
  expect(await chatSettings()).toMatchObject({ questions_enabled: true, question_rules: 'Ask before choosing between price models',
    question_timeout_minutes: 5, question_on_timeout: 'proceed', max_questions: 2 })
})

test('the hand-off asks the person through the lead, waits for the answer, and finishes', async ({ page }) => {
  const before = llmRequests().length
  await openAgentChat(page)
  await page.getByLabel('Message').fill('Compare the vendors on price for me. E2E_AGENT_QUESTION')
  await page.getByLabel('Message').press('Enter')
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
  conversationId = page.url().slice(page.url().lastIndexOf('/') + 1)
  const card = page.getByRole('region', { name: 'Proposed change' }).filter({ hasText: CARD }).first()
  await expect(card.getByText(CARD)).toBeVisible({ timeout: 60_000 })
  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByText('Confirmed', { exact: true })).toBeVisible()

  // The question waits under the hand-off, says who asked through whom, and the Chat entry shows a badge
  const waiting = page.getByRole('region', { name: 'Waiting for you' })
  await expect(waiting.getByText(QUESTION)).toBeVisible({ timeout: 90_000 })
  await expect(waiting.getByText(`Asked by ${SPECIALIST_ROLE} through ${LEAD_ROLE}`, { exact: false })).toBeVisible()
  const chatLink = page.getByRole('navigation').getByRole('link', { name: /Chat/ })
  await expect(chatLink.getByTitle('1 question waiting for you')).toBeVisible()
  // The composer stays open while the hand-off waits
  await expect(page.getByRole('textbox', { name: 'Message' })).toBeEnabled()

  await waiting.getByLabel('Your answer').fill('Use list prices.')
  await waiting.getByRole('button', { name: 'Send answer' }).click()
  await expect(toast(page, 'Sent your answer; the run carries on')).toBeVisible()
  const result = page.getByRole('listitem').filter({ hasText: `${SPECIALIST} (hand-off result)` })
  await expect(result.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 90_000 })
  await expect(chatLink.getByTitle(/waiting for you/)).toHaveCount(0)
  // The answered question folds away under its hand-off
  await expect(result.getByText('Earlier questions (1)')).toBeVisible()

  // The specialist was offered Ask the lead (it has no reads of its own); the lead desk only Ask a human
  const sent = llmRequestsSince(before)
  const specialist = sent.find((r) => r.text.includes('E2E_HANDOFF_ASK_LEAD') && r.text.includes('handed you this work'))
  expect(specialist?.tool_names ?? []).toEqual(['ask_the_lead'])
  const desk = sent.find((r) => r.text.includes('asks you, the lead, a question'))
  expect(desk?.tool_names ?? []).toEqual(['ask_a_human'])
})

test('the journal records the question to the lead, the question to the person and the answer', async () => {
  const admin = await apiAs('admin')
  const view = await unwrap<{ handoffs: { run_id: string; title: string }[] }>(await admin.get(`/api/chat/conversations/${conversationId}`))
  runId = view.handoffs.find((h) => h.title === 'E2E price comparison')!.run_id
  const run = await unwrap<{ status: string; cost_usd: number | null; crew_name: string }>(await admin.get(`/api/runs/${runId}`))
  expect(run).toMatchObject({ status: 'succeeded', crew_name: LEAD })
  expect(run.cost_usd ?? 0).toBeGreaterThan(0)
  const journal = await unwrap<{ entries: { kind: string; title: string }[] }>(await admin.get(`/api/runs/${runId}/journal`))
  const titles = journal.entries.map((e) => e.title)
  expect(titles.some((t) => t.startsWith(`${SPECIALIST_ROLE} asked the lead: List or negotiated prices?`))).toBeTruthy()
  expect(titles.some((t) => t.startsWith(`Asked a human for ${SPECIALIST_ROLE}: ${QUESTION}`))).toBeTruthy()
  expect(titles.some((t) => t.startsWith('E2E Admin answered: Use list prices.'))).toBeTruthy()
  expect(titles.some((t) => t === `${LEAD_ROLE} answered ${SPECIALIST_ROLE}`)).toBeTruthy()
  const questions = await unwrap<{ id: string; status: string; asked_for: string; handoff_title: string }[]>(
    await admin.get(`/api/chat/conversations/${conversationId}/questions`))
  expect(questions).toHaveLength(1)
  expect(questions[0]).toMatchObject({ status: 'answered', asked_for: SPECIALIST_ROLE, handoff_title: 'E2E price comparison' })
  questionId = questions[0].id
  await admin.dispose()
})

test('other people cannot list or answer the conversation questions', async () => {
  expect(questionId).not.toBe('')
  const operator = await apiAs('operator')
  expect((await operator.get(`/api/chat/conversations/${conversationId}/questions`)).status()).toBe(404)
  expect([403, 404]).toContain((await operator.post(`/api/questions/${questionId}/answer`, { data: { answer: 'Negotiated.' } })).status())
  await operator.dispose()
})
