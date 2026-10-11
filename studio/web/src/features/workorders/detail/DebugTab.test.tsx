import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { runsApi } from '../../../api/endpoints'
import { emptyTally } from '../../runs/timeline/spendTally'
import { initialTimeline, type TimelineState } from '../../runs/timeline/timelineReducer'
import { makeAttachment, makeRun } from '../testUtils'
import { DebugTab } from './DebugTab'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())

// The five debug views are owned by the runs feature; stub them so these tests cover only the shell.
vi.mock('../../runs/tabs/LlmCallsTab', () => ({ LlmCallsTab: ({ live }: { live: boolean }) => <div>LLM view live={String(live)}</div> }))
vi.mock('../../runs/tabs/LogsTab', () => ({ LogsTab: ({ logs }: { logs: unknown[] }) => <div>Console view {logs.length}</div> }))
vi.mock('../../runs/tabs/SpecTab', () => ({ SpecTab: ({ run }: { run: { id: string } }) => <div>Spec view {run.id}</div> }))
vi.mock('../../runs/tabs/CallbacksTab', () => ({ CallbacksTab: ({ runId }: { runId: string }) => <div>Callbacks view {runId}</div> }))
vi.mock('../../runs/timeline/TimelineView', () => ({
  TimelineFilters: ({ agents }: { agents: string[] }) => <div>Filters {agents.join(',')}</div>,
  TimelineList: ({ state }: { state: { eventCount: number } }) => <div>Timeline {state.eventCount}</div>,
}))

const artifacts = vi.mocked(runsApi.artifacts)
const run = makeRun({ id: 'run-9' })
const timeline = (over: Partial<TimelineState> = {}): TimelineState => ({ ...initialTimeline, ...over })

beforeEach(() => {
  artifacts.mockReset().mockResolvedValue([])
})
afterEach(cleanup)

describe('DebugTab', () => {
  it('starts on the LLM calls view', () => {
    render(<DebugTab run={run} live={false} timeline={timeline()} stream="closed" />)
    expect(screen.getByRole('tab', { name: /LLM calls/ }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByText('LLM view live=false')).toBeTruthy()
  })

  it.each([
    ['Console', 'Console view 0'],
    ['Spec', 'Spec view run-9'],
    ['Callbacks', 'Callbacks view run-9'],
    ['Events', 'Timeline 0'],
  ])('switches to %s', (label, text) => {
    render(<DebugTab run={run} live={false} timeline={timeline()} stream="closed" />)
    fireEvent.click(screen.getByRole('tab', { name: new RegExp(label) }))
    expect(screen.getByText(text)).toBeTruthy()
    expect(screen.queryByText(/LLM view/)).toBeNull()
  })

  it('shows event and console counts on the tabs (console lines excluded from events)', () => {
    render(<DebugTab run={run} live timeline={timeline({ eventCount: 15, logs: [{ seq: 1, line: 'a', at: '' }, { seq: 2, line: 'b', at: '' }] })} stream="live" />)
    expect(screen.getByRole('tab', { name: /Events/ }).textContent).toContain('13')
    expect(screen.getByRole('tab', { name: /Console/ }).textContent).toContain('2')
  })

  it('shows no counts when empty', () => {
    render(<DebugTab run={run} live={false} timeline={timeline()} stream="closed" />)
    expect(screen.getByRole('tab', { name: 'Events' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Console' })).toBeTruthy()
  })

  it('shows a loading state for events while the stream loads with nothing yet', () => {
    render(<DebugTab run={run} live={false} timeline={timeline()} stream="loading" />)
    fireEvent.click(screen.getByRole('tab', { name: /Events/ }))
    expect(screen.getByText('Loading events…')).toBeTruthy()
  })

  it('shows the timeline once events exist, even while still loading', () => {
    render(<DebugTab run={run} live={false} timeline={timeline({ eventCount: 3, agents: ['A', 'B'] })} stream="loading" />)
    fireEvent.click(screen.getByRole('tab', { name: /Events/ }))
    expect(screen.getByText('Timeline 3')).toBeTruthy()
    expect(screen.getByText('Filters A,B')).toBeTruthy()
  })
})

describe('transcript links', () => {
  it('links stored transcripts for download, ignoring other artifacts', async () => {
    artifacts.mockResolvedValue([
      makeAttachment({ id: 'tr', kind: 'transcript', filename: 'transcript.jsonl', size_bytes: 2048, sha256: 'deadbeef' }),
      makeAttachment({ id: 'ar', kind: 'artifact', filename: 'report.md' }),
    ])
    render(<DebugTab run={run} live={false} timeline={timeline()} stream="closed" />)
    const link = await screen.findByRole('link', { name: /transcript\.jsonl/ })
    expect(link.getAttribute('href')).toBe('/api/attachments/tr/content?download=1')
    expect(link.getAttribute('title')).toBe('sha256 deadbeef')
    expect(link.textContent).toContain('2.0 KB')
    expect(screen.queryByText(/report\.md/)).toBeNull()
  })

  it('says it is still loading, then that none was stored', async () => {
    render(<DebugTab run={run} live={false} timeline={timeline()} stream="closed" />)
    expect(screen.getByText('Loading…')).toBeTruthy()
    expect(await screen.findByText('No transcript stored for this run.')).toBeTruthy()
  })

  it('explains that records are saved when the run finishes while it is live', async () => {
    render(<DebugTab run={run} live timeline={timeline()} stream="live" />)
    expect(await screen.findByText('The transcript and console log are saved when the run finishes.')).toBeTruthy()
  })

  it('refetches artifacts when the run stops being live', async () => {
    const { rerender } = render(<DebugTab run={run} live timeline={timeline()} stream="live" />)
    await waitFor(() => expect(artifacts).toHaveBeenCalledTimes(1))
    artifacts.mockResolvedValue([makeAttachment({ id: 'tr', kind: 'transcript', filename: 'final.jsonl' })])
    rerender(<DebugTab run={run} live={false} timeline={timeline()} stream="closed" />)
    expect(await screen.findByRole('link', { name: /final\.jsonl/ })).toBeTruthy()
    expect(artifacts.mock.calls.length).toBeGreaterThanOrEqual(2)
  })

  it('shows spend by source, including searches the LLM calls list leaves out', () => {
    const tally = { ...emptyTally, llmCalls: 5, llmUsd: 0.0045, promptTokens: 128_000, completionTokens: 600, cachedTokens: 90_000, searches: 17, searchUsd: 0.127, searchTokens: 34_000 }
    render(<DebugTab run={run} live={false} timeline={timeline({ tally })} stream="closed" />)
    const strip = screen.getByLabelText('Spend breakdown')
    expect(strip.textContent).toContain('LLM$0.00455 calls · 129k tok, 90k cached')
    expect(strip.textContent).not.toContain('cache writes')
    expect(strip.textContent).toContain('Search$0.1317 searches · 34k tok')
    expect(strip.textContent).toContain('Total$0.13')
    expect(strip.textContent).not.toContain('Video')
  })

  it('shows the tokens written to the prompt cache', () => {
    const tally = { ...emptyTally, llmCalls: 2, llmUsd: 0.05, promptTokens: 40_000, cachedTokens: 18_000, cacheWriteTokens: 20_000 }
    render(<DebugTab run={run} live={false} timeline={timeline({ tally })} stream="closed" />)
    expect(screen.getByLabelText('Spend breakdown').textContent).toContain('40k tok, 18k cached, 20k cache writes')
  })
})
