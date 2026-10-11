import { expect, test, type Page } from '@playwright/test'
import { FAKE_KM2_URL } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { llmRequestsSince, llmRequests } from '../support/llmlog.ts'
import { toast, uid } from '../support/ui.ts'

// KM2 through the fake KM2 (harness/fake_km2.py): a KM2 connection made in the UI and tested (it lists the folders the
// key sees), a crew run whose agent saves a document with km_save_document (the fake LLM answers E2E_KM_SAVE with that
// tool call), the save on the crew's State tab, and a chat turn whose front desk asks KM2 (E2E_KM_ASK). The key never
// reaches the run. No real KM2 is contacted.

const KEY = 'km2_e2e-key'
const PROJECTS = '6a1f0000-0000-4000-8000-000000000001'
const ATLAS = '6a1f0000-0000-4000-8000-000000000002'
const HR = '6a1f0000-0000-4000-8000-000000000003'
const CONN = uid('E2E KM2')
const AGENT = uid('E2E KM2 Analyst')
const RUN_TIMEOUT = 120_000

type Saved = { id: string; folder_id: string; external_ref: string; title: string; metadata: Record<string, unknown> }
type KM2Request = { method: string; path: string; body: Record<string, unknown> }

let lib: Library
let agentId = ''
let taskId = ''
let crewId = ''
let connectionId = ''

const fakeKM2 = async <T,>(path: string): Promise<T> => (await (await fetch(`${FAKE_KM2_URL}${path}`)).json()) as T

test.describe.configure({ mode: 'serial' })

test.beforeAll(async () => {
  lib = await ensureLibrary()
  const api = await apiAs('admin')
  agentId = (await unwrap<{ id: string }>(await api.post('/api/agents', { data: {
    name: AGENT, role: 'Knowledge keeper', goal: 'Keep the Atlas knowledge in KM2 current', backstory: 'Files every weekly report.',
    llm_id: lib.plainLlmId, tools: ['km_save_document', 'km_search', 'km_ask'], max_iter: 4, allow_delegation: false, verbose: false, area_ids: [lib.areaId],
  } }), 201)).id
  taskId = (await unwrap<{ id: string }>(await api.post('/api/tasks', { data: {
    name: uid('E2E KM2 report'), description: 'Write the Atlas weekly report and save it to KM2. E2E_KM_SAVE', expected_output: 'A short report.',
    agent_id: agentId, context_task_ids: [], markdown: false, area_ids: [lib.areaId],
  } }), 201)).id
  crewId = (await unwrap<{ id: string }>(await api.post('/api/crews', { data: {
    name: uid('E2E KM2 Crew'), description: 'Saves to KM2', process: 'sequential', manager_llm_id: null, agent_ids: [agentId],
    task_ids: [taskId], inputs: [], budget_usd: 0.75, area_ids: [lib.areaId],
  } }), 201)).id
  await api.dispose()
})

test.afterAll(async () => {
  const api = await apiAs('admin')
  await api.put(`/api/crews/${crewId}/chat`, { data: { enabled: false, front_desk_agent_id: null, llm_id: null, daily_budget_usd: 1 } })
  await api.put(`/api/crews/${crewId}/connections`, { data: { connection_ids: [] } })
  if (connectionId) await api.delete(`/api/connections/${connectionId}`)
  await api.delete(`/api/crews/${crewId}`)
  await api.delete(`/api/tasks/${taskId}`)
  await api.delete(`/api/agents/${agentId}`)
  await api.dispose()
})

async function statusBadge(page: Page, status: string): Promise<void> {
  await expect(page.getByRole('main').locator('header').getByText(status, { exact: true }).first()).toBeVisible({ timeout: RUN_TIMEOUT })
}

test('a KM2 connection is added in the UI and its test lists the folders the key sees', async ({ page }) => {
  await page.goto('/connections')
  await page.getByRole('button', { name: 'New connection' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Name').fill(CONN)
  await dialog.getByLabel('Kind').selectOption('km2')
  await dialog.getByLabel('Base URL').fill(FAKE_KM2_URL)
  await dialog.getByLabel(/^Default folder for saves/).fill('not-a-folder')
  await dialog.getByLabel(/^Other folders crews may save to/).fill(PROJECTS)
  await dialog.getByLabel('Profile API key').fill(KEY)
  await dialog.getByRole('option', { name: new RegExp(`^${LIB.area}`) }).click()
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(dialog.getByText('Must be a KM2 folder id (a UUID)')).toBeVisible()
  await dialog.getByLabel(/^Default folder for saves/).fill(ATLAS)
  await dialog.getByRole('button', { name: 'Create' }).click()
  await expect(toast(page, `Saved ${CONN}`)).toBeVisible()

  const api = await apiAs('admin')
  const list = await unwrap<{ id: string; name: string; config: Record<string, string>; secret_keys: string[] }[]>(await api.get('/api/connections'))
  const conn = list.find((c) => c.name === CONN)!
  connectionId = conn.id
  expect(conn.config).toEqual({ base_url: FAKE_KM2_URL, default_folder_id: ATLAS, write_folder_ids: PROJECTS })
  expect(conn.secret_keys).toEqual(['api_key'])
  expect(JSON.stringify(list)).not.toContain(KEY)
  await unwrap(await api.put(`/api/crews/${crewId}/connections`, { data: { connection_ids: [connectionId] } }))
  await api.dispose()

  const row = page.getByRole('row').filter({ hasText: CONN })
  await row.getByRole('button', { name: 'Test' }).click()
  await expect(row.getByText('Connected: the key sees 3 folders.')).toBeVisible()
  await row.getByText('Folders the key can see (3)').click()
  const atlas = row.getByRole('listitem').filter({ hasText: 'Projects.Atlas' })
  await expect(atlas.getByText('default for saves')).toBeVisible()
  await expect(row.getByRole('listitem').filter({ hasText: /^Projects6a1f/ }).getByText('crews may save')).toBeVisible()
})

test('once the key is saved, the connection folders are picked from lists of names', async ({ page }) => {
  await page.goto('/connections')
  await page.getByRole('row').filter({ hasText: CONN }).getByRole('button', { name: 'Edit' }).click()
  const dialog = page.getByRole('dialog')
  const def = dialog.getByLabel('Default folder for saves')
  await expect(def.locator('option:checked')).toHaveText('Projects.Atlas')
  await expect(def.locator('option').first()).toHaveText('None (saving off)')
  const folders = dialog.getByTestId('write-folders')
  await expect(folders.getByLabel('Projects', { exact: true })).toBeChecked()
  await folders.getByLabel('HR', { exact: true }).check()
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(toast(page, `Saved ${CONN}`)).toBeVisible()
  const api = await apiAs('admin')
  const conn = await unwrap<{ config: Record<string, string> }>(await api.get(`/api/connections/${connectionId}`))
  expect(conn.config.write_folder_ids).toBe(`${PROJECTS},${HR}`)
  expect(conn.config.default_folder_id).toBe(ATLAS)
  await api.dispose()
})

test('a KM2 report destination picks its folder from the folders the connection allows', async ({ page }) => {
  const api = await apiAs('admin')
  const schedule = await unwrap<{ id: string }>(await api.post('/api/schedules', { data: {
    name: uid('E2E KM2 report schedule'), crew_id: crewId, cron: '0 9 * * 1', timezone: 'UTC', overlap_policy: 'allow', enabled: false, brief: 'Weekly', inputs: {},
  } }), 201)
  try {
    await page.goto(`/schedules/${schedule.id}`)
    const panel = page.locator('section').filter({ has: page.getByText('Report destinations', { exact: true }) })
    await panel.getByRole('button', { name: 'Add destination' }).click()
    const row = panel.getByTestId('destination-row')
    await row.getByLabel('Destination', { exact: true }).selectOption('km2')
    await row.getByLabel('Connection').selectOption(connectionId)
    const folder = row.getByLabel('Folder', { exact: true })
    await expect(folder.locator('option').first()).toHaveText("Connection's default folder (Projects.Atlas)")
    await folder.selectOption({ label: 'HR' })
    await panel.getByRole('button', { name: 'Save destinations' }).click()
    await expect(async () => {
      const view = await unwrap<{ destinations: { kind: string; folder: string }[] }>(await api.get(`/api/schedules/${schedule.id}/report-destinations`))
      expect(view.destinations).toEqual([expect.objectContaining({ kind: 'km2', folder: HR })])
    }).toPass({ timeout: 10_000 })
  } finally {
    await api.delete(`/api/schedules/${schedule.id}`)
    await api.dispose()
  }
})

test('a crew run saves a document to KM2, and the State tab lists the save', async ({ page }) => {
  const writes = async () => (await fakeKM2<KM2Request[]>('/__requests')).filter((r) => r.method === 'POST' && r.path === '/api/v1/knowledge/documents').length
  const writesBefore = await writes()
  const api = await apiAs('admin')
  const wo = await unwrap<{ id: string }>(await api.post('/api/work-orders', { data: {
    title: 'KM2 weekly', brief: 'File the weekly report.', crew_id: crewId, inputs: {}, budget_usd: null,
  } }), 201)
  await api.dispose()
  const before = llmRequests().length
  await page.goto(`/work-orders/${wo.id}`)
  await page.getByRole('button', { name: 'Submit' }).first().click()
  await statusBadge(page, 'succeeded')

  // The model was offered the KM2 tools, and the save reached KM2 with Studio's metadata and never the key
  const offered = llmRequestsSince(before).find((r) => r.has_tools)
  expect(offered?.tool_names).toEqual(expect.arrayContaining(['save_to_knowledge_km2', 'search_knowledge_km2', 'ask_knowledge_km2']))
  expect(await writes()).toBe(writesBefore + 1)
  const doc = (await fakeKM2<Saved[]>('/__saved')).find((d) => d.external_ref === 'e2e-atlas-weekly')!
  expect(doc).toMatchObject({ folder_id: ATLAS, external_ref: 'e2e-atlas-weekly', title: 'E2E Atlas weekly' })
  expect(doc.metadata).toMatchObject({ project: 'atlas', source: 'crew-studio' })
  const requests = await fakeKM2<KM2Request[]>('/__requests')
  expect(JSON.stringify(requests)).not.toContain(KEY)

  await page.goto(`/crews/${crewId}?tab=state`)
  const saves = page.getByRole('table', { name: 'Recent KM2 saves' })
  await expect(saves.getByText('E2E Atlas weekly')).toBeVisible()
  await expect(saves.getByText('e2e-atlas-weekly')).toBeVisible()
  // created the first time; the fake KM2 answers unchanged when the suite runs again against the same fake
  await expect(saves.getByText(/^(created|unchanged)$/).first()).toBeVisible()
  await expect(page.getByText('km2:save').first()).toBeVisible()
})

test('the front desk asks KM2 in chat, read-only', async ({ page }) => {
  const api = await apiAs('admin')
  await unwrap(await api.put(`/api/crews/${crewId}/chat`, { data: {
    enabled: true, front_desk_agent_id: agentId, llm_id: lib.plainLlmId, daily_budget_usd: 0.5,
  } }))
  await api.dispose()
  const before = llmRequests().length
  const asked = (await fakeKM2<KM2Request[]>('/__requests')).filter((r) => r.path === '/api/v1/search/chat').length

  await page.goto('/chat')
  await page.getByLabel('Crew').selectOption({ label: (await crewName()) })
  await page.getByLabel('Message').fill('Who owns Atlas? E2E_KM_ASK')
  await page.getByLabel('Message').press('Enter')
  await expect(page).toHaveURL(/\/chat\/[0-9a-f-]{36}$/)
  await expect(page.getByText('FAKE-LLM-ANSWER')).toBeVisible({ timeout: 60_000 })

  const calls = llmRequestsSince(before).filter((r) => r.has_tools)
  expect(calls.length).toBeGreaterThan(0)
  expect(calls[0].tool_names).toEqual(expect.arrayContaining(['search_knowledge_km2', 'ask_knowledge_km2']))
  expect(calls[0].tool_names).not.toContain('save_to_knowledge_km2')
  // The tool result the front desk saw came from KM2, fenced as untrusted data
  const followUp = llmRequestsSince(before).find((r) => r.text.includes('KM2-FAKE-ANSWER'))
  expect(followUp?.text).toContain('comes from KM2')
  expect((await fakeKM2<KM2Request[]>('/__requests')).filter((r) => r.path === '/api/v1/search/chat').length).toBe(asked + 1)
})

async function crewName(): Promise<string> {
  const api = await apiAs('admin')
  const crew = await unwrap<{ name: string }>(await api.get(`/api/crews/${crewId}`))
  await api.dispose()
  return crew.name
}
