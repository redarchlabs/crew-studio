// Sign-in, permissions, functional areas and admin types. Mirror studio/docs/api.md.

export type SsoProvider = { key: 'clerk' | 'microsoft'; label: string }

export type AuthStatus = {
  setup_required: boolean
  local_login: boolean
  providers: SsoProvider[]
}

export type Me = {
  id: string
  kind: UserKind
  name: string
  email: string
  all_areas: boolean
  area_ids: string[]
  permissions: Permission[]
  via: 'session' | 'api_key'
}

export const PERMISSIONS = [
  'overview.view',
  'crews.view', 'crews.create', 'crews.edit', 'crews.delete', 'crews.run', 'crews.chat',
  'state.view', 'state.manage',
  'agents.view', 'agents.create', 'agents.edit', 'agents.delete',
  'tasks.view', 'tasks.create', 'tasks.edit', 'tasks.delete',
  'work_orders.view', 'work_orders.create', 'work_orders.cancel', 'work_orders.delete',
  'runs.view', 'runs.cancel', 'runs.delete',
  'schedules.view', 'schedules.manage', 'triggers.view', 'triggers.manage',
  'llms.view', 'llms.manage', 'pricing.view', 'pricing.manage', 'settings.view', 'settings.manage',
  'connections.view', 'connections.manage',
  'history.view',
  'account.api_keys',
  'admin.users', 'admin.service_accounts', 'admin.roles', 'admin.areas', 'admin.access', 'admin.audit',
] as const
export type Permission = (typeof PERMISSIONS)[number]

export type PermissionDef = { key: Permission; feature: string; label: string; description: string }

export type AreaUsage = { crews: number; agents: number; tasks: number; users: number }

export type Area = {
  id: string
  name: string
  description: string
  /** Only returned to admin.areas */
  usage?: AreaUsage
  created_at: string
  updated_at: string
}
export type AreaInput = { name: string; description: string }

export type Role = {
  id: string
  name: string
  description: string
  builtin: boolean
  permissions: Permission[]
  user_count: number
  created_at: string
  updated_at: string
}
export type RoleInput = { name: string; description: string; permissions: Permission[] }

export type UserKind = 'human' | 'service'
export type UserStatus = 'active' | 'disabled'

export type UserInput = {
  kind: UserKind
  email: string
  name: string
  status: UserStatus
  all_areas: boolean
  area_ids: string[]
  role_ids: string[]
  permissions: Permission[]
}

export type LinkedIdentity = { provider: string; email: string; last_login_at: string }

export type User = Omit<UserInput, 'email'> & {
  id: string
  email: string | null
  effective_permissions: Permission[]
  has_password: boolean
  identities: LinkedIdentity[]
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export type ApiKey = {
  id: string
  user_id: string
  name: string
  prefix: string
  created_at: string
  expires_at: string | null
  last_used_at: string | null
  revoked_at: string | null
}
export type NewApiKey = { key: ApiKey; secret: string }
/** A key with its account, from the admin list of every key */
export type AccountApiKey = ApiKey & {
  user_name: string
  user_kind: UserKind
  created_by_name: string | null
  /** False when an admin lacks something the key's owner holds, so the server would refuse to revoke it */
  can_revoke: boolean
}
export type ApiKeyStatus = 'active' | 'expired' | 'revoked'
/** Paging and server-side filters for key lists */
export type ApiKeyQuery = { limit: number; offset: number; q?: string; kind?: UserKind; status?: ApiKeyStatus }

export type AccessSettings = {
  sso_auto_provision: boolean
  auto_provision_role_id: string | null
  callback_allowed_hosts: string[]
}

export type AuditEvent = {
  id: number
  action: 'login' | 'login_failed' | 'logout' | 'setup' | 'sessions_revoked' | 'api_key_created' | 'api_key_revoked' | 'api_key_rejected' | 'mcp_change' | string
  actor_id: string | null
  actor_name: string
  ip: string
  details: Record<string, unknown>
  created_at: string
}
