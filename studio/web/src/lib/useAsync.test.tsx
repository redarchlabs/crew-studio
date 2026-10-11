import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useAsync } from './useAsync'
import { autoCleanup } from './testUtils'

autoCleanup()

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('useAsync', () => {
  it('starts loading, then exposes data', async () => {
    const { result } = renderHook(() => useAsync(() => Promise.resolve('hello'), []))
    expect(result.current.loading).toBe(true)
    expect(result.current.data).toBeUndefined()
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toBe('hello')
    expect(result.current.error).toBeUndefined()
  })

  it('exposes errors and stops loading', async () => {
    const boom = new Error('boom')
    const { result } = renderHook(() => useAsync(() => Promise.reject(boom), []))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe(boom)
    expect(result.current.data).toBeUndefined()
  })

  it('ignores AbortError rejections', async () => {
    const loader = vi.fn(() => Promise.reject(new DOMException('x', 'AbortError')))
    const { result } = renderHook(() => useAsync(loader, []))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBeUndefined()
  })

  it('aborts the previous request and drops stale data when deps change', async () => {
    const signals: AbortSignal[] = []
    const loader = (id: string) => (signal: AbortSignal) => {
      signals.push(signal)
      return Promise.resolve(`data-${id}`)
    }
    const { result, rerender } = renderHook(({ id }) => useAsync(loader(id), [id]), { initialProps: { id: 'a' } })
    await waitFor(() => expect(result.current.data).toBe('data-a'))
    rerender({ id: 'b' })
    expect(signals[0].aborted).toBe(true)
    expect(result.current.data).toBeUndefined()
    await waitFor(() => expect(result.current.data).toBe('data-b'))
  })

  it('does not apply a result that arrives after the request was aborted', async () => {
    const first = deferred<string>()
    const second = deferred<string>()
    const queue = [first, second]
    const { result, rerender } = renderHook(({ id }) => useAsync(() => queue.shift()!.promise, [id]), { initialProps: { id: 1 } })
    rerender({ id: 2 })
    await act(async () => {
      second.resolve('second')
      await second.promise
    })
    await waitFor(() => expect(result.current.data).toBe('second'))
    await act(async () => {
      first.resolve('first (stale)')
      await first.promise
    })
    expect(result.current.data).toBe('second')
  })

  it('does not surface errors or loading changes from an aborted request', async () => {
    const first = deferred<string>()
    const { result, unmount } = renderHook(() => useAsync(() => first.promise, []))
    unmount()
    await act(async () => {
      first.reject(new Error('late'))
      await first.promise.catch(() => undefined)
    })
    expect(result.current.error).toBeUndefined()
  })

  it('reload refetches but keeps the old data visible meanwhile', async () => {
    let n = 0
    const d = deferred<string>()
    const loader = vi.fn(() => (n++ === 0 ? Promise.resolve('v1') : d.promise))
    const { result } = renderHook(() => useAsync(loader, []))
    await waitFor(() => expect(result.current.data).toBe('v1'))
    act(() => result.current.reload())
    expect(result.current.loading).toBe(true)
    expect(result.current.data).toBe('v1')
    await act(async () => {
      d.resolve('v2')
      await d.promise
    })
    await waitFor(() => expect(result.current.data).toBe('v2'))
    expect(loader).toHaveBeenCalledTimes(2)
  })

  it('clears a previous error when reloading', async () => {
    let n = 0
    const { result } = renderHook(() => useAsync(() => (n++ === 0 ? Promise.reject(new Error('first')) : Promise.resolve('ok')), []))
    await waitFor(() => expect(result.current.error).toBeInstanceOf(Error))
    act(() => result.current.reload())
    await waitFor(() => expect(result.current.data).toBe('ok'))
    expect(result.current.error).toBeUndefined()
  })

  it('setData updates locally without refetching', async () => {
    const loader = vi.fn(() => Promise.resolve([1, 2]))
    const { result } = renderHook(() => useAsync(loader, []))
    await waitFor(() => expect(result.current.data).toEqual([1, 2]))
    act(() => result.current.setData((prev) => [...(prev ?? []), 3]))
    expect(result.current.data).toEqual([1, 2, 3])
    expect(loader).toHaveBeenCalledTimes(1)
  })

  it('uses the latest loader closure without refetching on identity change', async () => {
    const calls: string[] = []
    const { rerender, result } = renderHook(({ tag }) => useAsync(() => { calls.push(tag); return Promise.resolve(tag) }, []), { initialProps: { tag: 'x' } })
    await waitFor(() => expect(result.current.data).toBe('x'))
    rerender({ tag: 'y' })
    expect(calls).toEqual(['x'])
    act(() => result.current.reload())
    await waitFor(() => expect(result.current.data).toBe('y'))
  })
})
