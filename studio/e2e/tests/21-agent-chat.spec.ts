import { expect, test, type Page } from '@playwright/test'
import { FAKE_TRACKER_URL } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { llmRequestsSince, llmRequests } from '../support/llmlog.ts'
import { toast, uid } from '../support/ui.ts'

// Agent chat (docs/agent-chat-design.md): a builder turns chat on for an agent with a specialist and a crew it may
// start; a person asks the agent, which asks the specialist in the same run (E2E_AGENT_ASK makes the fake LLM call
// Ask question to coworker) and proposes a run of the crew (E2E_AGENT_PROPOSE calls Start Crew Work Order). An agent
// with a tracker read and a listed Jira connection reads an issue in chat (E2E_AGENT_READ_ISSUE calls Get Tracker
// Issue), through the fake tracker (harness/fake_tracker.py).

const LEAD = 'E2E Lead'
const SPECIALIST = 'E2E Specialist'
const SPECIALIST_ROLE = 'E2E Specialist reviewer'
const CONN = uid('E2E Agent Jira')
let lib: Library
let leadId: string
let connectionId = ''

test.describe.configure({ mode: 'serial' })

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
  await ensure(SPECIALIST, SPECIALIST_ROLE)
  await api.dispose()
})

test.afterAll(async () => {
  const api = await apiAs('admin')
  // The lead goes back to no tools of its own; deleting the connection takes it off the agent's list
  const lead = await unwrap<Record<string, unknown>>(await api.get(`/api/agents/${leadId}`))
  await api.put(`/api/agents/${leadId}`, { data: { ...lead, tools: [] } })
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

test('a builder turns chat on for an agent with a specialist and a crew', async ({ page }) => {
  await page.goto(`/agents/${leadId}`)
  const panel = page.locator('section').filter({ has: page.getByText('People can chat with this agent') })
  await panel.getByRole('switch', { name: /People can chat with this agent/ }).click()
  await panel.getByLabel('Chat model').selectOption({ label: `${LIB.plainLlm} — ${LIB.model}` })
  await panel.getByRole('option', { name: new RegExp(`^${SPECIALIST} — `) }).click()
  await panel.getByRole('option', { name: new RegExp(`^${LIB.crew}`) }).click()
  await panel.getByRole('button', { name: 'Save chat settings' }).click()
  await expect(toast(page, 'Saved the chat settings')).toBeVisible()
  await expect(panel.getByText(/Last changed by E2E Admin/)).toBeVisible()
})

test('the agent asks its specialist in the same run', async ({ page }) => {
  const before = llmRequests().length
  await openAgentChat(page)
  await page.getByLabel('Message').fill('Is Atlas on track? E2E_AGENT_ASK')
  await page.getByLabel('Message').press('Enter')
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
  await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 60_000 })

  const calls = llmRequestsSince(before)
  // The agent was offered the coworker tools and the crew proposal, never a write tool
  const lead = calls.find((r) => r.tool_names?.includes('ask_question_to_coworker'))
  expect(lead, 'the agent was offered the coworker tools').toBeTruthy()
  expect([...(lead!.tool_names ?? [])].sort()).toEqual(['ask_question_to_coworker', 'delegate_work_to_coworker', 'start_crew_work_order'])
  // The specialist got the question, not the conversation
  const specialist = calls.find((r) => r.text.includes('What is the biggest risk for project Atlas?') && !r.tool_names?.includes('ask_question_to_coworker'))
  expect(specialist, 'the specialist was asked').toBeTruthy()
  expect(specialist!.text).not.toContain('Is Atlas on track?')

  // The turn is one chat run named after the agent, costed with the specialist's calls
  const admin = await apiAs('admin')
  const runs = await unwrap<{ id: string; kind: string; crew_name: string; cost_usd: number | null }[]>(await admin.get('/api/runs?kind=chat'))
  const run = runs.find((r) => r.crew_name === LEAD)
  expect(run).toBeTruthy()
  expect(run!.cost_usd ?? 0).toBeGreaterThan(0)
  // The hand-off is recorded as a delegation (CrewAI names the tool ask_question_to_coworker)
  const events = await unwrap<{ type: string; payload: Record<string, unknown> }[]>(await admin.get(`/api/runs/${run!.id}/events`))
  const handoff = events.find((ev) => ev.type === 'delegation')
  expect(handoff, 'the hand-off was recorded').toBeTruthy()
  expect(handoff!.payload.to_agent).toBe(SPECIALIST_ROLE)
  await admin.dispose()
})

test('a proposed run of a crew on its list starts a work order when confirmed', async ({ page }) => {
  const before = llmRequests().length
  await openAgentChat(page)
  await page.getByLabel('Message').fill('Please run the research on Atlas. E2E_AGENT_PROPOSE')
  await page.getByRole('button', { name: 'Send' }).click()
  const card = page.getByRole('region', { name: 'Proposed change' })
  await expect(card.getByText(`Start a work order on ${LIB.crew}: E2E agent run`)).toBeVisible({ timeout: 60_000 })
  await expect(card.getByText(LIB.crew, { exact: true })).toBeVisible()
  // The tool sent the input as a key and value pair, and told the agent exactly what it sent
  expect(llmRequestsSince(before).some((r) => r.text.includes('Inputs: topic=Atlas.')), 'the proposal echoed its inputs').toBeTruthy()
  await expect(page.getByText(/Runs the agent proposes wait for your Confirm\./)).toBeVisible()
  await card.getByRole('button', { name: 'Confirm' }).click()
  await expect(card.getByText('Confirmed', { exact: true })).toBeVisible()
  await card.getByRole('link', { name: /Open WO-\d+/ }).click()
  await expect(page).toHaveURL(/\/work-orders\/[0-9a-f-]{36}/)
  await expect(page.getByText('E2E agent run').first()).toBeVisible()
})

test('an agent with a listed tracker connection reads an issue in chat', async ({ page }) => {
  const api = await apiAs('admin')
  connectionId = (await unwrap<{ id: string }>(await api.post('/api/connections', { data: {
    name: CONN, kind: 'jira', area_ids: [lib.areaId], config: { site_url: FAKE_TRACKER_URL, email: 'e2e-bot@e2e.example' },
    secrets: { api_token: 'e2e-jira-token' },
  } }), 201)).id
  // The lead may read tracker issues and comment on them; only the read reaches chat
  const lead = await unwrap<Record<string, unknown>>(await api.get(`/api/agents/${leadId}`))
  await unwrap(await api.put(`/api/agents/${leadId}`, { data: { ...lead, tools: ['tracker_get_issue', 'tracker_comment'] } }))
  await api.dispose()

  // The builder lists the connection under Connections it may read; the tool picker marks the two tools
  await page.goto(`/agents/${leadId}`)
  await expect(page.getByRole('option', { name: /^Get Tracker Issue/ }).getByText('Reads', { exact: true })).toBeVisible()
  await expect(page.getByRole('option', { name: /^Comment on Tracker Issue/ }).getByText('Changes', { exact: true })).toBeVisible()
  const panel = page.locator('section').filter({ has: page.getByText('People can chat with this agent') })
  await panel.getByRole('option', { name: new RegExp(`^${CONN}`) }).click()
  await panel.getByRole('button', { name: 'Save chat settings' }).click()
  await expect(toast(page, 'Saved the chat settings')).toBeVisible()

  const before = llmRequests().length
  await openAgentChat(page)
  await page.getByLabel('Message').fill('What is PRJ-11 about? E2E_AGENT_READ_ISSUE')
  await page.getByLabel('Message').press('Enter')
  await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 60_000 })

  const calls = llmRequestsSince(before)
  const offered = calls.find((r) => r.tool_names?.includes('get_tracker_issue'))
  expect(offered, 'the agent was offered the tracker read').toBeTruthy()
  expect(offered!.tool_names).not.toContain('comment_on_tracker_issue')
  // The issue came back from the fake tracker through Studio, fenced as data
  expect(calls.some((r) => r.text.includes('Vendor access') && r.text.includes('issue PRJ-11')), 'the agent read the issue').toBeTruthy()

  const admin = await apiAs('admin')
  const runs = await unwrap<{ id: string; crew_name: string }[]>(await admin.get('/api/runs?kind=chat'))
  const run = runs.find((r) => r.crew_name === LEAD)!
  const events = await unwrap<{ type: string; payload: Record<string, unknown> }[]>(await admin.get(`/api/runs/${run.id}/events`))
  expect(events.some((ev) => String(ev.payload.message ?? '').startsWith(`Tracker: read PRJ-11 in ${CONN}`)), 'the read was journalled').toBeTruthy()
  await admin.dispose()
})

test('people without chat cannot list agents', async () => {
  const viewer = await apiAs('viewer')
  expect((await viewer.get('/api/chat/agents')).status()).toBe(403)
  await viewer.dispose()
})
