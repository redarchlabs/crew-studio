import { expect, test } from '@playwright/test'
import { LIB, statePath } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { llmRequests, llmRequestsSince } from '../support/llmlog.ts'
import { confirm, nav, toast } from '../support/ui.ts'

// Crew chat: turning it on in the crew builder, talking to the front desk through the real runner and the fake LLM,
// and confirming, declining and undoing what it proposes. The fake LLM answers E2E_CHAT_PROPOSE with a call to
// start_work_order and E2E_CHAT_NOTE with propose_note_change (harness/fake_llm.py).

const CHAT_TOOLS = ['list_notes', 'read_note', 'check_action_log', 'start_work_order', 'propose_note_change']
let lib: Library

test.describe.configure({ mode: 'serial' })

test.beforeAll(async () => {
  lib = await ensureLibrary()
})

test('a builder turns chat on for a crew in the crew builder', async ({ page }) => {
  await page.goto(`/crews/${lib.crewId}`)
  const panel = page.locator('section').filter({ has: page.getByLabel('Front desk agent') })
  await panel.getByRole('switch').click()
  await panel.getByLabel('Front desk agent').selectOption({ label: `${LIB.agent} — ${LIB.agentRole}` })
  await panel.getByLabel('Chat model').selectOption({ label: `${LIB.plainLlm} — ${LIB.model}` })
  await panel.getByLabel('Daily budget per conversation (USD)').fill('0.5')
  await panel.getByRole('button', { name: 'Save chat settings' }).click()
  await expect(toast(page, 'Saved the chat settings')).toBeVisible()
  await expect(panel.getByText(/Last changed by E2E Admin/)).toBeVisible()
})

test('the front desk answers with only chat tools and a confirmed proposal starts a work order', async ({ page }) => {
  const before = llmRequests().length
  await page.goto('/chat')
  await expect(nav(page).getByRole('link', { name: 'Chat' })).toBeVisible()
  await page.getByLabel('Crew').selectOption({ label: LIB.crew })
  await page.getByLabel('Message').fill('Please sweep Atlas now. E2E_CHAT_PROPOSE')
  await page.getByLabel('Message').press('Enter')
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)

  const card = page.getByRole('region', { name: 'Proposed change' })
  await expect(card.getByText('Start a work order: E2E chat sweep')).toBeVisible({ timeout: 45_000 })
  await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 30_000 })
  await expect(card.getByText('Waiting for you')).toBeVisible()

  // The model was offered exactly the chat tools, never the agent's own or a write tool
  const calls = llmRequestsSince(before).filter((r) => r.has_tools)
  expect(calls.length).toBeGreaterThan(0)
  expect([...(calls[0].tool_names ?? [])].sort()).toEqual([...CHAT_TOOLS].sort())
  // The front desk is told the crew's inputs, and the tool tells it what it sent
  expect(calls[0].text).toContain('The crew\'s run inputs: topic ("Topic", required).')
  expect(llmRequestsSince(before).some((r) => r.text.includes('Inputs: none, so the crew\'s defaults apply.'))).toBeTruthy()

  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByText('Confirmed', { exact: true })).toBeVisible()
  await expect(card.getByText(/Confirmed by E2E Admin/)).toBeVisible()
  await card.getByRole('link', { name: /Open WO-\d+/ }).click()
  await expect(page).toHaveURL(/\/work-orders\/[0-9a-f-]{36}/)
  await expect(page.getByText('E2E chat sweep').first()).toBeVisible()
  await expect(page.getByText('chat', { exact: true }).first()).toBeVisible()
})

test('a note change is confirmed with the person\'s name and undone', async ({ page }) => {
  await page.goto(`/chat?crew=${lib.crewId}`)
  await page.getByLabel('Message').fill('Note that the vendor is late. E2E_CHAT_NOTE')
  await page.getByRole('button', { name: 'Send' }).click()
  const card = page.getByRole('region', { name: 'Proposed change' })
  await expect(card.getByText('Create the note e2e-chat-note')).toBeVisible({ timeout: 45_000 })
  await expect(card.getByText('Added from chat.')).toBeVisible()
  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByRole('link', { name: 'Saved as version 1' })).toBeVisible()

  const admin = await apiAs('admin')
  const notes = await unwrap<{ name: string; written_by: string }[]>(await admin.get(`/api/crews/${lib.crewId}/state/notes`))
  expect(notes.find((n) => n.name === 'e2e-chat-note')?.written_by).toBe('E2E Admin')

  await card.getByRole('button', { name: 'Undo' }).click()
  await expect(card.getByText('Undone')).toBeVisible()
  const after = await unwrap<{ name: string }[]>(await admin.get(`/api/crews/${lib.crewId}/state/notes`))
  expect(after.some((n) => n.name === 'e2e-chat-note')).toBe(false)

  // Chat turns are runs of kind chat, filterable on the Runs page
  const chats = await unwrap<{ kind: string; crew_name: string }[]>(await admin.get('/api/runs?kind=chat'))
  expect(chats.length).toBeGreaterThanOrEqual(2)
  expect(chats.every((r) => r.kind === 'chat')).toBe(true)
  await admin.dispose()
  await page.goto('/runs?kind=chat')
  await expect(page.getByRole('row').filter({ hasText: LIB.crew }).first().getByText('chat', { exact: true })).toBeVisible()
})

test('a declined proposal changes nothing, and conversations can be deleted', async ({ page }) => {
  await page.goto(`/chat?crew=${lib.crewId}`)
  await page.getByLabel('Message').fill('Sweep again please. E2E_CHAT_PROPOSE')
  await page.getByLabel('Message').press('Enter')
  const card = page.getByRole('region', { name: 'Proposed change' })
  await expect(card.getByText('Waiting for you')).toBeVisible({ timeout: 45_000 })
  await card.getByRole('button', { name: 'Decline' }).click()
  await expect(card.getByText('Declined', { exact: true })).toBeVisible()
  await expect(card.getByRole('button', { name: 'Confirm' })).toHaveCount(0)
  // A conversation can be deleted once its reply has arrived
  await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 30_000 })
  await confirm(page.getByRole('button', { name: 'Delete' }))
  await expect(toast(page, 'Conversation deleted')).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`/chat\\?crew=${lib.crewId}$`))
})

test('people without crews.chat do not get chat, and proposals need the signed-in owner', async ({ browser }) => {
  const ctx = await browser.newContext({ storageState: statePath('viewer') })
  const page = await ctx.newPage()
  await page.goto('/')
  await expect(nav(page).getByRole('link', { name: 'Runs' })).toBeVisible()
  await expect(nav(page).getByRole('link', { name: 'Chat' })).toHaveCount(0)
  await ctx.close()
  const viewer = await apiAs('viewer')
  expect((await viewer.get('/api/chat/crews')).status()).toBe(403)
  await viewer.dispose()

  // An operator can chat, but cannot open or decide on the admin's conversations
  const admin = await apiAs('admin')
  const convs = await unwrap<{ id: string }[]>(await admin.get('/api/chat/conversations'))
  const view = await unwrap<{ messages: { id: string; proposal: unknown }[] }>(await admin.get(`/api/chat/conversations/${convs[0].id}`))
  const withProposal = view.messages.find((m) => m.proposal)!
  await admin.dispose()
  const operator = await apiAs('operator')
  expect((await operator.get('/api/chat/crews')).status()).toBe(200)
  expect((await operator.get(`/api/chat/conversations/${convs[0].id}`)).status()).toBe(404)
  expect((await operator.post(`/api/chat/messages/${withProposal.id}/confirm`, { data: {} })).status()).toBe(404)
  // Chat turns are private even to people who can view every run in the area
  expect(await unwrap<unknown[]>(await operator.get('/api/runs?kind=chat'))).toEqual([])
  await operator.dispose()
})
