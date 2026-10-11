import { afterEach, describe, expect, it, vi } from 'vitest'
import { peopleApi, trackingApi } from './endpoints.trackers'

function stub(data: unknown = {}) {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(new Response(JSON.stringify({ success: true, data, error: null }), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

const item = { connection_id: 'c', item_ref: 'PRJ-10', project: '', include_children: 'all', note: '', lead_account_id: '', schedule_id: null } as const

const CALLS: { name: string; run: () => Promise<unknown>; method: string; url: string; body?: unknown }[] = [
  { name: 'list', run: () => trackingApi.list('k1'), method: 'GET', url: '/crews/k1/state/tracking' },
  { name: 'list with history', run: () => trackingApi.list('k1', true), method: 'GET', url: '/crews/k1/state/tracking?include_removed=true' },
  { name: 'check', run: () => trackingApi.check('k1', item), method: 'POST', url: '/crews/k1/state/tracking/check', body: item },
  { name: 'add', run: () => trackingApi.add('k1', item), method: 'POST', url: '/crews/k1/state/tracking', body: item },
  { name: 'remove', run: () => trackingApi.remove('k1', 't1'), method: 'DELETE', url: '/crews/k1/state/tracking/t1' },
  { name: 'people', run: () => peopleApi.list('c1'), method: 'GET', url: '/connections/c1/people' },
  { name: 'sync', run: () => peopleApi.sync('c1'), method: 'POST', url: '/connections/c1/people/sync', body: {} },
  { name: 'link', run: () => peopleApi.link('c1', '712020:a/b', 'u1'), method: 'PUT', url: '/connections/c1/people/712020%3Aa%2Fb', body: { user_id: 'u1' } },
  { name: 'unlink', run: () => peopleApi.link('c1', 'a', null), method: 'PUT', url: '/connections/c1/people/a', body: { user_id: null } },
]

describe('tracker endpoints', () => {
  it.each(CALLS)('$name sends $method $url', async ({ run, method, url, body: expected }) => {
    const fn = stub({})
    await run()
    const [calledUrl, init] = fn.mock.calls[0]
    expect(calledUrl).toBe(`/api${url}`)
    expect(init?.method).toBe(method)
    if (expected === undefined) expect(init?.body).toBeUndefined()
    else expect(JSON.parse(init?.body as string)).toEqual(expected)
  })
})
