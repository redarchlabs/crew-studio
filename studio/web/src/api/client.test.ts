// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, buildUrl, request, requestPage } from './client'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function mockFetch(res: Response | Error) {
  const fn = vi.fn<typeof fetch>(() => (res instanceof Error ? Promise.reject(res) : Promise.resolve(res)))
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('buildUrl', () => {
  it('prefixes /api and drops empty query values', () => {
    expect(buildUrl('/runs', { crew_id: '', limit: 50, offset: 0, x: undefined })).toBe('/api/runs?limit=50&offset=0')
    expect(buildUrl('/llms')).toBe('/api/llms')
  })
})

describe('request', () => {
  it('unwraps the data from a successful envelope', async () => {
    mockFetch(jsonResponse(200, { success: true, data: { id: 'a' }, error: null }))
    await expect(request<{ id: string }>('GET', '/llms/a')).resolves.toEqual({ id: 'a' })
  })

  it('sends JSON content type and body on mutations', async () => {
    const fn = mockFetch(jsonResponse(201, { success: true, data: { id: 'b' }, error: null }))
    await request('POST', '/llms', { body: { name: 'x' } })
    const [url, init] = fn.mock.calls[0]
    expect(url).toBe('/api/llms')
    expect(init?.method).toBe('POST')
    expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(init?.body).toBe('{"name":"x"}')
  })

  it('sends JSON content type on body-less POST (cancel)', async () => {
    const fn = mockFetch(jsonResponse(200, { success: true, data: {}, error: null }))
    await request('POST', '/runs/r/cancel')
    const init = fn.mock.calls[0][1]
    expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json')
  })

  it('throws ApiError with code, message and details on failure', async () => {
    mockFetch(
      jsonResponse(422, {
        success: false,
        data: null,
        error: { code: 'validation_failed', message: 'Invalid agent', details: ['name: required'] },
      }),
    )
    const err = await request('POST', '/agents', { body: {} }).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    const apiErr = err as ApiError
    expect(apiErr.status).toBe(422)
    expect(apiErr.code).toBe('validation_failed')
    expect(apiErr.message).toBe('Invalid agent')
    expect(apiErr.details).toEqual(['name: required'])
  })

  it('treats success:false with a 200 status as an error', async () => {
    mockFetch(jsonResponse(200, { success: false, data: null, error: { code: 'weird', message: 'nope' } }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'weird', message: 'nope' })
  })

  it('returns undefined for 204 No Content', async () => {
    mockFetch(new Response(null, { status: 204 }))
    await expect(request('DELETE', '/llms/a')).resolves.toBeUndefined()
  })

  it('maps non-envelope error responses to a status-based ApiError', async () => {
    mockFetch(new Response('<html>Bad gateway</html>', { status: 502 }))
    await expect(request('GET', '/llms')).rejects.toMatchObject({ status: 502, code: 'http_502' })
  })

  it('maps network failures to a network_error ApiError', async () => {
    mockFetch(new TypeError('Failed to fetch'))
    await expect(request('GET', '/llms')).rejects.toMatchObject({ status: 0, code: 'network_error' })
  })
})

describe('requestPage', () => {
  it('returns items and meta', async () => {
    mockFetch(
      jsonResponse(200, { success: true, data: [{ id: 1 }], error: null, meta: { total: 9, limit: 1, offset: 0 } }),
    )
    await expect(requestPage('/history')).resolves.toEqual({
      items: [{ id: 1 }],
      meta: { total: 9, limit: 1, offset: 0 },
    })
  })

  it('treats null data as an empty page', async () => {
    mockFetch(jsonResponse(200, { success: true, data: null, error: null }))
    await expect(requestPage('/runs')).resolves.toEqual({ items: [], meta: { total: 0, limit: 0, offset: 0 } })
  })
})
