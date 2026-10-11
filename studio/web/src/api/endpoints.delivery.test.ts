import { afterEach, describe, expect, it, vi } from 'vitest'
import { destinationsApi, pmoApi, reportsApi } from './endpoints.delivery'
import type { PMORequest, ReportDestinationInput } from './types.delivery'

function stub(data: unknown = {}) {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(new Response(JSON.stringify({ success: true, data, error: null }), { status: 200, headers: { 'Content-Type': 'application/json' } })),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

const dest: ReportDestinationInput = { kind: 'email', connection_id: 'c1', recipients: ['a@example.com'], folder: '' }
const pmo: PMORequest = { name: 'PMO', area_ids: ['a'], tracker_connection_id: 't', timezone: 'UTC', coordinator_llm_id: null, reporter_llm_id: null, destinations: [], enabled: true }

const CALLS: { name: string; run: () => Promise<unknown>; method: string; url: string; body?: unknown }[] = [
  { name: 'destinations', run: () => destinationsApi.get('s1'), method: 'GET', url: '/schedules/s1/report-destinations' },
  { name: 'set destinations', run: () => destinationsApi.put('s1', [dest]), method: 'PUT', url: '/schedules/s1/report-destinations', body: { destinations: [dest] } },
  { name: 'reports', run: () => reportsApi.list('w1'), method: 'GET', url: '/work-orders/w1/reports' },
  { name: 'retry', run: () => reportsApi.retry('w1'), method: 'POST', url: '/work-orders/w1/reports/retry', body: {} },
  { name: 'pmo options', run: () => pmoApi.options(), method: 'GET', url: '/pmo/setup' },
  { name: 'pmo install', run: () => pmoApi.install(pmo), method: 'POST', url: '/pmo/setup', body: pmo },
]

describe('delivery endpoints', () => {
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
