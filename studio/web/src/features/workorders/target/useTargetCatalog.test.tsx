import { beforeEach, describe, expect, it, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { ApiError } from '../../../api/client'
import { crewsApi, tasksApi } from '../../../api/endpoints'
import { makeCrew, makeTask } from '../../../lib/testFixtures'
import { loadTargetCatalog, useTargetCatalog } from './useTargetCatalog'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())

const tasks = vi.mocked(tasksApi.list)
const crews = vi.mocked(crewsApi.list)

const forbidden = () => new ApiError(403, { code: 'forbidden', message: 'no' })

beforeEach(() => {
  tasks.mockReset()
  crews.mockReset()
})

describe('loadTargetCatalog', () => {
  it('loads crews (sorted by name) and tasks', async () => {
    tasks.mockResolvedValue([makeTask({ id: 'b', name: 'Beta' }), makeTask({ id: 'a', name: 'Alpha' })])
    crews.mockResolvedValue([makeCrew({ id: 'z', name: 'Zed' }), makeCrew({ id: 'y', name: 'Yan' })])
    const data = await loadTargetCatalog()
    expect(data.crews.map((c) => c.id)).toEqual(['y', 'z'])
    expect(data.tasks.map((t) => t.id)).toEqual(['b', 'a'])
    expect(data.forbidden).toEqual({ crews: false, tasks: false })
  })

  it('treats a 403 as an empty, forbidden list', async () => {
    tasks.mockRejectedValue(forbidden())
    crews.mockRejectedValue(forbidden())
    const data = await loadTargetCatalog()
    expect(data.tasks).toEqual([])
    expect(data.crews).toEqual([])
    expect(data.forbidden).toEqual({ crews: true, tasks: true })
  })

  it('rethrows other API errors and plain errors', async () => {
    tasks.mockRejectedValue(new ApiError(500, { code: 'x', message: 'server' }))
    crews.mockResolvedValue([])
    await expect(loadTargetCatalog()).rejects.toThrow('server')
    tasks.mockRejectedValue(new Error('network'))
    await expect(loadTargetCatalog()).rejects.toThrow('network')
  })

  it('passes the abort signal to each request', async () => {
    tasks.mockResolvedValue([])
    crews.mockResolvedValue([])
    const ctrl = new AbortController()
    await loadTargetCatalog(ctrl.signal)
    expect(tasks).toHaveBeenCalledWith(ctrl.signal)
    expect(crews).toHaveBeenCalledWith(ctrl.signal)
  })
})

describe('useTargetCatalog', () => {
  it('exposes loading then data', async () => {
    tasks.mockResolvedValue([])
    crews.mockResolvedValue([makeCrew({ id: 'a' })])
    const { result } = renderHook(() => useTargetCatalog())
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.data?.crews).toHaveLength(1))
  })
})
