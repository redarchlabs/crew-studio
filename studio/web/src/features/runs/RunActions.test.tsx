import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { crewsApi } from '../../api/endpoints'
import { resumeApi } from '../../api/endpoints.resume'
import { ApiError } from '../../api/client'
import type { ResumeHint, Run } from '../../api/types'
import { makeCrew } from '../../lib/testFixtures'
import { notifyError } from '../../lib/notify'
import { setPermissions } from '../workorders/testUtils'
import { ResumeLinks, RunActions } from './RunActions'
import { resumeLabel } from './ResumeRunModal'
import { makeRun, renderAt } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.resume')
vi.mock('../../lib/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('../../lib/auth', async () => (await import('../workorders/testUtils')).authMock())

const hint: ResumeHint = { start_position: 1, task_name: 'Lesson design', reused_tasks: 1, reused_cost_usd: 0.42 }

function failed(over: Partial<Run> = {}): Run {
  return makeRun({ id: 'run-old', status: 'failed', budget_usd: 5, inputs: { week: 'Oct 12' }, resumable: hint, ...over })
}

beforeEach(() => {
  vi.resetAllMocks()
  setPermissions('crews.run')
})
afterEach(cleanup)

describe('RunActions', () => {
  it('shows nothing for a run that cannot be resumed when the person cannot run crews', () => {
    setPermissions()
    renderAt(<RunActions run={makeRun()} />)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('resumes with the chosen budget and settings, then opens the new run', async () => {
    vi.mocked(resumeApi.resume).mockResolvedValue(makeRun({ id: 'run-new' }))
    const h = renderAt(<RunActions run={failed()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect(screen.getByText(/reuses task 1 \(\$0\.42 when they ran\) from this run at no cost/)).toBeTruthy()
    expect(screen.getByText(/Lesson design runs again from the start/)).toBeTruthy()
    expect(screen.getByText(/this run's budget of \$5\.00/)).toBeTruthy()

    fireEvent.click(screen.getByRole('switch'))
    fireEvent.change(screen.getByLabelText(/Budget \(USD\)/), { target: { value: '2' } })
    fireEvent.click(screen.getAllByRole('button', { name: 'Resume from task 2' })[1])
    await waitFor(() => expect(h.pathname()).toBe('/runs/run-new'))
    expect(resumeApi.resume).toHaveBeenCalledWith('run-old', { budget_usd: 2, use_current: true })
  })

  it('sends only use_current when the budget is left empty', async () => {
    vi.mocked(resumeApi.resume).mockResolvedValue(makeRun({ id: 'run-new' }))
    renderAt(<RunActions run={failed({ resumable: { ...hint, reused_tasks: 3, start_position: 3, reused_cost_usd: 0 } })} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 4' }))
    expect(screen.getByText(/reuses tasks 1–3 from this run/)).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', { name: 'Resume from task 4' })[1])
    await waitFor(() => expect(resumeApi.resume).toHaveBeenCalledWith('run-old', { use_current: false }))
  })

  it('says when the run spent its whole budget', () => {
    const { unmount } = renderAt(<RunActions run={failed({ cost_usd: 5.2 })} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    expect(screen.getByText(/spent \$5\.20 of its \$5\.00 budget/)).toBeTruthy()
    unmount()
    renderAt(<RunActions run={failed({ cost_usd: 1 })} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    expect(screen.queryByText(/of its \$5\.00 budget/)).toBeNull()
  })

  it('says what the failed task had spent and why the rest may fail as it was', () => {
    renderAt(<RunActions run={failed({ resumable: { ...hint, spent_on_task_usd: 0.07, warnings: ['LLM Sol (openai/gpt-6.1-sol): OPENAI_API_KEY is not set'] } })} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    expect(screen.getByText(/it had spent \$0\.07 before it stopped/)).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('OPENAI_API_KEY is not set')
  })

  it('refuses a budget of zero', () => {
    renderAt(<RunActions run={failed()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    fireEvent.change(screen.getByLabelText(/Budget \(USD\)/), { target: { value: '0' } })
    fireEvent.click(screen.getAllByRole('button', { name: 'Resume from task 2' })[1])
    expect(screen.getByText('Budget must be greater than 0')).toBeTruthy()
    expect(resumeApi.resume).not.toHaveBeenCalled()
  })

  it("shows the server's reasons when it refuses, and closes on cancel", async () => {
    vi.mocked(resumeApi.resume).mockRejectedValue(new ApiError(422, {
      code: 'validation_failed', message: 'This cannot resume with the crew\'s current settings.',
      details: ["The crew's first 1 tasks are no longer the ones this run finished"],
    }))
    renderAt(<RunActions run={failed()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Resume from task 2' })[1])
    expect(await screen.findByText(/no longer the ones this run finished/)).toBeTruthy()
    expect(notifyError).toHaveBeenCalledWith('Could not resume the run', expect.any(ApiError))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('reports a refusal without details', async () => {
    vi.mocked(resumeApi.resume).mockRejectedValue(new Error('offline'))
    renderAt(<RunActions run={failed()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Resume from task 2' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Resume from task 2' })[1])
    await waitFor(() => expect(notifyError).toHaveBeenCalled())
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('runs the crew again with the same inputs and budget', async () => {
    vi.mocked(crewsApi.get).mockResolvedValue(makeCrew({ id: 'crew-1', name: 'Lessons', budget_usd: 1,
      inputs: [{ key: 'week', label: 'Week', default: '', required: true }] }))
    renderAt(<RunActions run={failed({ resumable: undefined })} />)
    expect(screen.queryByText(/Resume from/)).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Run again' }))
    expect(await screen.findByText('Run Lessons')).toBeTruthy()
    expect(crewsApi.get).toHaveBeenCalledWith('crew-1')
    expect((screen.getByLabelText(/Week/) as HTMLTextAreaElement).value).toBe('Oct 12')
    expect((screen.getByLabelText(/Budget override/) as HTMLInputElement).value).toBe('5')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('says when the crew cannot be loaded', async () => {
    vi.mocked(crewsApi.get).mockRejectedValue(new Error('gone'))
    renderAt(<RunActions run={failed()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Run again' }))
    await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not load the crew', expect.any(Error)))
  })

  it.each<[string, Partial<Run>]>([
    ['work order attempts (re-run from the work order)', { work_order_id: 'wo-1' }],
    ['deleted crews', { crew_id: null }],
    ['chat turns', { kind: 'chat' }],
    ['hand-offs', { kind: 'handoff' }],
  ])('offers no Run again for %s', (_name, over) => {
    renderAt(<RunActions run={failed({ resumable: undefined, ...over })} />)
    expect(screen.queryByText('Run again')).toBeNull()
  })

  it('labels the resume by task number', () => {
    expect(resumeLabel({ ...hint, start_position: 0 })).toBe('Resume from task 1')
  })
})

describe('ResumeLinks', () => {
  it('shows nothing for a run with no resumes', () => {
    renderAt(<ResumeLinks run={makeRun()} />)
    expect(screen.queryByText(/Resumed/)).toBeNull()
  })

  it('links to the run this one resumed and says what it reused', () => {
    const { unmount } = renderAt(<ResumeLinks run={makeRun({ resumed_from_run_id: 'abcdef1234567', resume_position: 1 })} />)
    expect(screen.getByText('abcdef12').closest('a')?.getAttribute('href')).toBe('/runs/abcdef1234567')
    expect(screen.getByText(/reused task 1/)).toBeTruthy()
    unmount()
    renderAt(<ResumeLinks run={makeRun({ resumed_from_run_id: 'abcdef1234567', resume_position: 3 })} />)
    expect(screen.getByText(/reused tasks 1–3/)).toBeTruthy()
  })

  it('does not count reused tasks it does not know', () => {
    renderAt(<ResumeLinks run={makeRun({ resumed_from_run_id: 'abcdef1234567' })} />)
    expect(screen.queryByText(/reused/)).toBeNull()
  })

  it('links to the runs that resumed this one', () => {
    renderAt(<ResumeLinks run={makeRun({ resumed_by: ['11111111aaaa', '22222222bbbb'] })} />)
    expect(screen.getByText('11111111').closest('a')?.getAttribute('href')).toBe('/runs/11111111aaaa')
    expect(screen.getByText('22222222').closest('a')?.getAttribute('href')).toBe('/runs/22222222bbbb')
  })
})
