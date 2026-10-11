import { expect, test, type Locator, type Page } from '@playwright/test'
import { LIB } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary, oneTaskCrew } from '../support/library.ts'
import { llmRequests, llmRequestsSince } from '../support/llmlog.ts'
import { makePng } from '../support/png.ts'
import { confirm, linkRow, toast } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })

const RUN_TIMEOUT = 120_000
const TITLE = 'Screenshot review'
const BRIEF = 'Please review the attached screenshot and the notes.'
const NOTES = 'NOTES-FILE-CONTENT: remember the quarterly deadline.'
const TOPIC = 'solar panels'

type Named = { id: string; name: string }
const state: { id: string; number: number; url: string; requestsBefore: number } = { id: '', number: 0, url: '', requestsBefore: 0 }

test.beforeAll(async () => {
  await ensureLibrary()
})

/** Dispatches a paste event whose clipboard holds the given items; returns true when nothing cancelled it. */
async function paste(textarea: Locator, items: { text?: string; png?: string }): Promise<boolean> {
  return textarea.evaluate((el, { text, png }) => {
    const dt = new DataTransfer()
    if (text !== undefined) dt.setData('text/plain', text)
    if (png) {
      const bytes = Uint8Array.from(atob(png), (c) => c.charCodeAt(0))
      dt.items.add(new File([bytes], 'image.png', { type: 'image/png' }))
    }
    return el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }))
  }, items)
}

async function statusBadge(page: Page, status: string): Promise<void> {
  await expect(page.getByRole('main').locator('header').getByText(status, { exact: true }).first()).toBeVisible({ timeout: RUN_TIMEOUT })
}

async function chooseCrew(page: Page, name: string): Promise<void> {
  await page.getByRole('button', { name: /Choose a crew/ }).click()
  await page.getByPlaceholder('Search…').fill(name)
  await page.getByRole('option', { name: new RegExp(`^${name}\\b`) }).first().click()
}

test.describe('creating a work order', () => {
  test('the form says what is missing', async ({ page }) => {
    await page.goto('/work-orders/new')
    await page.getByRole('button', { name: 'Submit' }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Give the work order a title.' })).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: 'Choose a crew to run.' })).toBeVisible()
    await page.getByLabel('Title').fill('Incomplete')
    await chooseCrew(page, LIB.crew)
    // the {topic} input appears, prefilled with the crew's default; submitting it empty is refused
    await expect(page.getByLabel(/Topic/)).toHaveValue(LIB.topicDefault)
    await page.getByLabel(/Topic/).fill('')
    await page.getByRole('button', { name: 'Submit' }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Topic is required' })).toBeVisible()
    await page.getByLabel('Budget (USD)').fill('0')
    await page.getByRole('button', { name: 'Submit' }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Budget must be greater than 0' })).toBeVisible()
  })

  test('brief, a pasted screenshot, pasted text and an uploaded file, then submit', async ({ page }) => {
    state.requestsBefore = llmRequests().length
    await page.goto('/work-orders/new')
    await page.getByLabel('Title').fill(TITLE)
    await chooseCrew(page, LIB.crew)
    await page.getByLabel(/Topic/).fill(TOPIC)

    const brief = page.getByLabel('Brief', { exact: true })
    await brief.click()
    await page.keyboard.insertText(BRIEF) // typed text
    await expect(brief).toHaveValue(BRIEF)

    // Pasting text is left to the browser: the component must not swallow it or attach anything
    expect(await paste(brief, { text: 'copied words' })).toBe(true)
    // Office-style clipboards carry text and an image rendition: still no attachment
    expect(await paste(brief, { text: 'cell A1', png: makePng(16).toString('base64') })).toBe(true)
    await expect(page.getByText('Input files')).toHaveCount(0)

    // A clipboard with only an image becomes an attachment named pasted-<time>.png
    expect(await paste(brief, { png: makePng(48).toString('base64') })).toBe(false)
    await expect(toast(page, /Saved draft WO-\d+ so your files can upload/)).toBeVisible()
    const tray = page.getByRole('list').filter({ has: page.getByRole('button', { name: /^Remove pasted-/ }) })
    await expect(tray.getByText(/^pasted-\d{8}-\d{6}\.png$/)).toBeVisible()
    await expect(tray.getByText('uploading…')).toHaveCount(0)

    // upload a text file with the file input
    await page.locator('input[type=file]').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from(NOTES) })
    await expect(tray.getByText('notes.txt')).toBeVisible()
    await expect(page.getByText(/2\/20 files/)).toBeVisible()

    // pasted text still lands in the box through the normal text path
    await brief.click()
    await page.keyboard.press('End')
    await page.keyboard.insertText(' Pasted: deadline is Friday.')
    await expect(brief).toHaveValue(/Pasted: deadline is Friday\./)
    await page.getByRole('tab', { name: 'Preview' }).click()
    await expect(page.getByText(/Pasted: deadline is Friday\./)).toBeVisible()
    await page.getByRole('tab', { name: 'Write' }).click()

    await page.getByRole('button', { name: 'Submit' }).click()
    await expect(toast(page, /WO-\d+ submitted/)).toBeVisible()
    await expect(page).toHaveURL(/\/work-orders\/[0-9a-f-]{36}/)
    state.url = page.url()
    state.id = /work-orders\/([0-9a-f-]{36})/.exec(state.url)![1]
    state.number = Number(/WO-(\d+)/.exec(await page.getByRole('main').locator('header').innerText())![1])
  })

  test('the detail page shows the brief, input files and the final output', async ({ page }) => {
    await page.goto(state.url)
    await statusBadge(page, 'succeeded')
    const header = page.getByRole('main').locator('header')
    await expect(header).toContainText(TITLE)
    await expect(header).toContainText(`WO-${state.number}`)
    await expect(header).toContainText(LIB.crew)
    await expect(header).toContainText(/Tokens[\d,]{4,}/)

    await page.getByRole('tab', { name: /^Brief/ }).click()
    await expect(page.getByText(BRIEF).first()).toBeVisible()
    await expect(page.getByText('Input files (2)')).toBeVisible()
    await expect(page.getByText(/pasted-\d{8}-\d{6}\.png/).first()).toBeVisible()
    await expect(page.getByText('notes.txt').first()).toBeVisible()
    await expect(page.getByText('Final output')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'FAKE-LLM-ANSWER' }).first()).toBeVisible()
    await expect(page.getByText('topic').first()).toBeVisible()
    await expect(page.getByText(TOPIC).first()).toBeVisible()
  })

  test('the vision model received the pasted image as a base64 image part, and the text file inline', async () => {
    const seen = llmRequestsSince(state.requestsBefore).filter((r) => r.text.includes(BRIEF))
    expect(seen.length).toBeGreaterThan(0)
    const withImage = seen.filter((r) => r.image_count > 0)
    expect(withImage.length, 'the vision LLM should receive the pasted screenshot').toBeGreaterThan(0)
    expect(withImage[0].image_mimes).toContain('image/png') // sent as a data: URL (base64), not a link
    expect(withImage[0].path).toMatch(/chat\/completions$/)
    expect(withImage[0].text).toContain(NOTES)
    expect(withImage[0].text).toContain('is attached for you to look at')
    expect(withImage[0].text).toContain(`WO-${state.number}`)
  })

  test('the workflow tab draws the graph and the sequence of the attempt', async ({ page }) => {
    await page.goto(`${state.url}?tab=workflow`)
    await expect(page.getByRole('tab', { name: 'Graph' })).toHaveAttribute('aria-selected', 'true')
    const canvas = page.locator('.react-flow')
    await expect(canvas).toBeVisible()
    const nodes = canvas.locator('.react-flow__node')
    await expect(nodes.first()).toBeVisible()
    expect(await nodes.count()).toBeGreaterThanOrEqual(3) // work order, tasks, agent (and the model)
    await expect(nodes.filter({ hasText: `WO-${state.number}` }).first()).toBeVisible()
    await expect(nodes.filter({ hasText: LIB.task1 }).first()).toBeVisible()
    await expect(nodes.filter({ hasText: 'Analyst for' }).first()).toBeVisible()
    // selecting a node opens its details
    await nodes.filter({ hasText: LIB.task1 }).first().click()
    await expect(page.getByText('Close').or(page.getByRole('button', { name: 'Close' })).first()).toBeVisible()

    await page.getByRole('tab', { name: 'Sequence' }).click()
    await expect(page.getByText('Time', { exact: true })).toBeVisible()
    await expect(page.getByText(LIB.agentRole.replace('{topic}', TOPIC)).first()).toBeVisible()
    await expect(page.getByText(/\d+\/\d+ entries/)).toBeVisible()
    // each lane entry is clickable and shows its detail
    await page.getByRole('button', { name: /Asked gpt-6-luna|Finished/ }).first().click()
    await expect(page.getByText('FAKE-LLM-ANSWER').first()).toBeVisible()
  })

  test('the journal lists entries per agent and task', async ({ page }) => {
    await page.goto(`${state.url}?tab=journal`)
    await expect(page.getByText(LIB.task1).first()).toBeVisible()
    await expect(page.getByText(`Asked gpt-6-luna`).first()).toBeVisible()
    await expect(page.getByText(`Finished ${LIB.task1}`)).toBeVisible()
    // the rail filters by agent
    await page.getByRole('button', { name: new RegExp(LIB.agentRole.replace('{topic}', TOPIC)) }).first().click()
    await expect(page.getByText(`Finished ${LIB.task1}`)).toBeVisible()
  })

  test('the files tab has the input files, the transcript and the console log, all downloadable', async ({ page }) => {
    await page.goto(`${state.url}?tab=files`)
    await expect(page.getByText(/Input files · 2/)).toBeVisible()
    const png = page.getByRole('link', { name: /Download pasted-.*\.png/ })
    await expect(png).toBeVisible()
    const pngBytes = await (await page.request.get((await png.getAttribute('href'))!)).body()
    expect([...pngBytes.subarray(0, 4)]).toEqual([0x89, 0x50, 0x4e, 0x47])
    const notes = page.getByRole('link', { name: 'Download notes.txt' })
    expect(await (await page.request.get((await notes.getAttribute('href'))!)).text()).toBe(NOTES)

    const transcript = page.getByRole('link', { name: 'Download transcript.jsonl' })
    const consoleLog = page.getByRole('link', { name: 'Download console.log' })
    await expect(transcript).toBeVisible()
    await expect(consoleLog).toBeVisible()
    const t = await page.request.get((await transcript.getAttribute('href'))!)
    expect(t.status()).toBe(200)
    expect(await t.text()).toContain('FAKE-LLM-ANSWER')
    expect((await page.request.get((await consoleLog.getAttribute('href'))!)).status()).toBe(200)

    // a text file previews in place
    await page.getByRole('listitem').filter({ hasText: 'notes.txt' }).getByRole('button', { name: 'Preview' }).click()
    await expect(page.getByText(NOTES)).toBeVisible()
  })

  test('the debug tab shows the full prompts and responses', async ({ page }) => {
    await page.goto(`${state.url}?tab=debug`)
    await expect(page.getByText('Full record')).toBeVisible()
    await expect(page.getByRole('link', { name: /transcript\.jsonl/ })).toBeVisible()
    const call = page.getByRole('row').filter({ hasText: 'gpt-6-luna' }).first()
    await expect(call).toBeVisible()
    await call.click()
    // the prompt carries the brief and the text file; the response is the fake model's answer
    await expect(page.getByText(BRIEF).first()).toBeVisible()
    await expect(page.getByText(/NOTES-FILE-CONTENT/).first()).toBeVisible()
    await expect(page.getByText('FAKE-LLM-ANSWER').first()).toBeVisible()
    for (const view of ['Events', 'Console', 'Spec', 'Callbacks', 'LLM calls']) {
      await page.getByRole('tablist', { name: 'Debug view' }).getByRole('tab', { name: new RegExp(view) }).click()
    }
  })
})

test.describe('attempts, search and lifecycle', () => {
  test('re-running starts a second attempt', async ({ page }) => {
    await page.goto(state.url)
    await statusBadge(page, 'succeeded')
    await page.getByRole('button', { name: 'Re-run' }).click()
    await expect(toast(page, 'Started attempt 2')).toBeVisible()
    await expect(page.getByText('Attempt 2 of 2')).toBeVisible()
    await statusBadge(page, 'succeeded')
    const attempts = page.getByRole('tablist', { name: 'Attempts' })
    await expect(attempts.getByRole('tab')).toHaveCount(2)
    await expect(page.getByRole('main').locator('header')).toContainText(/Attempts\s*2/)
    // the earlier attempt is still there to look at
    await attempts.getByRole('tab', { name: /#1/ }).click()
    await expect(page.getByText('Attempt 1 of 2')).toBeVisible()
  })

  test('the work order list finds it by WO-number and by title, and filters', async ({ page }) => {
    await page.goto('/work-orders')
    await expect(linkRow(page, `WO-${state.number} ${TITLE}`).or(page.getByRole('row').filter({ hasText: TITLE }).first())).toBeVisible()
    const search = page.getByLabel('Search work orders')
    await search.fill(`WO-${state.number}`)
    await expect(page).toHaveURL(new RegExp(`q=WO-${state.number}`))
    const rows = page.getByRole('row').filter({ has: page.getByRole('link') })
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toContainText(TITLE)
    await expect(rows.first()).toContainText('2 attempts')
    await expect(rows.first()).toContainText(LIB.crew)

    await search.fill('no-such-work-order-xyz')
    await expect(page.getByText('No work orders match these filters')).toBeVisible()
    await search.fill('screenshot')
    await expect(rows.first()).toContainText(TITLE)
    await page.getByLabel('Filter by status').selectOption('failed')
    await expect(page.getByText('No work orders match these filters')).toBeVisible()
    await page.getByLabel('Filter by status').selectOption('succeeded')
    await page.getByLabel('Filter by source').selectOption('manual')
    await page.getByLabel('Filter by crew').selectOption({ label: LIB.crew })
    await expect(rows.first()).toContainText(TITLE)
    // the run list links back to the work order
    await page.goto('/runs')
    await expect(page.getByRole('link', { name: 'Work order' }).first()).toBeVisible()
  })

  test('an agent on a model without vision gets file names instead of the image', async ({ page }) => {
    const api = await apiAs('admin')
    const lib = await ensureLibrary()
    const plainAgent = await unwrap<Named>(
      await api.post('/api/agents', { data: { name: 'E2E Plain Analyst', role: 'Plain analyst for {topic}', goal: 'g', backstory: 'b', llm_id: lib.plainLlmId, tools: [], max_iter: 5, allow_delegation: false, verbose: false, area_ids: [lib.areaId] } }),
      201,
    )
    const plainTask = await unwrap<Named>(
      await api.post('/api/tasks', { data: { name: 'E2E Plain Task', description: 'Describe {topic}.', expected_output: 'Text.', agent_id: plainAgent.id, context_task_ids: [], markdown: false, area_ids: [lib.areaId] } }),
      201,
    )
    const plainCrew = await oneTaskCrew(api, plainTask.id)
    const wo = await unwrap<{ id: string; number: number }>(
      await api.post('/api/work-orders', { data: { title: 'Plain model review', brief: 'PLAIN-BRIEF look at the picture', crew_id: plainCrew.id, inputs: { topic: 'wind' }, budget_usd: null } }),
      201,
    )
    const form = await api.post(`/api/work-orders/${wo.id}/attachments`, {
      headers: { 'X-Requested-With': 'crewstudio' }, // multipart uploads must carry this header (CSRF guard)
      multipart: { file: { name: 'plain.png', mimeType: 'image/png', buffer: makePng(32, [10, 120, 200]) } },
    })
    expect(form.status()).toBeLessThan(300)
    await api.dispose()

    const before = llmRequests().length
    await page.goto(`/work-orders/${wo.id}`)
    await page.getByRole('button', { name: 'Submit' }).first().click()
    await statusBadge(page, 'succeeded')
    const seen = llmRequestsSince(before).filter((r) => r.text.includes('PLAIN-BRIEF'))
    expect(seen.length).toBeGreaterThan(0)
    expect(seen.every((r) => r.image_count === 0)).toBe(true)
    expect(seen[0].text).toMatch(/plain\.png \(image\/png\) is in the workspace at/)
  })

  test('a draft can be saved, edited and then submitted', async ({ page }) => {
    await page.goto('/work-orders/new')
    await page.getByLabel('Title').fill('Draft only')
    await chooseCrew(page, LIB.crew)
    await page.getByLabel(/Topic/).fill('wind farms')
    await page.getByRole('button', { name: 'Save draft' }).click()
    await expect(toast(page, /Saved WO-\d+/)).toBeVisible()
    await page.goto('/work-orders')
    const r = page.getByRole('row').filter({ hasText: 'Draft only' })
    await expect(r).toContainText('draft')
    await r.getByRole('link', { name: /Draft only/ }).click()
    await expect(page.getByText('Not submitted yet')).toBeVisible()
    await page.getByRole('button', { name: 'Edit' }).first().click()
    await page.getByLabel('Title').fill('Draft only (edited)')
    await page.getByRole('button', { name: 'Save draft' }).click()
    await expect(toast(page, /Saved WO-\d+/)).toBeVisible()
    // saving returns to the detail page
    await expect(page.getByRole('heading', { name: 'Draft only (edited)' })).toBeVisible()
    await expect(page.getByText('Not submitted yet')).toBeVisible()
    await page.getByRole('button', { name: 'Submit' }).first().click()
    await statusBadge(page, 'succeeded')
    // finished work orders can be deleted
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, /Deleted WO-\d+/)).toBeVisible()
    await expect(page).toHaveURL(/\/work-orders$/)
  })

  test('a single task runs through a crew made from it on the task page', async ({ page }) => {
    const lib = await ensureLibrary()
    await page.goto(`/tasks/${lib.task1Id}`)
    await page.getByRole('button', { name: 'Make a crew' }).click()
    await expect(page).toHaveURL(new RegExp(`/crews/new\\?fromTask=${lib.task1Id}`))
    const name = page.getByLabel('Name', { exact: true })
    await expect(name).toHaveValue(new RegExp(`^${LIB.task1}`))
    const crewName = await name.inputValue()
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created crew')).toBeVisible()

    await page.goto('/work-orders/new')
    await page.getByLabel('Title').fill('One task only')
    await chooseCrew(page, crewName)
    await expect(page.getByLabel(/Topic/)).toHaveValue(LIB.topicDefault)
    await page.getByRole('button', { name: 'Submit' }).click()
    await statusBadge(page, 'succeeded')
    await expect(page.getByRole('main').locator('header')).toContainText(crewName)
  })

  test('a running work order can be cancelled', async ({ page }) => {
    const api = await apiAs('admin')
    const lib = await ensureLibrary()
    const task = await unwrap<Named>(
      await api.post('/api/tasks', { data: { name: 'E2E WO Slow Task', description: 'E2E_SLOW_MARKER for {topic}', expected_output: 'x', agent_id: lib.agentId, context_task_ids: [], markdown: false, area_ids: [lib.areaId] } }),
      201,
    )
    const slowCrew = await oneTaskCrew(api, task.id)
    const wo = await unwrap<{ id: string }>(
      await api.post('/api/work-orders', { data: { title: 'Slow one', brief: '', crew_id: slowCrew.id, inputs: { topic: 'slow' }, budget_usd: null } }),
      201,
    )
    await api.dispose()
    const before = llmRequests().length
    await page.goto(`/work-orders/${wo.id}`)
    await page.getByRole('button', { name: 'Submit' }).first().click()
    await expect.poll(() => llmRequestsSince(before).some((r) => r.text.includes('E2E_SLOW_MARKER')), { timeout: RUN_TIMEOUT, intervals: [500] }).toBe(true)
    await page.getByRole('button', { name: 'Cancel', exact: true }).click()
    await expect(toast(page, 'Cancelling the current attempt')).toBeVisible()
    await statusBadge(page, 'cancelled')
  })
})
