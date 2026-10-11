import { describe, expect, it } from 'vitest'
import type { ApiKey } from '../../api/types'
import { accountPath, expiresAtFromDate, keyStatus, maskedKey, newKeyProblems, sortKeys } from './apiKeys'

const NOW = Date.parse('2026-06-01T12:00:00Z')
const key = (over: Partial<ApiKey>): ApiKey => ({ id: 'k', user_id: 'u', name: 'k', prefix: 'ab12', created_at: '2026-01-01T00:00:00Z', expires_at: null, last_used_at: null, revoked_at: null, ...over })

describe('keyStatus', () => {
  it('prefers revoked, then expired, else active', () => {
    expect(keyStatus(key({ revoked_at: '2026-02-01T00:00:00Z', expires_at: '2020-01-01T00:00:00Z' }), NOW)).toBe('revoked')
    expect(keyStatus(key({ expires_at: '2026-05-01T00:00:00Z' }), NOW)).toBe('expired')
    expect(keyStatus(key({ expires_at: '2027-01-01T00:00:00Z' }), NOW)).toBe('active')
    expect(keyStatus(key({}), NOW)).toBe('active')
  })
})

describe('maskedKey', () => {
  it('shows only the prefix', () => {
    expect(maskedKey('ab12')).toBe('csk_ab12_…')
  })
})

describe('expiry dates', () => {
  it('uses the end of the chosen day', () => {
    const iso = expiresAtFromDate('2026-07-04')
    expect(iso).not.toBeNull()
    const d = new Date(iso as string)
    expect([d.getFullYear(), d.getMonth(), d.getDate(), d.getHours(), d.getMinutes()]).toEqual([2026, 6, 4, 23, 59])
  })

  it('treats an empty date as no expiry and rejects garbage', () => {
    expect(expiresAtFromDate('')).toBeNull()
    expect(expiresAtFromDate('nope')).toBeNull()
  })

  it('validates the new key form', () => {
    expect(newKeyProblems('', '', NOW).name).toBeDefined()
    expect(newKeyProblems('CI', '2026-01-01', NOW).expires).toMatch(/future/)
    expect(newKeyProblems('CI', '2027-01-01', NOW)).toEqual({})
    expect(newKeyProblems('CI', '', NOW)).toEqual({})
  })
})

describe('sortKeys', () => {
  it('lists active keys first, newest first', () => {
    const keys = [
      key({ id: 'old', created_at: '2026-01-01T00:00:00Z' }),
      key({ id: 'revoked', created_at: '2026-05-01T00:00:00Z', revoked_at: '2026-05-02T00:00:00Z' }),
      key({ id: 'new', created_at: '2026-04-01T00:00:00Z' }),
    ]
    expect(sortKeys(keys, NOW).map((k) => k.id)).toEqual(['new', 'old', 'revoked'])
  })
})

describe('accountPath', () => {
  it('links people and service accounts to their editors', () => {
    expect(accountPath({ user_id: 'u1', user_kind: 'human' })).toBe('/admin/users/u1')
    expect(accountPath({ user_id: 'u2', user_kind: 'service' })).toBe('/admin/service-accounts/u2')
  })
})
