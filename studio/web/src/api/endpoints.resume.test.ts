import { afterEach, describe, expect, it, vi } from 'vitest'
import { resumeApi } from './endpoints.resume'

function stub() {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(new Response(JSON.stringify({ success: true, data: { id: 'r2' }, error: null }), {
      status: 201, headers: { 'Content-Type': 'application/json' },
    })),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

describe('resumeApi', () => {
  it('posts the resume with its options', async () => {
    const fetchFn = stub()
    expect(await resumeApi.resume('r1', { budget_usd: 2, use_current: true })).toEqual({ id: 'r2' })
    const [url, init] = fetchFn.mock.calls[0]
    expect(String(url)).toContain('/api/runs/r1/resume')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ budget_usd: 2, use_current: true })
  })

  it('sends an empty body by default', async () => {
    const fetchFn = stub()
    await resumeApi.resume('r1')
    expect(JSON.parse(String(fetchFn.mock.calls[0][1]?.body))).toEqual({})
  })
})
