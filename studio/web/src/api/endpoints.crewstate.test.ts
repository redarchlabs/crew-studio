import { afterEach, describe, expect, it, vi } from 'vitest'
import { connectionsApi, crewStateApi } from './endpoints.crewstate'

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

const body = { name: 'x' } as never
const key = { schedule_id: null, subject: 'project:atlas', name: 'weekly-report' }
const keyQs = 'schedule_id=none&subject=project%3Aatlas&name=weekly-report'

const CALLS: { name: string; run: () => Promise<unknown>; method: string; url: string; body?: unknown }[] = [
  { name: 'kinds', run: () => connectionsApi.kinds(), method: 'GET', url: '/connections/kinds' },
  { name: 'list', run: () => connectionsApi.list(), method: 'GET', url: '/connections' },
  { name: 'get', run: () => connectionsApi.get('c1'), method: 'GET', url: '/connections/c1' },
  { name: 'km2Folders', run: () => connectionsApi.km2Folders('c1'), method: 'GET', url: '/connections/c1/km2/folders' },
  { name: 'create', run: () => connectionsApi.create(body), method: 'POST', url: '/connections', body },
  { name: 'update', run: () => connectionsApi.update('c1', body), method: 'PUT', url: '/connections/c1', body },
  { name: 'replaceSecret', run: () => connectionsApi.replaceSecret('c1', { api_key: 'k' }), method: 'PUT', url: '/connections/c1/secret', body: { secrets: { api_key: 'k' } } },
  { name: 'test', run: () => connectionsApi.test('c1'), method: 'POST', url: '/connections/c1/test', body: {} },
  { name: 'remove', run: () => connectionsApi.remove('c1'), method: 'DELETE', url: '/connections/c1' },
  { name: 'tools', run: () => connectionsApi.tools('c1'), method: 'GET', url: '/connections/c1/tools' },
  { name: 'discoverTools', run: () => connectionsApi.discoverTools('c1'), method: 'POST', url: '/connections/c1/tools/discover', body: {} },
  { name: 'setTools', run: () => connectionsApi.setTools('c1', [{ name: 't', effect: 'reads', enabled: true }]), method: 'PUT', url: '/connections/c1/tools',
    body: { tools: [{ name: 't', effect: 'reads', enabled: true }] } },
  { name: 'notes', run: () => crewStateApi.notes('k1'), method: 'GET', url: '/crews/k1/state/notes' },
  { name: 'notes (crew-wide)', run: () => crewStateApi.notes('k1', 'none'), method: 'GET', url: '/crews/k1/state/notes?schedule_id=none' },
  { name: 'versions', run: () => crewStateApi.versions('k1', key), method: 'GET', url: `/crews/k1/state/notes/versions?${keyQs}` },
  { name: 'putNote', run: () => crewStateApi.putNote('k1', key, 'text', 3), method: 'PUT', url: '/crews/k1/state/notes', body: { ...key, content: 'text', expected_version: 3 } },
  { name: 'rollback', run: () => crewStateApi.rollback('k1', key, 1, 3), method: 'POST', url: '/crews/k1/state/notes/rollback', body: { ...key, version: 1, expected_version: 3 } },
  { name: 'deleteNote', run: () => crewStateApi.deleteNote('k1', { ...key, schedule_id: 's1' }), method: 'DELETE', url: '/crews/k1/state/notes?schedule_id=s1&subject=project%3Aatlas&name=weekly-report' },
  { name: 'log', run: () => crewStateApi.log('k1', { subject: 'PRJ', action: '', limit: 50, offset: 0 }), method: 'GET', url: '/crews/k1/state/log?subject=PRJ&limit=50&offset=0' },
  { name: 'deleteLogEntry', run: () => crewStateApi.deleteLogEntry('k1', 'e1'), method: 'DELETE', url: '/crews/k1/state/log/e1' },
  { name: 'connections', run: () => crewStateApi.connections('k1'), method: 'GET', url: '/crews/k1/connections' },
  { name: 'setConnections', run: () => crewStateApi.setConnections('k1', ['c1']), method: 'PUT', url: '/crews/k1/connections', body: { connection_ids: ['c1'] } },
]

describe('crew state and connection endpoints', () => {
  it.each(CALLS)('$name sends $method $url', async ({ run, method, url, body: expected }) => {
    const fn = stub([])
    await run()
    const [calledUrl, init] = fn.mock.calls[0]
    expect(calledUrl).toBe(`/api${url}`)
    expect(init?.method).toBe(method)
    if (expected === undefined) expect(init?.body).toBeUndefined()
    else expect(JSON.parse(init?.body as string)).toEqual(expected)
  })

  it.each([
    ['list', () => connectionsApi.list()],
    ['tools', () => connectionsApi.tools('c1')],
    ['setTools', () => connectionsApi.setTools('c1', [])],
    ['notes', () => crewStateApi.notes('k1')],
    ['versions', () => crewStateApi.versions('k1', key)],
    ['connections', () => crewStateApi.connections('k1')],
    ['setConnections', () => crewStateApi.setConnections('k1', [])],
  ])('%s tolerates a null payload', async (_n, run) => {
    stub(null)
    await expect(run()).resolves.toEqual([])
  })

  it('log returns items and paging meta', async () => {
    stub([{ id: 'e1' }])
    await expect(crewStateApi.log('k1', { limit: 5, offset: 0 })).resolves.toEqual({ items: [{ id: 'e1' }], meta: { total: 1, limit: 5, offset: 0 } })
  })
})
