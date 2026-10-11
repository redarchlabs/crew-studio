import { describe, expect, it } from 'vitest'
import { curlExample, exampleIdempotencyKey, samplePayload, shellQuote } from './hookExamples'

describe('samplePayload', () => {
  it('builds nested objects from dotted paths', () => {
    expect(samplePayload({ company: 'customer.name', region: 'customer.address.region', topic: 'topic' })).toEqual({
      customer: { name: '<company>', address: { region: '<region>' } },
      topic: '<topic>',
    })
  })

  it('creates arrays for numeric segments', () => {
    expect(samplePayload({ first: 'items.0.name', second: 'items.1.name' })).toEqual({ items: [{ name: '<first>' }, { name: '<second>' }] })
  })

  it('skips empty paths and falls back to a generic body', () => {
    expect(samplePayload({ a: '  ' })).toEqual({ event: 'example', message: 'Anything JSON up to 1 MB' })
  })
})

describe('curlExample', () => {
  it('includes the token, JSON content type, idempotency key and body', () => {
    const cmd = curlExample('https://studio.example/api/hooks/t1', 'cst_ab_secret', { name: "O'Brien" }, 'event-1')
    expect(cmd).toContain("curl -X POST 'https://studio.example/api/hooks/t1'")
    expect(cmd).toContain("-H 'Authorization: Bearer cst_ab_secret'")
    expect(cmd).toContain("-H 'Content-Type: application/json'")
    expect(cmd).toContain("-H 'Idempotency-Key: event-1'")
    expect(cmd).toContain(`--data '{"name":"O'\\''Brien"}'`)
  })

  it('quotes for the shell', () => {
    expect(shellQuote("it's")).toBe(`'it'\\''s'`)
  })

  it('makes example keys', () => {
    expect(exampleIdempotencyKey(() => 0.5)).toBe('event-80008000')
  })
})
