import { adminApi } from '../../api/endpoints'
import { PERMISSIONS, type PermissionDef, type Role } from '../../api/types'
import { useAsync, type AsyncState } from '../../lib/useAsync'
import { fallbackDefs } from './permissions'

export type AdminRefs = {
  roles: Role[]
  /** False when the person cannot list roles (needs admin.roles or admin.users) */
  rolesVisible: boolean
  defs: PermissionDef[]
}

/**
 * Roles and the permission catalog, shared by the people, service account and role screens. Either may be
 * unreadable for someone with a narrow admin permission; the screens then degrade instead of failing.
 */
export function useAdminRefs(canListRoles: boolean): AsyncState<AdminRefs> {
  return useAsync(async (signal) => {
    const [roles, defs] = await Promise.all([
      canListRoles ? adminApi.roles.list(signal) : Promise.resolve<Role[]>([]),
      adminApi.permissions(signal).catch(() => [] as PermissionDef[]),
    ])
    return { roles: [...roles].sort((a, b) => Number(b.builtin) - Number(a.builtin) || a.name.localeCompare(b.name)), rolesVisible: canListRoles, defs: defs.length > 0 ? defs : fallbackDefs(PERMISSIONS) }
  }, [canListRoles])
}
