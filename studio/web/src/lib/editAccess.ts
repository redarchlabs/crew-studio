import type { Permission } from '../api/types'

export type BuildFeature = 'agents' | 'tasks' | 'crews'

const PERMS: Record<BuildFeature, { create: Permission; edit: Permission; delete: Permission }> = {
  agents: { create: 'agents.create', edit: 'agents.edit', delete: 'agents.delete' },
  tasks: { create: 'tasks.create', edit: 'tasks.edit', delete: 'tasks.delete' },
  crews: { create: 'crews.create', edit: 'crews.edit', delete: 'crews.delete' },
}

export type EditAccess = {
  canSave: boolean
  canDelete: boolean
  /** The permission a read-only viewer is missing, for the "Read-only" note; null when they can save */
  missing: Permission | null
}

/** What the signed-in person may do in an agent, task or crew editor. */
export function editAccess(feature: BuildFeature, isNew: boolean, can: (p: Permission) => boolean): EditAccess {
  const perms = PERMS[feature]
  const needed = isNew ? perms.create : perms.edit
  const canSave = can(needed)
  return { canSave, canDelete: !isNew && can(perms.delete), missing: canSave ? null : needed }
}

export function createPermission(feature: BuildFeature): Permission {
  return PERMS[feature].create
}
