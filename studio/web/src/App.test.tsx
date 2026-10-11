import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Every feature page is replaced by a stub that names itself; this file tests routing, guards and the shell.
vi.mock('./api/endpoints', () => ({ authApi: { status: vi.fn(), me: vi.fn(), logout: vi.fn() } }))
vi.mock('sonner', () => ({ Toaster: () => null, toast: { error: vi.fn(), success: vi.fn() } }))

vi.mock('./features/auth/LoginPage', () => ({ LoginPage: () => <div>page:LoginPage</div> }))
vi.mock('./features/auth/SetupPage', () => ({ SetupPage: () => <div>page:SetupPage</div> }))
vi.mock('./features/overview/OverviewPage', () => ({ OverviewPage: () => <div>page:OverviewPage</div> }))
vi.mock('./features/llms/LlmsPage', () => ({ LlmsPage: () => <div>page:LlmsPage</div> }))
vi.mock('./features/pricing/PricingPage', () => ({ PricingPage: () => <div>page:PricingPage</div> }))
vi.mock('./features/settings/SettingsPage', () => ({ SettingsPage: () => <div>page:SettingsPage</div> }))
vi.mock('./features/agents/AgentsPage', () => ({ AgentsPage: () => <div>page:AgentsPage</div> }))
vi.mock('./features/agents/AgentEditorPage', () => ({ AgentEditorPage: () => <div>page:AgentEditorPage</div> }))
vi.mock('./features/tasks/TasksPage', () => ({ TasksPage: () => <div>page:TasksPage</div> }))
vi.mock('./features/tasks/TaskEditorPage', () => ({ TaskEditorPage: () => <div>page:TaskEditorPage</div> }))
vi.mock('./features/crews/CrewsPage', () => ({ CrewsPage: () => <div>page:CrewsPage</div> }))
vi.mock('./features/runs/RunsPage', () => ({ RunsPage: () => <div>page:RunsPage</div> }))
vi.mock('./features/history/HistoryPage', () => ({ HistoryPage: () => <div>page:HistoryPage</div> }))
vi.mock('./features/workorders/WorkOrdersPage', () => ({ WorkOrdersPage: () => <div>page:WorkOrdersPage</div> }))
vi.mock('./features/workorders/NewWorkOrderPage', () => ({ NewWorkOrderPage: () => <div>page:NewWorkOrderPage</div> }))
vi.mock('./features/schedules/SchedulesPage', () => ({ SchedulesPage: () => <div>page:SchedulesPage</div> }))
vi.mock('./features/schedules/ScheduleEditorPage', () => ({ ScheduleEditorPage: () => <div>page:ScheduleEditorPage</div> }))
vi.mock('./features/triggers/TriggersPage', () => ({ TriggersPage: () => <div>page:TriggersPage</div> }))
vi.mock('./features/triggers/TriggerEditorPage', () => ({ TriggerEditorPage: () => <div>page:TriggerEditorPage</div> }))
vi.mock('./features/admin/UsersPage', () => ({ UsersPage: () => <div>page:UsersPage</div> }))
vi.mock('./features/admin/UserEditorPage', () => ({ UserEditorPage: () => <div>page:UserEditorPage</div> }))
vi.mock('./features/admin/ServiceAccountsPage', () => ({ ServiceAccountsPage: () => <div>page:ServiceAccountsPage</div> }))
vi.mock('./features/admin/ServiceAccountEditorPage', () => ({ ServiceAccountEditorPage: () => <div>page:ServiceAccountEditorPage</div> }))
vi.mock('./features/admin/RolesPage', () => ({ RolesPage: () => <div>page:RolesPage</div> }))
vi.mock('./features/admin/RoleEditorPage', () => ({ RoleEditorPage: () => <div>page:RoleEditorPage</div> }))
vi.mock('./features/admin/AreasPage', () => ({ AreasPage: () => <div>page:AreasPage</div> }))
vi.mock('./features/admin/AccessPage', () => ({ AccessPage: () => <div>page:AccessPage</div> }))
vi.mock('./features/admin/AuditPage', () => ({ AuditPage: () => <div>page:AuditPage</div> }))
vi.mock('./features/admin/AllApiKeysPage', () => ({ AllApiKeysPage: () => <div>page:AllApiKeysPage</div> }))
vi.mock('./features/account/MyApiKeysPage', () => ({ MyApiKeysPage: () => <div>page:MyApiKeysPage</div> }))
vi.mock('./features/crews/CrewBuilderPage', () => ({ default: () => <div>page:CrewBuilderPage</div> }))
vi.mock('./features/runs/RunDetailPage', () => ({ default: () => <div>page:RunDetailPage</div> }))
vi.mock('./features/workorders/WorkOrderPage', () => ({ default: () => <div>page:WorkOrderPage</div> }))
vi.mock('./features/apidocs/ApiDocsPage', () => ({ default: () => <div>page:ApiDocsPage</div> }))

import type { Permission } from './api/types'
import { PERMISSIONS } from './api/types'
import { makeMe, makeStatus } from './lib/testUtils'

type Api = { status: ReturnType<typeof vi.fn>; me: ReturnType<typeof vi.fn> }

/** The router is created at import time from window.location, so each test loads a fresh App at its URL. */
async function renderAt(path: string, opts: { permissions?: Permission[]; signedIn?: boolean; setupRequired?: boolean } = {}) {
  vi.resetModules()
  window.history.replaceState({}, '', path)
  const { authApi } = (await import('./api/endpoints')) as unknown as { authApi: Api }
  authApi.status.mockResolvedValue(makeStatus({ setup_required: opts.setupRequired ?? false }))
  // Import after resetModules so the class matches the one the app's own modules see
  const { ApiError } = await import('./api/client')
  if (opts.signedIn === false) authApi.me.mockRejectedValue(new ApiError(401, { code: 'unauthenticated', message: 'no' }))
  else authApi.me.mockResolvedValue(makeMe({ permissions: opts.permissions ?? [...PERMISSIONS] }))
  const { App } = await import('./App')
  render(<App />)
  return { authApi }
}

beforeEach(() => localStorage.clear())
afterEach(() => cleanup())

describe('App routing', () => {
  it.each([
    ['/', 'OverviewPage'],
    ['/work-orders', 'WorkOrdersPage'],
    ['/work-orders/new', 'NewWorkOrderPage'],
    ['/work-orders/abc', 'WorkOrderPage'],
    ['/runs', 'RunsPage'],
    ['/runs/r1', 'RunDetailPage'],
    ['/crews', 'CrewsPage'],
    ['/crews/c1', 'CrewBuilderPage'],
    ['/agents', 'AgentsPage'],
    ['/agents/a1', 'AgentEditorPage'],
    ['/tasks', 'TasksPage'],
    ['/tasks/t1', 'TaskEditorPage'],
    ['/schedules', 'SchedulesPage'],
    ['/schedules/s1', 'ScheduleEditorPage'],
    ['/triggers', 'TriggersPage'],
    ['/triggers/t1', 'TriggerEditorPage'],
    ['/llms', 'LlmsPage'],
    ['/pricing', 'PricingPage'],
    ['/settings', 'SettingsPage'],
    ['/history', 'HistoryPage'],
    ['/api-docs', 'ApiDocsPage'],
    ['/admin/users', 'UsersPage'],
    ['/admin/users/u1', 'UserEditorPage'],
    ['/admin/service-accounts', 'ServiceAccountsPage'],
    ['/admin/service-accounts/u1', 'ServiceAccountEditorPage'],
    ['/admin/api-keys', 'AllApiKeysPage'],
    ['/account/api-keys', 'MyApiKeysPage'],
    ['/admin/roles', 'RolesPage'],
    ['/admin/roles/r1', 'RoleEditorPage'],
    ['/admin/areas', 'AreasPage'],
    ['/admin/access', 'AccessPage'],
    ['/admin/audit', 'AuditPage'],
  ])('%s renders %s inside the shell for a fully-permitted person', async (path, page) => {
    await renderAt(path)
    expect(await screen.findByText(`page:${page}`)).toBeTruthy()
    expect(screen.getByRole('navigation')).toBeTruthy()
    expect(screen.getByRole('main')).toBeTruthy()
  })

  it('shows the not-found page inside the shell for unknown addresses', async () => {
    await renderAt('/definitely/not/a/page')
    expect(await screen.findByText('Page not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to overview' }).getAttribute('href')).toBe('/')
  })

  it('login and setup pages render outside the shell and without sign-in', async () => {
    await renderAt('/login', { signedIn: false })
    expect(await screen.findByText('page:LoginPage')).toBeTruthy()
    expect(screen.queryByRole('navigation')).toBeNull()
    cleanup()
    await renderAt('/setup', { signedIn: false, setupRequired: true })
    expect(await screen.findByText('page:SetupPage')).toBeTruthy()
  })
})

describe('App guards', () => {
  it('redirects a signed-out visitor to /login keeping where they were going', async () => {
    await renderAt('/runs?status=failed', { signedIn: false })
    expect(await screen.findByText('page:LoginPage')).toBeTruthy()
    expect(window.location.pathname).toBe('/login')
    expect(new URLSearchParams(window.location.search).get('return_to')).toBe('/runs?status=failed')
  })

  it('redirects to /setup when first-run setup is pending', async () => {
    await renderAt('/', { signedIn: false, setupRequired: true })
    expect(await screen.findByText('page:SetupPage')).toBeTruthy()
    expect(window.location.pathname).toBe('/setup')
  })

  it('opens the admin key list with either admin permission, and My API keys without any', async () => {
    await renderAt('/admin/api-keys', { permissions: ['admin.users'] })
    expect(await screen.findByText('page:AllApiKeysPage')).toBeTruthy()
    cleanup()
    await renderAt('/admin/api-keys', { permissions: ['runs.view'] })
    expect(await screen.findByText(/don.t have access/i)).toBeTruthy()
    cleanup()
    // People who lost account.api_keys can still see and revoke their old keys
    await renderAt('/account/api-keys', { permissions: [] })
    expect(await screen.findByText('page:MyApiKeysPage')).toBeTruthy()
  })

  it('shows "no access" instead of the page when a permission is missing', async () => {
    await renderAt('/admin/users', { permissions: ['runs.view'] })
    expect(await screen.findByText(/don.t have access/i)).toBeTruthy()
    expect(screen.queryByText('page:UsersPage')).toBeNull()
    expect(screen.getByText('admin.users')).toBeTruthy()
  })

  it('"any of" routes admit either permission', async () => {
    await renderAt('/admin/roles', { permissions: ['admin.users'] })
    expect(await screen.findByText('page:RolesPage')).toBeTruthy()
    cleanup()
    await renderAt('/history', { permissions: ['admin.audit'] })
    expect(await screen.findByText('page:HistoryPage')).toBeTruthy()
  })

  it('the API explorer needs only a signed-in session', async () => {
    await renderAt('/api-docs', { permissions: [] })
    expect(await screen.findByText('page:ApiDocsPage')).toBeTruthy()
  })

  it('creating a work order needs work_orders.create, not just view', async () => {
    await renderAt('/work-orders/new', { permissions: ['work_orders.view'] })
    expect(await screen.findByText('work_orders.create')).toBeTruthy()
    expect(screen.queryByText('page:NewWorkOrderPage')).toBeNull()
  })

  it('the overview needs overview.view', async () => {
    await renderAt('/', { permissions: ['runs.view'] })
    expect(await screen.findByText('overview.view')).toBeTruthy()
  })

  it('sidebar hides links the person cannot use', async () => {
    await renderAt('/runs', { permissions: ['runs.view'] })
    await screen.findByText('page:RunsPage')
    const nav = screen.getByRole('navigation')
    expect(nav.textContent).toContain('Runs')
    expect(nav.textContent).not.toContain('People')
    await waitFor(() => expect(nav.textContent).toContain('API explorer'))
  })

  it('applies the stored theme to the document', async () => {
    localStorage.setItem('crew-studio.theme', 'light')
    await renderAt('/')
    await screen.findByText('page:OverviewPage')
    await act(async () => {})
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
