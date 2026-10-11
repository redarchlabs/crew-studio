import { describe, expect, it } from 'vitest'
import type { User } from '../../api/types'
import { areaWarning, emptyAccount, normalizeAccount, toAccountInput, validateAccount } from './accountForm'

describe('account form', () => {
  it('needs a name, and an email for people only', () => {
    expect(Object.keys(validateAccount(emptyAccount('human'))).sort()).toEqual(['email', 'name'])
    expect(Object.keys(validateAccount(emptyAccount('service')))).toEqual(['name'])
    expect(validateAccount({ ...emptyAccount('human'), name: 'Ada', email: 'ada@example.com' })).toEqual({})
  })

  it('drops the email for service accounts and trims fields', () => {
    expect(normalizeAccount({ ...emptyAccount('service'), name: ' Bot ', email: 'x@y.z' })).toMatchObject({ name: 'Bot', email: '' })
    expect(normalizeAccount({ ...emptyAccount('human'), email: ' a@b.co ' }).email).toBe('a@b.co')
  })

  it('maps a user to its editable fields', () => {
    const user = { ...emptyAccount('service'), email: null, id: 'u1', effective_permissions: [], has_password: false, identities: [], last_login_at: null, created_at: '', updated_at: '' } as User
    expect(toAccountInput(user)).toEqual({ ...emptyAccount('service') })
  })

  it('warns when someone would see no area-scoped items', () => {
    expect(areaWarning({ all_areas: false, area_ids: [] })).toMatch(/No functional areas/)
    expect(areaWarning({ all_areas: true, area_ids: [] })).toBeNull()
    expect(areaWarning({ all_areas: false, area_ids: ['a'] })).toBeNull()
  })
})

describe('conflictText', () => {
  it('explains the last administrator rule', async () => {
    const { conflictText } = await import('./accountForm')
    const t = conflictText('Cannot remove the last active administrator.')
    expect(t.title).toBe('This is the last administrator')
    expect(t.body).toMatch(/Give that permission to another active person first/)
  })
  it('passes other conflicts through', async () => {
    const { conflictText } = await import('./accountForm')
    expect(conflictText('Cannot do that', 'last_admin').title).toBe('This is the last administrator')
    expect(conflictText('Email already in use')).toEqual({ title: 'That conflicts with another account', body: 'Email already in use' })
  })
  it('recognises the server\'s last_admin code whatever the wording', async () => {
    const { conflictText } = await import('./accountForm')
    // The exact text the server sends today, which mentions neither "last" nor "admin"
    const t = conflictText('At least one active person must keep the Manage users permission.', 'last_admin')
    expect(t.title).toBe('This is the last administrator')
    expect(t.body).toBe('At least one active person must keep the Manage users permission. Give that permission to another active person first, then try again.')
    expect(conflictText('At least one active person must keep the Manage users permission.', 'conflict').title).toBe('That conflicts with another account')
  })
})
