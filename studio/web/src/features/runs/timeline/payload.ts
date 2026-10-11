import type { RunEvent, RunEventType, RunStatus } from '../../../api/types'
import { RUN_STATUSES } from '../../../api/types'

/** Safe accessors for the loosely-typed event payloads. */
export function str(payload: Record<string, unknown>, key: string): string | null {
  const v = payload[key]
  return typeof v === 'string' && v !== '' ? v : null
}

export function num(payload: Record<string, unknown>, key: string): number | null {
  const v = payload[key]
  return typeof v === 'number' && Number.isFinite(v) ? v : null
}

export function bool(payload: Record<string, unknown>, key: string): boolean | null {
  const v = payload[key]
  return typeof v === 'boolean' ? v : null
}

export function obj(payload: Record<string, unknown>, key: string): Record<string, unknown> | null {
  const v = payload[key]
  return typeof v === 'object' && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : null
}

export function runStatusOf(payload: Record<string, unknown>): RunStatus | null {
  const s = str(payload, 'status')
  return s && (RUN_STATUSES as readonly string[]).includes(s) ? (s as RunStatus) : null
}

export const TERMINAL_STATUSES: readonly RunStatus[] = ['succeeded', 'failed', 'cancelled']

export function isTerminal(status: RunStatus | null | undefined): boolean {
  return status !== null && status !== undefined && TERMINAL_STATUSES.includes(status)
}

/** Event categories used by the timeline filter chips. */
export const EVENT_CATEGORIES = ['crew', 'task', 'agent', 'llm', 'tool', 'search', 'delegation', 'budget'] as const
export type EventCategory = (typeof EVENT_CATEGORIES)[number]

export function eventCategory(type: RunEventType): EventCategory | null {
  switch (type) {
    case 'run_status':
    case 'crew_started':
    case 'crew_completed':
    case 'crew_failed':
    case 'artifacts':
      return 'crew'
    case 'task_started':
    case 'task_completed':
    case 'task_failed':
      return 'task'
    case 'agent_started':
      return 'agent'
    case 'llm_call':
      return 'llm'
    case 'tool_started':
    case 'tool_finished':
    case 'tool_error':
    case 'video':
    case 'state':
      return 'tool'
    case 'search':
      return 'search'
    case 'delegation':
      return 'delegation'
    case 'budget':
      return 'budget'
    case 'context_compacted':
      return 'llm'
    case 'log':
      return null
  }
}

/** Agent roles an event involves (delegations involve two). */
export function eventAgents(event: RunEvent): string[] {
  const p = event.payload
  return [str(p, 'agent_role'), str(p, 'from_agent'), str(p, 'to_agent')].filter((a): a is string => a !== null)
}

/** Parses one SSE `data:` line into a RunEvent, or null when it is not a well-formed event. */
export function parseRunEvent(data: string): RunEvent | null {
  let raw: unknown
  try {
    raw = JSON.parse(data)
  } catch {
    return null
  }
  if (typeof raw !== 'object' || raw === null) return null
  const r = raw as Record<string, unknown>
  if (typeof r.seq !== 'number' || typeof r.type !== 'string') return null
  const payload = typeof r.payload === 'object' && r.payload !== null && !Array.isArray(r.payload) ? (r.payload as Record<string, unknown>) : {}
  return { seq: r.seq, type: r.type as RunEventType, payload, created_at: typeof r.created_at === 'string' ? r.created_at : new Date().toISOString() }
}
