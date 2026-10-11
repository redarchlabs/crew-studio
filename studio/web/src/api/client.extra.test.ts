import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, UNAUTHENTICATED_EVENT, errorMessage, isAbort, request, requestEnvelope, requestPage } from './client'

function res(status: number, body: unknown, raw = false): Response {
  return new Response(raw ? (body as string) : JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}
function mockFetch(r: Response | Error) {
  const fn = vi.fn<typeof fetch>(() => (r instanceof Response ? Promise.resolve(r) : Promise.reject(r)))
  vi.stubGlobal('fetch', fn)
  return fn
}
const ok = (data: unknown, extra: object = {}) => res(200, { success: true, data, error: null, ...extra })

afterEach(() => vi.unstubAllGlobals())

describe('request headers and bodies', () => {
  it('GET sends Accept, same-origin credentials, and no body or content type', async () => {
    const fn = mockFetch(ok({}))
    const ctrl = new AbortController()
    await request('GET', '/x', { signal: ctrl.signal, query: { a: 1 } })
    const [url, init] = fn.mock.calls[0]
    expect(url).toBe('/api/x?a=1')
    expect(init?.credentials).toBe('same-origin')
    expect(init?.signal).toBe(ctrl.signal)
    expect(init?.body).toBeUndefined()
    const h = init?.headers as Record<string, string>
    expect(h.Accept).toBe('application/json')
    expect(h['Content-Type']).toBeUndefined()
  })

  it('FormData bodies are passed through with X-Requested-With and no JSON content type', async () => {
    const fn = mockFetch(ok([]))
    const form = new FormData()
    form.append('file', new Blob(['x']), 'x.txt')
    await request('POST', '/up', { body: form })
    const init = fn.mock.calls[0][1]
    const h = init?.headers as Record<string, string>
    expect(init?.body).toBe(form)
    expect(h['X-Requested-With']).toBe('crewstudio')
    expect(h['Content-Type']).toBeUndefined()
  })

  it('JSON requests do not carry X-Requested-With', async () => {
    const fn = mockFetch(ok({}))
    await request('PUT', '/x', { body: { a: 1 } })
    expect((fn.mock.calls[0][1]?.headers as Record<string, string>)['X-Requested-With']).toBeUndefined()
  })

  it('serialises falsy-but-defined bodies', async () => {
    const fn = mockFetch(ok({}))
    await request('POST', '/x', { body: 0 })
    expect(fn.mock.calls[0][1]?.body).toBe('0')
  })

  it('DELETE without a body still declares JSON', async () => {
    const fn = mockFetch(new Response(null, { status: 204 }))
    await request('DELETE', '/x')
    expect((fn.mock.calls[0][1]?.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(fn.mock.calls[0][1]?.body).toBeUndefined()
  })
})

describe('401 handling', () => {
  it('dispatches UNAUTHENTICATED_EVENT on a 401 from a normal route', async () => {
    mockFetch(res(401, { success: false, data: null, error: { code: 'unauthenticated', message: 'No session' } }))
    const handler = vi.fn()
    window.addEventListener(UNAUTHENTICATED_EVENT, handler)
    try {
      await expect(request('GET', '/runs')).rejects.toMatchObject({ status: 401, code: 'unauthenticated', message: 'No session' })
      expect(handler).toHaveBeenCalledTimes(1)
    } finally {
      window.removeEventListener(UNAUTHENTICATED_EVENT, handler)
    }
  })

  it('does not dispatch for /auth/ routes (a failed login is not an ended session)', async () => {
    mockFetch(res(401, { success: false, data: null, error: { code: 'bad_credentials', message: 'Wrong password' } }))
    const handler = vi.fn()
    window.addEventListener(UNAUTHENTICATED_EVENT, handler)
    try {
      await expect(request('POST', '/auth/login', { body: {} })).rejects.toMatchObject({ code: 'bad_credentials' })
      expect(handler).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener(UNAUTHENTICATED_EVENT, handler)
    }
  })

  it('uses the status fallback message for a non-envelope 401 and still dispatches', async () => {
    mockFetch(new Response('', { status: 401 }))
    const handler = vi.fn()
    window.addEventListener(UNAUTHENTICATED_EVENT, handler)
    try {
      await expect(request('GET', '/runs')).rejects.toMatchObject({ code: 'http_401', message: 'Your session has ended. Sign in again.' })
      expect(handler).toHaveBeenCalledTimes(1)
    } finally {
      window.removeEventListener(UNAUTHENTICATED_EVENT, handler)
    }
  })
})

describe('error mapping', () => {
  it.each([
    [400, 'The request was malformed.'],
    [403, 'You do not have permission to do that.'],
    [404, 'Not found.'],
    [409, 'Conflict: the name is taken or the item is still referenced.'],
    [413, 'That file is too large.'],
    [415, 'The server rejected the content type.'],
    [422, 'Validation failed.'],
    [500, 'The server hit an internal error.'],
    [503, 'The Crew Studio API is unavailable.'],
    [504, 'The Crew Studio API timed out.'],
    [418, 'Request failed with status 418.'],
  ])('falls back to a friendly message for HTTP %i', async (status, message) => {
    mockFetch(new Response('not json', { status }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ status, code: `http_${status}`, message })
  })

  it('502 hints that the backend may not be running', async () => {
    mockFetch(new Response('', { status: 502 }))
    await expect(request('GET', '/x')).rejects.toThrow(/backend running/)
  })

  it('uses the status fallback when an error envelope has a malformed error object', async () => {
    mockFetch(res(404, { success: false, data: null, error: 'boom' }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'http_404', message: 'Not found.', details: [] })
  })

  it('keeps the server code but falls back to the status message when message is empty or not a string', async () => {
    mockFetch(res(409, { success: false, data: null, error: { code: 'conflict', message: '' } }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'conflict', message: expect.stringContaining('Conflict') })
    mockFetch(res(409, { success: false, data: null, error: { code: 7, message: 3 } }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'http_409' })
  })

  it('filters non-string details', async () => {
    mockFetch(res(422, { success: false, data: null, error: { code: 'v', message: 'm', details: ['a: b', 5, null, 'c: d'] } }))
    const err = (await request('POST', '/x', { body: {} }).catch((e: unknown) => e)) as ApiError
    expect(err.details).toEqual(['a: b', 'c: d'])
  })

  it('maps a 200 success:false without an error object to unknown_error / 500', async () => {
    mockFetch(res(200, { success: false, data: null }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ status: 500, code: 'unknown_error', message: 'The request failed.' })
  })

  it('rejects a 200 with a non-envelope body as bad_response', async () => {
    mockFetch(res(200, { hello: 'world' }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ status: 200, code: 'bad_response' })
  })

  it('rejects a 200 with an array or empty body as bad_response', async () => {
    mockFetch(res(200, []))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'bad_response' })
    mockFetch(new Response('', { status: 200 }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'bad_response' })
  })

  it('rejects an envelope that lacks a data key', async () => {
    mockFetch(res(200, { success: true }))
    await expect(request('GET', '/x')).rejects.toMatchObject({ code: 'bad_response' })
  })

  it('an ApiError has the right name and default empty details', () => {
    const e = new ApiError(500, { code: 'c', message: 'm' })
    expect(e).toBeInstanceOf(Error)
    expect(e.name).toBe('ApiError')
    expect(e.details).toEqual([])
  })

  it('rethrows aborts untouched instead of wrapping them as network errors', async () => {
    const abort = new DOMException('Aborted', 'AbortError')
    mockFetch(abort)
    const err = await request('GET', '/x').catch((e: unknown) => e)
    expect(err).toBe(abort)
    expect(isAbort(err)).toBe(true)
  })
})

describe('requestEnvelope / requestPage', () => {
  it('returns meta alongside data', async () => {
    mockFetch(ok([1], { meta: { total: 3, limit: 1, offset: 2 } }))
    await expect(requestEnvelope('GET', '/x')).resolves.toEqual({ data: [1], meta: { total: 3, limit: 1, offset: 2 } })
  })

  it('synthesises meta from the item count when the server omits it', async () => {
    mockFetch(ok([1, 2]))
    await expect(requestPage('/x')).resolves.toEqual({ items: [1, 2], meta: { total: 2, limit: 2, offset: 0 } })
  })
})

describe('errorMessage / isAbort', () => {
  it('extracts messages from ApiError and Error, and falls back for other values', () => {
    expect(errorMessage(new ApiError(400, { code: 'c', message: 'api says' }))).toBe('api says')
    expect(errorMessage(new Error('plain'))).toBe('plain')
    expect(errorMessage('str')).toBe('Something went wrong.')
    expect(errorMessage(null)).toBe('Something went wrong.')
  })

  it('isAbort only matches AbortError DOMExceptions', () => {
    expect(isAbort(new DOMException('x', 'AbortError'))).toBe(true)
    expect(isAbort(new DOMException('x', 'NotFoundError'))).toBe(false)
    expect(isAbort(new Error('AbortError'))).toBe(false)
    expect(isAbort(undefined)).toBe(false)
  })
})
