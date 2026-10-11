import type { ReactNode } from 'react'
import { afterEach, vi } from 'vitest'
import { cleanup, render } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, useLocation, type RouteObject } from 'react-router'
import { ApiError } from '../../api/client'
import type {
  AccessSettings, AccountApiKey, ApiKey, Area, AuditEvent, AuthStatus, LinkedIdentity, Me, Permission, PermissionDef, Page, Role, User,
} from '../../api/types'

// Vitest runs with globals off, so testing-library's automatic cleanup is not registered
afterEach(cleanup)

// ---------------------------------------------------------------- mocks
const fn = () => vi.fn()

/** Every endpoint these screens use; wire it with vi.mock('.../api/endpoints', async () => (await import('./testUtils')).endpointsMock). */
export const api = {
  authApi: {
    status: fn(), me: fn(), login: fn(), setup: fn(), logout: fn(),
    ssoLoginUrl: vi.fn((provider: string, returnTo: string) => `/api/auth/${provider}/login?return_to=${encodeURIComponent(returnTo)}`),
  },
  areasApi: { list: fn(), create: fn(), update: fn(), remove: fn() },
  settingsApi: { get: fn(), update: fn() },
  accountApi: { apiKeys: fn(), createApiKey: fn(), revokeApiKey: fn() },
  adminApi: {
    apiKeys: fn(),
    permissions: fn(),
    roles: { list: fn(), get: fn(), create: fn(), update: fn(), remove: fn() },
    users: {
      list: fn(), get: fn(), create: fn(), update: fn(), remove: fn(), setPassword: fn(), clearPassword: fn(), revokeSessions: fn(),
      apiKeys: fn(), createApiKey: fn(), revokeApiKey: fn(), signingSecret: fn(), rotateSigningSecret: fn(),
    },
    access: { get: fn(), update: fn() },
    audit: fn(),
  },
}
export const endpointsMock = api

/** Toast spies; wire with vi.mock('.../lib/notify', async () => (await import('./testUtils')).notifyMock). */
export const notifyMock = { notifyError: vi.fn(), notifySuccess: vi.fn() }

/** Signed-in principal seen by useAuth(); tests mutate it through setAuth(). */
export type AuthStub = {
  me: Me | null
  status: AuthStatus | null
  loading: boolean
  error: unknown
  setMe: ReturnType<typeof vi.fn>
  reload: ReturnType<typeof vi.fn>
  logout: ReturnType<typeof vi.fn>
}
export const auth: AuthStub = { me: null, status: null, loading: false, error: undefined, setMe: vi.fn(), reload: vi.fn(), logout: vi.fn() }

export function setAuth(over: Partial<AuthStub>): void {
  Object.assign(auth, over)
}

/** Wire with vi.mock('.../lib/auth', async () => (await import('./testUtils')).authMock). */
export const authMock = {
  useAuth: () => {
    const perms = new Set(auth.me?.permissions ?? [])
    return {
      ...auth,
      can: (p: Permission) => perms.has(p),
      canAny: (...ps: Permission[]) => ps.some((p) => perms.has(p)),
      inMyAreas: () => !!auth.me,
    }
  },
}

/** Fresh mocks and a default principal; call from beforeEach. */
export function resetAll(permissions: Permission[] = [], me: Partial<Me> = {}): void {
  for (const group of [api.authApi, api.areasApi, api.settingsApi, api.accountApi, api.adminApi, api.adminApi.roles, api.adminApi.users, api.adminApi.access]) {
    for (const value of Object.values(group)) if (typeof value === 'function' && 'mockReset' in value) (value as ReturnType<typeof vi.fn>).mockReset()
  }
  api.authApi.ssoLoginUrl.mockImplementation((provider: string, returnTo: string) => `/api/auth/${provider}/login?return_to=${encodeURIComponent(returnTo)}`)
  api.areasApi.list.mockResolvedValue([])
  api.adminApi.permissions.mockResolvedValue(DEFS)
  // Every account editor lists the account's API keys
  api.adminApi.users.apiKeys.mockResolvedValue(keyPage([]))
  notifyMock.notifyError.mockReset()
  notifyMock.notifySuccess.mockReset()
  auth.setMe.mockReset()
  auth.reload.mockReset()
  auth.logout.mockReset()
  setAuth({ me: makeMe({ permissions, ...me }), status: makeStatus(), loading: false, error: undefined })
}

// ---------------------------------------------------------------- fixtures
const TS = '2026-01-01T00:00:00Z'

export const DEFS: PermissionDef[] = [
  { key: 'crews.view', feature: 'Crews', label: 'View', description: 'See crews' },
  { key: 'crews.edit', feature: 'Crews', label: 'Edit', description: 'Change crews' },
  { key: 'runs.view', feature: 'Runs', label: 'View', description: 'See runs' },
  { key: 'admin.users', feature: 'Admin', label: 'Users', description: 'Manage people' },
  { key: 'admin.roles', feature: 'Admin', label: 'Roles', description: 'Manage roles' },
]

export function makeMe(over: Partial<Me> = {}): Me {
  return { id: 'me-1', kind: 'human', name: 'Me Myself', email: 'me@example.com', all_areas: false, area_ids: [], permissions: [], via: 'session', ...over }
}

export function makeStatus(over: Partial<AuthStatus> = {}): AuthStatus {
  return { setup_required: false, local_login: true, providers: [], ...over }
}

export function makeUser(over: Partial<User> = {}): User {
  return {
    id: 'u-1', kind: 'human', email: 'ada@example.com', name: 'Ada Lovelace', status: 'active', all_areas: false,
    area_ids: [], role_ids: [], permissions: [], effective_permissions: [], has_password: false, identities: [],
    last_login_at: null, created_at: TS, updated_at: TS, ...over,
  }
}

export function makeIdentity(over: Partial<LinkedIdentity> = {}): LinkedIdentity {
  return { provider: 'microsoft', email: 'ada@example.com', last_login_at: TS, ...over }
}

export function makeRole(over: Partial<Role> = {}): Role {
  return { id: 'r-1', name: 'Analyst', description: '', builtin: false, permissions: [], user_count: 0, created_at: TS, updated_at: TS, ...over }
}

export function makeArea(over: Partial<Area> = {}): Area {
  return { id: 'a-1', name: 'Finance', description: '', created_at: TS, updated_at: TS, ...over }
}

export function makeKey(over: Partial<ApiKey> = {}): ApiKey {
  return { id: 'k-1', user_id: 'u-1', name: 'ERP', prefix: 'abc123', created_at: TS, expires_at: null, last_used_at: null, revoked_at: null, ...over }
}

/** A key as the list endpoints return it: with its account and whether you may revoke it. */
export function makeAccountKey(over: Partial<AccountApiKey> = {}): AccountApiKey {
  return { ...makeKey(), user_name: 'ERP', user_kind: 'service', created_by_name: null, can_revoke: true, ...over }
}

/** A page of keys, as accountApi.apiKeys, adminApi.apiKeys and adminApi.users.apiKeys resolve. */
export function keyPage(keys: readonly Partial<AccountApiKey>[], meta: Partial<Page<AccountApiKey>['meta']> = {}): Page<AccountApiKey> {
  const items = keys.map((k) => makeAccountKey(k))
  return { items, meta: { total: items.length, limit: 20, offset: 0, ...meta } }
}

export function makeAccess(over: Partial<AccessSettings> = {}): AccessSettings {
  return { sso_auto_provision: false, auto_provision_role_id: null, callback_allowed_hosts: [], ...over }
}

export function makeAudit(over: Partial<AuditEvent> = {}): AuditEvent {
  return { id: 1, action: 'login', actor_id: 'u-1', actor_name: 'Ada', ip: '10.0.0.1', details: {}, created_at: TS, ...over }
}

export function apiError(status: number, message: string, code = 'error', details: string[] = []): ApiError {
  return new ApiError(status, { code, message, details })
}

// ---------------------------------------------------------------- rendering
function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="location">{`${loc.pathname}${loc.search}`}</div>
}

/**
 * Renders `element` at `path` inside a data router (the unsaved-changes guard needs one) and exposes the
 * current location through a `location` test id. Extra routes let tests observe redirects.
 */
export function renderRoute(element: ReactNode, opts: { path?: string; url?: string; extra?: string[] } = {}) {
  const path = opts.path ?? '/'
  const routes: RouteObject[] = [
    { path, element: <>{element}<LocationProbe /></> },
    ...(opts.extra ?? []).map((p) => ({ path: p, element: <><div>{`route:${p}`}</div><LocationProbe /></> })),
    { path: '*', element: <><div>unmatched</div><LocationProbe /></> },
  ]
  const router = createMemoryRouter(routes, { initialEntries: [opts.url ?? path] })
  const view = render(<RouterProvider router={router} />)
  return { ...view, router }
}

/** Replaces window.location so full-page navigations (SSO) can be observed; returns the assign spy. */
export function stubLocationAssign(): ReturnType<typeof vi.fn> {
  const assign = vi.fn()
  Object.defineProperty(window, 'location', { configurable: true, value: { ...window.location, assign } })
  return assign
}

/** True for controls disabled directly or by an ancestor fieldset (the `disabled` property ignores the latter). */
export function isDisabled(el: Element): boolean {
  return el.matches(':disabled')
}
