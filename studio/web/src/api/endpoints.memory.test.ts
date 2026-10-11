import { afterEach, describe, expect, it, vi } from 'vitest'
import { memoryApi } from './endpoints.memory'

function stub(data: unknown = {}) {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(
      new Response(JSON.stringify({ success: true, data, error: null, meta: { total: 1, limit: 5, offset: 0 } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

const settings = { mode: 'recall_only' } as never
const input = { content: 'Sam replies late on Fridays', scope: '/people/sam' }

const CALLS: { name: string; run: () => Promise<unknown>; method: string; url: string; body?: unknown }[] = [
  { name: 'settings', run: () => memoryApi.settings('k1'), method: 'GET', url: '/crews/k1/memory/settings' },
  { name: 'saveSettings', run: () => memoryApi.saveSettings('k1', settings), method: 'PUT', url: '/crews/k1/memory/settings', body: settings },
  { name: 'list', run: () => memoryApi.list('k1', { limit: 25, offset: 0 }), method: 'GET', url: '/crews/k1/memory?limit=25&offset=0' },
  {
    name: 'list (filtered)',
    run: () => memoryApi.list('k1', { q: 'friday', scope: '/people', status: 'pending', pinned: true, run_id: 'r1', limit: 25, offset: 25 }),
    method: 'GET',
    url: '/crews/k1/memory?q=friday&scope=%2Fpeople&status=pending&run_id=r1&limit=25&offset=25&pinned=true',
  },
  { name: 'list (not pinned only)', run: () => memoryApi.list('k1', { pinned: false, status: '', limit: 5, offset: 0 }), method: 'GET', url: '/crews/k1/memory?limit=5&offset=0' },
  { name: 'scopes', run: () => memoryApi.scopes('k1'), method: 'GET', url: '/crews/k1/memory/scopes' },
  { name: 'create', run: () => memoryApi.create('k1', input), method: 'POST', url: '/crews/k1/memory', body: input },
  { name: 'update', run: () => memoryApi.update('k1', 'm1', { pinned: true }), method: 'PATCH', url: '/crews/k1/memory/m1', body: { pinned: true } },
  { name: 'approve', run: () => memoryApi.approve('k1', 'm1'), method: 'PATCH', url: '/crews/k1/memory/m1', body: { status: 'active' } },
  { name: 'remove', run: () => memoryApi.remove('k1', 'm1'), method: 'DELETE', url: '/crews/k1/memory/m1' },
  {
    name: 'bulkDelete',
    run: () => memoryApi.bulkDelete('k1', { scope_prefix: '/people/sam', dry_run: true }),
    method: 'POST',
    url: '/crews/k1/memory/delete',
    body: { scope_prefix: '/people/sam', dry_run: true },
  },
  {
    name: 'promote',
    run: () => memoryApi.promote('k1', { memory_ids: ['m1'], title: 'Vendor lessons' }),
    method: 'POST',
    url: '/crews/k1/memory/promote',
    body: { memory_ids: ['m1'], title: 'Vendor lessons' },
  },
  { name: 'promoteFolders', run: () => memoryApi.promoteFolders('k1'), method: 'GET', url: '/crews/k1/memory/promote/folders' },
]

describe('memory endpoints', () => {
  it.each(CALLS)('$name sends $method $url', async ({ run, method, url, body: expected }) => {
    const fn = stub([])
    await run()
    const [calledUrl, init] = fn.mock.calls[0]
    expect(calledUrl).toBe(`/api${url}`)
    expect(init?.method).toBe(method)
    if (expected === undefined) expect(init?.body).toBeUndefined()
    else expect(JSON.parse(init?.body as string)).toEqual(expected)
  })

  it('scopes tolerates a null payload', async () => {
    stub(null)
    await expect(memoryApi.scopes('k1')).resolves.toEqual([])
  })

  it('list returns items and paging meta', async () => {
    stub([{ id: 'm1' }])
    await expect(memoryApi.list('k1', { limit: 5, offset: 0 })).resolves.toEqual({ items: [{ id: 'm1' }], meta: { total: 1, limit: 5, offset: 0 } })
  })
})
