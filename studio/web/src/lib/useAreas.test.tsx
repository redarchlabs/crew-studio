import { renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api/endpoints', () => ({ areasApi: { list: vi.fn() } }))

import { areasApi } from '../api/endpoints'
import { invalidateAreas, useAreas } from './useAreas'
import { autoCleanup, makeArea } from './testUtils'

autoCleanup()
const list = areasApi.list as unknown as ReturnType<typeof vi.fn>

beforeEach(() => {
  list.mockReset()
  invalidateAreas()
})

describe('useAreas', () => {
  it('starts loading and then exposes areas and a byId index', async () => {
    const a = makeArea({ id: 'a', name: 'Alpha' })
    list.mockResolvedValue([a])
    const { result } = renderHook(() => useAreas())
    expect(result.current.loading).toBe(true)
    expect(result.current.areas).toEqual([])
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.areas).toEqual([a])
    expect(result.current.byId.get('a')?.name).toBe('Alpha')
    expect(result.current.error).toBeUndefined()
  })

  it('shares one fetch between consumers', async () => {
    list.mockResolvedValue([])
    const one = renderHook(() => useAreas())
    const two = renderHook(() => useAreas())
    await waitFor(() => expect(one.result.current.loading).toBe(false))
    await waitFor(() => expect(two.result.current.loading).toBe(false))
    expect(list).toHaveBeenCalledTimes(1)
  })

  it('invalidateAreas forces a refetch for the next consumer', async () => {
    list.mockResolvedValueOnce([makeArea({ id: 'old' })]).mockResolvedValueOnce([makeArea({ id: 'new' })])
    const first = renderHook(() => useAreas())
    await waitFor(() => expect(first.result.current.areas[0]?.id).toBe('old'))
    invalidateAreas()
    const second = renderHook(() => useAreas())
    await waitFor(() => expect(second.result.current.areas[0]?.id).toBe('new'))
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('reports the error, stops loading, and retries on the next mount', async () => {
    const boom = new Error('down')
    list.mockRejectedValueOnce(boom).mockResolvedValueOnce([makeArea({ id: 'z' })])
    const first = renderHook(() => useAreas())
    await waitFor(() => expect(first.result.current.loading).toBe(false))
    expect(first.result.current.error).toBe(boom)
    expect(first.result.current.areas).toEqual([])
    const second = renderHook(() => useAreas())
    await waitFor(() => expect(second.result.current.areas).toHaveLength(1))
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('does not update state after unmount', async () => {
    let resolve!: (v: unknown[]) => void
    list.mockReturnValue(new Promise((r) => (resolve = r)))
    const err = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const { result, unmount } = renderHook(() => useAreas())
    unmount()
    resolve([makeArea({ id: 'late' })])
    await Promise.resolve()
    await Promise.resolve()
    expect(result.current.loading).toBe(true)
    expect(err).not.toHaveBeenCalled()
    err.mockRestore()
  })
})
