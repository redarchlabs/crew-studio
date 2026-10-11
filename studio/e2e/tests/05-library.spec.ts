import { expect, test, type Page } from '@playwright/test'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { confirm, linkRow, row, toast, uid } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })

/** Ticks options (functional areas, context tasks...) by their label; names are unique on these pages. */
async function pick(page: Page, _within: string, names: string[]): Promise<void> {
  for (const name of names) {
    const opt = page.getByRole('option', { name: new RegExp(`^${name}\\b`) })
    if ((await opt.getAttribute('aria-selected')) !== 'true') await opt.click()
  }
}

test.describe('LLMs', () => {
  test('a name and a model are required', async ({ page }) => {
    await page.goto('/llms')
    await page.getByRole('button', { name: 'New LLM' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog.getByText('Name is required')).toBeVisible()
    await expect(dialog.getByText('Model is required')).toBeVisible()
    await dialog.getByLabel('Name', { exact: true }).fill('Bad responses LLM')
    await dialog.getByLabel('Model', { exact: true }).fill('anthropic/claude-haiku-4-5')
    await dialog.getByLabel('API', { exact: true }).selectOption('responses')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog.getByText('The Responses API is only for openai/ models')).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toHaveCount(0)
  })

  test('create a vision LLM and a plain one, then edit the first', async ({ page }) => {
    await page.goto('/llms')
    await page.getByRole('button', { name: 'New LLM' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name', { exact: true }).fill(LIB.visionLlm)
    await dialog.getByLabel('Model', { exact: true }).fill(LIB.model)
    await expect(dialog.getByText('gpt-6-luna', { exact: true })).toBeVisible() // price match badge
    await dialog.getByLabel('Max completion tokens').fill('4096')
    await dialog.getByRole('switch', { name: /Vision/ }).click()
    await expect(dialog.getByRole('switch', { name: /Vision/ })).toHaveAttribute('aria-checked', 'true')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(toast(page, 'LLM created')).toBeVisible()
    await expect(row(page, LIB.visionLlm)).toContainText(LIB.model)

    await page.getByRole('button', { name: 'New LLM' }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(LIB.plainLlm)
    await dialog.getByLabel('Model', { exact: true }).fill(LIB.model)
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(row(page, LIB.plainLlm)).toBeVisible()

    // duplicate names are refused by the server
    await page.getByRole('button', { name: 'New LLM' }).click()
    await dialog.getByLabel('Name', { exact: true }).fill(LIB.plainLlm)
    await dialog.getByLabel('Model', { exact: true }).fill(LIB.model)
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(toast(page, 'Could not create LLM')).toBeVisible()
    await dialog.getByRole('button', { name: 'Cancel' }).click()

    // edit: lower the token limit and check the vision flag persisted
    await page.getByRole('button', { name: `Edit ${LIB.visionLlm}` }).click()
    await expect(dialog.getByRole('switch', { name: /Vision/ })).toHaveAttribute('aria-checked', 'true')
    await dialog.getByLabel('Max completion tokens').fill('2048')
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(toast(page, 'LLM updated')).toBeVisible()
    await expect(row(page, LIB.visionLlm)).toContainText('2048')
    await page.reload()
    await expect(row(page, LIB.visionLlm)).toContainText('2048')
  })
})

test.describe('agents', () => {
  test('required fields are enforced', async ({ page }) => {
    await page.goto('/agents/new')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Name is required' })).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: 'Role is required' })).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: 'Goal is required' })).toBeVisible()
    await expect(page.getByText('Choose at least one functional area').first()).toBeVisible()
  })

  test('create an agent, then edit it', async ({ page }) => {
    await page.goto('/agents/new')
    await page.getByLabel('Name', { exact: true }).fill(LIB.agent)
    await page.getByLabel('Role', { exact: true }).fill(LIB.agentRole)
    await page.getByLabel('Goal', { exact: true }).fill('Find and summarise facts about {topic}')
    await page.getByLabel('Backstory', { exact: true }).fill('A careful analyst who always cites sources.')
    await expect(page.getByText('{topic}').first()).toBeVisible() // placeholder chip
    await pick(page, 'Identity', [LIB.area])
    await page.getByLabel('LLM', { exact: true }).selectOption({ label: `${LIB.visionLlm} — ${LIB.model}` })
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created agent')).toBeVisible()
    await expect(page).toHaveURL(/\/agents\/[0-9a-f-]{36}$/)
    await expect(page.getByRole('heading', { name: LIB.agent })).toBeVisible()

    await page.getByLabel('Goal', { exact: true }).fill('Find, verify and summarise facts about {topic}')
    await page.getByLabel('Max iterations').fill('7')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved agent')).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Max iterations')).toHaveValue('7')
    await expect(page.getByLabel('Goal', { exact: true })).toHaveValue(/verify/)

    await page.goto('/agents')
    await expect(page.getByRole('link', { name: new RegExp(LIB.agent) })).toContainText('Not used yet')
    await page.getByPlaceholder('Filter agents…').fill('zzz-no-such-agent')
    await expect(page.getByText(/No agents match/)).toBeVisible()
  })
})

test.describe('tasks', () => {
  test('required fields are enforced', async ({ page }) => {
    await page.goto('/tasks/new')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Description is required' })).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: 'Expected output is required' })).toBeVisible()
  })

  test('create two tasks, the second reading the first as context', async ({ page }) => {
    const agentLabel = `${LIB.agent} — ${LIB.agentRole}`
    await page.goto('/tasks/new')
    await page.getByLabel('Name', { exact: true }).fill(LIB.task1)
    await page.getByLabel('Description', { exact: true }).fill('Research {topic} and list five key facts.')
    await page.getByLabel('Expected output', { exact: true }).fill('A bullet list of five facts about {topic}.')
    await pick(page, 'Task', [LIB.area])
    await page.getByLabel('Agent', { exact: true }).selectOption({ label: agentLabel })
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created task')).toBeVisible()
    await expect(page).toHaveURL(/\/tasks\/[0-9a-f-]{36}$/)

    await page.goto('/tasks/new')
    await page.getByLabel('Name', { exact: true }).fill(LIB.task2)
    await page.getByLabel('Description', { exact: true }).fill('Write a two sentence summary of the findings about {topic}.')
    await page.getByLabel('Expected output', { exact: true }).fill('Two sentences.')
    await pick(page, 'Task', [LIB.area])
    await page.getByLabel('Agent', { exact: true }).selectOption({ label: agentLabel })
    await pick(page, 'Context tasks', [LIB.task1])
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created task')).toBeVisible()

    await page.goto('/tasks')
    const r = linkRow(page, LIB.task2)
    await expect(r).toContainText(LIB.task1) // context column
    await expect(r).toContainText('topic') // placeholder chip
    await expect(r.getByRole('link', { name: LIB.agent })).toBeVisible()
    await expect(linkRow(page, LIB.task1)).toContainText(LIB.area)
  })
})

test.describe('crews', () => {
  test('build a crew: tasks, inputs, graph and validation', async ({ page }) => {
    await page.goto('/crews/new')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Name is required' })).toBeVisible()
    await expect(page.getByText('Save the crew to run server validation.')).toBeVisible()

    await page.getByLabel('Name', { exact: true }).fill(LIB.crew)
    await page.getByLabel('Description', { exact: true }).fill('Crew built by the e2e suite')
    await pick(page, 'Crew', [LIB.area])
    const work = page.locator('section').filter({ has: page.getByLabel('Task to add') })
    for (const t of [LIB.task1, LIB.task2]) {
      await work.getByLabel('Task to add').selectOption({ label: t })
      await work.getByRole('button', { name: 'Add', exact: true }).click()
    }
    await expect(work.getByRole('listitem').filter({ hasText: LIB.task2 })).toContainText(LIB.agent)
    // the agent came with its task
    await expect(work.getByRole('option', { name: new RegExp(`^${LIB.agent}`) })).toHaveAttribute('aria-selected', 'true')

    // the live graph shows the crew, its tasks and the agent
    const canvas = page.locator('.react-flow')
    await expect(canvas).toBeVisible()
    await expect(canvas.getByText(LIB.task1)).toBeVisible()
    await expect(canvas.getByText(LIB.agent).first()).toBeVisible()

    // inputs: detect {topic} from the tasks, then describe it
    await page.getByRole('button', { name: /Detect from placeholders/ }).click()
    await expect(toast(page, 'Added 1 input')).toBeVisible()
    await page.getByLabel('Input 1 label').fill('Topic')
    await page.getByLabel('Input 1 default').fill(LIB.topicDefault)
    await page.getByLabel('Budget per run (USD)').fill('0.5')

    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created crew')).toBeVisible()
    await expect(page).toHaveURL(/\/crews\/[0-9a-f-]{36}$/)
    await expect(page.getByText('Ready to run')).toBeVisible()

    await page.goto('/crews')
    const r = row(page, LIB.crew)
    await expect(r).toContainText(`${LIB.task1} → ${LIB.task2}`)
    await expect(r).toContainText('topic')
    await expect(r).toContainText('$0.50')
    await expect(r.getByRole('button', { name: 'Run' })).toBeVisible()
  })

  test('an input key must be lower snake case', async ({ page }) => {
    await page.goto('/crews')
    await page.getByRole('link', { name: LIB.crew }).click()
    await page.getByLabel('Input 1 key').fill('Bad Key')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText(/key must match/).first()).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Input 1 key')).toHaveValue('topic')
  })

  test('edit the crew; a context task placed after its reader is flagged', async ({ page }) => {
    await page.goto('/crews')
    await page.getByRole('link', { name: LIB.crew }).click()
    await page.getByLabel('Budget per run (USD)').fill('0.75')
    await page.getByLabel('Description', { exact: true }).fill('Crew built by the e2e suite (edited)')
    await expect(page.getByText('unsaved', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved crew')).toBeVisible()

    // swap the order: the summary would run before the task it reads
    const work = page.locator('section').filter({ has: page.getByLabel('Task to add') })
    await work.getByRole('listitem').filter({ hasText: LIB.task2 }).getByRole('button', { name: 'Move up' }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved crew')).toBeVisible()
    await page.getByRole('button', { name: 'Re-validate' }).click()
    await expect(page.getByText('Ready to run')).toHaveCount(0)
    await expect(page.locator('section').filter({ hasText: 'Validation' }).getByRole('listitem').first()).toBeVisible()

    // put it back
    await work.getByRole('listitem').filter({ hasText: LIB.task2 }).getByRole('button', { name: 'Move down' }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved crew')).toBeVisible()
    await page.getByRole('button', { name: 'Re-validate' }).click()
    await expect(page.getByText('Ready to run')).toBeVisible()
  })

  test('history shows each revision with who and what changed', async ({ page }) => {
    await page.goto('/crews')
    await page.getByRole('link', { name: LIB.crew }).click()
    await expect(page.getByRole('heading', { name: LIB.crew })).toBeVisible()
    await page.getByRole('main').getByRole('link', { name: 'History' }).click()
    await expect(page).toHaveURL(/\/history\?entity_type=crew&entity_id=/)
    const rows = page.getByRole('row').filter({ hasText: LIB.crew })
    await expect(rows.filter({ hasText: 'create' })).toHaveCount(1)
    expect(await rows.filter({ hasText: 'update' }).count()).toBeGreaterThanOrEqual(3)
    await expect(rows.first()).toContainText('E2E Admin')
    // the oldest revision is the one with the first budget
    await rows.filter({ hasText: 'create' }).click()
    await expect(page.locator('pre').first()).toContainText('"budget_usd": 0.5')
    // newest first: the latest update carries the final description
    await rows.first().click()
    await expect(page.locator('pre').last()).toContainText('(edited)')
  })
})

test.describe('deleting things that are in use', () => {
  test('the server refuses until nothing uses an item, then allows it', async ({ page }) => {
    const admin = await apiAs('admin')
    const tag = uid('Tmp').replace(/\s/g, '')
    const areas = await unwrap<{ id: string; name: string }[]>(await admin.get('/api/areas'))
    const areaIds = [areas.find((a) => a.name === LIB.area)!.id]
    const llm = await unwrap<{ id: string }>(await admin.post('/api/llms', { data: { name: `${tag} LLM`, model: LIB.model, temperature: null, max_completion_tokens: null, reasoning_effort: null, api: 'completions', vision: false } }), 201)
    const agent = await unwrap<{ id: string }>(await admin.post('/api/agents', { data: { name: `${tag} Agent`, role: 'r', goal: 'g', backstory: 'b', llm_id: llm.id, tools: [], max_iter: 5, allow_delegation: false, verbose: false, area_ids: areaIds } }), 201)
    const task = await unwrap<{ id: string }>(await admin.post('/api/tasks', { data: { name: `${tag} Task`, description: 'd', expected_output: 'e', agent_id: agent.id, context_task_ids: [], markdown: false, area_ids: areaIds } }), 201)
    const crew = await unwrap<{ id: string }>(await admin.post('/api/crews', { data: { name: `${tag} Crew`, description: '', process: 'sequential', manager_llm_id: null, agent_ids: [agent.id], task_ids: [task.id], inputs: [], budget_usd: 1, area_ids: areaIds } }), 201)
    await admin.dispose()

    // LLM in use by the agent
    await page.goto('/llms')
    await confirm(row(page, `${tag} LLM`).getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Could not delete ${tag} LLM`)).toBeVisible()
    await expect(toast(page, /still used by/)).toBeVisible()
    await expect(row(page, `${tag} LLM`)).toBeVisible()

    // agent in use by the task and crew
    await page.goto(`/agents/${agent.id}`)
    // The "Used by" panel; the agent's Chat panel also offers the crew under "Crews it may start"
    const usedBy = page.locator('section').filter({ has: page.getByText('Used by', { exact: true }) })
    await expect(usedBy.getByRole('link', { name: `${tag} Crew` })).toBeVisible()
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Could not delete ${tag} Agent`)).toBeVisible()
    await expect(page).toHaveURL(new RegExp(`/agents/${agent.id}$`))

    // task in use by the crew
    await page.goto(`/tasks/${task.id}`)
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Could not delete ${tag} Task`)).toBeVisible()

    // delete from the top down: crew, task, agent, LLM
    await page.goto(`/crews/${crew.id}`)
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${tag} Crew`)).toBeVisible()
    await expect(page).toHaveURL(/\/crews$/)
    await page.goto(`/tasks/${task.id}`)
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${tag} Task`)).toBeVisible()
    await page.goto(`/agents/${agent.id}`)
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${tag} Agent`)).toBeVisible()
    await page.goto('/llms')
    await confirm(row(page, `${tag} LLM`).getByRole('button', { name: 'Delete' }))
    await expect(toast(page, 'LLM deleted')).toBeVisible()
    await expect(row(page, `${tag} LLM`)).toHaveCount(0)
  })
})
