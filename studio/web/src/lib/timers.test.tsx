import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePollWhile } from './usePollWhile'
import { useNow } from './useNow'
import { autoCleanup } from './testUtils'

autoCleanup()
beforeEach(() => vi.useFakeTimers())
afterEach(() => vi.useRealTimers())

describe('usePollWhile', () => {
  it('does nothing while inactive', () => {
    const reload = vi.fn()
    renderHook(() => usePollWhile(false, reload))
    vi.advanceTimersByTime(30_000)
    expect(reload).not.toHaveBeenCalled()
  })

  it('calls reload every 3 seconds by default while active', () => {
    const reload = vi.fn()
    renderHook(() => usePollWhile(true, reload))
    vi.advanceTimersByTime(2999)
    expect(reload).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(reload).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(6000)
    expect(reload).toHaveBeenCalledTimes(3)
  })

  it('honours a custom interval', () => {
    const reload = vi.fn()
    renderHook(() => usePollWhile(true, reload, 500))
    vi.advanceTimersByTime(1500)
    expect(reload).toHaveBeenCalledTimes(3)
  })

  it('stops when active turns false and restarts when it turns true again', () => {
    const reload = vi.fn()
    const { rerender } = renderHook(({ active }) => usePollWhile(active, reload, 1000), { initialProps: { active: true } })
    vi.advanceTimersByTime(1000)
    expect(reload).toHaveBeenCalledTimes(1)
    rerender({ active: false })
    vi.advanceTimersByTime(5000)
    expect(reload).toHaveBeenCalledTimes(1)
    rerender({ active: true })
    vi.advanceTimersByTime(1000)
    expect(reload).toHaveBeenCalledTimes(2)
  })

  it('clears the interval on unmount', () => {
    const reload = vi.fn()
    const { unmount } = renderHook(() => usePollWhile(true, reload, 1000))
    unmount()
    vi.advanceTimersByTime(5000)
    expect(reload).not.toHaveBeenCalled()
  })
})

describe('useNow', () => {
  it('returns the current time and stays fixed while inactive', () => {
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    const { result } = renderHook(() => useNow(false))
    const first = result.current
    expect(first).toBe(Date.parse('2026-01-01T00:00:00Z'))
    act(() => vi.advanceTimersByTime(10_000))
    expect(result.current).toBe(first)
  })

  it('ticks every second by default while active', () => {
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    const { result } = renderHook(() => useNow(true))
    const start = result.current
    act(() => vi.advanceTimersByTime(1000))
    expect(result.current).toBe(start + 1000)
    act(() => vi.advanceTimersByTime(2000))
    expect(result.current).toBe(start + 3000)
  })

  it('honours the interval and stops on unmount', () => {
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'))
    const clear = vi.spyOn(globalThis, 'clearInterval')
    const { result, unmount } = renderHook(() => useNow(true, 250))
    const start = result.current
    act(() => vi.advanceTimersByTime(250))
    expect(result.current).toBe(start + 250)
    unmount()
    expect(clear).toHaveBeenCalled()
  })
})
