import { lazy, type ReactNode } from 'react'
import { createBrowserRouter, Link, RouterProvider } from 'react-router'
import { AppShell } from './layout/AppShell'
import { ThemeProvider } from './lib/theme'
import { AuthProvider, RequireAuth, RequirePermission } from './lib/auth'
import type { Permission } from './api/types'
import { EmptyState } from './components/States'
import { LoginPage } from './features/auth/LoginPage'
import { SetupPage } from './features/auth/SetupPage'
import { OverviewPage } from './features/overview/OverviewPage'
import { LlmsPage } from './features/llms/LlmsPage'
import { PricingPage } from './features/pricing/PricingPage'
import { SettingsPage } from './features/settings/SettingsPage'
import { AgentsPage } from './features/agents/AgentsPage'
import { AgentEditorPage } from './features/agents/AgentEditorPage'
import { TasksPage } from './features/tasks/TasksPage'
import { TaskEditorPage } from './features/tasks/TaskEditorPage'
import { CrewsPage } from './features/crews/CrewsPage'
import { RunsPage } from './features/runs/RunsPage'
import { HistoryPage } from './features/history/HistoryPage'
import { WorkOrdersPage } from './features/workorders/WorkOrdersPage'
import { NewWorkOrderPage } from './features/workorders/NewWorkOrderPage'
import { SchedulesPage } from './features/schedules/SchedulesPage'
import { ScheduleEditorPage } from './features/schedules/ScheduleEditorPage'
import { TriggersPage } from './features/triggers/TriggersPage'
import { TriggerEditorPage } from './features/triggers/TriggerEditorPage'
import { UsersPage } from './features/admin/UsersPage'
import { UserEditorPage } from './features/admin/UserEditorPage'
import { ServiceAccountsPage } from './features/admin/ServiceAccountsPage'
import { ServiceAccountEditorPage } from './features/admin/ServiceAccountEditorPage'
import { RolesPage } from './features/admin/RolesPage'
import { RoleEditorPage } from './features/admin/RoleEditorPage'
import { AreasPage } from './features/admin/AreasPage'
import { AccessPage } from './features/admin/AccessPage'
import { AuditPage } from './features/admin/AuditPage'
import { AllApiKeysPage } from './features/admin/AllApiKeysPage'
import { MyApiKeysPage } from './features/account/MyApiKeysPage'
import { ConnectionsPage } from './features/connections/ConnectionsPage'
import { RouteError } from './components/RouteError'

// Graph-heavy screens load on demand to keep the first paint light.
const CrewBuilderPage = lazy(() => import('./features/crews/CrewBuilderPage'))
const RunDetailPage = lazy(() => import('./features/runs/RunDetailPage'))
const WorkOrderPage = lazy(() => import('./features/workorders/WorkOrderPage'))
// Swagger UI is large; only people opening the API explorer download it
const ApiDocsPage = lazy(() => import('./features/apidocs/ApiDocsPage'))
const HelpPage = lazy(() => import('./features/help/HelpPage'))
const ChatPage = lazy(() => import('./features/chat/ChatPage'))
const PmoSetupPage = lazy(() => import('./features/pmo/PmoSetupPage'))

function NotFound() {
  return <EmptyState title="Page not found" hint="That address does not match any screen." action={<Link className="text-accent" to="/">Back to overview</Link>} />
}

/** A route whose page needs a permission (or any of several). */
function guarded(path: string, element: ReactNode, perm: Permission | Permission[]) {
  const props = Array.isArray(perm) ? { any: perm } : { perm }
  return { path, element: <RequirePermission {...props}>{element}</RequirePermission> }
}

const router = createBrowserRouter([
  { path: '/login', element: <LoginPage />, errorElement: <RouteError /> },
  { path: '/setup', element: <SetupPage />, errorElement: <RouteError /> },
  {
    element: <RequireAuth />,
    errorElement: <RouteError />,
    children: [
      {
        path: '/',
        element: <AppShell />,
        errorElement: <RouteError />,
        children: [
          { index: true, element: <RequirePermission perm="overview.view"><OverviewPage /></RequirePermission> },
          guarded('work-orders', <WorkOrdersPage />, 'work_orders.view'),
          guarded('work-orders/new', <NewWorkOrderPage />, 'work_orders.create'),
          guarded('work-orders/:id', <WorkOrderPage />, 'work_orders.view'),
          guarded('runs', <RunsPage />, 'runs.view'),
          guarded('runs/:id', <RunDetailPage />, 'runs.view'),
          guarded('chat', <ChatPage />, 'crews.chat'),
          guarded('chat/:conversationId', <ChatPage />, 'crews.chat'),
          guarded('crews', <CrewsPage />, 'crews.view'),
          guarded('crews/:id', <CrewBuilderPage />, 'crews.view'),
          guarded('pmo/setup', <PmoSetupPage />, 'crews.create'),
          guarded('agents', <AgentsPage />, 'agents.view'),
          guarded('agents/:id', <AgentEditorPage />, 'agents.view'),
          guarded('tasks', <TasksPage />, 'tasks.view'),
          guarded('tasks/:id', <TaskEditorPage />, 'tasks.view'),
          guarded('schedules', <SchedulesPage />, 'schedules.view'),
          guarded('schedules/:id', <ScheduleEditorPage />, 'schedules.view'),
          guarded('triggers', <TriggersPage />, 'triggers.view'),
          guarded('triggers/:id', <TriggerEditorPage />, 'triggers.view'),
          guarded('llms', <LlmsPage />, 'llms.view'),
          guarded('pricing', <PricingPage />, 'pricing.view'),
          guarded('settings', <SettingsPage />, 'settings.view'),
          guarded('connections', <ConnectionsPage />, 'connections.view'),
          guarded('history', <HistoryPage />, ['history.view', 'admin.audit']),
          { path: 'api-docs', element: <ApiDocsPage /> },
          // Anyone may see and revoke their own keys; creating one needs account.api_keys
          { path: 'account/api-keys', element: <MyApiKeysPage /> },
          // Everyone signed in; the server only returns help for what the person can use
          { path: 'help', element: <HelpPage /> },
          { path: 'help/:slug', element: <HelpPage /> },
          guarded('admin/users', <UsersPage />, 'admin.users'),
          guarded('admin/users/:id', <UserEditorPage />, 'admin.users'),
          guarded('admin/service-accounts', <ServiceAccountsPage />, 'admin.service_accounts'),
          guarded('admin/service-accounts/:id', <ServiceAccountEditorPage />, 'admin.service_accounts'),
          guarded('admin/api-keys', <AllApiKeysPage />, ['admin.users', 'admin.service_accounts']),
          guarded('admin/roles', <RolesPage />, ['admin.roles', 'admin.users']),
          guarded('admin/roles/:id', <RoleEditorPage />, ['admin.roles', 'admin.users']),
          guarded('admin/areas', <AreasPage />, 'admin.areas'),
          guarded('admin/access', <AccessPage />, 'admin.access'),
          guarded('admin/audit', <AuditPage />, 'admin.audit'),
          { path: '*', element: <NotFound /> },
        ],
      },
    ],
  },
])

export function App() {
  return (
    <ThemeProvider>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </ThemeProvider>
  )
}
