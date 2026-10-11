import { request, requestPage } from './client'
import type {
  AccessSettings,
  AccountApiKey,
  ApiKeyLimits,
  ApiKeyQuery,
  Area,
  AreaInput,
  Attachment,
  AuditEvent,
  AuthStatus,
  Delivery,
  Journal,
  Me,
  NewApiKey,
  PermissionDef,
  Role,
  RoleInput,
  RunKind,
  RunStatus,
  Schedule,
  ScheduleInput,
  SchedulePreview,
  SubmitWorkOrderBody,
  Trigger,
  TriggerInput,
  TriggerType,
  User,
  UserInput,
  UserKind,
  WorkOrder,
  WorkOrderInput,
  WorkOrderSource,
  WorkOrderStatus,
  Agent,
  AgentInput,
  Crew,
  CrewBody,
  CrewDraft,
  CrewValidation,
  EntityType,
  Interactions,
  LLM,
  LLMInput,
  LlmCall,
  ModelPrice,
  ModelPriceInput,
  Overview,
  Page,
  Revision,
  Run,
  RunEvent,
  Settings,
  StartRunBody,
  Task,
  TaskInput,
  Tool,
} from './types'

type Signal = AbortSignal | undefined

/** Builds the five CRUD calls shared by every editable resource. */
function crud<T, In>(base: string) {
  return {
    list: (signal?: Signal) => request<T[] | null>('GET', base, { signal }).then((d) => d ?? []),
    get: (id: string, signal?: Signal) => request<T>('GET', `${base}/${id}`, { signal }),
    create: (body: In) => request<T>('POST', base, { body }),
    update: (id: string, body: In) => request<T>('PUT', `${base}/${id}`, { body }),
    remove: (id: string) => request<void>('DELETE', `${base}/${id}`),
  }
}

export const llmsApi = crud<LLM, LLMInput>('/llms')
export const pricingApi = crud<ModelPrice, ModelPriceInput>('/pricing')
export const agentsApi = crud<Agent, AgentInput>('/agents')
export const tasksApi = {
  ...crud<Task, TaskInput>('/tasks'),
  /** An unsaved crew body for running just this task (needs tasks.view and crews.create) */
  crewDraft: (id: string, signal?: Signal) => request<CrewDraft>('GET', `/tasks/${id}/crew-draft`, { signal }),
}

export const crewsApi = {
  ...crud<Crew, CrewBody>('/crews'),
  validate: (id: string, signal?: Signal) => request<CrewValidation>('GET', `/crews/${id}/validate`, { signal }),
  startRun: (id: string, body: StartRunBody) => request<Run>('POST', `/crews/${id}/runs`, { body }),
}

export const toolsApi = {
  list: (signal?: Signal) => request<Tool[] | null>('GET', '/tools', { signal }).then((d) => d ?? []),
}

export const settingsApi = {
  get: (signal?: Signal) => request<Settings>('GET', '/settings', { signal }),
  update: (body: Settings) => request<Settings>('PUT', '/settings', { body }),
}

export type HistoryQuery = { entity_type?: EntityType; entity_id?: string; limit: number; offset: number }

export const historyApi = {
  list: (query: HistoryQuery, signal?: Signal): Promise<Page<Revision>> =>
    requestPage<Revision>('/history', { query, signal }),
}

export type RunsQuery = {
  crew_id?: string
  work_order_id?: string
  status?: RunStatus
  trigger_type?: TriggerType
  limit: number
  offset: number
  kind?: RunKind
}

export const runsApi = {
  list: (query: RunsQuery, signal?: Signal): Promise<Page<Run>> => requestPage<Run>('/runs', { query, signal }),
  get: (id: string, signal?: Signal) => request<Run>('GET', `/runs/${id}`, { signal }),
  cancel: (id: string) => request<Run>('POST', `/runs/${id}/cancel`),
  /** Asks a running attempt to compact its agents' context before their next LLM calls */
  compact: (id: string) => request<{ compact_requests: number }>('POST', `/runs/${id}/compact`, { body: {} }),
  remove: (id: string) => request<void>('DELETE', `/runs/${id}`),
  interactions: (id: string, signal?: Signal) => request<Interactions>('GET', `/runs/${id}/interactions`, { signal }),
  llmCalls: (id: string, signal?: Signal) =>
    request<LlmCall[] | null>('GET', `/runs/${id}/llm-calls`, { signal }).then((d) => d ?? []),
  events: (id: string, after: number, signal?: Signal) =>
    request<RunEvent[] | null>('GET', `/runs/${id}/events`, { query: { after }, signal }).then((d) => d ?? []),
  streamUrl: (id: string, after: number) => `/api/runs/${id}/stream?after=${after}`,
  artifacts: (id: string, signal?: Signal) =>
    request<Attachment[] | null>('GET', `/runs/${id}/artifacts`, { signal }).then((d) => d ?? []),
  deliveries: (id: string, signal?: Signal) =>
    request<Delivery[] | null>('GET', `/runs/${id}/deliveries`, { signal }).then((d) => d ?? []),
  journal: (id: string, signal?: Signal) => request<Journal>('GET', `/runs/${id}/journal`, { signal }),
}

export const overviewApi = {
  get: (signal?: Signal) => request<Overview>('GET', '/overview', { signal }),
}

// ---------------------------------------------------------------- Sign-in
export const authApi = {
  status: (signal?: Signal) => request<AuthStatus>('GET', '/auth/status', { signal }),
  me: (signal?: Signal) => request<Me>('GET', '/auth/me', { signal }),
  login: (email: string, password: string) => request<Me>('POST', '/auth/login', { body: { email, password } }),
  setup: (body: { token: string; email: string; name: string; password: string }) =>
    request<Me>('POST', '/auth/setup', { body }),
  logout: () => request<void>('POST', '/auth/logout', { body: {} }),
  /** Full-page navigation target that starts an SSO sign-in. */
  ssoLoginUrl: (provider: string, returnTo: string) =>
    `/api/auth/${encodeURIComponent(provider)}/login?return_to=${encodeURIComponent(returnTo)}`,
}

// ---------------------------------------------------------------- Areas and admin
export const areasApi = {
  list: (signal?: Signal) => request<Area[] | null>('GET', '/areas', { signal }).then((d) => d ?? []),
  create: (body: AreaInput) => request<Area>('POST', '/areas', { body }),
  update: (id: string, body: AreaInput) => request<Area>('PUT', `/areas/${id}`, { body }),
  remove: (id: string) => request<void>('DELETE', `/areas/${id}`),
}

export type NewApiKeyInput = { name: string; expires_at: string | null } & ApiKeyLimits

/** The signed-in person's own API keys */
export const accountApi = {
  apiKeys: (query: ApiKeyQuery, signal?: Signal): Promise<Page<AccountApiKey>> =>
    requestPage<AccountApiKey>('/account/api-keys', { query, signal }),
  createApiKey: (body: NewApiKeyInput) => request<NewApiKey>('POST', '/account/api-keys', { body }),
  revokeApiKey: (keyId: string) => request<void>('DELETE', `/account/api-keys/${keyId}`),
}

export const adminApi = {
  /** Every key the caller may manage: people's with admin.users, service accounts' with admin.service_accounts */
  apiKeys: (query: ApiKeyQuery, signal?: Signal): Promise<Page<AccountApiKey>> =>
    requestPage<AccountApiKey>('/admin/api-keys', { query, signal }),
  permissions: (signal?: Signal) =>
    request<PermissionDef[] | null>('GET', '/admin/permissions', { signal }).then((d) => d ?? []),
  roles: crud<Role, RoleInput>('/admin/roles'),
  users: {
    list: (kind: UserKind, signal?: Signal) =>
      request<User[] | null>('GET', '/admin/users', { query: { kind }, signal }).then((d) => d ?? []),
    get: (id: string, signal?: Signal) => request<User>('GET', `/admin/users/${id}`, { signal }),
    create: (body: UserInput) => request<User>('POST', '/admin/users', { body }),
    update: (id: string, body: UserInput) => request<User>('PUT', `/admin/users/${id}`, { body }),
    remove: (id: string) => request<void>('DELETE', `/admin/users/${id}`),
    setPassword: (id: string, password: string) =>
      request<void>('PUT', `/admin/users/${id}/password`, { body: { password } }),
    clearPassword: (id: string) => request<void>('DELETE', `/admin/users/${id}/password`),
    revokeSessions: (id: string) => request<{ ended: number }>('POST', `/admin/users/${id}/sessions/revoke`, { body: {} }),
    apiKeys: (id: string, query: ApiKeyQuery, signal?: Signal): Promise<Page<AccountApiKey>> =>
      requestPage<AccountApiKey>(`/admin/users/${id}/api-keys`, { query, signal }),
    createApiKey: (id: string, body: NewApiKeyInput) =>
      request<NewApiKey>('POST', `/admin/users/${id}/api-keys`, { body }),
    revokeApiKey: (id: string, keyId: string) => request<void>('DELETE', `/admin/users/${id}/api-keys/${keyId}`),
    signingSecret: (id: string, signal?: Signal) =>
      request<{ signing_secret: string }>('GET', `/admin/users/${id}/signing-secret`, { signal }),
    rotateSigningSecret: (id: string) =>
      request<{ signing_secret: string }>('POST', `/admin/users/${id}/signing-secret/rotate`, { body: {} }),
  },
  access: {
    get: (signal?: Signal) => request<AccessSettings>('GET', '/admin/access', { signal }),
    update: (body: AccessSettings) => request<AccessSettings>('PUT', '/admin/access', { body }),
  },
  audit: (query: { limit: number; offset: number }, signal?: Signal): Promise<Page<AuditEvent>> =>
    requestPage<AuditEvent>('/admin/audit', { query, signal }),
}

// ---------------------------------------------------------------- Work orders
export type WorkOrdersQuery = {
  status?: WorkOrderStatus
  source?: WorkOrderSource
  source_id?: string
  crew_id?: string
  q?: string
  limit: number
  offset: number
}

export const workOrdersApi = {
  list: (query: WorkOrdersQuery, signal?: Signal): Promise<Page<WorkOrder>> =>
    requestPage<WorkOrder>('/work-orders', { query, signal }),
  get: (id: string, signal?: Signal) => request<WorkOrder>('GET', `/work-orders/${id}`, { signal }),
  create: (body: WorkOrderInput) => request<WorkOrder>('POST', '/work-orders', { body }),
  update: (id: string, body: WorkOrderInput) => request<WorkOrder>('PUT', `/work-orders/${id}`, { body }),
  remove: (id: string) => request<void>('DELETE', `/work-orders/${id}`),
  submit: (id: string, body: SubmitWorkOrderBody = {}) => request<Run>('POST', `/work-orders/${id}/submit`, { body }),
  cancel: (id: string) => request<Run>('POST', `/work-orders/${id}/cancel`, { body: {} }),
  journal: (id: string, runId?: string, signal?: Signal) =>
    request<Journal>('GET', `/work-orders/${id}/journal`, { query: { run_id: runId }, signal }),
  upload: (id: string, files: File[]) => {
    const form = new FormData()
    for (const file of files) form.append('file', file, file.name)
    return request<Attachment[] | null>('POST', `/work-orders/${id}/attachments`, { body: form }).then((d) => d ?? [])
  },
  removeAttachment: (id: string, attachmentId: string) =>
    request<void>('DELETE', `/work-orders/${id}/attachments/${attachmentId}`),
}

export const attachmentsApi = {
  /** URL for <img src> or a download link; the session cookie authorises it. */
  contentUrl: (id: string, download = false) => `/api/attachments/${id}/content${download ? '?download=1' : ''}`,
}

// ---------------------------------------------------------------- Schedules and triggers
export const schedulesApi = {
  ...crud<Schedule, ScheduleInput>('/schedules'),
  runNow: (id: string) => request<WorkOrder>('POST', `/schedules/${id}/run-now`, { body: {} }),
  preview: (cron: string, timezone: string) =>
    request<SchedulePreview>('POST', '/schedules/preview', { body: { cron, timezone } }),
}

export const triggersApi = {
  ...crud<Trigger, TriggerInput>('/triggers'),
  rotate: (id: string) => request<Trigger>('POST', `/triggers/${id}/rotate`, { body: {} }),
  /** Absolute URL other systems POST to. */
  hookUrl: (id: string) => `${window.location.origin}/api/hooks/${id}`,
}
