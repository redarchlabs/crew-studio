import type { ReactElement } from 'react'
import { render } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, useLocation } from 'react-router'
import { vi } from 'vitest'
import type {
  Interactions, Journal, JournalEntry, LlmCall, Permission, Run, RunEvent, RunEventType, RunSpec, Revision, WorkOrder,
} from '../../api/types'
import { makeAgent, makeCrew, makeLlm, makeTask } from '../../lib/testFixtures'

const TS = '2026-01-01T00:00:00Z'

export function makeRun(over: Partial<Run> = {}): Run {
  return {
    id: 'run-12345678-abcd',
    crew_id: 'crew-1',
    crew_name: 'Research crew',
    status: 'succeeded',
    inputs: {},
    budget_usd: 1,
    output: null,
    error: null,
    cost_usd: 0.25,
    usage: null,
    area_ids: ['area-1'],
    created_by: null,
    created_by_name: '',
    trigger_type: 'manual',
    work_order_id: null,
    created_at: TS,
    started_at: '2026-01-01T00:00:01Z',
    finished_at: '2026-01-01T00:00:11Z',
    ...over,
  }
}

export function makeSpec(over: Partial<RunSpec> = {}): RunSpec {
  return {
    crew: makeCrew(),
    agents: [makeAgent({ id: 'agent-1', name: 'Analyst', role: 'Analyst' })],
    tasks: [makeTask({ id: 'task-1', name: 'Research' })],
    llms: [makeLlm({ id: 'llm-1' })],
    pricing: [],
    settings: {
      default_budget_usd: 1, hard_stop_multiplier: 2, search_model: 'x', grounded_search_cost_per_1000: 0,
      grounded_searches_free: 0, grounding_free_period: 'day', max_concurrent_runs: 1, compact_context_tokens: 0,
    },
    inputs: { topic: 'cats' },
    budget_usd: 1,
    ...over,
  }
}

export function makeWorkOrder(over: Partial<WorkOrder> = {}): WorkOrder {
  return {
    id: 'wo-1', number: 7, title: 'Write report', brief: '', crew_id: 'crew-1',
    target_name: 'Research crew', inputs: {}, budget_usd: null, area_ids: [], source: 'manual', source_id: null,
    created_by: null, created_by_name: '', status: 'succeeded', current_run_id: null, cost_usd: 0.5, run_count: 1,
    submitted_at: null, finished_at: null, created_at: TS, updated_at: TS, ...over,
  } as WorkOrder
}

export function makeEvent(seq: number, type: RunEventType, payload: Record<string, unknown> = {}): RunEvent {
  return { seq, type, payload, created_at: `2026-01-01T00:00:${String(seq % 60).padStart(2, '0')}Z` }
}

export function makeEntry(over: Partial<JournalEntry> & { seq: number }): JournalEntry {
  return {
    at: TS, agent_role: 'Analyst', task_name: 'Research', kind: 'llm_call', title: 'Entry', body: '', model: '',
    cost_usd: 0, tokens: 0, ...over,
  }
}

export function makeJournal(over: Partial<Journal> = {}): Journal {
  return {
    run_id: 'run-1', status: 'running', agents: [], tasks: [], entries: [], truncated: false, ...over,
  }
}

export function makeLlmCall(over: Partial<LlmCall> & { seq: number }): LlmCall {
  return {
    agent_role: 'Analyst', task_name: 'Research', model: 'openai/gpt-x',
    usage: { prompt_tokens: 100, completion_tokens: 20, cached_prompt_tokens: 0 }, cost_usd: 0.01,
    messages: [{ role: 'user', content: 'hello' }], response: 'world', created_at: TS, ...over,
  }
}

export function makeRevision(over: Partial<Revision> & { id: number }): Revision {
  return {
    entity_type: 'agent', entity_id: 'abcdef123456', action: 'update', snapshot: { name: 'Analyst' },
    actor_id: null, actor_name: 'Alice', created_at: TS, ...over,
  }
}

export const EMPTY_INTERACTIONS: Interactions = { nodes: [], edges: [] }

// ------------------------------------------------------------------ auth
const authState: { perms: Set<string> } = { perms: new Set() }

export function setPerms(...perms: Permission[]): void {
  authState.perms = new Set(perms)
}

/** Factory for vi.mock('.../lib/auth', ...): a useAuth driven by setPerms(). */
export function authMockModule() {
  const useAuth = () => ({
    me: null,
    can: (p: string) => authState.perms.has(p),
    canAny: (...ps: string[]) => ps.some((p) => authState.perms.has(p)),
  })
  return { useAuth }
}

// ------------------------------------------------------------------ router
function LocationProbe({ onLocation }: { onLocation: (search: string, pathname: string) => void }) {
  const loc = useLocation()
  onLocation(loc.search, loc.pathname)
  return null
}

export type RouterHarness = { search: () => string; pathname: () => string; unmount: () => void; rerender: () => void }

/** Renders `ui` at `path` inside a memory router, exposing the current location. */
export function renderAt(ui: ReactElement, { route = '/', path = '*' }: { route?: string; path?: string } = {}): RouterHarness {
  let search = ''
  let pathname = ''
  const router = createMemoryRouter(
    [{ path, element: <>{ui}<LocationProbe onLocation={(s, p) => { search = s; pathname = p }} /></> }, { path: '*', element: <LocationProbe onLocation={(s, p) => { search = s; pathname = p }} /> }],
    { initialEntries: [route] },
  )
  const r = render(<RouterProvider router={router} />)
  return { search: () => search, pathname: () => pathname, unmount: r.unmount, rerender: () => r.rerender(<RouterProvider router={router} />) }
}

// ------------------------------------------------------------------ SSE
export class FakeEventSource {
  static instances: FakeEventSource[] = []
  static reset(): void {
    FakeEventSource.instances = []
  }
  static get last(): FakeEventSource {
    return FakeEventSource.instances[FakeEventSource.instances.length - 1]
  }
  url: string
  closed = false
  private listeners = new Map<string, Set<(e: unknown) => void>>()
  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, fn: (e: unknown) => void): void {
    const set = this.listeners.get(type) ?? new Set()
    set.add(fn)
    this.listeners.set(type, set)
  }
  removeEventListener(type: string, fn: (e: unknown) => void): void {
    this.listeners.get(type)?.delete(fn)
  }
  close(): void {
    this.closed = true
  }
  emit(type: string, data?: string): void {
    for (const fn of this.listeners.get(type) ?? []) fn(data === undefined ? {} : { data })
  }
  open(): void {
    this.emit('open')
  }
  message(event: RunEvent, type: 'run_event' | 'message' = 'run_event'): void {
    this.emit(type, JSON.stringify(event))
  }
  error(): void {
    this.emit('error')
  }
}

export function installFakeEventSource(): void {
  FakeEventSource.reset()
  vi.stubGlobal('EventSource', FakeEventSource)
}
