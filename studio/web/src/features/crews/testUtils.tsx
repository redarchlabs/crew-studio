import { type ReactNode } from 'react'
import { render } from '@testing-library/react'
import { vi } from 'vitest'
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router'
import {
  agentsApi,
  areasApi,
  authApi,
  crewsApi,
  llmsApi,
  pricingApi,
  schedulesApi,
  tasksApi,
  toolsApi,
  triggersApi,
  workOrdersApi,
} from '../../api/endpoints'
import type { Area, Me, Permission, Schedule, Trigger, WorkOrder } from '../../api/types'
import { PERMISSIONS } from '../../api/types'
import { AuthProvider, useAuth } from '../../lib/auth'
import { invalidateAreas } from '../../lib/useAreas'

// NOTE: every test file using these helpers must call `vi.mock('../../api/endpoints')` (or the
// right relative path) so that the endpoint modules below are automocked.

const TS = '2026-01-01T00:00:00Z'

export const AREAS: Area[] = [
  { id: 'area-1', name: 'Finance', description: 'Money', created_at: TS, updated_at: TS },
  { id: 'area-2', name: 'Ops', description: '', created_at: TS, updated_at: TS },
]

export function makeMe(over: Partial<Me> = {}): Me {
  return {
    id: 'me-1',
    kind: 'human',
    name: 'Tester',
    email: 't@example.com',
    all_areas: true,
    area_ids: [],
    permissions: [...PERMISSIONS],
    via: 'session',
    ...over,
  } as Me
}

/** Resets every API mock and installs sane empty defaults. Call from beforeEach. */
export function setupApi(me: Me | null = makeMe()): void {
  vi.resetAllMocks()
  invalidateAreas()
  vi.mocked(authApi.status).mockResolvedValue({ setup_required: false, local_login: true, providers: [] })
  vi.mocked(authApi.me).mockResolvedValue(me as Me)
  vi.mocked(areasApi.list).mockResolvedValue(AREAS)
  for (const api of [agentsApi, tasksApi, crewsApi, llmsApi, pricingApi, schedulesApi, triggersApi, toolsApi]) {
    vi.mocked(api.list).mockResolvedValue([])
  }
  vi.mocked(workOrdersApi.list).mockResolvedValue({ items: [], meta: { total: 0, limit: 10, offset: 0 } })
  vi.mocked(triggersApi.hookUrl).mockImplementation((id: string) => `http://localhost/api/hooks/${id}`)
}

/** Shows the router location so tests can assert navigation. */
function Where() {
  const loc = useLocation()
  return <div data-testid="where">{`${loc.pathname}${loc.search}`}</div>
}

/** Waits for the auth round trip so pages mount with the final permissions. */
function Gate({ children }: { children: ReactNode }) {
  const { loading } = useAuth()
  return loading ? null : <>{children}</>
}

export type RenderOpts = {
  /** Route pattern the element is mounted at, e.g. '/crews/:id' */
  path?: string
  /** URL to start at, e.g. '/crews/crew-1?area=area-1' */
  url?: string
  perms?: readonly Permission[]
  me?: Partial<Me>
  /** Extra routes (path -> element) */
  routes?: { path: string; element: ReactNode }[]
}

/** Renders `ui` in a data router with a real AuthProvider backed by the mocked authApi. */
export function renderRoute(ui: ReactNode, opts: RenderOpts = {}) {
  const { path = '/', url = path, perms, me, routes = [] } = opts
  setMe({ ...me, ...(perms ? { permissions: [...perms] } : {}) })
  const router = createMemoryRouter(
    [
      { path, element: <Gate>{ui}</Gate> },
      ...routes.map((r) => ({ path: r.path, element: r.element })),
      { path: '*', element: <Where /> },
    ],
    { initialEntries: [url] },
  )
  const result = render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  )
  return { router, ...result }
}

function setMe(over: Partial<Me>): void {
  vi.mocked(authApi.me).mockResolvedValue(makeMe(over))
}

/** Every permission except the listed ones. */
export function without(...denied: Permission[]): Permission[] {
  return PERMISSIONS.filter((p) => !denied.includes(p))
}

type StubNode = { id: string; data: { label: string } }

/** Stand-in for FlowCanvas: one button per node that fires onNodeClick like React Flow would. */
export function FlowCanvasStub({ nodes, onNodeClick }: { nodes: StubNode[]; onNodeClick?: (e: unknown, n: StubNode) => void }) {
  return (
    <div data-testid="flow">
      {nodes.map((n) => (
        <button key={n.id} type="button" data-testid={`node-${n.id}`} onClick={(e) => onNodeClick?.(e, n)}>
          {`node:${n.data.label}`}
        </button>
      ))}
    </div>
  )
}

export function makeSchedule(over: Partial<Schedule> = {}): Schedule {
  return {
    id: 's1',
    name: 'Weekly digest',
    description: '',
    crew_id: 'crew-1',
    cron: '0 9 * * 1-5',
    timezone: 'UTC',
    brief: '',
    inputs: {},
    budget_usd: null,
    enabled: true,
    overlap_policy: 'skip',
    target_name: 'Research',
    run_as: 'me-1',
    run_as_name: 'Tester',
    area_ids: ['area-1'],
    next_run_at: null,
    last_run_at: null,
    last_work_order_id: null,
    last_error: null,
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makeTrigger(over: Partial<Trigger> = {}): Trigger {
  return {
    id: 'tr1',
    name: 'Inbound hook',
    description: '',
    crew_id: 'crew-1',
    enabled: true,
    input_map: {},
    brief: '',
    include_payload: false,
    budget_usd: null,
    callback_url: null,
    target_name: 'Research',
    token_prefix: 'cs_abcd',
    run_as: 'me-1',
    run_as_name: 'Tester',
    area_ids: ['area-1'],
    last_fired_at: null,
    fire_count: 0,
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makeWorkOrder(over: Partial<WorkOrder> = {}): WorkOrder {
  return {
    id: 'wo-1',
    number: 7,
    title: 'WO',
    brief: '',
    crew_id: 'crew-1',
    target_name: 'Research',
    inputs: {},
    budget_usd: null,
    area_ids: ['area-1'],
    source: 'schedule',
    source_id: 's1',
    created_by: null,
    created_by_name: '',
    status: 'draft',
    current_run_id: null,
    cost_usd: null,
    run_count: 0,
    submitted_at: null,
    finished_at: null,
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}
