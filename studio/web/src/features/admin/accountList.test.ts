import { describe, expect, it } from 'vitest'
import type { User } from '../../api/types'
import { filterAccounts, providersOf } from './accountList'

const user = (over: Partial<User>): User => ({
  id: 'u', kind: 'human', email: null, name: 'x', status: 'active', all_areas: false, area_ids: [], role_ids: [], permissions: [],
  effective_permissions: [], has_password: false, identities: [], last_login_at: null, created_at: '', updated_at: '', ...over,
})

const users = [user({ id: '1', name: 'Zoe', email: 'zoe@acme.com' }), user({ id: '2', name: 'adam', email: 'adam@acme.com', status: 'disabled' }), user({ id: '3', name: 'Bot' })]

describe('filterAccounts', () => {
  it('sorts by name and matches name or email case-insensitively', () => {
    expect(filterAccounts(users, '', '').map((u) => u.id)).toEqual(['2', '3', '1'])
    expect(filterAccounts(users, 'ACME', '').map((u) => u.id)).toEqual(['2', '1'])
    expect(filterAccounts(users, 'bot', '').map((u) => u.id)).toEqual(['3'])
  })

  it('filters by status', () => {
    expect(filterAccounts(users, '', 'disabled').map((u) => u.id)).toEqual(['2'])
  })
})

describe('providersOf', () => {
  it('lists each provider once', () => {
    const u = user({ identities: [{ provider: 'microsoft', email: 'a', last_login_at: '' }, { provider: 'clerk', email: 'b', last_login_at: '' }, { provider: 'microsoft', email: 'c', last_login_at: '' }] })
    expect(providersOf(u)).toEqual(['clerk', 'microsoft'])
  })
})
