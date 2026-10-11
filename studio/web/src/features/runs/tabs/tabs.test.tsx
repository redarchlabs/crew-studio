import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import { runsApi } from '../../../api/endpoints'
import type { Attachment, Delivery, Interactions, LlmCall } from '../../../api/types'
import { copyText, downloadText } from '../../../lib/download'
import { initialTimeline, timelineReducer } from '../timeline/timelineReducer'
import { LlmCallsContext, useLlmCallsResource } from '../useLlmCalls'
import { makeEvent, makeLlmCall, makeRun, makeSpec, renderAt } from '../testUtils'
import { CallbacksTab } from './CallbacksTab'
import { FilesTab } from './FilesTab'
import { InteractionsTab } from './InteractionsTab'
import { LlmCallsTab } from './LlmCallsTab'
import { LogsTab } from './LogsTab'
import { contentText, LlmCallBody, MessageBlock } from './MessageView'
import { OutputsTab } from './OutputsTab'
import { SpecTab } from './SpecTab'

vi.mock('../../../api/endpoints', () => ({
  runsApi: { deliveries: vi.fn(), artifacts: vi.fn(), llmCalls: vi.fn() },
  attachmentsApi: { contentUrl: (id: string, dl = false) => `/api/attachments/${id}/content${dl ? '?download=1' : ''}` },
}))
vi.mock('../../../lib/download', async (orig) => ({ ...(await orig<typeof import('../../../lib/download')>()), downloadText: vi.fn(), copyText: vi.fn().mockResolvedValue(undefined) }))
vi.mock('../../../lib/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('../../../components/Markdown', () => ({ Markdown: ({ children }: { children: string }) => <div data-testid="md">{children}</div> }))
vi.mock('../../../components/graph/FlowCanvas', () => ({
  FlowCanvas: ({ nodes, edges, onNodeClick, onPaneClick }: { nodes: { id: string; data: { label: string; focused: boolean } }[]; edges: unknown[]; onNodeClick: (e: unknown, n: unknown) => void; onPaneClick: () => void }) => (
    <div data-testid="canvas" data-edges={edges.length}>
      {nodes.map((n) => <button key={n.id} data-focused={String(n.data.focused)} onClick={(e) => onNodeClick(e, n)}>{n.data.label}</button>)}
      <button onClick={onPaneClick}>pane</button>
    </div>
  ),
}))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('contentText / MessageBlock', () => {
  it('handles strings, part arrays, objects and nullish', () => {
    expect(contentText('hi')).toBe('hi')
    expect(contentText(['a', { text: 'b' }, { type: 'image', url: 'u' }, 5])).toBe('a\nb\n{"type":"image","url":"u"}\n5')
    expect(contentText({ a: 1 })).toBe('{\n  "a": 1\n}')
    expect(contentText(null)).toBe('')
    expect(contentText(undefined)).toBe('')
    expect(contentText(7)).toBe('7')
  })

  it('shows role, char count and (empty) for empty content', () => {
    render(<MessageBlock message={{ role: 'user', content: '' }} />)
    expect(screen.getByText('user')).toBeTruthy()
    expect(screen.getByText('(empty)')).toBeTruthy()
    expect(screen.getByText('0 chars')).toBeTruthy()
  })

  it('uses a neutral tone for unknown roles', () => {
    render(<MessageBlock message={{ role: 'weird', content: 'x' }} />)
    expect(screen.getByText('weird').className).toContain('text-muted')
  })

  it('LlmCallBody lists messages and response, with empty response placeholder', () => {
    render(<LlmCallBody call={makeLlmCall({ seq: 1, messages: [{ role: 'system', content: 'a' }, { role: 'user', content: 'b' }], response: '' })} />)
    expect(screen.getByText('Messages (2)')).toBeTruthy()
    expect(screen.getAllByText('(empty)')).toHaveLength(1)
  })
})

describe('LogsTab', () => {
  const logs = [
    { seq: 1, line: 'alpha line', at: '' }, { seq: 2, line: 'Beta LINE', at: '' }, { seq: 3, line: 'gamma', at: '' },
  ]

  it('shows an empty state without logs', () => {
    render(<LogsTab logs={[]} />)
    expect(screen.getByText('No log output')).toBeTruthy()
  })

  it('lists lines and filters case-insensitively', () => {
    render(<LogsTab logs={logs} />)
    expect(screen.getByText('3/3 lines')).toBeTruthy()
    fireEvent.change(screen.getByPlaceholderText('Filter lines…'), { target: { value: '  line ' } })
    expect(screen.getByText('2/3 lines')).toBeTruthy()
    expect(screen.queryByText('gamma')).toBeNull()
  })

  it('pausing freezes the view and shows how many new lines arrived; resuming catches up', () => {
    const { rerender } = render(<LogsTab logs={logs} />)
    fireEvent.click(screen.getByText('Pause'))
    rerender(<LogsTab logs={[...logs, { seq: 4, line: 'delta', at: '' }, { seq: 5, line: 'eps', at: '' }]} />)
    expect(screen.queryByText('delta')).toBeNull()
    expect(screen.getByText('Resume (2 new)')).toBeTruthy()
    fireEvent.click(screen.getByText(/Resume/))
    expect(screen.getByText('delta')).toBeTruthy()
    expect(screen.getByText('Pause')).toBeTruthy()
  })

  it('shows plain Resume when nothing new arrived while paused', () => {
    render(<LogsTab logs={logs} />)
    fireEvent.click(screen.getByText('Pause'))
    expect(screen.getByText('Resume')).toBeTruthy()
  })

  it('copies all visible lines', async () => {
    render(<LogsTab logs={logs} />)
    fireEvent.click(screen.getByText('Copy all'))
    await waitFor(() => expect(copyText).toHaveBeenCalledWith('alpha line\nBeta LINE\ngamma'))
  })
})

describe('OutputsTab', () => {
  const noTimeline = initialTimeline

  it('shows an empty state, with the run error when there is one', () => {
    render(<OutputsTab run={makeRun()} timeline={noTimeline} />)
    expect(screen.getByText('Outputs appear as tasks complete.')).toBeTruthy()
    cleanup()
    render(<OutputsTab run={makeRun({ error: 'bad' })} timeline={noTimeline} />)
    expect(screen.getByText('The run ended with an error: bad')).toBeTruthy()
  })

  it('shows the final output and task outputs sorted by position', () => {
    const run = makeRun({ output: 'FINAL', task_outputs: [{ position: 1, task_name: 'Second', agent_role: 'B', raw: 'two' }, { position: 0, task_name: 'First', agent_role: 'A', raw: 'one' }] })
    render(<OutputsTab run={run} timeline={noTimeline} />)
    const titles = screen.getAllByText(/Final output|^\d\. /).map((e) => e.textContent)
    expect(titles).toEqual(['Final output', '1. First', '2. Second'])
    expect(screen.getAllByTestId('md').map((e) => e.textContent)).toEqual(['FINAL', 'one', 'two'])
  })

  it('falls back to task_completed events while running', () => {
    const timeline = timelineReducer(initialTimeline, { type: 'append', events: [
      makeEvent(1, 'task_completed', { task_name: 'T1', agent_role: 'A', output: 'out1' }),
      makeEvent(2, 'task_completed', {}),
    ] })
    render(<OutputsTab run={makeRun({ status: 'running', task_outputs: [] })} timeline={timeline} />)
    expect(screen.getByText('1. T1')).toBeTruthy()
    expect(screen.getByText('2. task')).toBeTruthy()
  })

  it("shows each task's cost, and which run a reused output came from", () => {
    const run = makeRun({ task_outputs: [
      { position: 0, task_name: 'Research', agent_role: 'A', raw: 'one', cost_usd: 0.42, reused_from_run_id: 'run-earlier-99' },
      { position: 1, task_name: 'Design', agent_role: 'B', raw: 'two', cost_usd: 0.1 },
      { position: 2, task_name: 'Old', agent_role: '', raw: 'three', reused_from_run_id: 'run-older-77' },
    ] })
    renderAt(<OutputsTab run={run} timeline={noTimeline} />)
    expect(screen.getByText('run-earl').closest('a')?.getAttribute('href')).toBe('/runs/run-earlier-99')
    expect(screen.getByText(/\$0 here, \$0\.42 when it ran/)).toBeTruthy()
    expect(screen.getByText('$0.10')).toBeTruthy()
    expect(screen.getByText('run-olde').parentElement?.textContent).toBe('reused from run run-olde · $0 here')
  })

  it('adds tasks a resumed run finished since it loaded, by position', () => {
    const timeline = timelineReducer(initialTimeline, { type: 'append', events: [
      makeEvent(1, 'task_completed', { task_name: 'Compaction', output: 'summary' }),
      makeEvent(2, 'task_completed', { task_name: 'Research', output: 'stale copy', position: 0 }),
      makeEvent(3, 'task_completed', { task_name: 'Design', agent_role: 'B', output: 'designed', position: 1 }),
    ] })
    const run = makeRun({ status: 'running', task_outputs: [{ position: 0, task_name: 'Research', agent_role: 'A', raw: 'reused', reused_from_run_id: 'run-earlier-99' }] })
    renderAt(<OutputsTab run={run} timeline={timeline} />)
    expect(screen.getAllByText(/^\d\. /).map((e) => e.textContent)).toEqual(['1. Research', '2. Design'])
    expect(screen.getAllByTestId('md').map((e) => e.textContent)).toEqual(['reused', 'designed'])
  })

  it('does not link to a run a reused output came from when it is unavailable', () => {
    const run = makeRun({ task_outputs: [{ position: 0, task_name: 'Research', agent_role: '', raw: 'one', reused_from_run_id: 'gone-run-1', reused_from_unavailable: true }] })
    renderAt(<OutputsTab run={run} timeline={noTimeline} />)
    expect(screen.getByText(/reused from an earlier run that is no longer available/)).toBeTruthy()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('downloads a card as a safely named markdown file', () => {
    render(<OutputsTab run={makeRun({ crew_name: 'My Crew!', id: 'abcdef123456', output: 'FINAL' })} timeline={noTimeline} />)
    fireEvent.click(screen.getByText('.md'))
    expect(downloadText).toHaveBeenCalledWith('my-crew-abcdef12.md', 'FINAL')
  })
})

describe('SpecTab', () => {
  it('explains when there is no spec', () => {
    render(<SpecTab run={makeRun()} />)
    expect(screen.getByText('No spec recorded')).toBeTruthy()
  })

  it('renders spec sections and expands them', () => {
    render(<SpecTab run={makeRun({ spec: makeSpec() })} />)
    expect(screen.getByText('Research crew · sequential')).toBeTruthy()
    expect(screen.getByText('1 inputs · $1.00')).toBeTruthy()
    fireEvent.click(screen.getByText('Tasks'))
    fireEvent.click(screen.getByText('1. Research'))
    expect(screen.getByText(/"expected_output"/)).toBeTruthy()
    fireEvent.click(screen.getByText('Agents'))
    fireEvent.click(screen.getByText('Analyst', { selector: 'span.flex-1' }))
    expect(screen.getByText(/"allow_delegation"/)).toBeTruthy()
    fireEvent.click(screen.getByText('LLMs'))
    fireEvent.click(screen.getByText('Pricing'))
    fireEvent.click(screen.getByText('Settings'))
    expect(screen.getByText(/"hard_stop_multiplier"/)).toBeTruthy()
  })
})

describe('InteractionsTab', () => {
  const data: Interactions = {
    nodes: [
      { id: 'crew:c', kind: 'crew', label: 'Crew', stats: { llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, duration_ms: 0 } },
      { id: 'task:t', kind: 'task', label: 'Task', stats: { llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, duration_ms: 0 } },
    ],
    edges: [{ id: 'e', source: 'crew:c', target: 'task:t', kind: 'runs', count: 1, cost_usd: 0 }],
  }
  const base = { reload: vi.fn(), focus: null, onFocus: vi.fn() }

  it('shows loading, error with retry, and nothing when idle', () => {
    const { rerender } = render(<InteractionsTab {...base} data={undefined} error={undefined} loading />)
    expect(screen.getByText('Loading interaction graph…')).toBeTruthy()
    rerender(<InteractionsTab {...base} data={undefined} error={new Error('x')} loading={false} />)
    fireEvent.click(screen.getByText('Retry'))
    expect(base.reload).toHaveBeenCalled()
    cleanup()
    const { container } = render(<InteractionsTab {...base} data={undefined} error={undefined} loading={false} />)
    expect(container.textContent).toBe('')
  })

  it('shows an empty state with no nodes', () => {
    render(<InteractionsTab {...base} data={{ nodes: [], edges: [] }} error={undefined} loading={false} />)
    expect(screen.getByText('No interactions yet')).toBeTruthy()
  })

  it('keeps showing stale data when a refresh errors', () => {
    render(<InteractionsTab {...base} data={data} error={new Error('x')} loading={false} />)
    expect(screen.getByTestId('canvas')).toBeTruthy()
  })

  it('renders the graph and legend and focuses on node click', () => {
    const onFocus = vi.fn()
    render(<InteractionsTab {...base} onFocus={onFocus} data={data} error={undefined} loading={false} />)
    expect(screen.getByTestId('canvas').getAttribute('data-edges')).toBe('1')
    expect(screen.getByText('delegation')).toBeTruthy()
    expect(screen.getByText('tool call')).toBeTruthy()
    fireEvent.click(screen.getByText('Task'))
    expect(onFocus).toHaveBeenLastCalledWith({ kind: 'task', value: 't' })
    fireEvent.click(screen.getByText('Crew'))
    expect(onFocus).toHaveBeenLastCalledWith(null)
  })

  it('clears focus on pane click only when something is focused', () => {
    const onFocus = vi.fn()
    const { rerender } = render(<InteractionsTab {...base} onFocus={onFocus} data={data} error={undefined} loading={false} />)
    fireEvent.click(screen.getByText('pane'))
    expect(onFocus).not.toHaveBeenCalled()
    rerender(<InteractionsTab {...base} onFocus={onFocus} focus={{ kind: 'task', value: 't' }} data={data} error={undefined} loading={false} />)
    expect(screen.getByText('Task').getAttribute('data-focused')).toBe('true')
    fireEvent.click(screen.getByText('pane'))
    expect(onFocus).toHaveBeenCalledWith(null)
  })
})

describe('CallbacksTab', () => {
  const NOW = Date.parse('2026-01-01T00:00:00Z')
  const delivery = (over: Partial<Delivery> = {}): Delivery => ({
    id: 'd1', run_id: 'r', url: 'https://x.test/hook', status: 'delivered', attempts: 1, next_attempt_at: '2026-01-01T00:05:00Z',
    last_status_code: 200, last_error: null, delivered_at: '2025-12-31T23:50:00Z', created_at: '', ...over,
  })
  const deliveries = vi.mocked(runsApi.deliveries)

  it('shows loading, then an empty state', async () => {
    deliveries.mockResolvedValue([])
    render(<CallbacksTab runId="r" live={false} />)
    expect(screen.getByText('Loading deliveries…')).toBeTruthy()
    expect(await screen.findByText('No callbacks')).toBeTruthy()
  })

  it('shows an error with retry', async () => {
    deliveries.mockRejectedValueOnce(new Error('bad')).mockResolvedValue([])
    render(<CallbacksTab runId="r" live={false} />)
    expect(await screen.findByText('bad')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('No callbacks')).toBeTruthy()
  })

  it('renders delivered, failed and pending deliveries', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(NOW)
    deliveries.mockResolvedValue([
      delivery(),
      delivery({ id: 'd2', status: 'failed', attempts: 5, last_status_code: 500, last_error: 'server exploded', delivered_at: null }),
      delivery({ id: 'd3', status: 'pending', attempts: 0, last_status_code: null, last_error: null, delivered_at: null }),
    ])
    render(<CallbacksTab runId="r" live={false} />)
    expect(await screen.findByText('delivered')).toBeTruthy()
    expect(screen.getByText('failed')).toBeTruthy()
    expect(screen.getByText('pending')).toBeTruthy()
    expect(screen.getByText('200')).toBeTruthy()
    expect(screen.getByText('server exploded')).toBeTruthy()
    expect(screen.getByText('10m ago')).toBeTruthy()
    expect(screen.getByText('in 5m')).toBeTruthy()
  })

  it('polls while the run is live or a delivery is pending, not otherwise', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    deliveries.mockResolvedValue([delivery()])
    const { unmount } = render(<CallbacksTab runId="r" live={false} />)
    await screen.findByText('delivered')
    await act(async () => { await vi.advanceTimersByTimeAsync(11000) })
    expect(deliveries).toHaveBeenCalledTimes(1)
    unmount()
    deliveries.mockClear()
    render(<CallbacksTab runId="r" live />)
    await screen.findByText('delivered')
    await act(async () => { await vi.advanceTimersByTimeAsync(5100) })
    expect(deliveries).toHaveBeenCalledTimes(2)
  })
})

describe('FilesTab', () => {
  const att = (over: Partial<Attachment> & { id: string }): Attachment => ({
    work_order_id: null, run_id: 'r', kind: 'artifact', filename: over.id + '.txt', content_type: 'application/octet-stream', size_bytes: 1024,
    sha256: 'abcdef1234567890', created_by_name: '', created_at: '2026-01-01T00:00:00Z', ...over,
  })
  const artifacts = vi.mocked(runsApi.artifacts)
  const input = { id: 'in1', filename: 'brief.pdf', content_type: 'application/pdf', size_bytes: 2048 }

  it('shows input files and empty artifact states for a finished run', async () => {
    artifacts.mockResolvedValue([])
    render(<FilesTab inputs={[input]} runId="r" live={false} />)
    expect(screen.getByText('Input files · 1 · 2.0 KB')).toBeTruthy()
    expect(screen.getByText('brief.pdf')).toBeTruthy()
    expect(await screen.findByText('The agents wrote no files')).toBeTruthy()
    expect(screen.getByText('No transcript was stored for this run.')).toBeTruthy()
  })

  it('shows no input files note and live placeholders', async () => {
    artifacts.mockResolvedValue([])
    render(<FilesTab inputs={[]} runId="r" live />)
    expect(screen.getByText('No input files.')).toBeTruthy()
    expect(await screen.findByText('Files are saved when the run finishes')).toBeTruthy()
    expect(screen.getByText('Saved when the run finishes.')).toBeTruthy()
  })

  it('lists artifacts sorted by name and transcripts separately', async () => {
    artifacts.mockResolvedValue([att({ id: 'zeta' }), att({ id: 'alpha' }), att({ id: 'tr', kind: 'transcript', filename: 'transcript.jsonl' }), att({ id: 'inp', kind: 'input', filename: 'ignored.txt' })])
    render(<FilesTab inputs={[]} runId="r" live={false} />)
    await screen.findByText('Files the agents wrote · 2 · 2.0 KB')
    const names = screen.getAllByText(/\.txt$|\.jsonl$/).map((e) => e.textContent)
    expect(names).toEqual(['alpha.txt', 'zeta.txt', 'transcript.jsonl'])
    expect(screen.queryByText('ignored.txt')).toBeNull()
    expect(screen.getByText(/untruncated record/)).toBeTruthy()
  })

  it('shows an error with retry and supports manual refresh', async () => {
    artifacts.mockRejectedValue(new Error('denied'))
    render(<FilesTab inputs={[]} runId="r" live={false} />)
    expect(await screen.findByText('denied')).toBeTruthy()
    artifacts.mockResolvedValue([])
    fireEvent.click(screen.getByText('Retry'))
    await screen.findByText('The agents wrote no files')
    const before = artifacts.mock.calls.length
    fireEvent.click(screen.getByText('Refresh'))
    await waitFor(() => expect(artifacts).toHaveBeenCalledTimes(before + 1))
  })

  it('fetches once for a finished run and again when a live run finishes', async () => {
    artifacts.mockResolvedValue([])
    const { rerender } = render(<FilesTab inputs={[]} runId="r" live={false} />)
    await screen.findByText('The agents wrote no files')
    expect(artifacts).toHaveBeenCalledTimes(1)
    artifacts.mockClear()
    rerender(<FilesTab inputs={[]} runId="r2" live />)
    await waitFor(() => expect(artifacts).toHaveBeenCalledTimes(1))
    rerender(<FilesTab inputs={[]} runId="r2" live={false} />)
    await waitFor(() => expect(artifacts).toHaveBeenCalledTimes(2))
  })

  it('does not fetch or show output panels without a run id', () => {
    render(<FilesTab inputs={[]} runId={null} live={false} />)
    expect(screen.queryByText(/Files the agents wrote/)).toBeNull()
    expect(artifacts).not.toHaveBeenCalled()
  })

  it('refetches when the run finishes', async () => {
    artifacts.mockResolvedValue([])
    const { rerender } = render(<FilesTab inputs={[]} runId="r" live />)
    await screen.findByText('Files are saved when the run finishes')
    rerender(<FilesTab inputs={[]} runId="r" live={false} />)
    await waitFor(() => expect(artifacts.mock.calls.length).toBeGreaterThanOrEqual(2))
  })

  it('previews small text files and reports preview failures', async () => {
    artifacts.mockResolvedValue([att({ id: 'note', filename: 'note.txt', content_type: 'text/plain', size_bytes: 10 })])
    const fetchMock = vi.fn().mockResolvedValueOnce({ ok: true, text: () => Promise.resolve('hello file') }).mockResolvedValueOnce({ ok: false, status: 404 })
    vi.stubGlobal('fetch', fetchMock)
    render(<FilesTab inputs={[]} runId="r" live={false} />)
    fireEvent.click(await screen.findByText('Preview'))
    expect(await screen.findByText('hello file')).toBeTruthy()
    fireEvent.click(screen.getByText('Hide'))
    fireEvent.click(screen.getByText('Preview'))
    expect(await screen.findByText('The server answered 404.')).toBeTruthy()
    vi.unstubAllGlobals()
  })

  it('shows images in a gallery and opens the lightbox', async () => {
    artifacts.mockResolvedValue([att({ id: 'pic', filename: 'pic.png', content_type: 'image/png' })])
    render(<FilesTab inputs={[]} runId="r" live={false} />)
    const thumb = await screen.findByAltText('pic.png')
    expect(thumb.getAttribute('src')).toBe('/api/attachments/pic/content')
    fireEvent.click(thumb)
    expect(document.body.querySelector('[role="dialog"]')).toBeTruthy()
  })
})

describe('LlmCallsTab and useLlmCallsResource', () => {
  const llmCalls = vi.mocked(runsApi.llmCalls)
  function Harness({ live = false, runId = 'r', refreshKey }: { live?: boolean; runId?: string; refreshKey?: number }) {
    const res = useLlmCallsResource(runId)
    return <LlmCallsContext.Provider value={res}><LlmCallsTab live={live} refreshKey={refreshKey} /></LlmCallsContext.Provider>
  }
  const calls: LlmCall[] = [
    makeLlmCall({ seq: 2, model: 'm2', cost_usd: 0.02, usage: { prompt_tokens: 10, completion_tokens: 5, cached_prompt_tokens: 4 } }),
    makeLlmCall({ seq: 1, model: 'm1', cost_usd: 0.01, usage: null }),
  ]

  beforeEach(() => llmCalls.mockResolvedValue(calls))

  it('loads on mount, sorts by seq and totals cost', async () => {
    render(<Harness />)
    expect(screen.getByText('Loading LLM calls…')).toBeTruthy()
    expect(await screen.findByText('2 calls · $0.03')).toBeTruthy()
    const rows = within(screen.getByRole('table')).getAllByRole('row').slice(1) as HTMLTableRowElement[]
    expect(rows.map((x) => x.cells[1].textContent)).toEqual(['1', '2'])
    expect(screen.getByText('(4c)')).toBeTruthy()
  })

  it('does not refetch when remounted data is cached, but refresh forces it', async () => {
    render(<Harness />)
    await screen.findByText('2 calls · $0.03')
    expect(llmCalls).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByText('Refresh'))
    await waitFor(() => expect(llmCalls).toHaveBeenCalledTimes(2))
  })

  it('forces a refetch each time when live toggles', async () => {
    const { rerender } = render(<Harness live />)
    await screen.findByText('2 calls · $0.03')
    rerender(<Harness live={false} />)
    await waitFor(() => expect(llmCalls).toHaveBeenCalledTimes(1))
  })

  it('refetches as new calls are counted during a live run', async () => {
    const { rerender } = render(<Harness live refreshKey={1} />)
    await screen.findByText('2 calls · $0.03')
    rerender(<Harness live refreshKey={2} />)
    await waitFor(() => expect(llmCalls).toHaveBeenCalledTimes(2))
  })

  it('expands a row to show the call body and collapses it again', async () => {
    render(<Harness />)
    await screen.findByText('2 calls · $0.03')
    fireEvent.click(screen.getByText('m2'))
    expect(screen.getAllByText('hello').length).toBe(1)
    fireEvent.click(screen.getByText('m2'))
    expect(screen.queryByText('hello')).toBeNull()
  })

  it('shows an empty state', async () => {
    llmCalls.mockResolvedValue([])
    render(<Harness />)
    expect(await screen.findByText('No LLM calls recorded')).toBeTruthy()
  })

  it('shows errors with retry and clears them on success', async () => {
    llmCalls.mockRejectedValueOnce(new Error('kaput'))
    render(<Harness />)
    expect(await screen.findByText('kaput')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('2 calls · $0.03')).toBeTruthy()
  })

  it('hook: resets cached calls when the run changes and loads the new run', async () => {
    const { result, rerender } = renderHook(({ id }) => useLlmCallsResource(id), { initialProps: { id: 'a' } })
    act(() => result.current.load())
    await waitFor(() => expect(result.current.calls).toHaveLength(2))
    llmCalls.mockResolvedValue([makeLlmCall({ seq: 1 })])
    rerender({ id: 'b' })
    await waitFor(() => expect(result.current.calls).toBeUndefined())
    act(() => result.current.load())
    await waitFor(() => expect(result.current.calls).toHaveLength(1))
    expect(llmCalls).toHaveBeenLastCalledWith('b')
  })

  it('hook: refetches only when forced or when a wanted seq is missing', async () => {
    const { result } = renderHook(() => useLlmCallsResource('a'))
    act(() => result.current.load())
    await waitFor(() => expect(result.current.calls).toHaveLength(2))
    act(() => result.current.load({ wantSeq: 1 }))
    expect(llmCalls).toHaveBeenCalledTimes(1)
    act(() => result.current.load({ wantSeq: 99 }))
    await waitFor(() => expect(llmCalls).toHaveBeenCalledTimes(2))
    act(() => result.current.load({ force: true }))
    await waitFor(() => expect(llmCalls).toHaveBeenCalledTimes(3))
  })

  it('does not fetch concurrently while a request is in flight', async () => {
    let resolve: (v: LlmCall[]) => void = () => {}
    llmCalls.mockReturnValue(new Promise((res) => { resolve = res }))
    render(<Harness />)
    await waitFor(() => expect(llmCalls).toHaveBeenCalledTimes(1))
    await act(async () => { resolve(calls) })
    await screen.findByText('2 calls · $0.03')
    expect(llmCalls).toHaveBeenCalledTimes(1)
  })

  it('useLlmCalls requires its provider', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<LlmCallsTab live={false} />)).toThrow('useLlmCalls must be used inside LlmCallsContext')
    spy.mockRestore()
  })
})
