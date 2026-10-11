// Watches: a schedule that checks a source on every tick and only starts work when something changed or a
// follow-up is due. See docs/crew-state-and-pmo-design.md, sections 5 and 6.

import type { NotifyPerson, Rollup } from './types.trackers'

export type WatchSourceKindKey = 'http_json' | 'web_page' | 'workspace_file' | 'tracker' | 'tracking_list'
export type WatchRunWhen = 'changed' | 'always' | 'changed_or_followups'
export type WatchCheckOutcome = 'started' | 'no_changes' | 'busy' | 'error'

export type WatchSource = {
  kind: WatchSourceKindKey
  path?: string
  items_path?: string
  url?: string
  selector?: string
  file?: string
  format?: '' | 'csv' | 'json'
  /** tracker: Jira project key, Linear team key or KM2 project key, an optional extra JQL clause (Jira), done issues too, a lead override */
  project?: string
  jql?: string
  include_done?: boolean
  lead?: string
  /** tracker and tracking_list: report new comments; days without an update before a child is stale in roll-ups */
  comments?: boolean
  stale_days?: number
}

export type FollowupStep = { name: string; offset: string; notify: string; repeat?: string }

export type FollowupLadder = {
  date_field: string
  skip_when?: Record<string, string[]>
  steps: FollowupStep[]
  quiet_days?: string[]
  timezone?: string
  max_per_tick?: number
}

export type WatchInput = {
  connection_id: string | null
  source: WatchSource
  id_field: string
  tracked_fields: string[]
  run_when: WatchRunWhen
  followups: FollowupLadder | null
  dry_run: boolean
  max_items: number
  /** Tracker tools: how often one person may be mentioned a day (default 3), and comments per run (default 30) */
  mention_cap_per_day?: number
  comment_cap_per_run?: number
}

export type Watch = WatchInput & {
  id: string
  schedule_id: string
  schedule_name: string
  crew_id: string | null
  connection_name: string
  last_checked_at: string | null
  last_changed_at: string | null
  last_error: string | null
  snapshot_at: string | null
  snapshot_items: number
  created_at: string
  updated_at: string
}

export type WatchStats = {
  days: number
  checks: number
  runs_started: number
  runs_skipped: number
  avg_run_tokens: number
  est_tokens_saved: number
  est_cost_saved_usd: number
}

export type WatchCheck = {
  id: string
  checked_at: string
  outcome: WatchCheckOutcome
  added: number
  changed: number
  removed: number
  unchanged: number
  followups_due: number
  work_order_id: string | null
  message: string
}

/** connection is the connection kind a source reads ("tracker" means Jira, Linear or KM2 issues); fields, when set, are the only trackable fields */
export type WatchSourceKindInfo = { kind: WatchSourceKindKey; label: string; connection: string; items: boolean; followups: boolean; fields?: string[] }

export type WatchConnection = { id: string; name: string; kind: string }

export type WatchView = {
  watch: Watch | null
  stats: WatchStats | null
  checks: WatchCheck[]
  source_kinds: WatchSourceKindInfo[]
  connections: WatchConnection[]
}

/** The "changes" input of a watched work order. */
export type WatchChanges = {
  source: string
  checked_at: string
  previous_check_at: string | null
  added: { id: string; fields?: Record<string, unknown> }[]
  changed: { id: string; fields: Record<string, { old: unknown; new: unknown }> }[]
  removed: { id: string; reason: string }[]
  new_comments?: { id: string; author: string; at: string; excerpt: string; truncated?: boolean }[]
  new_comments_omitted?: number
  unchanged_count: number
  added_fields_omitted?: boolean
}

export type FollowupDue = {
  item: string
  subject: string
  step: string
  action: string
  notify: string
  notify_value?: unknown
  /** Tracker items: who "owner" or "lead" is, to mention by account_id */
  notify_person?: NotifyPerson
  date: string
  step_date: string
  days_late: number
  repeat?: string
  last_sent_at: string | null
  fields: Record<string, unknown>
}

/** The "followups_due" input of a watched work order. */
export type WatchFollowups = { due: FollowupDue[]; more_waiting: number; undated_items?: number; dry_run: boolean; how_to_record: string }

export type WatchPreview = {
  changes: WatchChanges
  followups_due: WatchFollowups | null
  would_run: boolean
  reason: string
  item_count: number
  sample: { id: string; fields: Record<string, unknown> }[]
  /** tracking_list sources: every tracked item's roll-up */
  rollups?: { items: Rollup[]; stale_days: number }
}

/** A watch on the crew's State tab. */
export type CrewWatch = Watch & { stats: WatchStats }
