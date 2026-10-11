import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { runsApi, workOrdersApi } from '../../../api/endpoints'
import type { Interactions } from '../../../api/types'
import { initialTimeline, type TimelineState } from '../../runs/timeline/timelineReducer'
import { makeInteractions, makeJournal, makeRun, makeWorkOrder } from '../testUtils'
import { useAttempt } from './useAttempt'

const h = vi.hoisted(() => ({
  timeline: { current: null as unknown as TimelineState },
  stream: { current: 'idle' as string },
  useRunTimeline: vi.fn(),
}))

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())
vi.mock('../../runs/timeline/useRunTimeline', () => ({
  useRunTimeline: (...args: unknown[]) => {
    h.useRunTimeline(...args)
    return { state: h.timeline.current, stream: h.stream.current }
  },
}))
vi.mock('../../runs/useLlmCalls', () => ({
  useLlmCallsResource: (runId: string) => ({ calls: undefined, loading: false, error: undefined, load: vi.fn(), runId }),
}))

const runsGet = vi.mocked(runsApi.get)
const interactionsGet = vi.mocked(runsApi.interactions)
const journalGet = vi.mocked(workOrdersApi.journal)

const running = makeRun({ id: 'r1', status: 'running', finished_at: null })
const done = makeRun({ id: 'r1', status: 'succeeded' })
const wo = (runs = [running]) => makeWorkOrder({ id: 'wo-1', status: 'running', current_run_id: 'r1', runs })

beforeEach(() => {
  h.timeline.current = { ...initialTimeline }
  h.stream.current = 'idle'
  h.useRunTimeline.mockReset()
  runsGet.mockReset().mockResolvedValue(running)
  interactionsGet.mockReset().mockResolvedValue(makeInteractions([]))
  journalGet.mockReset().mockResolvedValue(makeJournal())
})
afterEach(() => vi.useRealTimers())

/** Advance fake timers inside act so React flushes the resulting state updates. */
const advance = (ms: number) => act(async () => { await vi.advanceTimersByTimeAsync(ms) })

describe('without a run', () => {
  it('is idle: no run, no status, nothing fetched', async () => {
    const { result } = renderHook(() => useAttempt(makeWorkOrder({ runs: [] }), null, true, vi.fn()))
    await waitFor(() => expect(result.current.journal.loading).toBe(false))
    expect(result.current.run).toBeNull()
    expect(result.current.status).toBeNull()
    expect(result.current.live).toBe(false)
    expect(runsGet).not.toHaveBeenCalled()
    expect(journalGet).not.toHaveBeenCalled()
    expect(interactionsGet).not.toHaveBeenCalled()
    expect(h.useRunTimeline).toHaveBeenCalledWith('', undefined)
  })
})

describe('with permission to view runs', () => {
  it('uses the work order summary at first, then the fetched run', async () => {
    runsGet.mockResolvedValue({ ...running, output: 'full detail' })
    const { result } = renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
    expect(result.current.run).toMatchObject({ id: 'r1', output: null })
    await waitFor(() => expect(result.current.run?.output).toBe('full detail'))
    expect(runsGet).toHaveBeenCalledWith('r1', expect.anything())
    expect(journalGet).toHaveBeenCalledWith('wo-1', 'r1', expect.anything())
    expect(interactionsGet).toHaveBeenCalledWith('r1', expect.anything())
  })

  it('is live while the run is active and follows the stream (live=true)', async () => {
    const { result } = renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
    expect(result.current.live).toBe(true)
    expect(result.current.status).toBe('running')
    expect(h.useRunTimeline).toHaveBeenLastCalledWith('r1', true)
  })

  it('loads stored events (live=false) for a finished run and is not live', async () => {
    runsGet.mockResolvedValue(done)
    const { result } = renderHook(() => useAttempt(wo([done]), 'r1', true, vi.fn()))
    expect(result.current.live).toBe(false)
    expect(result.current.status).toBe('succeeded')
    expect(h.useRunTimeline).toHaveBeenLastCalledWith('r1', false)
  })

  it('prefers the stream status over the stored status while the run is active', () => {
    h.timeline.current = { ...initialTimeline, runStatus: 'queued' }
    const { result } = renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
    expect(result.current.status).toBe('queued')
  })

  it('ignores the stream status once the stored run is finished', async () => {
    h.timeline.current = { ...initialTimeline, runStatus: 'running' }
    runsGet.mockResolvedValue(done)
    const { result } = renderHook(() => useAttempt(wo([done]), 'r1', true, vi.fn()))
    expect(result.current.status).toBe('succeeded')
  })

  it('does not start a stream when the run is unknown', () => {
    renderHook(() => useAttempt(wo([]), 'r1', true, vi.fn()))
    expect(h.useRunTimeline).toHaveBeenLastCalledWith('r1', undefined)
  })

  it('refetches everything and tells the page when the stream reports a terminal status', async () => {
    const onFinished = vi.fn()
    const { result, rerender } = renderHook(() => useAttempt(wo(), 'r1', true, onFinished))
    await waitFor(() => expect(result.current.journal.loading).toBe(false))
    const before = { run: runsGet.mock.calls.length, journal: journalGet.mock.calls.length, inter: interactionsGet.mock.calls.length }
    h.timeline.current = { ...initialTimeline, runStatus: 'succeeded' }
    runsGet.mockResolvedValue(done)
    rerender()
    await waitFor(() => expect(onFinished).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(runsGet.mock.calls.length).toBe(before.run + 1))
    expect(journalGet.mock.calls.length).toBe(before.journal + 1)
    expect(interactionsGet.mock.calls.length).toBe(before.inter + 1)
    await waitFor(() => expect(result.current.live).toBe(false))
  })

  it('does not poll the work order (events stream instead)', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const onFinished = vi.fn()
    renderHook(() => useAttempt(wo(), 'r1', true, onFinished))
    await advance(10_000)
    expect(onFinished).not.toHaveBeenCalled()
  })

  it('reloads the journal and graph at most once per interval while events keep arriving', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { result, rerender } = renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
    await advance(10)
    expect(result.current.journal.loading).toBe(false)
    const j0 = journalGet.mock.calls.length
    const i0 = interactionsGet.mock.calls.length
    for (const n of [1, 2, 3]) {
      h.timeline.current = { ...initialTimeline, eventCount: n }
      rerender()
    }
    expect(journalGet.mock.calls.length).toBe(j0)
    await advance(1600)
    expect(journalGet.mock.calls.length).toBe(j0 + 1)
    expect(interactionsGet.mock.calls.length).toBe(i0 + 1)
  })

  it('does not reload for events when no events have arrived', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
    await advance(10)
    const j0 = journalGet.mock.calls.length
    await advance(5000)
    expect(journalGet.mock.calls.length).toBe(j0)
  })

  it('exposes reloadRun to refetch the run', async () => {
    const { result } = renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
    await waitFor(() => expect(runsGet).toHaveBeenCalledTimes(1))
    act(() => result.current.reloadRun())
    await waitFor(() => expect(runsGet).toHaveBeenCalledTimes(2))
  })

  it('switches attempts when the run id changes', async () => {
    const other = makeRun({ id: 'r2', status: 'failed' })
    const w = wo([running, other])
    const { result, rerender } = renderHook(({ id }) => useAttempt(w, id, true, vi.fn()), { initialProps: { id: 'r1' } })
    expect(result.current.run?.id).toBe('r1')
    runsGet.mockResolvedValue(other)
    rerender({ id: 'r2' })
    expect(result.current.run?.id).toBe('r2')
    await waitFor(() => expect(journalGet).toHaveBeenLastCalledWith('wo-1', 'r2', expect.anything()))
  })
})

describe('without permission to view runs', () => {
  it('uses the summary only and never calls the runs endpoints or opens a stream', async () => {
    const { result } = renderHook(() => useAttempt(wo(), 'r1', false, vi.fn()))
    await waitFor(() => expect(result.current.journal.loading).toBe(false))
    expect(runsGet).not.toHaveBeenCalled()
    expect(interactionsGet).not.toHaveBeenCalled()
    expect(result.current.run?.id).toBe('r1')
    expect(result.current.live).toBe(true)
    expect(h.useRunTimeline).toHaveBeenLastCalledWith('r1', undefined)
    expect(journalGet).toHaveBeenCalled()
  })

  it('polls the work order and the journal while live', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const onFinished = vi.fn()
    renderHook(() => useAttempt(wo(), 'r1', false, onFinished))
    await advance(10)
    const j0 = journalGet.mock.calls.length
    await advance(3100)
    expect(onFinished).toHaveBeenCalledTimes(1)
    expect(journalGet.mock.calls.length).toBe(j0 + 1)
  })

  it('stops polling once the run is finished', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const onFinished = vi.fn()
    renderHook(() => useAttempt(wo([done]), 'r1', false, onFinished))
    await advance(10_000)
    expect(onFinished).not.toHaveBeenCalled()
  })
})

it('returns typed interactions data', async () => {
  const data: Interactions = makeInteractions([])
  interactionsGet.mockResolvedValue(data)
  const { result } = renderHook(() => useAttempt(wo(), 'r1', true, vi.fn()))
  await waitFor(() => expect(result.current.interactions.data).toBe(data))
})
