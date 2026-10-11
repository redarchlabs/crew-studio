import type { Permission, PermissionDef, Role } from '../../api/types'

export type PermissionGroup = { feature: string; defs: PermissionDef[] }
export type GroupState = 'all' | 'some' | 'none'

/** Groups the catalog by feature, keeping the server's order for both features and permissions. */
export function groupPermissions(defs: readonly PermissionDef[]): PermissionGroup[] {
  const groups: PermissionGroup[] = []
  for (const def of defs) {
    const existing = groups.find((g) => g.feature === def.feature)
    if (existing) existing.defs.push(def)
    else groups.push({ feature: def.feature, defs: [def] })
  }
  return groups
}

/** How much of a feature group is selected, for the group's "select all" checkbox. */
export function groupState(selected: readonly Permission[], group: PermissionGroup): GroupState {
  const on = group.defs.filter((d) => selected.includes(d.key)).length
  if (on === 0) return 'none'
  return on === group.defs.length ? 'all' : 'some'
}

/** Turns a whole feature group on or off, leaving other permissions alone. Returns a new array. */
export function setGroup(selected: readonly Permission[], group: PermissionGroup, on: boolean): Permission[] {
  const keys = group.defs.map((d) => d.key)
  if (!on) return selected.filter((p) => !keys.includes(p))
  return [...selected, ...keys.filter((k) => !selected.includes(k))]
}

export function togglePermission(selected: readonly Permission[], key: Permission): Permission[] {
  return selected.includes(key) ? selected.filter((p) => p !== key) : [...selected, key]
}

/** Orders permissions like the catalog; unknown keys go last. */
export function sortByCatalog(perms: readonly Permission[], catalog: readonly Permission[]): Permission[] {
  const rank = (p: Permission) => {
    const i = catalog.indexOf(p)
    return i === -1 ? Number.MAX_SAFE_INTEGER : i
  }
  return [...new Set(perms)].sort((a, b) => rank(a) - rank(b))
}

/**
 * What someone ends up holding: the union of their roles' permissions and direct grants. The built-in
 * Administrator role always holds every permission, whatever its stored list says.
 */
export function effectivePermissions(
  roles: readonly Pick<Role, 'id' | 'builtin' | 'permissions'>[],
  roleIds: readonly string[],
  direct: readonly Permission[],
  catalog: readonly Permission[],
): Permission[] {
  const held = roles.filter((r) => roleIds.includes(r.id))
  if (held.some((r) => r.builtin)) return [...catalog]
  return sortByCatalog([...held.flatMap((r) => r.permissions), ...direct], catalog)
}

/** Effective permissions grouped for a compact read-only summary ("Crews: View, Edit"). */
export function summarizeByFeature(perms: readonly Permission[], defs: readonly PermissionDef[]): { feature: string; labels: string[]; all: boolean }[] {
  return groupPermissions(defs)
    .map((g) => {
      const held = g.defs.filter((d) => perms.includes(d.key))
      return { feature: g.feature, labels: held.map((d) => d.label), all: held.length === g.defs.length }
    })
    .filter((g) => g.labels.length > 0)
}

const FEATURE_NAMES: Record<string, string> = {
  overview: 'Overview',
  crews: 'Crews',
  agents: 'Agents',
  tasks: 'Tasks',
  work_orders: 'Work orders',
  runs: 'Runs',
  schedules: 'Schedules',
  triggers: 'Triggers',
  llms: 'LLMs',
  pricing: 'Pricing',
  settings: 'Settings',
  history: 'History',
  connections: 'Connections',
  state: 'Crew state',
  admin: 'Admin',
}

/** A catalog built from the permission keys alone, for when /admin/permissions cannot be read. */
export function fallbackDefs(keys: readonly Permission[]): PermissionDef[] {
  return keys.map((key) => {
    const [prefix, action = ''] = key.split('.')
    const label = action.replace(/_/g, ' ')
    return { key, feature: FEATURE_NAMES[prefix] ?? prefix, label: label.charAt(0).toUpperCase() + label.slice(1), description: key }
  })
}
