import type { ApiKeyLimits, Me, Permission, User } from '../../api/types'

/** What a key's account holds: the most the key can be given. */
export type KeyScope = { permissions: readonly Permission[]; allAreas: boolean; areaIds: readonly string[] }

export function scopeOfMe(me: Pick<Me, 'permissions' | 'all_areas' | 'area_ids'>): KeyScope {
  return { permissions: me.permissions, allAreas: me.all_areas, areaIds: me.area_ids }
}

export function scopeOfUser(u: Pick<User, 'effective_permissions' | 'all_areas' | 'area_ids'>): KeyScope {
  return { permissions: u.effective_permissions, allAreas: u.all_areas, areaIds: u.area_ids }
}

/** The limits as the form holds them: a limit that is off keeps its choices, so turning it back on restores them. */
export type LimitsDraft = {
  api: boolean
  mcp: boolean
  limitPermissions: boolean
  permissions: Permission[]
  limitAreas: boolean
  areaIds: string[]
}

export const OPEN_LIMITS: LimitsDraft = { api: true, mcp: true, limitPermissions: false, permissions: [], limitAreas: false, areaIds: [] }

export type LimitsProblems = { channels?: string; permissions?: string; areas?: string }

export function limitsProblems(d: LimitsDraft): LimitsProblems {
  const errs: LimitsProblems = {}
  if (!d.api && !d.mcp) errs.channels = 'Choose where the key works: the REST API, MCP or both'
  if (d.limitPermissions && d.permissions.length === 0) errs.permissions = 'Choose at least one permission'
  if (d.limitAreas && d.areaIds.length === 0) errs.areas = 'Choose at least one area'
  return errs
}

/** The request body's limits. Choices the account no longer holds are dropped rather than sent to be refused. */
export function toLimits(d: LimitsDraft, scope: KeyScope): ApiKeyLimits {
  return {
    allow_api: d.api,
    allow_mcp: d.mcp,
    permissions: d.limitPermissions ? d.permissions.filter((p) => scope.permissions.includes(p)) : null,
    area_ids: d.limitAreas ? d.areaIds.filter((id) => scope.allAreas || scope.areaIds.includes(id)) : null,
  }
}

/** The "Read only" preset: every view permission the account holds. */
export function readOnly(scope: KeyScope): Permission[] {
  return scope.permissions.filter((p) => p.endsWith('.view'))
}

/** The areas a key of this account may be limited to, from every area there is. */
export function limitableAreas<A extends { id: string }>(areas: readonly A[], scope: KeyScope): A[] {
  return scope.allAreas ? [...areas] : areas.filter((a) => scope.areaIds.includes(a.id))
}

export type AccessSummary = { channels: string; permissions: string; areas: string; areasTitle?: string; permissionsTitle?: string }

/** A key's limits in a few words for key lists, with the full lists for tooltips. */
export function accessSummary(k: ApiKeyLimits, areaName: (id: string) => string): AccessSummary {
  const channels = k.allow_api && k.allow_mcp ? 'API and MCP' : k.allow_api ? 'API only' : 'MCP only'
  const count = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`
  return {
    channels,
    permissions: k.permissions ? count(k.permissions.length, 'permission') : 'All permissions',
    permissionsTitle: k.permissions?.join(', '),
    areas: k.area_ids ? k.area_ids.map(areaName).join(', ') : 'All areas',
    areasTitle: k.area_ids ? undefined : 'Every area the account is in',
  }
}
