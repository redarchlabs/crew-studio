// Work orders, attachments, journals, schedules, triggers and callbacks. Mirror studio/docs/api.md.

import type { Run, RunStatus } from './types'

export type WorkOrderSource = 'manual' | 'api' | 'schedule' | 'webhook' | 'chat'
export type WorkOrderStatus = 'draft' | RunStatus

export type WorkOrderInput = {
  title: string
  brief: string
  /** The crew to run; null only in a form before one is chosen */
  crew_id: string | null
  inputs: Record<string, string>
  budget_usd: number | null
  submit?: boolean
}

export type WorkOrder = {
  id: string
  number: number
  title: string
  brief: string
  /** Null once the crew was deleted; target_name keeps its name */
  crew_id: string | null
  target_name: string
  inputs: Record<string, string>
  budget_usd: number | null
  area_ids: string[]
  source: WorkOrderSource
  source_id: string | null
  created_by: string | null
  created_by_name: string
  status: WorkOrderStatus
  current_run_id: string | null
  cost_usd: number | null
  run_count: number
  submitted_at: string | null
  finished_at: string | null
  created_at: string
  updated_at: string
  attachments?: Attachment[]
  runs?: Run[]
}

export type SubmitWorkOrderBody = { budget_usd?: number; callback_url?: string }

export type AttachmentKind = 'input' | 'artifact' | 'transcript'

export type Attachment = {
  id: string
  work_order_id: string | null
  run_id: string | null
  kind: AttachmentKind
  filename: string
  content_type: string
  size_bytes: number
  sha256: string
  created_by_name: string
  created_at: string
}

/** Upload limits enforced by the server (docs/api.md, Attachments). */
export const ATTACHMENT_LIMITS = { maxFileBytes: 25 << 20, maxFiles: 20, maxTotalBytes: 100 << 20 } as const

export type JournalEntryKind =
  | 'run'
  | 'task_started'
  | 'task_completed'
  | 'task_failed'
  | 'agent_started'
  | 'llm_call'
  | 'tool_call'
  | 'tool_result'
  | 'tool_error'
  | 'search'
  | 'video'
  | 'delegation'
  /** A specialist's question to the lead, and its outcome (agent chat) */
  | 'consultation'
  | 'budget'
  | 'artifacts'
  | 'state'
  | 'compaction'

export type JournalEntry = {
  seq: number
  at: string
  agent_role: string
  task_name: string
  kind: JournalEntryKind
  title: string
  body: string
  model: string
  cost_usd: number
  tokens: number
}

export type JournalAgent = {
  role: string
  tasks: string[]
  entries: number
  llm_calls: number
  tool_calls: number
  prompt_tokens: number
  completion_tokens: number
  cost_usd: number
  first_at: string | null
  last_at: string | null
}

export type JournalTask = {
  name: string
  agent_role: string
  status: 'pending' | 'running' | 'done' | 'failed'
  started_at: string | null
  finished_at: string | null
  output: string | null
}

export type Journal = {
  run_id: string
  status: RunStatus
  agents: JournalAgent[]
  tasks: JournalTask[]
  entries: JournalEntry[]
  truncated: boolean
}

export type OverlapPolicy = 'skip' | 'allow'

export type ScheduleInput = {
  name: string
  description: string
  /** The crew to run; null only in a form before one is chosen */
  crew_id: string | null
  cron: string
  timezone: string
  brief: string
  inputs: Record<string, string>
  budget_usd: number | null
  enabled: boolean
  overlap_policy: OverlapPolicy
}

export type Schedule = ScheduleInput & {
  id: string
  target_name: string
  run_as: string | null
  run_as_name: string
  area_ids: string[]
  next_run_at: string | null
  last_run_at: string | null
  last_work_order_id: string | null
  last_error: string | null
  created_at: string
  updated_at: string
}

export type SchedulePreview = { next: string[] }

export type TriggerInput = {
  name: string
  description: string
  /** The crew to run; null only in a form before one is chosen */
  crew_id: string | null
  enabled: boolean
  input_map: Record<string, string>
  brief: string
  include_payload: boolean
  budget_usd: number | null
  callback_url: string | null
}

export type Trigger = TriggerInput & {
  id: string
  target_name: string
  token_prefix: string
  signing_secret?: string
  run_as: string | null
  run_as_name: string
  area_ids: string[]
  last_fired_at: string | null
  fire_count: number
  created_at: string
  updated_at: string
  /** Only in the create and rotate responses */
  token?: string
}

export type Delivery = {
  id: string
  run_id: string
  url: string
  status: 'pending' | 'delivered' | 'failed'
  attempts: number
  next_attempt_at: string
  last_status_code: number | null
  last_error: string | null
  delivered_at: string | null
  created_at: string
}
