import { describe, expect, it } from 'vitest'
import { prepareRequest, type SwaggerRequest } from './prepareRequest'

describe('prepareRequest', () => {
  it('marks multipart uploads and keeps the session cookie', () => {
    const form = new FormData()
    const out = prepareRequest<SwaggerRequest>({ url: '/api/work-orders/x/attachments', headers: {}, body: form })
    expect(out.headers['X-Requested-With']).toBe('crewstudio')
    expect(out.credentials).toBe('same-origin')
  })
  it('leaves JSON requests alone and does not mutate the input', () => {
    const req: SwaggerRequest = { url: '/api/crews', headers: { 'Content-Type': 'application/json' }, body: '{}' }
    const out = prepareRequest(req)
    expect(out.headers['X-Requested-With']).toBeUndefined()
    expect(req.headers).toEqual({ 'Content-Type': 'application/json' })
  })
})
