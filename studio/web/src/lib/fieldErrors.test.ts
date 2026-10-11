import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/client'
import { NO_ERRORS, parseFieldErrors } from './fieldErrors'

const err = (details: string[]) => new ApiError(422, { code: 'validation_failed', message: 'x', details })

describe('parseFieldErrors', () => {
  const known = ['name', 'inputs', 'tools'] as const
  it('returns NO_ERRORS for non-ApiErrors and for errors without details', () => {
    expect(parseFieldErrors(new Error('x'), known)).toBe(NO_ERRORS)
    expect(parseFieldErrors(null, known)).toBe(NO_ERRORS)
    expect(parseFieldErrors(err([]), known)).toBe(NO_ERRORS)
  })
  it('maps "field: problem" onto known fields', () => {
    expect(parseFieldErrors(err(['name: is required']), known)).toEqual({ fields: { name: 'is required' }, general: [] })
  })
  it('reduces nested paths to their root field', () => {
    const r = parseFieldErrors(err(['inputs[2].key: duplicate', 'tools.0: unknown tool']), known)
    expect(r.fields).toEqual({ inputs: 'duplicate', tools: 'unknown tool' })
  })
  it('joins multiple messages for the same field', () => {
    expect(parseFieldErrors(err(['name: too short', 'name: bad chars']), known).fields.name).toBe('too short; bad chars')
  })
  it('sends unknown fields and colon-less details to general with the raw text', () => {
    const r = parseFieldErrors(err(['colour: invalid', 'something broke', ': leading colon']), known)
    expect(r.fields).toEqual({})
    expect(r.general).toEqual(['colour: invalid', 'something broke', ': leading colon'])
  })
  it('keeps message text containing further colons intact', () => {
    expect(parseFieldErrors(err(['name: a: b']), known).fields.name).toBe('a: b')
  })
})
