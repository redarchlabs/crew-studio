import type { User, UserInput, UserKind } from '../../api/types'
import { looksLikeEmail } from '../auth/authLogic'

export const ACCOUNT_FIELDS = ['kind', 'email', 'name', 'status', 'all_areas', 'area_ids', 'role_ids', 'permissions'] as const

export function emptyAccount(kind: UserKind): UserInput {
  return { kind, email: '', name: '', status: 'active', all_areas: false, area_ids: [], role_ids: [], permissions: [] }
}

export function toAccountInput(u: User): UserInput {
  const { kind, email, name, status, all_areas, area_ids, role_ids, permissions } = u
  return {
    kind,
    email: email ?? '',
    name,
    status,
    all_areas,
    area_ids: area_ids ?? [],
    role_ids: role_ids ?? [],
    permissions: permissions ?? [],
  }
}

export function validateAccount(v: UserInput): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.name.trim()) errs.name = 'Name is required'
  if (v.kind === 'human' && !looksLikeEmail(v.email)) errs.email = 'Enter the email address they sign in with'
  return errs
}

export function normalizeAccount(v: UserInput): UserInput {
  return {
    ...v,
    name: v.name.trim(),
    // Service accounts have no email; people are matched to SSO sign-ins by it
    email: v.kind === 'human' ? v.email.trim() : '',
    // "All areas" makes the list irrelevant; keep what was picked so toggling back restores it
    area_ids: [...v.area_ids],
  }
}

/** Plain-language note when an account would see nothing area-scoped. */
export function areaWarning(v: Pick<UserInput, 'all_areas' | 'area_ids'>): string | null {
  if (v.all_areas || v.area_ids.length > 0) return null
  return 'No functional areas: they will not see any crews, agents, tasks, work orders or runs.'
}

/** Title + explanation for a 409 from saving or deleting an account. */
export function conflictText(message: string, code = ''): { title: string; body: string } {
  // The server marks this case with its own code; the wording check covers older servers
  if (code === 'last_admin' || (/last/i.test(message) && /admin/i.test(message))) {
    return {
      title: 'This is the last administrator',
      body: `${message} Give that permission to another active person first, then try again.`,
    }
  }
  return { title: 'That conflicts with another account', body: message }
}
