import { RUN_KINDS, RUN_STATUSES, TRIGGER_TYPES, type RunKind, type RunStatus, type TriggerType } from '../../api/types'

/** Filters the runs list keeps in the URL. Empty strings mean "any". */
export type RunsFilter = { crew_id: string; status: RunStatus | ''; trigger_type: TriggerType | ''; kind: RunKind | ''; offset: number }

export const TRIGGER_LABELS: Record<TriggerType, string> = {
  manual: 'Manual',
  api: 'API',
  schedule: 'Schedule',
  webhook: 'Webhook',
  work_order: 'Work order',
}

export const KIND_LABELS: Record<RunKind, string> = { crew: 'Crew runs', chat: 'Chat turns', handoff: 'Hand-offs' }

function oneOf<T extends string>(allowed: readonly T[], raw: string | null): T | '' {
  return raw !== null && (allowed as readonly string[]).includes(raw) ? (raw as T) : ''
}

/** Reads the filter from the query string, ignoring unknown statuses or trigger types. */
export function parseRunsFilter(params: URLSearchParams): RunsFilter {
  return {
    crew_id: params.get('crew_id') ?? '',
    status: oneOf(RUN_STATUSES, params.get('status')),
    trigger_type: oneOf(TRIGGER_TYPES, params.get('trigger_type')),
    kind: oneOf(RUN_KINDS, params.get('kind')),
    offset: Math.max(0, Math.floor(Number(params.get('offset')) || 0)),
  }
}

/** Writes a filter back to a query string, leaving out empty values and a zero offset. */
export function runsFilterToParams(filter: RunsFilter): URLSearchParams {
  const qs = new URLSearchParams()
  if (filter.crew_id) qs.set('crew_id', filter.crew_id)
  if (filter.status) qs.set('status', filter.status)
  if (filter.trigger_type) qs.set('trigger_type', filter.trigger_type)
  if (filter.kind) qs.set('kind', filter.kind)
  if (filter.offset > 0) qs.set('offset', String(filter.offset))
  return qs
}

/** True when any filter narrows the list (used to word the empty state). */
export function isFiltered(filter: RunsFilter): boolean {
  return Boolean(filter.crew_id || filter.status || filter.trigger_type || filter.kind)
}
