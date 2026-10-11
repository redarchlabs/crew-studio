import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { ApiError } from '../../api/client'
import { crewsApi, tasksApi, workOrdersApi } from '../../api/endpoints'
import type { WorkOrder } from '../../api/types'
import { makeCrew, makeTask } from '../../lib/testFixtures'
import { initialTimeline, type TimelineState } from '../runs/timeline/timelineReducer'
import type { Attempt } from './detail/useAttempt'
import { makeAttachment, makeJournal, makeRun, makeWorkOrder, renderRoute, setPermissions } from './testUtils'
import WorkOrderPage from './WorkOrderPage'
import { resumeApi } from '../../api/endpoints.resume'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock())
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock())
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))
vi.mock('../../api/endpoints.resume', () => ({ resumeApi: { resume: vi.fn() } }))

const h = vi.hoisted(() => ({ attempt: { current: null as unknown as Attempt }, useAttempt: vi.fn() }))
vi.mock('./detail/useAttempt', () => ({
  useAttempt: (...args: unknown[]) => {
    h.useAttempt(...args)
    return h.attempt.current
  },
}))
vi.mock('./detail/WorkflowTab', () => ({ WorkflowTab: (p: { status: string | null; canViewRuns: boolean }) => <div>WorkflowTab status={String(p.status)} canView={String(p.canViewRuns)}</div> }))
vi.mock('./detail/DebugTab', () => ({ DebugTab: (p: { run: { id: string } }) => <div>DebugTab {p.run.id}</div> }))
vi.mock('./detail/ConversationTab', () => ({ ConversationTab: (p: { workOrderId: string; active: boolean }) => <div>ConversationTab {p.workOrderId} active={String(p.active)}</div> }))
vi.mock('../runs/tabs/FilesTab', () => ({ FilesTab: (p: { inputs: unknown[]; runId: string | null }) => <div>FilesTab inputs={p.inputs.length} run={String(p.runId)}</div> }))
vi.mock('../delivery/ReportsTab', () => ({ ReportsTab: (p: { workOrderId: string; canRetry: boolean }) => <div>ReportsTab {p.workOrderId} retry={String(p.canRetry)}</div> }))
vi.mock('../runs/tabs/journal/JournalTab', () => ({ JournalTab: (p: { live: boolean }) => <div>JournalTab live={String(p.live)}</div> }))

const api = vi.mocked(workOrdersApi)

const asyncState = <T,>(data?: T) => ({ data, error: undefined, loading: false, reload: vi.fn(), setData: vi.fn() })

function makeAttempt(over: Partial<Attempt> = {}): Attempt {
  return {
    run: null,
    status: null,
    live: false,
    timeline: { ...initialTimeline } as TimelineState,
    stream: 'idle',
    journal: asyncState(undefined) as Attempt['journal'],
    interactions: asyncState(undefined) as Attempt['interactions'],
    llmCalls: { calls: undefined, loading: false, error: undefined, load: vi.fn() },
    reloadRun: vi.fn(),
    ...over,
  }
}

const run1 = makeRun({ id: 'r1', status: 'succeeded' })
const finished = (over: Partial<WorkOrder> = {}) => makeWorkOrder({ id: 'wo-1', status: 'succeeded', current_run_id: 'r1', run_count: 1, runs: [run1], ...over })
const draft = (over: Partial<WorkOrder> = {}) => makeWorkOrder({ id: 'wo-1', status: 'draft', ...over })

function renderPage(wo: WorkOrder, url = '/work-orders/wo-1') {
  api.get.mockResolvedValue(wo)
  return renderRoute(<WorkOrderPage />, { path: '/work-orders/:id', url })
}

beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset()
  h.useAttempt.mockReset()
  vi.mocked(toast.error).mockReset()
  vi.mocked(toast.success).mockReset()
  vi.mocked(tasksApi.list).mockReset().mockResolvedValue([makeTask({ id: 'task-1', name: 'Research' })])
  vi.mocked(crewsApi.list).mockReset().mockResolvedValue([makeCrew({ id: 'crew-1', name: 'Research crew', task_ids: ['task-1'] })])
  h.attempt.current = makeAttempt({ run: run1, status: 'succeeded' })
  setPermissions('work_orders.create', 'work_orders.delete', 'work_orders.cancel', 'runs.view')
})
afterEach(cleanup)

const tab = (name: RegExp | string) => screen.getByRole('tab', { name })

describe('loading', () => {
  it('shows a loading state then the work order', async () => {
    renderPage(finished())
    expect(screen.getByText('Loading work order…')).toBeTruthy()
    expect(await screen.findByRole('heading', { name: 'Quarterly review' })).toBeTruthy()
    expect(api.get).toHaveBeenCalledWith('wo-1', expect.anything())
  })

  it('shows an error with retry and a way back', async () => {
    api.get.mockImplementationOnce(() => Promise.reject(new ApiError(404, { code: 'not_found', message: 'work order not found' })))
    api.get.mockResolvedValue(finished())
    renderRoute(<WorkOrderPage />, { path: '/work-orders/:id', url: '/work-orders/wo-1' })
    expect(await screen.findByText('work order not found')).toBeTruthy()
    expect(screen.getByText('Could not load this work order')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to work orders' }).getAttribute('href')).toBe('/work-orders')
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByRole('heading', { name: 'Quarterly review' })).toBeTruthy()
  })
})

describe('draft', () => {
  it('explains it is not submitted and shows the brief', async () => {
    renderPage(draft({ brief: 'Draft brief text' }))
    expect(await screen.findByText('Not submitted yet')).toBeTruthy()
    expect(screen.getByText('Draft brief text')).toBeTruthy()
    expect(screen.queryAllByRole('tab')).toHaveLength(0)
  })

  it('submits from the empty state, switching to the workflow tab on the new run', async () => {
    const run = makeRun({ id: 'new-run', status: 'queued' })
    api.submit.mockResolvedValue(run)
    const view = renderPage(draft())
    await screen.findByText('Not submitted yet')
    // two Submit buttons: header and empty state
    fireEvent.click(screen.getAllByRole('button', { name: /Submit/ })[1])
    await waitFor(() => expect(api.submit).toHaveBeenCalledWith('wo-1'))
    await waitFor(() => expect(view.location().search).toContain('run=new-run'))
    expect(view.location().search).toContain('tab=workflow')
    expect(toast.success).toHaveBeenCalledWith('WO-7 submitted')
  })

  it('hides Edit/Submit in the empty state without permission to create', async () => {
    setPermissions('runs.view')
    renderPage(draft())
    await screen.findByText('Not submitted yet')
    expect(screen.queryByRole('button', { name: /Submit/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Edit/ })).toBeNull()
  })

  it('treats a non-draft work order without runs the same as a draft', async () => {
    renderPage(makeWorkOrder({ status: 'queued', current_run_id: null, runs: [] }))
    expect(await screen.findByText('Not submitted yet')).toBeTruthy()
  })
})

describe('tabs', () => {
  it('opens on Brief and lists all seven tabs', async () => {
    renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual(['Brief', 'Conversation', 'Workflow', 'Journal', 'Files', 'Reports', 'Debug'])
    expect(tab(/^Brief/).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByText('Review the')).toBeTruthy()
  })

  it('hides Conversation from people who can neither chat nor revise, even by URL', async () => {
    setPermissions('runs.view')
    renderPage(finished(), '/work-orders/wo-1?tab=conversation')
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).not.toContain('Conversation')
    expect(tab(/^Brief/).getAttribute('aria-selected')).toBe('true')
    expect(screen.queryByText(/ConversationTab/)).toBeNull()
  })

  it.each([
    ['Conversation', /ConversationTab wo-1 active=false/, 'conversation'],
    ['Workflow', /WorkflowTab status=succeeded canView=true/, 'workflow'],
    ['Journal', /JournalTab live=false/, 'journal'],
    ['Files', /FilesTab inputs=0 run=r1/, 'files'],
    ['Reports', /ReportsTab wo-/, 'reports'],
    ['Debug', /DebugTab r1/, 'debug'],
  ])('switches to %s and records it in the URL', async (label, content, key) => {
    const view = renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(tab(new RegExp(`^${label}`)))
    expect(await screen.findByText(content)).toBeTruthy()
    expect(view.location().search).toBe(`?tab=${key}`)
  })

  it('honours ?tab= on load', async () => {
    renderPage(finished(), '/work-orders/wo-1?tab=journal')
    expect(await screen.findByText(/JournalTab/)).toBeTruthy()
  })

  it('ignores an unknown tab name', async () => {
    renderPage(finished(), '/work-orders/wo-1?tab=bogus')
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(tab(/^Brief/).getAttribute('aria-selected')).toBe('true')
  })

  it('hides the Debug tab without run permission, even when asked for', async () => {
    setPermissions('work_orders.create')
    renderPage(finished(), '/work-orders/wo-1?tab=debug')
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.queryByRole('tab', { name: /Debug/ })).toBeNull()
    expect(tab(/^Brief/).getAttribute('aria-selected')).toBe('true')
  })

  it('does not pass the run id to the Files tab without run permission', async () => {
    setPermissions('work_orders.create')
    renderPage(finished(), '/work-orders/wo-1?tab=files')
    expect(await screen.findByText('FilesTab inputs=0 run=null')).toBeTruthy()
  })

  it('does not render the debug tab content before a run is loaded', async () => {
    h.attempt.current = makeAttempt({ run: null })
    renderPage(finished(), '/work-orders/wo-1?tab=debug')
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.queryByText(/DebugTab/)).toBeNull()
  })

  it('badges the Brief tab with the file count and Journal with the entry count', async () => {
    h.attempt.current = makeAttempt({ run: run1, status: 'succeeded', journal: asyncState(makeJournal({ entries: [{ seq: 1 } as never, { seq: 2 } as never] })) as Attempt['journal'] })
    renderPage(finished({ attachments: [makeAttachment(), makeAttachment({ id: 'b', filename: 'b.txt' })] }))
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(tab(/^Brief/).textContent).toContain('2')
    expect(tab(/^Journal/).textContent).toContain('2')
    expect(tab(/^Workflow/).textContent).toBe('Workflow')
  })
})

describe('attempts', () => {
  const run2 = makeRun({ id: 'r2', status: 'failed' })

  it('selects the current attempt by default and passes it to useAttempt', async () => {
    renderPage(finished({ runs: [run2, run1], run_count: 2, current_run_id: 'r2' }))
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(h.useAttempt.mock.calls.at(-1)![1]).toBe('r2')
    expect(screen.getByText('Attempt 2 of 2')).toBeTruthy()
  })

  it('falls back to the newest run when there is no current one', async () => {
    renderPage(finished({ runs: [run2, run1], current_run_id: null }))
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(h.useAttempt.mock.calls.at(-1)![1]).toBe('r2')
  })

  it('honours ?run= when it is one of the runs, otherwise ignores it', async () => {
    const { unmount } = renderPage(finished({ runs: [run2, run1], run_count: 2, current_run_id: 'r2' }), '/work-orders/wo-1?run=r1')
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(h.useAttempt.mock.calls.at(-1)![1]).toBe('r1')
    unmount()
    renderPage(finished({ runs: [run2, run1], run_count: 2, current_run_id: 'r2' }), '/work-orders/wo-1?run=nope')
    await waitFor(() => expect(h.useAttempt.mock.calls.at(-1)![1]).toBe('r2'))
  })

  it('shows the attempt selector only with several attempts, and switching updates the URL', async () => {
    const view = renderPage(finished({ runs: [run2, run1], run_count: 2, current_run_id: 'r2' }))
    await screen.findByText('Attempt 2 of 2')
    fireEvent.click(screen.getAllByRole('tab', { name: /#1/ })[0])
    await waitFor(() => expect(view.location().search).toBe('?run=r1'))
    expect(h.useAttempt.mock.calls.at(-1)![1]).toBe('r1')
  })

  it('hides the selector with a single attempt', async () => {
    renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.queryByText(/Attempt 1 of/)).toBeNull()
  })

  it('uses the live attempt status for the current attempt but the stored one for older attempts', async () => {
    h.attempt.current = makeAttempt({ run: run2, status: 'running', live: true })
    const { unmount } = renderPage(finished({ status: 'queued', runs: [run2, run1], run_count: 2, current_run_id: 'r2' }))
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.getAllByText('running').length).toBeGreaterThan(0)
    unmount()
    renderPage(finished({ status: 'queued', runs: [run2, run1], run_count: 2, current_run_id: 'r2' }), '/work-orders/wo-1?run=r1')
    await screen.findByRole('heading', { name: 'Quarterly review' })
    expect(screen.getAllByText('queued').length).toBeGreaterThan(0)
  })

  it('shows live spend from the stream and the stored cost for finished attempts', async () => {
    h.attempt.current = makeAttempt({ run: makeRun({ id: 'r1', status: 'running', cost_usd: 0.1, finished_at: null }), status: 'running', live: true, timeline: { ...initialTimeline, spentUsd: 0.77 } })
    renderPage(finished({ status: 'running', runs: [makeRun({ id: 'r1', status: 'running' })] }))
    expect(await screen.findByText('$0.77')).toBeTruthy()
  })

  it('falls back to the run cost while live with no stream figure', async () => {
    h.attempt.current = makeAttempt({ run: makeRun({ id: 'r1', status: 'running', cost_usd: 0.33, finished_at: null }), status: 'running', live: true })
    renderPage(finished({ status: 'running' }))
    expect(await screen.findByText('$0.33')).toBeTruthy()
  })

  it('prefers stored cost over stream spend once finished', async () => {
    h.attempt.current = makeAttempt({ run: makeRun({ id: 'r1', cost_usd: 1.25 }), status: 'succeeded', live: false, timeline: { ...initialTimeline, spentUsd: 9 } })
    renderPage(finished())
    expect(await screen.findByText('$1.25')).toBeTruthy()
  })
})

describe('actions', () => {
  it('re-runs a finished work order and selects the new attempt', async () => {
    const newRun = makeRun({ id: 'r2', status: 'queued' })
    api.submit.mockResolvedValue(newRun)
    const view = renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Re-run/ }))
    await waitFor(() => expect(view.location().search).toBe('?run=r2'))
    expect(toast.success).toHaveBeenCalledWith('Started attempt 2')
    // work order refetched after starting
    await waitFor(() => expect(api.get.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('resumes the latest attempt from where it failed and selects the new attempt', async () => {
    const hint = { start_position: 1, task_name: 'Write', reused_tasks: 1, reused_cost_usd: 0.2 }
    const failedRun = makeRun({ id: 'r1', status: 'failed', resumable: hint })
    h.attempt.current = makeAttempt({ run: failedRun, status: 'failed' })
    vi.mocked(resumeApi.resume).mockResolvedValue(makeRun({ id: 'r2', status: 'queued' }))
    const view = renderPage(finished({ status: 'failed', runs: [failedRun] }))
    fireEvent.click(await screen.findByRole('button', { name: 'Resume from task 2' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Resume from task 2' })[1])
    await waitFor(() => expect(view.location().search).toBe('?run=r2'))
    expect(resumeApi.resume).toHaveBeenCalledWith('r1', { use_current: false })
    expect(toast.success).toHaveBeenCalledWith('Started attempt 2, resuming from task 2')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('closes the resume dialog without resuming', async () => {
    const failedRun = makeRun({ id: 'r1', status: 'failed', resumable: { start_position: 1, task_name: 'Write', reused_tasks: 1, reused_cost_usd: 0 } })
    h.attempt.current = makeAttempt({ run: failedRun, status: 'failed' })
    renderPage(finished({ status: 'failed', runs: [failedRun] }))
    fireEvent.click(await screen.findByRole('button', { name: 'Resume from task 2' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(resumeApi.resume).not.toHaveBeenCalled()
  })

  it('reports a failed submit and re-enables the button', async () => {
    api.submit.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'invalid', details: ['inputs: topic is required'] }))
    renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Re-run/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not start the work order', { description: 'inputs: topic is required' }))
    await waitFor(() => expect((screen.getByRole('button', { name: /Re-run/ }) as HTMLButtonElement).disabled).toBe(false))
  })

  it('cancels an active attempt, then reloads the attempt and work order', async () => {
    api.cancel.mockResolvedValue(makeRun())
    const running = makeRun({ id: 'r1', status: 'running', finished_at: null })
    const reloadRun = vi.fn()
    h.attempt.current = makeAttempt({ run: running, status: 'running', live: true, reloadRun })
    renderPage(finished({ status: 'running', runs: [running] }))
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Cancel/ }))
    await waitFor(() => expect(api.cancel).toHaveBeenCalledWith('wo-1'))
    await waitFor(() => expect(reloadRun).toHaveBeenCalled())
    expect(toast.success).toHaveBeenCalledWith('Cancelling the current attempt')
    await waitFor(() => expect(api.get.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('reports a failed cancel', async () => {
    api.cancel.mockRejectedValue(new Error('already finished'))
    const running = makeRun({ id: 'r1', status: 'running', finished_at: null })
    h.attempt.current = makeAttempt({ run: running, status: 'running', live: true })
    renderPage(finished({ status: 'running', runs: [running] }))
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Cancel/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not cancel the work order', { description: 'already finished' }))
  })

  it('deletes after confirmation and returns to the list', async () => {
    api.remove.mockResolvedValue(undefined)
    const view = renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Delete/ }))
    expect(api.remove).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.remove).toHaveBeenCalledWith('wo-1'))
    await waitFor(() => expect(view.location().pathname).toBe('/work-orders'))
    expect(view.location().state).toEqual({ skipUnsavedGuard: true })
    expect(toast.success).toHaveBeenCalledWith('Deleted WO-7')
  })

  it('reports a failed delete and stays on the page', async () => {
    api.remove.mockRejectedValue(new Error('in use'))
    const view = renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Delete/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete the work order', { description: 'in use' }))
    expect(view.location().pathname).toBe('/work-orders/wo-1')
  })
})

describe('editing', () => {
  it('opens the composer, and cancel returns to the detail view', async () => {
    renderPage(finished({ title: 'Original title' }))
    await screen.findByRole('heading', { name: 'Original title' })
    fireEvent.click(screen.getByRole('button', { name: /Edit/ }))
    expect(await screen.findByRole('heading', { name: 'Edit work order' })).toBeTruthy()
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Original title')
    expect(screen.getByRole('button', { name: /Research crew/ })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('heading', { name: 'Original title' })).toBeTruthy()
  })

  it('saving returns to the detail view and refetches', async () => {
    const wo = finished({ crew_id: 'crew-1', title: 'Original title' })
    api.update.mockResolvedValue({ ...wo, title: 'Renamed' })
    renderPage(wo)
    await screen.findByRole('heading', { name: 'Original title' })
    fireEvent.click(screen.getByRole('button', { name: /Edit/ }))
    await screen.findByRole('heading', { name: 'Edit work order' })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Renamed' } })
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }))
    await waitFor(() => expect(api.update).toHaveBeenCalledWith('wo-1', expect.objectContaining({ title: 'Renamed' })))
    expect(await screen.findByRole('heading', { name: /Original title|Quarterly review/ })).toBeTruthy()
    await waitFor(() => expect(api.get.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('save & re-run starts the new attempt', async () => {
    const wo = finished({ crew_id: 'crew-1' })
    api.submit.mockResolvedValue(makeRun({ id: 'r9', status: 'queued' }))
    const view = renderPage(wo)
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Edit/ }))
    await screen.findByRole('heading', { name: 'Edit work order' })
    fireEvent.click(screen.getByRole('button', { name: /Save & re-run/ }))
    await waitFor(() => expect(view.location().search).toBe('?run=r9'))
  })

  it('shows the catalog error inside the editor', async () => {
    vi.mocked(tasksApi.list).mockImplementation(() => Promise.reject(new Error('catalog gone')))
    renderPage(finished())
    await screen.findByRole('heading', { name: 'Quarterly review' })
    fireEvent.click(screen.getByRole('button', { name: /Edit/ }))
    expect(await screen.findByText('catalog gone')).toBeTruthy()
  })
})
