import type { User, UserStatus } from '../../api/types'

export type StatusFilter = UserStatus | ''

/** Accounts matching a free-text query (name or email) and status, sorted by name. */
export function filterAccounts(users: readonly User[], query: string, status: StatusFilter): User[] {
  const q = query.trim().toLowerCase()
  return users
    .filter((u) => !status || u.status === status)
    .filter((u) => !q || `${u.name} ${u.email ?? ''}`.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** Distinct sign-in providers someone has used, for the SSO column. */
export function providersOf(user: Pick<User, 'identities'>): string[] {
  return [...new Set((user.identities ?? []).map((i) => i.provider))].sort()
}
