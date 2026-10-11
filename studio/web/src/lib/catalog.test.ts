import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Permission } from '../api/types'

vi.mock('../api/endpoints', () => {
  const list = () => vi.fn((_s?: AbortSignal) => Promise.resolve([] as unknown[]))
  return {
    agentsApi: { list: list() },
    tasksApi: { list: list() },
    crewsApi: { list: list() },
    llmsApi: { list: list() },
    toolsApi: { list: list() },
    pricingApi: { list: list() },
  }
})

import * as endpoints from '../api/endpoints'
import { byId, loadCatalog, sortByName } from './catalog'
import { loadVisibleCatalog, visibleCollections } from './visibleCatalog'

const apis = [endpoints.agentsApi, endpoints.tasksApi, endpoints.crewsApi, endpoints.llmsApi, endpoints.toolsApi, endpoints.pricingApi]
const listMock = (i: number) => apis[i].list as unknown as ReturnType<typeof vi.fn>

beforeEach(() => {
  apis.forEach((_, i) => listMock(i).mockClear())
})

describe('loadCatalog', () => {
  it('loads every collection and forwards the abort signal', async () => {
    const data = ['agents', 'tasks', 'crews', 'llms', 'tools', 'pricing']
    data.forEach((d, i) => listMock(i).mockResolvedValueOnce([d]))
    const ctrl = new AbortController()
    await expect(loadCatalog(ctrl.signal)).resolves.toEqual({
      agents: ['agents'], tasks: ['tasks'], crews: ['crews'], llms: ['llms'], tools: ['tools'], pricing: ['pricing'],
    })
    apis.forEach((_, i) => expect(listMock(i)).toHaveBeenCalledWith(ctrl.signal))
  })

  it('rejects if any one collection fails', async () => {
    listMock(3).mockRejectedValueOnce(new Error('llms down'))
    await expect(loadCatalog()).rejects.toThrow('llms down')
  })
})

describe('visibleCollections', () => {
  it('maps each collection to its view permission (tools ride on agents.view)', () => {
    const calls: Permission[] = []
    const can = (p: Permission) => {
      calls.push(p)
      return p === 'agents.view' || p === 'pricing.view'
    }
    expect(visibleCollections(can)).toEqual({ agents: true, tasks: false, crews: false, llms: false, tools: true, pricing: true })
    expect(calls).toEqual(expect.arrayContaining(['agents.view', 'tasks.view', 'crews.view', 'llms.view', 'pricing.view']))
  })
})

describe('loadVisibleCatalog', () => {
  it('only requests permitted collections and returns empty arrays for the rest', async () => {
    listMock(2).mockResolvedValueOnce(['crew'])
    const cat = await loadVisibleCatalog((p) => p === 'crews.view')
    expect(cat).toEqual({ agents: [], tasks: [], crews: ['crew'], llms: [], tools: [], pricing: [] })
    expect(listMock(2)).toHaveBeenCalledTimes(1)
    for (const i of [0, 1, 3, 4, 5]) expect(listMock(i)).not.toHaveBeenCalled()
  })

  it('requests everything when everything is permitted', async () => {
    await loadVisibleCatalog(() => true)
    apis.forEach((_, i) => expect(listMock(i)).toHaveBeenCalledTimes(1))
  })

  it('requests nothing for someone with no view permissions', async () => {
    await expect(loadVisibleCatalog(() => false)).resolves.toEqual({ agents: [], tasks: [], crews: [], llms: [], tools: [], pricing: [] })
    apis.forEach((_, i) => expect(listMock(i)).not.toHaveBeenCalled())
  })
})

describe('byId / sortByName', () => {
  it('byId indexes items by id', () => {
    const m = byId([{ id: 'a', v: 1 }, { id: 'b', v: 2 }])
    expect(m.get('b')?.v).toBe(2)
    expect(m.size).toBe(2)
  })
  it('sortByName sorts without mutating the input', () => {
    const input = [{ name: 'b' }, { name: 'A' }, { name: 'c' }]
    expect(sortByName(input).map((i) => i.name)).toEqual(['A', 'b', 'c'])
    expect(input.map((i) => i.name)).toEqual(['b', 'A', 'c'])
  })
})
