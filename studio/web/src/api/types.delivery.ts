/** Report delivery and the PMO setup (docs/crew-state-and-pmo-design.md, section 12). */

export type DestinationKind = 'slack' | 'teams' | 'email' | 'km2'
export type DeliveryKind = DestinationKind | 'work_order' | 'tracker'
export type DeliveryStatus = 'pending' | 'sent' | 'would_send' | 'failed'

/** One place a schedule's reports go. */
export type ReportDestination = {
  id: string
  schedule_id: string
  position: number
  kind: DestinationKind
  connection_id: string
  connection_name: string
  recipients: string[]
  folder: string
  created_at: string
}

export type ReportDestinationInput = {
  kind: DestinationKind
  connection_id: string
  recipients: string[]
  folder: string
}

/** A connection a destination may use (never its secrets). */
export type DestinationConnection = { id: string; name: string; kind: string }

export type DestinationsView = {
  destinations: ReportDestination[]
  /** Only for people who can manage the schedule */
  connections: DestinationConnection[]
  kinds: DestinationKind[]
}

export type ReportDelivery = {
  id: string
  report_id: string
  position: number
  kind: DeliveryKind
  connection_id: string | null
  connection_name: string
  recipients: string[]
  folder: string
  status: DeliveryStatus
  message: string
  attempts: number
  sent_at: string | null
  updated_at: string
}

export type ReportCadence = 'weekly' | 'daily'

/** One open tracked item in a report snapshot. All text comes from the tracker: render it as text, links via safeHref. */
export type DueItem = {
  key: string
  title: string
  url?: string
  assignee?: string
  status: string
  status_category: string
  due_date?: string
  labels: string[]
  blocked: boolean
  parent?: string
  epic?: string
  updated_at?: string
}

export type DueKPIs = { open: number; overdue: number; due_today: number; due_7: number; blocked: number; unassigned_soon: number; done_since_prev: number | null }

export type DuePerson = { name: string; open: number; overdue: number; due_7: number }

export type DueMove = { key: string; title?: string; from: string; to: string }

/** The project's tracked work when the report was delivered, computed by Crew Studio (not the model). */
export type DueSnapshot = {
  version: number
  generated_at: string
  timezone: string
  /** The local date in the schedule's time zone */
  today: string
  cadence: ReportCadence
  project: string
  previous_report_id?: string
  truncated: boolean
  items: DueItem[]
  kpis: DueKPIs
  /** Change since the previous report of the same cadence; null on the first */
  deltas: DueKPIs | null
  sections: {
    overdue: { key: string; days_late: number }[]
    due_today: string[]
    due_next_7: string[]
    blocked: string[]
    unassigned_soon: string[]
    by_person: DuePerson[]
  }
  changes: { newly_overdue: string[]; finished: { key: string; title: string }[]; slipped: DueMove[]; reassigned: DueMove[]; new: string[] } | null
  timeline: { date: string; count: number }[]
  status_mix: { to_do: number; in_progress: number; done: number; canceled: number }
  trend: ({ date: string } & DueKPIs)[]
}

export type Report = {
  id: string
  crew_id: string
  schedule_id: string | null
  run_id: string | null
  work_order_id: string | null
  project: string
  project_slug: string
  /** The ISO week the report was made in */
  week: string
  /** weekly (the status report) or daily; older servers leave it out, which means weekly */
  cadence?: ReportCadence
  /** What the report covers: the ISO week (2026-W41), or a daily report's local date (2026-10-06) */
  period?: string
  /** The tracked work when it was delivered; null when the tracker could not be read then (older servers leave it out) */
  snapshot?: DueSnapshot | null
  title: string
  markdown: string
  bytes: number
  dry_run: boolean
  created_at: string
  deliveries: ReportDelivery[]
  /** How many deliveries "Retry failed deliveries" would try again */
  retryable: number
}

export type WorkOrderReports = {
  reports: Report[]
  /** Set by a retry */
  retried: number
  sent: number
}

export type PMOChoice = { id: string; name: string; kind?: string; area_ids?: string[]; /** LLMs: what is wrong before it runs, such as its provider key missing */ warning?: string }

export type PMOOptions = {
  trackers: PMOChoice[]
  destinations: PMOChoice[]
  llms: PMOChoice[]
  coordinator_llm_id: string | null
  reporter_llm_id: string | null
  sweep_cron: string
  report_cron: string
}

export type PMORequest = {
  name: string
  area_ids: string[]
  tracker_connection_id: string
  timezone: string
  coordinator_llm_id: string | null
  reporter_llm_id: string | null
  destinations: ReportDestinationInput[]
  enabled: boolean
}

export type PMOInstalled = {
  crew: { id: string; name: string }
  agents: { id: string; name: string }[]
  tasks: { id: string; name: string }[]
  schedules: { id: string; name: string; cron: string; timezone: string }[]
}
