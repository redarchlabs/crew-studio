import { expect, test } from '@playwright/test'
import { readState } from '../harness/state.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'
import { llmRequestsSince, llmRequests } from '../support/llmlog.ts'
import { uid } from '../support/ui.ts'

// Resuming a failed run (POST /runs/{id}/resume): the provider runs out of credits on the second task (the fake LLM
// answers 429 insufficient_quota to E2E_QUOTA_MARKER while its quota is exhausted). After "adding credits", Resume from
// task 2 starts a run that reuses the first task's output without asking the model again, and finishes.

const RUN_TIMEOUT = 180_000
const FIRST = 'E2E resume first task: list three facts about tides.'
const SECOND = 'E2E_QUOTA_MARKER: write one sentence from the facts.'

type Run = {
  id: string
  status: string
  resumed_from_run_id?: string
  resumable?: { start_position: number; task_name: string }
  task_outputs?: { position: number; raw: string; reused_from_run_id?: string }[]
}

async function quota(state: 'exhaust' | 'restore'): Promise<void> {
  const res = await fetch(`${readState().fakeLlmURL}/__quota/${state}`, { method: 'POST' })
  expect(res.ok).toBe(true)
}

async function waitForStatus(id: string, want: string): Promise<Run> {
  const api = await apiAs('admin')
  try {
    let run: Run | undefined
    await expect.poll(async () => {
      run = await unwrap<Run>(await api.get(`/api/runs/${id}`))
      return run.status
    }, { timeout: RUN_TIMEOUT, intervals: [1000] }).toBe(want)
    return run!
  } finally {
    await api.dispose()
  }
}

test.describe('resuming a failed run', () => {
  test.afterAll(async () => quota('restore'))

  test('reuses the finished task and runs only the rest', async ({ page }) => {
    test.setTimeout(2 * RUN_TIMEOUT + 60_000)
    const lib = await ensureLibrary()
    const api = await apiAs('admin')
    const name = uid('E2E Resume Crew')
    const task = async (taskName: string, description: string, context: string[]) =>
      (await unwrap<{ id: string }>(await api.post('/api/tasks', { data: {
        name: taskName, description, expected_output: 'Short text.', agent_id: lib.agentId, context_task_ids: context,
        markdown: false, area_ids: [lib.areaId],
      } }), 201)).id
    const first = await task(`${name} research`, FIRST, [])
    const second = await task(`${name} write`, SECOND, [first])
    const crew = await unwrap<{ id: string }>(await api.post('/api/crews', { data: {
      name, description: '', process: 'sequential', manager_llm_id: null, agent_ids: [lib.agentId], task_ids: [first, second],
      inputs: [{ key: 'topic', label: 'Topic', default: 'tides', required: true }], budget_usd: 1, area_ids: [lib.areaId],
    } }), 201)

    // Out of credits on the second task
    await quota('exhaust')
    const started = await unwrap<Run>(await api.post(`/api/crews/${crew.id}/runs`, { data: {} }), 201)
    const failed = await waitForStatus(started.id, 'failed')
    expect(failed.task_outputs?.map((o) => o.position)).toEqual([0])
    expect(failed.resumable).toMatchObject({ start_position: 1, task_name: `${name} write` })
    await api.dispose()

    // Credits added: resume from the run's page
    await quota('restore')
    const before = llmRequests().length
    await page.goto(`/runs/${started.id}`)
    await page.getByRole('button', { name: 'Resume from task 2' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText(`${name} write runs again from the start`)).toBeVisible()
    await dialog.getByRole('button', { name: 'Resume from task 2' }).click()
    await expect(page).not.toHaveURL(new RegExp(`/runs/${started.id}`))
    const resumedId = page.url().match(/\/runs\/([0-9a-f-]{36})/)![1]
    await expect(page.getByText('Resumed from run')).toBeVisible()

    const resumed = await waitForStatus(resumedId, 'succeeded')
    expect(resumed.resumed_from_run_id).toBe(started.id)
    expect(resumed.task_outputs?.[0]).toMatchObject({ position: 0, reused_from_run_id: started.id, raw: failed.task_outputs![0].raw })
    expect(resumed.task_outputs?.[1]?.reused_from_run_id).toBeUndefined()

    // The model was asked for the second task only, with the first task's output (which the fake LLM echoes the task
    // line into) as its context
    const sent = llmRequestsSince(before).map((r) => r.text)
    expect(sent.some((t) => t.includes(`Current Task: ${SECOND}`))).toBe(true)
    expect(sent.some((t) => t.includes(`Current Task: ${FIRST}`))).toBe(false)
    expect(sent.some((t) => t.includes(SECOND) && t.includes(failed.task_outputs![0].raw))).toBe(true)

    await page.reload()
    await page.getByRole('tab', { name: /Outputs/ }).click()
    await expect(page.getByText('reused from run')).toBeVisible()
    await page.goto(`/runs/${started.id}`)
    await expect(page.getByText('Resumed as')).toBeVisible()
  })
})
