import type { Role, RoleInput } from '../../api/types'

export const ROLE_FIELDS = ['name', 'description', 'permissions'] as const

export const EMPTY_ROLE: RoleInput = { name: '', description: '', permissions: [] }

export function toRoleInput(r: Role): RoleInput {
  return { name: r.name, description: r.description, permissions: r.permissions ?? [] }
}

export function validateRole(v: RoleInput): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.name.trim()) errs.name = 'Name is required'
  return errs
}

export function normalizeRole(v: RoleInput): RoleInput {
  return { ...v, name: v.name.trim(), description: v.description.trim() }
}

/** Why the role editor is read-only, or null when it can be edited. */
export function roleLockReason(role: Pick<Role, 'builtin'> | null, canEditRoles: boolean): 'builtin' | 'permission' | null {
  if (role?.builtin) return 'builtin'
  if (!canEditRoles) return 'permission'
  return null
}
