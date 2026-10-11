import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { runsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import type { Interactions, Run } from '../../api/types'
import { notifyError, notifySuccess } from '../../lib/notify'
import { setPermissions } from '../workorders/testUtils'
import RunDetailPage from './RunDetailPage'
import { FakeEventSource, installFakeEventSource, makeEntry, makeEvent, makeJournal, makeRun, makeSpec, renderAt } from './testUtils'

vi.mock('../../api/endpoints', () => ({
  runsApi: {
    get: vi.fn(), cancel: vi.fn(), compact: vi.fn(), interactions: vi.fn(), journal: vi.fn(), events: vi.fn(), llmCalls: vi.fn(), artifacts: vi.fn(), deliveries: vi.fn(),
    streamUrl: (id: string, after: number) => `/api/runs/${id}/stream?after=${after}`,
  },
  attachmentsApi: { contentUrl: (id: string) => `/api/attachments/${id}/content` },
}))
vi.mock('../../lib/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('../../lib/auth', async () => (await import('../workorders/testUtils')).authMock())
vi.mock('../../components/Markdown', () => ({ Markdown: ({ children }: { children: string }) => <div>{children}</div> }))
vi.mock('../../components/graph/FlowCanvas', () => ({
  FlowCanvas: ({ nodes, onNodeClick }: { nodes: { id: string; data: { label: string } }[]; onNodeClick: (e: unknown, n: unknown) => void }) => (
    <div data-testid="canvas">{nodes.map((n) => <button key={n.id} onClick={(e) => onNodeClick(e, n)}>{n.data.label}</button>)}</div>
  ),
}))

const api = vi.mocked(runsApi)
const stats = { llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, duration_ms: 0 }
const graph: Interactions = {
  nodes: [{ id: 'crew:c', kind: 'crew', label: 'The crew', stats }, { id: 'task:Research', kind: 'task', label: 'Research task', stats }],
  edges: [{ id: 'e', source: 'crew:c', target: 'task:Research', kind: 'runs', count: 1, cost_usd: 0 }],
}
const storedEvents = [
  makeEvent(1, 'crew_started', { crew_name: 'Research crew' }),
  makeEvent(2, 'task_started', { task_name: 'Research', agent_role: 'Analyst' }),
  makeEvent(3, 'log', { line: 'console line one' }),
  makeEvent(4, 'task_completed', { task_name: 'Research', agent_role: 'Analyst', output: 'result' }),
]

const ID = 'run-12345678-abcd'
const show = (route = `/runs/${ID}`) => renderAt(<RunDetailPage />, { route, path: '/runs/:id' })

beforeEach(() => {
  installFakeEventSource()
  api.get.mockResolvedValue(makeRun({ spec: makeSpec() }))
  api.interactions.mockResolvedValue(graph)
  api.journal.mockResolvedValue(makeJournal({ entries: [makeEntry({ seq: 1, title: 'Journal entry one', body: 'b' })] }))
  api.events.mockResolvedValue(storedEvents)
  api.llmCalls.mockResolvedValue([])
  api.artifacts.mockResolvedValue([])
  api.deliveries.mockResolvedValue([])
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

const tab = (name: string) => screen.getByRole('tab', { name: new RegExp(`^${name}`) })

describe('RunDetailPage loading', () => {
  it('shows a loading state, then the run', async () => {
    show()
    expect(screen.getByText('Loading run…')).toBeTruthy()
    expect(await screen.findByText('Research crew')).toBeTruthy()
    expect(api.get).toHaveBeenCalledWith(ID, expect.anything())
  })

  it('shows an error with a retry and a link back', async () => {
    api.get.mockRejectedValueOnce(new ApiError(404, { code: 'not_found', message: 'run not found' }))
    show()
    expect(await screen.findByText('run not found')).toBeTruthy()
    expect(screen.getByText('not_found')).toBeTruthy()
    expect(screen.getByText('Back to runs').getAttribute('href')).toBe('/runs')
    fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('Research crew')).toBeTruthy()
  })
})

describe('RunDetailPage tabs (finished run)', () => {
  it('defaults to the interactions tab and focuses a node into a timeline panel', async () => {
    show()
    expect(await screen.findByTestId('canvas')).toBeTruthy()
    expect(tab('Interactions').getAttribute('aria-selected')).toBe('true')
    expect(screen.queryByText('Timeline for selection')).toBeNull()
    fireEvent.click(screen.getByText('Research task'))
    expect(await screen.findByText('Timeline for selection')).toBeTruthy()
    const panel = screen.getByText('Timeline for selection').closest('section')!
    await within(panel).findByText('Task completed by', { exact: false })
    expect(within(panel).queryByText('Crew start')).toBeNull()
    fireEvent.click(within(panel).getByLabelText('Clear focus'))
    expect(screen.queryByText('Timeline for selection')).toBeNull()
  })

  it('"Open in timeline" switches the tab', async () => {
    const h = show()
    fireEvent.click(await screen.findByText('Research task'))
    fireEvent.click(await screen.findByText('Open in timeline'))
    expect(h.search()).toBe('?tab=timeline')
    expect(tab('Timeline').getAttribute('aria-selected')).toBe('true')
  })

  it('falls back to interactions for an unknown tab key', async () => {
    show(`/runs/${ID}?tab=bogus`)
    expect(await screen.findByTestId('canvas')).toBeTruthy()
  })

  it('shows tab badges for interactions, timeline events (excluding logs), journal and logs', async () => {
    show()
    await screen.findByTestId('canvas')
    await waitFor(() => expect(tab('Interactions').textContent).toContain('2'))
    await waitFor(() => expect(tab('Timeline').textContent).toBe('Timeline3'))
    await waitFor(() => expect(tab('Journal').textContent).toBe('Journal1'))
    expect(tab('Logs').textContent).toBe('Logs1')
    expect(tab('Outputs').textContent).toBe('Outputs')
  })

  it('switches to each tab via the URL-backed tab control', async () => {
    const h = show()
    await screen.findByTestId('canvas')
    fireEvent.click(tab('Timeline'))
    expect(h.search()).toBe('?tab=timeline')
    expect(await screen.findByText('Crew start')).toBeTruthy()
    fireEvent.click(tab('Journal'))
    expect(await screen.findByText('Journal entry one')).toBeTruthy()
    fireEvent.click(tab('LLM calls'))
    expect(await screen.findByText('No LLM calls recorded')).toBeTruthy()
    fireEvent.click(tab('Outputs'))
    // no stored task outputs yet: falls back to the task_completed event
    expect(screen.getByText('1. Research')).toBeTruthy()
    fireEvent.click(tab('Files'))
    expect(await screen.findByText('The agents wrote no files')).toBeTruthy()
    fireEvent.click(tab('Logs'))
    expect(screen.getByText('console line one')).toBeTruthy()
    fireEvent.click(tab('Callbacks'))
    expect(await screen.findByText('No callbacks')).toBeTruthy()
    fireEvent.click(tab('Spec'))
    expect(screen.getByText(/Frozen when the run started/)).toBeTruthy()
  })

  it('timeline tab shows filters and hides categories', async () => {
    show(`/runs/${ID}?tab=timeline`)
    await screen.findByText('Crew start')
    fireEvent.click(screen.getByText('crew'))
    expect(screen.queryByText('Crew start')).toBeNull()
  })

  it('timeline tab shows a loader before any event arrives', async () => {
    api.events.mockReturnValue(new Promise(() => {}))
    show(`/runs/${ID}?tab=timeline`)
    expect(await screen.findByText('Loading events…')).toBeTruthy()
  })

  it('outputs tab shows run output and task outputs', async () => {
    api.get.mockResolvedValue(makeRun({ output: 'FINAL TEXT', task_outputs: [{ position: 0, task_name: 'T', agent_role: 'A', raw: 'task text' }] }))
    show(`/runs/${ID}?tab=outputs`)
    expect(await screen.findByText('FINAL TEXT')).toBeTruthy()
    expect(screen.getByText('task text')).toBeTruthy()
    expect(tab('Outputs').textContent).toBe('Outputs1')
  })

  it('files tab lists work order input files from the spec', async () => {
    const spec = makeSpec({ work_order: { id: 'wo', number: 3, title: 'T', brief: '', attachments: [{ id: 'a1', filename: 'brief.pdf', content_type: 'application/pdf', size_bytes: 100, path: '' }] } })
    api.get.mockResolvedValue(makeRun({ spec }))
    show(`/runs/${ID}?tab=files`)
    expect(await screen.findByText('brief.pdf')).toBeTruthy()
  })

  it('does not stream for a finished run and loads stored events instead', async () => {
    show()
    await screen.findByTestId('canvas')
    await waitFor(() => expect(api.events).toHaveBeenCalled())
    expect(FakeEventSource.instances).toHaveLength(0)
  })

  it('links back to the work order when the run belongs to one', async () => {
    api.get.mockResolvedValue(makeRun({ work_order_id: 'wo-1', spec: makeSpec({ work_order: { id: 'wo-1', number: 7, title: 'Write report', brief: '', attachments: [] } }) }))
    show()
    const link = (await screen.findByText('WO-7')).closest('a')!
    expect(link.getAttribute('href')).toBe(`/work-orders/wo-1?run=${ID}`)
    expect(screen.getByText('Write report')).toBeTruthy()
  })

  it('links to the work order even without a spec', async () => {
    api.get.mockResolvedValue(makeRun({ work_order_id: 'wo-1', spec: undefined }))
    show()
    expect(await screen.findByText('open')).toBeTruthy()
  })

  it('shows the spent amount from the run when finished, falling back to 0', async () => {
    api.get.mockResolvedValue(makeRun({ cost_usd: 0.4, budget_usd: 2 }))
    show()
    expect(await screen.findByText('$0.40')).toBeTruthy()
    cleanup()
    api.get.mockResolvedValue(makeRun({ cost_usd: null, budget_usd: 2 }))
    show()
    expect(await screen.findByText('$0.00')).toBeTruthy()
  })

  it('reloads interactions and the journal once the run is finished', async () => {
    show()
    await screen.findByTestId('canvas')
    await waitFor(() => expect(api.interactions.mock.calls.length).toBeGreaterThanOrEqual(2))
    expect(api.journal.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('does not render the cancel button for a finished run', async () => {
    show()
    await screen.findByText('Research crew')
    expect(screen.queryByText('Cancel run')).toBeNull()
  })
})

describe('RunDetailPage live run', () => {
  const live = (over: Partial<Run> = {}) => makeRun({ status: 'running', finished_at: null, cost_usd: null, usage: null, ...over, spec: makeSpec() })

  it('streams events, reflects the live status and spend, and keeps the cancel button', async () => {
    api.get.mockResolvedValue(live())
    show(`/runs/${ID}?tab=timeline`)
    await screen.findByText('Cancel run')
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    expect(FakeEventSource.last.url).toBe(`/api/runs/${ID}/stream?after=0`)
    act(() => {
      FakeEventSource.last.open()
      FakeEventSource.last.message(makeEvent(1, 'crew_started', { crew_name: 'Research crew' }))
      FakeEventSource.last.message(makeEvent(2, 'llm_call', { agent_role: 'A', model: 'm', spent_usd: 0.6, cost_usd: 0.6 }))
    })
    expect(await screen.findByText('Crew start')).toBeTruthy()
    expect(screen.getByText('live')).toBeTruthy()
    expect(screen.getByText('$0.60')).toBeTruthy()
    expect(api.events).not.toHaveBeenCalled()
  })

  it('shows the budget stop state from budget events', async () => {
    api.get.mockResolvedValue(live())
    show()
    await screen.findByText('Cancel run')
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    act(() => FakeEventSource.last.message(makeEvent(1, 'budget', { state: 'hard_stopped', spent_usd: 2, budget_usd: 1 })))
    expect(await screen.findByText('hard stop')).toBeTruthy()
  })

  it('refetches the run when the stream reports a terminal status', async () => {
    api.get.mockResolvedValueOnce(live()).mockResolvedValue(makeRun({ status: 'succeeded', cost_usd: 0.9, output: 'DONE OUT' }))
    show(`/runs/${ID}?tab=outputs`)
    await screen.findByText('Cancel run')
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    act(() => FakeEventSource.last.message(makeEvent(1, 'run_status', { status: 'succeeded' })))
    expect(await screen.findByText('DONE OUT')).toBeTruthy()
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(screen.queryByText('Cancel run')).toBeNull()
  })

  it('throttles interactions and journal refreshes to one per second while events arrive', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    api.get.mockResolvedValue(live())
    show()
    await screen.findByText('Cancel run')
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
    const before = api.interactions.mock.calls.length
    act(() => {
      for (let i = 1; i <= 5; i++) FakeEventSource.last.message(makeEvent(i, 'log', { line: `l${i}` }))
    })
    await act(async () => { await vi.advanceTimersByTimeAsync(200) })
    expect(api.interactions.mock.calls.length).toBe(before)
    await act(async () => { await vi.advanceTimersByTimeAsync(1100) })
    expect(api.interactions.mock.calls.length).toBe(before + 1)
    expect(api.journal.mock.calls.length).toBe(before + 1)
  })

  it('compacts a running attempt from its journal, for people who may cancel it', async () => {
    setPermissions('runs.view', 'runs.cancel')
    api.get.mockResolvedValue(live())
    api.compact.mockResolvedValue({ compact_requests: 1 })
    show(`/runs/${ID}?tab=journal`)
    fireEvent.click(await screen.findByRole('button', { name: 'Compact context' }))
    await waitFor(() => expect(api.compact).toHaveBeenCalledWith(ID))
    expect(notifySuccess).toHaveBeenCalledWith("Asked the agents to compact their context; it shows in the journal when it's done")
    api.compact.mockRejectedValue(new Error('gone'))
    fireEvent.click(screen.getByRole('button', { name: 'Compact context' }))
    await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not compact the context', expect.any(Error)))
    cleanup()
    setPermissions('runs.view')
    show(`/runs/${ID}?tab=journal`)
    await screen.findByText('Journal entry one')
    expect(screen.queryByRole('button', { name: 'Compact context' })).toBeNull()
  })

  it('offers no compaction for a hand-off, which compacts by itself', async () => {
    setPermissions('runs.view', 'runs.cancel')
    api.get.mockResolvedValue({ ...live(), kind: 'handoff' })
    show(`/runs/${ID}?tab=journal`)
    await screen.findByText('Journal entry one')
    expect(screen.queryByRole('button', { name: 'Compact context' })).toBeNull()
  })

  it('offers no compaction once the run has finished', async () => {
    setPermissions('runs.view', 'runs.cancel')
    show(`/runs/${ID}?tab=journal`)
    await screen.findByText('Journal entry one')
    expect(screen.queryByRole('button', { name: 'Compact context' })).toBeNull()
  })

  it('cancels the run, replaces it with the response and confirms', async () => {
    api.get.mockResolvedValue(live())
    api.cancel.mockResolvedValue(makeRun({ status: 'cancelled', finished_at: '2026-01-01T00:00:20Z', spec: undefined, task_outputs: undefined }))
    show()
    fireEvent.click(await screen.findByText('Cancel run'))
    await waitFor(() => expect(notifySuccess).toHaveBeenCalledWith('Run cancelled'))
    expect(api.cancel).toHaveBeenCalledWith(ID)
    expect(await screen.findByText('cancelled')).toBeTruthy()
    expect(screen.queryByText('Cancel run')).toBeNull()
    // the frozen spec is kept when the response omits it
    fireEvent.click(tab('Spec'))
    expect(await screen.findByText(/Frozen when the run started/)).toBeTruthy()
  })

  it('reports a failed cancel and keeps the button', async () => {
    api.get.mockResolvedValue(live())
    const err = new Error('cannot')
    api.cancel.mockRejectedValue(err)
    show()
    fireEvent.click(await screen.findByText('Cancel run'))
    await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not cancel the run', err))
    await waitFor(() => expect((screen.getByText('Cancel run').closest('button') as HTMLButtonElement).disabled).toBe(false))
  })

  it('live tabs: callbacks poll and files show the saved-later hint', async () => {
    api.get.mockResolvedValue(live())
    show(`/runs/${ID}?tab=files`)
    expect(await screen.findByText('Files are saved when the run finishes')).toBeTruthy()
    fireEvent.click(tab('Callbacks'))
    expect(await screen.findByText('No callbacks')).toBeTruthy()
  })

  it('journal tab gets the live hint when empty', async () => {
    api.get.mockResolvedValue(live())
    api.journal.mockResolvedValue(makeJournal())
    show(`/runs/${ID}?tab=journal`)
    expect(await screen.findByText('Entries appear as agents start working.')).toBeTruthy()
  })

  it('exposes work-order runs with a deleted crew (no crew link)', async () => {
    api.get.mockResolvedValue(live({ crew_id: null, crew_name: 'Orphan' }))
    show()
    expect((await screen.findByText('Orphan')).closest('a')).toBeNull()
  })
})

describe('RunDetailPage failure display', () => {
  it('shows the run error in the header', async () => {
    api.get.mockResolvedValue(makeRun({ status: 'failed', error: 'LLM exploded' }))
    show()
    expect(await screen.findByText('LLM exploded')).toBeTruthy()
  })

  it('outputs tab mentions the error when there is no output', async () => {
    api.get.mockResolvedValue(makeRun({ status: 'failed', error: 'LLM exploded' }))
    show(`/runs/${ID}?tab=outputs`)
    expect(await screen.findByText('The run ended with an error: LLM exploded')).toBeTruthy()
  })

  it('timeline load failure notifies', async () => {
    const err = new Error('events down')
    api.events.mockRejectedValue(err)
    show()
    await screen.findByText('Research crew')
    await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not load run events', err))
  })

  it('interactions error shows a retry', async () => {
    api.interactions.mockRejectedValue(new Error('graph down'))
    show()
    expect(await screen.findByText('graph down')).toBeTruthy()
  })
})
