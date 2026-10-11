import type { Tone } from '../../components/StatusBadge'

export const AUDIT_ACTION_LABELS: Record<string, string> = {
  login: 'Signed in',
  login_failed: 'Sign-in failed',
  logout: 'Signed out',
  setup: 'First-run setup',
  sessions_revoked: 'Signed out everywhere',
  api_key_created: 'API key created',
  api_key_revoked: 'API key revoked',
  api_key_rejected: 'API key refused',
  mcp_change: 'Changed through MCP',
  connection_created: 'Connection added',
  connection_updated: 'Connection changed',
  connection_secret_replaced: 'Connection secret replaced',
  connection_deleted: 'Connection deleted',
  crew_connections_set: 'Crew connections changed',
  note_written: 'Crew note edited',
  note_rolled_back: 'Crew note rolled back',
  note_deleted: 'Crew note deleted',
  action_log_entry_deleted: 'Action log entry deleted',
  watch_saved: 'Schedule watch saved',
  watch_deleted: 'Schedule watch removed',
  watch_snapshot_cleared: 'Watch snapshot cleared',
}

export const AUDIT_ACTION_TONES: Record<string, Tone> = {
  login: 'ok',
  login_failed: 'danger',
  logout: 'neutral',
  setup: 'violet',
  sessions_revoked: 'warn',
  api_key_created: 'blue',
  api_key_revoked: 'warn',
  api_key_rejected: 'danger',
  mcp_change: 'violet',
  connection_created: 'blue',
  connection_updated: 'neutral',
  connection_secret_replaced: 'warn',
  connection_deleted: 'warn',
  crew_connections_set: 'neutral',
  note_written: 'neutral',
  note_rolled_back: 'neutral',
  note_deleted: 'warn',
  action_log_entry_deleted: 'warn',
  watch_saved: 'neutral',
  watch_deleted: 'warn',
  watch_snapshot_cleared: 'neutral',
}

const MAX_VALUE = 80

function show(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  const json = JSON.stringify(value)
  return json.length > MAX_VALUE ? `${json.slice(0, MAX_VALUE - 1)}…` : json
}

/** Audit details as compact "key: value" pairs, in a stable (alphabetical) order. */
export function detailPairs(details: Record<string, unknown> | null | undefined): { key: string; value: string }[] {
  return Object.keys(details ?? {})
    .sort()
    .map((key) => ({ key, value: show((details ?? {})[key]) }))
}
