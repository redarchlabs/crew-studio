import { describe, expect, it } from 'vitest'
import { prepareRequest } from './prepareRequest'

describe('prepareRequest (more cases)', () => {
  it('adds X-Requested-With for a FormData body even without a content-type header', () => {
    const req = prepareRequest({ url: '/api/x', headers: {} as Record<string, string>, body: new FormData() })
    expect(req.headers['X-Requested-With']).toBe('crewstudio')
  })

  it('matches the multipart content type case-insensitively on the header name', () => {
    const req = prepareRequest({ url: '/x', headers: { 'content-type': 'multipart/form-data; boundary=abc' } as Record<string, string> })
    expect(req.headers['X-Requested-With']).toBe('crewstudio')
  })

  it('does not add the header for JSON or when there is no content type', () => {
    expect(prepareRequest({ url: '/x', headers: { 'Content-Type': 'application/json' } as Record<string, string>, body: '{}' }).headers['X-Requested-With']).toBeUndefined()
    expect(prepareRequest({ url: '/x', headers: {} as Record<string, string> }).headers['X-Requested-With']).toBeUndefined()
  })

  it('always forces same-origin credentials, even over a caller value', () => {
    expect(prepareRequest({ url: '/x', headers: {} as Record<string, string>, credentials: 'include' }).credentials).toBe('same-origin')
  })

  it('does not mutate the incoming request or headers, and keeps other fields', () => {
    const headers: Record<string, string> = { 'Content-Type': 'multipart/form-data' }
    const req = { url: '/x', method: 'POST', headers, extra: 1 }
    const out = prepareRequest(req)
    expect(headers).toEqual({ 'Content-Type': 'multipart/form-data' })
    expect(req).not.toHaveProperty('credentials')
    expect(out).not.toBe(req)
    expect(out.headers).not.toBe(headers)
    expect(out).toMatchObject({ url: '/x', method: 'POST', extra: 1 })
  })
})
