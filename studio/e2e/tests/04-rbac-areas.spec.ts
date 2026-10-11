import { expect, test } from '@playwright/test'
import { SEED, statePath } from '../support/accounts.ts'
import { apiAs } from '../support/api.ts'
import { fixmeIfOverviewBroken, nav, row } from '../support/ui.ts'

const AI_AGENT = 'Research Analyst'
const AI_TASKS = ['AI Research Task', 'AI Research Report']
const PE_TASKS = ['Corporate Profile', 'Financial Picture', 'Market and Competition', 'Investment Memo']

async function expectNav(page: import('@playwright/test').Page, present: string[], absent: string[]): Promise<void> {
  for (const label of present) await expect(nav(page).getByRole('link', { name: label, exact: true })).toBeVisible()
  for (const label of absent) await expect(nav(page).getByRole('link', { name: label, exact: true })).toHaveCount(0)
}

test.describe('viewer (Finance + IT, read-only)', () => {
  test.use({ storageState: statePath('viewer') })

  test('sees the lists but no create, edit, delete or run buttons', async ({ page }) => {
    await page.goto('/crews')
    await expect(page.getByRole('link', { name: SEED.peCrew })).toBeVisible()
    await expect(page.getByRole('link', { name: SEED.aiCrew })).toBeVisible()
    await expect(page.getByRole('link', { name: 'New crew' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Run', exact: true })).toHaveCount(0)

    await page.goto('/agents')
    await expect(page.getByRole('link', { name: new RegExp(`^${AI_AGENT}`) }).first()).toBeVisible()
    await expect(page.getByRole('link', { name: 'New agent' })).toHaveCount(0)

    await page.goto('/tasks')
    await expect(page.getByRole('link', { name: 'Corporate Profile' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'New task' })).toHaveCount(0)

    await page.goto('/llms')
    await expect(page.getByRole('note')).toContainText('llms.manage')
    await expect(page.getByRole('button', { name: 'New LLM' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /^Edit / })).toHaveCount(0)

    await page.goto('/work-orders')
    await expect(page.getByRole('heading', { name: 'Work orders' })).toBeVisible()
    await expect(page.getByRole('link', { name: /New work order/ })).toHaveCount(0)
    await page.goto('/schedules')
    await expect(page.getByRole('link', { name: /New schedule/ })).toHaveCount(0)
    await page.goto('/triggers')
    await expect(page.getByRole('link', { name: /New trigger/ })).toHaveCount(0)
  })

  test('opening an editor by its URL is read-only', async ({ page }) => {
    for (const [path, title] of [[`/crews/${SEED.peCrewId}`, SEED.peCrew], [`/tasks/${SEED.peTaskId}`, 'Corporate Profile'], [`/agents/${SEED.aiAgentId}`, 'Research Analyst']] as const) {
      await page.goto(path)
      await expect(page.getByRole('heading', { name: title })).toBeVisible()
      await expect(page.getByText('read-only', { exact: true })).toBeVisible()
      await expect(page.getByRole('note')).toContainText(/Read-only: you need/)
      await expect(page.getByRole('button', { name: 'Save' })).toHaveCount(0)
      await expect(page.getByRole('button', { name: 'Delete' })).toHaveCount(0)
      await expect(page.getByLabel('Name', { exact: true })).toBeDisabled()
    }
    // and the Run button is not offered
    await page.goto(`/crews/${SEED.peCrewId}`)
    await expect(page.getByRole('button', { name: 'Run', exact: true })).toHaveCount(0)
  })

  test('the navigation hides what the role cannot use', async ({ page }) => {
    await page.goto('/')
    await expectNav(page, ['Overview', 'Work orders', 'Runs', 'Crews', 'Agents', 'Tasks', 'Schedules', 'Triggers', 'LLMs', 'Pricing', 'History', 'API explorer'], ['Settings', 'People', 'Service accounts', 'Roles', 'Functional areas', 'Sign-in settings', 'Audit log'])
  })

  test('a page behind a permission explains what is missing', async ({ page }) => {
    await page.goto('/admin/users')
    await expect(page.getByText("You don't have access to this page")).toBeVisible()
    await expect(page.getByText('admin.users', { exact: true })).toBeVisible()
    await page.goto('/settings')
    await expect(page.getByText("You don't have access to this page")).toBeVisible()
    await page.goto('/work-orders/new')
    await expect(page.getByText("You don't have access to this page")).toBeVisible()
    await page.getByRole('button', { name: 'Back to overview' }).click()
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
  })

  test('the API refuses writes with 403', async () => {
    const api = await apiAs('viewer')
    expect((await api.post('/api/agents', { data: { name: 'x', role: 'x', goal: 'x', backstory: 'x', tools: [], max_iter: 5, allow_delegation: false, verbose: false, area_ids: [] } })).status()).toBe(403)
    expect((await api.delete(`/api/crews/${SEED.peCrewId}`)).status()).toBe(403)
    expect((await api.post(`/api/crews/${SEED.peCrewId}/runs`, { data: { inputs: {} } })).status()).toBe(403)
    expect((await api.get('/api/admin/users')).status()).toBe(403)
    expect((await api.get('/api/crews')).status()).toBe(200)
    await api.dispose()
  })
})

test.describe('operator (Finance + IT, can run, cannot edit)', () => {
  test.use({ storageState: statePath('operator') })

  test('can start a run from the crew list and the crew page but not change the crew', async ({ page }) => {
    await page.goto('/crews')
    await expect(page.getByRole('link', { name: 'New crew' })).toHaveCount(0)
    const ai = row(page, SEED.aiCrew)
    await ai.getByRole('button', { name: 'Run' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: `Run ${SEED.aiCrew}` })).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'Start run' })).toBeEnabled()
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toHaveCount(0)

    await page.goto(`/crews/${SEED.aiCrewId}`)
    await expect(page.getByRole('button', { name: 'Run', exact: true })).toBeEnabled()
    await expect(page.getByRole('note')).toContainText('crews.edit')
    await expect(page.getByRole('button', { name: 'Save' })).toHaveCount(0)
    await expect(page.getByLabel('Name', { exact: true })).toBeDisabled()
  })

  test('can open the work order form and sees the Operator navigation', async ({ page }) => {
    await page.goto('/work-orders/new')
    await expect(page.getByRole('heading', { name: /New work order/ })).toBeVisible()
    await expectNav(page, ['Overview', 'Work orders', 'Runs', 'Crews'], ['Settings', 'History', 'People'])
  })

  test('the API refuses edits but accepts what the role allows', async () => {
    const api = await apiAs('operator')
    expect((await api.delete(`/api/crews/${SEED.aiCrewId}`)).status()).toBe(403)
    const crew = await (await api.get(`/api/crews/${SEED.aiCrewId}`)).json()
    expect((await api.put(`/api/crews/${SEED.aiCrewId}`, { data: crew.data })).status()).toBe(403)
    await api.dispose()
  })
})

test.describe('builder in Finance only', () => {
  test.use({ storageState: statePath('builder') })

  test('sees Finance items and none from IT', async ({ page }) => {
    await page.goto('/crews')
    await expect(page.getByRole('link', { name: SEED.peCrew })).toBeVisible()
    await expect(page.getByRole('link', { name: SEED.aiCrew })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'New crew' })).toBeVisible()

    await page.goto('/agents')
    await expect(page.getByRole('link', { name: /Corporate Investigator/ })).toBeVisible()
    await expect(page.getByRole('link', { name: new RegExp(`^${AI_AGENT}`) })).toHaveCount(0)

    await page.goto('/tasks')
    for (const t of PE_TASKS) await expect(page.getByRole('link', { name: t, exact: true })).toBeVisible()
    for (const t of AI_TASKS) await expect(page.getByRole('link', { name: t, exact: true })).toHaveCount(0)
  })

  test('an IT item opened by URL is "not found"', async ({ page }) => {
    await page.goto(`/crews/${SEED.aiCrewId}`)
    await expect(page.getByText('Crew not found')).toBeVisible()
    await page.goto(`/tasks/${SEED.aiTaskId}`)
    await expect(page.getByText('Task not found')).toBeVisible()
    await page.goto(`/agents/${SEED.aiAgentId}`)
    await expect(page.getByText('Agent not found')).toBeVisible()
    // The Finance one opens normally and is editable
    await page.goto(`/crews/${SEED.peCrewId}`)
    await expect(page.getByRole('heading', { name: SEED.peCrew })).toBeVisible()
    await expect(page.getByLabel('Name', { exact: true })).toBeEnabled()
  })

  test('the API agrees: IT items are 404 and absent from lists', async () => {
    const api = await apiAs('builder')
    expect((await api.get(`/api/crews/${SEED.aiCrewId}`)).status()).toBe(404)
    expect((await api.get(`/api/tasks/${SEED.aiTaskId}`)).status()).toBe(404)
    expect((await api.get(`/api/agents/${SEED.aiAgentId}`)).status()).toBe(404)
    expect((await api.put(`/api/crews/${SEED.aiCrewId}`, { data: {} })).status()).toBe(404)
    expect((await api.post(`/api/crews/${SEED.aiCrewId}/runs`, { data: { inputs: {} } })).status()).toBe(404)
    const crews = (await (await api.get('/api/crews')).json()).data as { name: string }[]
    expect(crews.map((c) => c.name)).toEqual([SEED.peCrew])
    await api.dispose()
  })

  test('cannot place a new item in another area', async ({ page }) => {
    await page.goto('/agents/new')
    const picker = page.locator('section').filter({ hasText: 'Identity' })
    await expect(picker.getByRole('option', { name: /^IT/ })).toBeDisabled()
    await expect(picker.getByRole('option', { name: /^Finance/ })).toBeEnabled()
    // the person's only area is preselected
    await expect(picker.getByRole('option', { name: /^Finance/ })).toHaveAttribute('aria-selected', 'true')
  })

  test('the navigation shows build and run sections but no admin or settings pages', async ({ page }) => {
    await page.goto('/')
    await expectNav(page, ['Overview', 'Crews', 'Agents', 'Tasks', 'Schedules', 'Triggers', 'History'], ['People', 'Service accounts', 'Roles', 'Functional areas', 'Audit log', 'Sign-in settings'])
    // builder holds settings.view but not settings.manage; the nav shows the page, the page is read-only
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
  })
})

test.describe('user in HR only (no items in their area)', () => {
  test.use({ storageState: statePath('outsider') })

  test('the lists are empty and the seeded crews are not found', async ({ page }) => {
    await page.goto('/crews')
    await expect(page.getByText('No crews yet')).toBeVisible()
    await expect(page.getByRole('link', { name: SEED.peCrew })).toHaveCount(0)
    await page.goto('/tasks')
    await expect(page.getByText('No tasks yet')).toBeVisible()
    await page.goto(`/crews/${SEED.peCrewId}`)
    await expect(page.getByText('Crew not found')).toBeVisible()
  })

  test('the overview has no runs for them', async ({ page }) => {
    await fixmeIfOverviewBroken(page, test)
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
    await expect(page.getByText('No runs yet')).toBeVisible()
  })

  test('the API shows nothing from other areas', async () => {
    const api = await apiAs('outsider')
    const tasks = (await (await api.get('/api/tasks')).json()).data as unknown[]
    expect(tasks).toHaveLength(0)
    expect((await api.get(`/api/crews/${SEED.peCrewId}/validate`)).status()).toBe(404)
    await api.dispose()
  })
})
