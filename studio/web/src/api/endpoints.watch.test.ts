import { afterEach, describe, expect, it, vi } from 'vitest'
import { watchesApi } from './endpoints.watch'

function stub(data: unknown = {}) {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(new Response(JSON.stringify({ success: true, data, error: null }), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

const body = { source: { kind: 'web_page', url: 'https://x' } } as never

const CALLS: { name: string; run: () => Promise<unknown>; method: string; url: string; body?: unknown }[] = [
  { name: 'get', run: () => watchesApi.get('s1'), method: 'GET', url: '/schedules/s1/watch' },
  { name: 'save', run: () => watchesApi.save('s1', body), method: 'PUT', url: '/schedules/s1/watch', body },
  { name: 'remove', run: () => watchesApi.remove('s1'), method: 'DELETE', url: '/schedules/s1/watch' },
  { name: 'preview', run: () => watchesApi.preview('s1', body), method: 'POST', url: '/schedules/s1/watch/preview', body },
  { name: 'clearSnapshot', run: () => watchesApi.clearSnapshot('s1'), method: 'DELETE', url: '/schedules/s1/watch/snapshot' },
  { name: 'ofCrew', run: () => watchesApi.ofCrew('c1'), method: 'GET', url: '/crews/c1/state/watches' },
]

describe('watch endpoints', () => {
  it.each(CALLS)('$name sends $method $url', async ({ run, method, url, body: expected }) => {
    const fn = stub([])
    await run()
    const [calledUrl, init] = fn.mock.calls[0]
    expect(calledUrl).toBe(`/api${url}`)
    expect(init?.method).toBe(method)
    if (expected === undefined) expect(init?.body).toBeUndefined()
    else expect(JSON.parse(init?.body as string)).toEqual(expected)
  })

  it('ofCrew tolerates a null payload', async () => {
    stub(null)
    await expect(watchesApi.ofCrew('c1')).resolves.toEqual([])
  })
})
