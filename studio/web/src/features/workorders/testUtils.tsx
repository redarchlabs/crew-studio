import { vi } from 'vitest'
import { render } from '@testing-library/react'
import type { ReactElement, ReactNode } from 'react'
import { createMemoryRouter, Outlet, RouterProvider, useLocation } from 'react-router'
import type {
  Attachment,
  InteractionEdge,
  InteractionNode,
  InteractionStats,
  Interactions,
  Journal,
  JournalEntry,
  Run,
  WorkOrder,
} from '../../api/types'

const TS = '2026-10-03T10:00:00Z'

// ----------------------------------------------------------------------------- fixtures
export function makeWorkOrder(over: Partial<WorkOrder> = {}): WorkOrder {
  return {
    id: 'wo-1',
    number: 7,
    title: 'Quarterly review',
    brief: 'Review the **vendors**',
    crew_id: 'crew-1',
    target_name: 'Research',
    inputs: {},
    budget_usd: null,
    area_ids: ['area-1'],
    source: 'manual',
    source_id: null,
    created_by: 'u1',
    created_by_name: 'Ada',
    status: 'draft',
    current_run_id: null,
    cost_usd: null,
    run_count: 0,
    submitted_at: null,
    finished_at: null,
    created_at: TS,
    updated_at: TS,
    attachments: [],
    runs: [],
    ...over,
  }
}

export function makeRun(over: Partial<Run> = {}): Run {
  return {
    id: 'run-1',
    crew_id: null,
    crew_name: '',
    status: 'succeeded',
    inputs: {},
    budget_usd: 2,
    output: null,
    error: null,
    cost_usd: 0.5,
    usage: null,
    area_ids: ['area-1'],
    created_by: 'u1',
    created_by_name: 'Ada',
    trigger_type: 'work_order',
    work_order_id: 'wo-1',
    created_at: TS,
    started_at: TS,
    finished_at: '2026-10-03T10:01:00Z',
    ...over,
  }
}

export function makeAttachment(over: Partial<Attachment> = {}): Attachment {
  return {
    id: 'att-1',
    work_order_id: 'wo-1',
    run_id: null,
    kind: 'input',
    filename: 'notes.txt',
    content_type: 'text/plain',
    size_bytes: 120,
    sha256: 'abcdef0123456789abcdef',
    created_by_name: 'Ada',
    created_at: TS,
    ...over,
  }
}

let seq = 0
export function makeEntry(over: Partial<JournalEntry> = {}): JournalEntry {
  seq += 1
  return { seq, at: TS, agent_role: '', task_name: '', kind: 'run', title: `entry ${seq}`, body: '', model: '', cost_usd: 0, tokens: 0, ...over }
}

export function makeJournal(over: Partial<Journal> = {}): Journal {
  return { run_id: 'run-1', status: 'running', agents: [], tasks: [], entries: [], truncated: false, ...over }
}

export const ZERO_STATS: InteractionStats = { llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, duration_ms: 0 }

export function makeNode(over: Partial<InteractionNode> & { id: string }): InteractionNode {
  return { kind: 'task', label: over.id, stats: { ...ZERO_STATS }, ...over }
}

export function makeEdge(over: Partial<InteractionEdge> & { id: string; source: string; target: string }): InteractionEdge {
  return { kind: 'assigned', count: 1, cost_usd: 0, ...over }
}

export function makeInteractions(nodes: InteractionNode[], edges: InteractionEdge[] = []): Interactions {
  return { nodes, edges }
}

// ----------------------------------------------------------------------------- module mocks
/** Factory for vi.mock('.../api/endpoints'): every function is a fresh vi.fn. */
export function endpointsMock() {
  return {
    workOrdersApi: {
      list: vi.fn(),
      get: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      remove: vi.fn(),
      submit: vi.fn(),
      cancel: vi.fn(),
      journal: vi.fn(),
      upload: vi.fn(),
      removeAttachment: vi.fn(),
    },
    runsApi: {
      get: vi.fn(),
      interactions: vi.fn(),
      artifacts: vi.fn(),
      llmCalls: vi.fn(),
      events: vi.fn(),
      deliveries: vi.fn(),
    },
    attachmentsApi: { contentUrl: (id: string, download = false) => `/api/attachments/${id}/content${download ? '?download=1' : ''}` },
    tasksApi: { list: vi.fn() },
    crewsApi: { list: vi.fn() },
    agentsApi: { list: vi.fn() },
    areasApi: { list: vi.fn().mockResolvedValue([{ id: 'area-1', name: 'Finance', description: '' }]) },
  }
}

/** Mutable permission set behind the mocked lib/auth module. */
export const authState = { permissions: new Set<string>() }
export function setPermissions(...perms: string[]) {
  authState.permissions = new Set(perms)
}

/** Factory for vi.mock('.../lib/auth'). */
export function authMock() {
  const can = (p: string) => authState.permissions.has(p)
  const canAny = (...ps: string[]) => ps.some(can)
  return {
    useAuth: () => ({ can, canAny, me: { id: 'u1', name: 'Ada', all_areas: true, area_ids: [], permissions: [...authState.permissions] }, inMyAreas: () => true }),
    Can: ({ perm, any, children, fallback = null }: { perm?: string; any?: string[]; children: ReactNode; fallback?: ReactNode }) => {
      const allowed = perm ? can(perm) : any ? canAny(...any) : false
      return <>{allowed ? children : fallback}</>
    },
  }
}

// ----------------------------------------------------------------------------- rendering
export type RouterHarness = ReturnType<typeof render> & { location: () => { pathname: string; search: string; state: unknown } }

/**
 * Renders under a data router (useBlocker needs one). Navigating to a path no route matches shows
 * "NAV:<pathname>" so tests can assert where the page went.
 */
export function renderRoute(ui: ReactElement, opts: { path?: string; url?: string; extraRoutes?: { path: string; element: ReactElement }[] } = {}): RouterHarness {
  const { path = '/', url = path, extraRoutes = [] } = opts
  const current = { pathname: '/', search: '', state: null as unknown }
  function Layout() {
    const l = useLocation()
    current.pathname = l.pathname
    current.search = l.search
    current.state = l.state
    return <Outlet />
  }
  function Fallback() {
    const l = useLocation()
    return <div data-testid="nav">NAV:{l.pathname}</div>
  }
  const router = createMemoryRouter(
    [{ element: <Layout />, children: [{ path, element: ui }, ...extraRoutes, { path: '*', element: <Fallback /> }] }],
    { initialEntries: [url] },
  )
  const result = render(<RouterProvider router={router} />)
  return { ...result, location: () => ({ ...current }) }
}

/** Flush pending promises/microtasks. */
export const tick = () => new Promise<void>((r) => setTimeout(r, 0))

export function imageFile(name = 'image.png', type = 'image/png', size = 8): File {
  return new File([new Uint8Array(size)], name, { type })
}
