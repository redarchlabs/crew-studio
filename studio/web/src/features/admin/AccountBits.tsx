import { Shapes } from 'lucide-react'
import type { Role, User } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { Chip } from '../../components/StatusBadge'

export function StatusChip({ status }: { status: User['status'] }) {
  return <Chip tone={status === 'active' ? 'ok' : 'danger'}>{status}</Chip>
}

/** Role names for an account; ids of roles the viewer cannot list are counted instead. */
export function RoleChips({ roleIds, roles }: { roleIds: readonly string[]; roles: ReadonlyMap<string, Role> }) {
  if (roleIds.length === 0) return <span className="text-[11.5px] text-faint">No roles</span>
  const known = roleIds.flatMap((id) => roles.get(id) ?? [])
  const unknown = roleIds.length - known.length
  return (
    <span className="inline-flex flex-wrap gap-1">
      {known.map((r) => <Chip key={r.id} tone={r.builtin ? 'accent' : 'violet'}>{r.name}</Chip>)}
      {unknown > 0 && <Chip title="Roles you cannot list">+{unknown}</Chip>}
    </span>
  )
}

export function AccountAreas({ user }: { user: Pick<User, 'all_areas' | 'area_ids'> }) {
  if (user.all_areas) return <Chip tone="teal"><Shapes className="size-3" /> All areas</Chip>
  return <AreaChips areaIds={user.area_ids ?? []} max={2} />
}
