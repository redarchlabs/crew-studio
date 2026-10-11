import { describe, expect, it } from 'vitest'
import { ApiError } from '../../api/client'
import { passwordLoginError, passwordProblem, sanitizeReturnTo, ssoErrorMessage, validateSetup } from './authLogic'

describe('sanitizeReturnTo', () => {
  it('keeps same-site paths with query and hash', () => {
    expect(sanitizeReturnTo('/work-orders/123')).toBe('/work-orders/123')
    expect(sanitizeReturnTo('/runs?status=failed#top')).toBe('/runs?status=failed#top')
  })

  it('defaults to / when missing', () => {
    expect(sanitizeReturnTo(null)).toBe('/')
    expect(sanitizeReturnTo('')).toBe('/')
  })

  it('rejects absolute and protocol-relative URLs', () => {
    expect(sanitizeReturnTo('https://evil.example/')).toBe('/')
    expect(sanitizeReturnTo('//evil.example')).toBe('/')
    expect(sanitizeReturnTo('/\\evil.example')).toBe('/')
    expect(sanitizeReturnTo('javascript:alert(1)')).toBe('/')
    expect(sanitizeReturnTo('crews')).toBe('/')
  })

  it('rejects control characters that browsers strip', () => {
    expect(sanitizeReturnTo('/\t/evil.example')).toBe('/')
    expect(sanitizeReturnTo('/crews\n')).toBe('/crews')
    expect(sanitizeReturnTo('/a\u0000b')).toBe('/')
  })

  it('never returns to the sign-in or setup screens', () => {
    expect(sanitizeReturnTo('/login?return_to=/x')).toBe('/')
    expect(sanitizeReturnTo('/setup')).toBe('/')
    expect(sanitizeReturnTo('/loginhelp')).toBe('/loginhelp')
  })
})

describe('ssoErrorMessage', () => {
  it('explains known codes', () => {
    expect(ssoErrorMessage('not_invited')).toBe("Your account hasn't been set up yet. Ask an administrator to invite you.")
    expect(ssoErrorMessage('expired')).toMatch(/expired/)
  })
  it('has a fallback and is null without a code', () => {
    expect(ssoErrorMessage('weird')).toMatch(/did not complete/)
    expect(ssoErrorMessage(null)).toBeNull()
  })
})

describe('passwordLoginError', () => {
  it('maps status codes to friendly text', () => {
    expect(passwordLoginError(new ApiError(401, { code: 'invalid_credentials', message: 'x' }))).toMatch(/don't match/)
    expect(passwordLoginError(new ApiError(423, { code: 'locked', message: 'x' }))).toMatch(/locked for 15 minutes/)
    expect(passwordLoginError(new ApiError(429, { code: 'rate_limited', message: 'x' }))).toMatch(/Too many/)
  })
  it('falls back to the error message', () => {
    expect(passwordLoginError(new Error('boom'))).toBe('boom')
  })
})

describe('setup validation', () => {
  const ok = { token: 't', email: 'a@b.co', name: 'Ada', password: 'x'.repeat(12), confirm: 'x'.repeat(12) }
  it('accepts a valid form', () => {
    expect(validateSetup(ok)).toEqual({})
  })
  it('enforces password length 12-72', () => {
    expect(passwordProblem('x'.repeat(11))).toMatch(/at least 12/)
    expect(passwordProblem('x'.repeat(73))).toMatch(/at most 72/)
    expect(passwordProblem('x'.repeat(72))).toBeUndefined()
  })
  it('flags every missing field and a mismatched confirmation', () => {
    expect(Object.keys(validateSetup({ token: '', email: 'nope', name: ' ', password: 'short', confirm: '' })).sort()).toEqual(['email', 'name', 'password', 'token'])
    expect(validateSetup({ ...ok, confirm: 'y'.repeat(12) }).confirm).toMatch(/do not match/)
  })
})
