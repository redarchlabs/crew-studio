import { expect, test, type Page } from '@playwright/test'
import { FAKE_TRACKER_URL } from '../harness/paths.ts'
import { ADMIN } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { llmRequests, llmRequestsSince } from '../support/llmlog.ts'
import { toast, uid } from '../support/ui.ts'

// Specialists that propose (docs/agent-chat-handoffs-design.md, slice 3d): the specialist has Comment on Tracker Issue,
// which never reaches chat itself. A builder lets specialists propose changes; the person hands work to the specialist
// (E2E_PROPOSE_HANDOFF), whose brief makes it call Propose Tracker Comment on PRJ-10 (E2E_SPECIALIST_PROPOSE). The card
// appears in the conversation with a one-line lead-in; confirming posts the comment to the fake tracker as the person.
// Nobody else can confirm it.

const LEAD = 'E2E Lead'
const SPECIALIST = 'E2E Specialist'
const SPECIALIST_ROLE = 'E2E Specialist reviewer'
const CARD = `Hand off to ${SPECIALIST}: E2E summary post`
const CONN = uid('E2E Propose Jira')
let lib: Library
let leadId = ''
let specialistId = ''
let connectionId = ''
let cardId = ''

test.describe.configure({ mode: 'serial' })

type Settings = Record<string, unknown>
type Posted = { issue: string; author: string; body: unknown }

async function putChatSettings(change: Settings): Promise<void> {
  const api = await apiAs('admin')
  const current = await unwrap<Settings>(await api.get(`/api/agents/${leadId}/chat`))
  await unwrap(await api.put(`/api/agents/${leadId}/chat`, { data: { ...current, ...change } }), 200)
  await api.dispose()
}

async function setTools(id: string, tools: string[]): Promise<void> {
  const api = await apiAs('admin')
  const agent = await unwrap<Record<string, unknown>>(await api.get(`/api/agents/${id}`))
  await unwrap(await api.put(`/api/agents/${id}`, { data: { ...agent, tools } }))
  await api.dispose()
}

const posted = async (): Promise<Posted[]> => (await (await fetch(`${FAKE_TRACKER_URL}/__posted`)).json()) as Posted[]

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
  connectionId = (await unwrap<{ id: string }>(await api.post('/api/connections', { data: {
    name: CONN, kind: 'jira', area_ids: [lib.areaId], config: { site_url: FAKE_TRACKER_URL, email: 'e2e-bot@e2e.example' },
    secrets: { api_token: 'e2e-jira-token' },
  } }), 201)).id
  await api.dispose()
  // The specialist may comment on tracker issues; in chat that only ever becomes a card
  await setTools(specialistId, ['tracker_comment'])
  await putChatSettings({ enabled: true, llm_id: lib.plainLlmId, specialist_ids: [specialistId], crew_ids: [], connection_ids: [connectionId],
    daily_budget_usd: 1, person_daily_budget_usd: 5, handoffs_enabled: true, handoff_budget_usd: 0.5, handoff_timeout_minutes: 10,
    max_active_handoffs: 2, questions_enabled: false, specialists_may_propose: false })
})

test.afterAll(async () => {
  await putChatSettings({ handoffs_enabled: false, specialists_may_propose: false, connection_ids: [] })
  await setTools(specialistId, [])
  const api = await apiAs('admin')
  if (connectionId) await api.delete(`/api/connections/${connectionId}`)
  await api.dispose()
})

async function openAgentChat(page: Page) {
  await page.goto('/chat')
  await expect(page.getByRole('link', { name: 'New conversation' })).toBeVisible()
  const tab = page.getByRole('tab', { name: 'Agents' })
  if (await tab.count()) await tab.click()
  await page.getByLabel('Agent').selectOption({ label: LEAD })
  await expect(page.getByText(`Can ask: ${SPECIALIST}.`)).toBeVisible()
}

test('a builder lets the specialists propose changes', async ({ page }) => {
  await page.goto(`/agents/${leadId}`)
  const panel = page.locator('section').filter({ has: page.getByText('People can chat with this agent') })
  const cards = panel.getByRole('list', { name: 'Tools that become cards' })
  await expect(cards).toContainText(`${SPECIALIST}: Comment on Tracker Issue (only with this on).`)
  await panel.getByRole('switch', { name: /Specialists may propose changes/ }).click()
  await expect(cards).toContainText(`${SPECIALIST}: Comment on Tracker Issue.`)
  await panel.getByRole('button', { name: 'Save chat settings' }).click()
  await expect(toast(page, 'Saved the chat settings')).toBeVisible()
  const api = await apiAs('admin')
  expect(await unwrap<Settings>(await api.get(`/api/agents/${leadId}/chat`))).toMatchObject({ specialists_may_propose: true })
  await api.dispose()
})

test('a specialist’s comment during a hand-off is a card, posted as the person on Confirm', async ({ page }) => {
  const before = llmRequests().length
  const postedBefore = (await posted()).length
  await openAgentChat(page)
  await page.getByLabel('Message').fill('Hand the summary to the reviewer and post it on PRJ-10. E2E_PROPOSE_HANDOFF')
  await page.getByLabel('Message').press('Enter')
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
  const handoffCard = page.getByRole('region', { name: 'Proposed change' }).filter({ hasText: CARD }).first()
  await expect(handoffCard.getByText(CARD)).toBeVisible({ timeout: 60_000 })
  await handoffCard.getByRole('button', { name: 'Confirm' }).click()
  await expect(handoffCard.getByText('Confirmed', { exact: true })).toBeVisible()

  // The specialist proposes the comment: it was offered Propose Tracker Comment, never the tracker write itself
  const item = page.getByRole('listitem').filter({ hasText: 'proposed this during the hand-off' })
  await expect(item.getByText(`${SPECIALIST} proposed this during the hand-off “E2E summary post”.`)).toBeVisible({ timeout: 90_000 })
  const worker = llmRequestsSince(before).find((r) => r.text.includes('E2E_SPECIALIST_PROPOSE') && r.tool_names?.includes('propose_tracker_comment'))
  expect(worker, 'the specialist was offered Propose Tracker Comment').toBeTruthy()
  expect(worker!.tool_names ?? []).not.toContain('comment_on_tracker_issue')
  const card = item.getByRole('region', { name: 'Proposed change' })
  await expect(card.getByText('Comment on PRJ-10')).toBeVisible()
  await expect(card.getByText('Summary posted from E2E.')).toBeVisible()
  await expect(card.getByText(CONN)).toBeVisible()
  await expect(card.getByText(/while nobody was watching/)).toBeVisible()
  // Nothing is posted until the person confirms
  expect((await posted()).length).toBe(postedBefore)

  const url = page.url()
  const conversationId = url.slice(url.lastIndexOf('/') + 1)
  const admin = await apiAs('admin')
  const view = await unwrap<{ messages: { id: string; handoff_proposal?: boolean }[] }>(await admin.get(`/api/chat/conversations/${conversationId}`))
  cardId = view.messages.find((m) => m.handoff_proposal)!.id
  await admin.dispose()

  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByText('Confirmed', { exact: true })).toBeVisible()
  await expect(card.getByText(`Confirmed by ${ADMIN.name}`, { exact: false })).toBeVisible()
  const now = await posted()
  expect(now.length).toBe(postedBefore + 1)
  const comment = now[now.length - 1]
  expect(comment.issue).toBe('PRJ-10')
  // Posted through the connection's account, saying who confirmed it and which agent drafted it
  expect(comment.author).toBe('e2e-bot')
  const body = JSON.stringify(comment.body)
  expect(body).toContain('Summary posted from E2E.')
  expect(body).toContain(`Posted by ${ADMIN.name} from a Crew Studio agent chat; drafted by ${SPECIALIST}.`)
})

test('nobody else can confirm the card', async () => {
  expect(cardId).not.toBe('')
  const operator = await apiAs('operator')
  expect((await operator.post(`/api/chat/messages/${cardId}/confirm`, { data: {} })).status()).toBe(404)
  await operator.dispose()
})
