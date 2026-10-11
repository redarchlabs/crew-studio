import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook } from '@testing-library/react'
import { runsApi } from '../../../api/endpoints'
import { notifyError } from '../../../lib/notify'
import { FakeEventSource, installFakeEventSource, makeEvent } from '../testUtils'
import { useRunTimeline } from './useRunTimeline'

vi.mock('../../../api/endpoints', () => ({
  runsApi: { events: vi.fn(), streamUrl: (id: string, after: number) => `/api/runs/${id}/stream?after=${after}` },
}))
vi.mock('../../../lib/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))

const events = vi.mocked(runsApi.events)

beforeEach(() => {
  installFakeEventSource()
  vi.useFakeTimers()
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

const flush = () => act(async () => { await vi.advanceTimersByTimeAsync(130) })

describe('useRunTimeline: finished runs', () => {
  it('loads stored events and ends closed', async () => {
    events.mockResolvedValue([makeEvent(1, 'crew_started', { crew_name: 'C' }), makeEvent(2, 'log', { line: 'hi' })])
    const { result } = renderHook(() => useRunTimeline('r1', false))
    expect(result.current.stream).toBe('loading')
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(result.current.stream).toBe('closed')
    expect(result.current.state.eventCount).toBe(2)
    expect(result.current.state.logs).toHaveLength(1)
    expect(events).toHaveBeenCalledWith('r1', 0, expect.any(AbortSignal))
    expect(FakeEventSource.instances).toHaveLength(0)
  })

  it('pages through events 1000 at a time, resuming after the last seq', async () => {
    const first = Array.from({ length: 1000 }, (_, i) => makeEvent(i + 1, 'log', { line: String(i) }))
    events.mockResolvedValueOnce(first).mockResolvedValueOnce([makeEvent(1001, 'log', { line: 'last' })])
    const { result } = renderHook(() => useRunTimeline('r1', false))
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(events).toHaveBeenCalledTimes(2)
    expect(events.mock.calls[1][1]).toBe(1000)
    expect(result.current.state.eventCount).toBe(1001)
    expect(result.current.stream).toBe('closed')
  })

  it('handles an empty run', async () => {
    events.mockResolvedValue([])
    const { result } = renderHook(() => useRunTimeline('r1', false))
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(result.current.stream).toBe('closed')
    expect(result.current.state.eventCount).toBe(0)
  })

  it('notifies and closes when loading fails', async () => {
    const err = new Error('down')
    events.mockRejectedValue(err)
    const { result } = renderHook(() => useRunTimeline('r1', false))
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(notifyError).toHaveBeenCalledWith('Could not load run events', err)
    expect(result.current.stream).toBe('closed')
  })

  it('stays silent when the load is aborted by unmount', async () => {
    events.mockRejectedValue(new DOMException('aborted', 'AbortError'))
    const { unmount } = renderHook(() => useRunTimeline('r1', false))
    unmount()
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(notifyError).not.toHaveBeenCalled()
  })

  it('does nothing when live is undefined', () => {
    const { result } = renderHook(() => useRunTimeline('r1', undefined))
    expect(result.current.stream).toBe('idle')
    expect(events).not.toHaveBeenCalled()
    expect(FakeEventSource.instances).toHaveLength(0)
  })

  it('resets state when the run id changes', async () => {
    events.mockResolvedValue([makeEvent(1, 'log', { line: 'a' })])
    const { result, rerender } = renderHook(({ id }) => useRunTimeline(id, false), { initialProps: { id: 'r1' } })
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(result.current.state.eventCount).toBe(1)
    events.mockResolvedValue([makeEvent(1, 'log', { line: 'b' }), makeEvent(2, 'log', { line: 'c' })])
    rerender({ id: 'r2' })
    await act(async () => { await vi.advanceTimersByTimeAsync(10) })
    expect(result.current.state.eventCount).toBe(2)
    expect(result.current.state.logs.map((l) => l.line)).toEqual(['b', 'c'])
  })
})

describe('useRunTimeline: live runs', () => {
  it('connects, marks live on open and batches events', async () => {
    const { result } = renderHook(() => useRunTimeline('r1', true))
    expect(result.current.stream).toBe('connecting')
    const es = FakeEventSource.last
    expect(es.url).toBe('/api/runs/r1/stream?after=0')
    act(() => es.open())
    expect(result.current.stream).toBe('live')
    act(() => { es.message(makeEvent(1, 'crew_started', { crew_name: 'C' })); es.message(makeEvent(2, 'task_started', { task_name: 'T' })) })
    expect(result.current.state.eventCount).toBe(0)
    await flush()
    expect(result.current.state.eventCount).toBe(2)
    expect(result.current.state.groups.map((g) => g.key)).toEqual(['crew:start', 'task:T'])
  })

  it('accepts events on both the run_event and message channels', async () => {
    const { result } = renderHook(() => useRunTimeline('r1', true))
    const es = FakeEventSource.last
    act(() => { es.message(makeEvent(1, 'log', { line: 'a' }), 'run_event'); es.message(makeEvent(2, 'log', { line: 'b' }), 'message') })
    await flush()
    expect(result.current.state.logs.map((l) => l.line)).toEqual(['a', 'b'])
  })

  it('ignores malformed, duplicate and out-of-order events', async () => {
    const { result } = renderHook(() => useRunTimeline('r1', true))
    const es = FakeEventSource.last
    act(() => {
      es.emit('run_event', 'not json')
      es.emit('run_event', JSON.stringify({ nope: true }))
      es.message(makeEvent(5, 'log', { line: 'five' }))
      es.message(makeEvent(5, 'log', { line: 'dup' }))
      es.message(makeEvent(3, 'log', { line: 'old' }))
    })
    await flush()
    expect(result.current.state.logs.map((l) => l.line)).toEqual(['five'])
  })

  it('stops, flushes immediately and closes on a terminal run_status', async () => {
    const { result } = renderHook(() => useRunTimeline('r1', true))
    const es = FakeEventSource.last
    act(() => { es.message(makeEvent(1, 'log', { line: 'a' })); es.message(makeEvent(2, 'run_status', { status: 'succeeded' })) })
    expect(es.closed).toBe(true)
    expect(result.current.stream).toBe('closed')
    expect(result.current.state.eventCount).toBe(2)
    expect(result.current.state.runStatus).toBe('succeeded')
    // an error after a terminal status must not reconnect
    act(() => es.error())
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('keeps streaming on a non-terminal run_status', async () => {
    const { result } = renderHook(() => useRunTimeline('r1', true))
    const es = FakeEventSource.last
    act(() => es.message(makeEvent(1, 'run_status', { status: 'running' })))
    await flush()
    expect(es.closed).toBe(false)
    expect(result.current.state.runStatus).toBe('running')
  })

  it('reconnects with exponential backoff, resuming after the last seq', async () => {
    const { result } = renderHook(() => useRunTimeline('r1', true))
    const first = FakeEventSource.last
    act(() => { first.open(); first.message(makeEvent(7, 'log', { line: 'x' })) })
    await flush()
    act(() => first.error())
    expect(first.closed).toBe(true)
    expect(result.current.stream).toBe('reconnecting')
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(FakeEventSource.instances).toHaveLength(2)
    expect(FakeEventSource.last.url).toBe('/api/runs/r1/stream?after=7')
    expect(result.current.stream).toBe('reconnecting')
    act(() => FakeEventSource.last.open())
    expect(result.current.stream).toBe('live')
  })

  it('doubles the delay on consecutive failures up to a 10s cap', async () => {
    renderHook(() => useRunTimeline('r1', true))
    const delays = [1000, 2000, 4000, 8000, 10000, 10000]
    for (let i = 0; i < delays.length; i++) {
      const count = FakeEventSource.instances.length
      act(() => FakeEventSource.last.error())
      await act(async () => { await vi.advanceTimersByTimeAsync(delays[i] - 1) })
      expect(FakeEventSource.instances).toHaveLength(count)
      await act(async () => { await vi.advanceTimersByTimeAsync(1) })
      expect(FakeEventSource.instances).toHaveLength(count + 1)
    }
  })

  it('resets the backoff once an event arrives', async () => {
    renderHook(() => useRunTimeline('r1', true))
    act(() => FakeEventSource.last.error())
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    act(() => FakeEventSource.last.error())
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    act(() => FakeEventSource.last.message(makeEvent(1, 'log', { line: 'ok' })))
    const n = FakeEventSource.instances.length
    act(() => FakeEventSource.last.error())
    await act(async () => { await vi.advanceTimersByTimeAsync(1000) })
    expect(FakeEventSource.instances).toHaveLength(n + 1)
  })

  it('closes the source and cancels pending work on unmount, flushing buffered events', async () => {
    const { result, unmount } = renderHook(() => useRunTimeline('r1', true))
    const es = FakeEventSource.last
    act(() => es.message(makeEvent(1, 'log', { line: 'late' })))
    unmount()
    expect(es.closed).toBe(true)
    void result
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('does not reconnect after unmount even if a retry was scheduled', async () => {
    const { unmount } = renderHook(() => useRunTimeline('r1', true))
    act(() => FakeEventSource.last.error())
    unmount()
    await act(async () => { await vi.advanceTimersByTimeAsync(20000) })
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('reconnects to the new run when the id changes and clears old events', async () => {
    const { result, rerender } = renderHook(({ id }) => useRunTimeline(id, true), { initialProps: { id: 'r1' } })
    act(() => FakeEventSource.last.message(makeEvent(1, 'log', { line: 'a' })))
    await flush()
    expect(result.current.state.eventCount).toBe(1)
    rerender({ id: 'r2' })
    expect(FakeEventSource.instances[0].closed).toBe(true)
    await flush()
    expect(result.current.state.eventCount).toBe(0)
    expect(FakeEventSource.last.url).toBe('/api/runs/r2/stream?after=0')
  })

  it('tolerates an error event after unmount-disposal', () => {
    const { unmount } = renderHook(() => useRunTimeline('r1', true))
    const es = FakeEventSource.last
    unmount()
    expect(() => act(() => es.error())).not.toThrow()
  })
})
