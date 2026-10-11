import { ADMIN_ENTITY_TYPES, ENTITY_TYPES, type EntityType, type Revision } from '../../api/types'

type Snapshot = Record<string, unknown>

function str(s: Snapshot, key: string): string | null {
  const v = s[key]
  return typeof v === 'string' && v ? v : null
}

/** Where each kind of revision opens; the snapshot helps where one type has two screens (people vs service accounts). */
const PATHS: Record<EntityType, (id: string, s: Snapshot) => string | null> = {
  agent: (id) => `/agents/${id}`,
  task: (id) => `/tasks/${id}`,
  crew: (id) => `/crews/${id}`,
  llm: (id) => `/llms?edit=${id}`,
  pricing: () => '/pricing',
  settings: () => '/settings',
  work_order: (id) => `/work-orders/${id}`,
  schedule: (id) => `/schedules/${id}`,
  trigger: (id) => `/triggers/${id}`,
  area: () => '/admin/areas',
  role: (id) => `/admin/roles/${id}`,
  user: (id, s) => (s.kind === 'service' ? `/admin/service-accounts/${id}` : `/admin/users/${id}`),
  // API keys belong to a service account; open it when the snapshot says which
  api_key: (_id, s) => (str(s, 'user_id') ? `/admin/service-accounts/${str(s, 'user_id')}` : null),
  access_settings: () => '/admin/access',
}

/** Page for the revision's entity, or null when there is none (or it was deleted). */
export function entityPath(rev: Pick<Revision, 'entity_type' | 'entity_id' | 'action' | 'snapshot'>): string | null {
  // A deleted item has no page; for singletons and lists the page still exists
  const listPages: readonly EntityType[] = ['pricing', 'settings', 'area', 'access_settings']
  if (rev.action === 'delete' && !listPages.includes(rev.entity_type)) return null
  const build = PATHS[rev.entity_type]
  return build ? build(rev.entity_id, rev.snapshot ?? {}) : null
}

/** Entity types the person may filter by: admin types need admin.audit, the rest history.view. */
export function visibleEntityTypes(canHistory: boolean, canAudit: boolean): EntityType[] {
  return ENTITY_TYPES.filter((t) => (ADMIN_ENTITY_TYPES.includes(t) ? canAudit : canHistory))
}

export const ENTITY_LABELS: Record<EntityType, string> = {
  llm: 'LLM',
  pricing: 'Price',
  agent: 'Agent',
  task: 'Task',
  crew: 'Crew',
  settings: 'Settings',
  work_order: 'Work order',
  schedule: 'Schedule',
  trigger: 'Trigger',
  area: 'Functional area',
  role: 'Role',
  user: 'Person / service account',
  api_key: 'API key',
  access_settings: 'Sign-in settings',
}

/** Human name for the changed entity, taken from its snapshot. */
export function snapshotName(rev: Pick<Revision, 'entity_type' | 'entity_id' | 'snapshot'>): string {
  const s = rev.snapshot ?? {}
  if (rev.entity_type === 'settings') return 'Settings'
  if (rev.entity_type === 'access_settings') return 'Sign-in settings'
  if (rev.entity_type === 'work_order' && typeof s.number === 'number') {
    return `WO-${s.number}${str(s, 'title') ? ` · ${str(s, 'title')}` : ''}`
  }
  if (rev.entity_type === 'api_key' && str(s, 'prefix')) return `${str(s, 'name') ?? 'Key'} (csk_${str(s, 'prefix')}_…)`
  return str(s, 'name') ?? str(s, 'model') ?? str(s, 'email') ?? rev.entity_id.slice(0, 8)
}
