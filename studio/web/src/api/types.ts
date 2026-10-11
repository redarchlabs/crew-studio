// Types mirror studio/docs/api.md exactly. Keep field names in snake_case.

import type { WorkOrder } from './types.workorders'

export type ApiErrorBody = {
  code: string
  message: string
  details?: string[]
}

export type PageMeta = {
  total: number
  limit: number
  offset: number
}

export type Envelope<T> = {
  success: boolean
  data: T | null
  error: ApiErrorBody | null
  meta?: PageMeta
}

export type Page<T> = {
  items: T[]
  meta: PageMeta
}

// ---------------------------------------------------------------- LLMs
export type LLM = {
  id: string
  name: string
  model: string
  temperature: number | null
  max_completion_tokens: number | null
  /** Reasoning depth for reasoning models (OpenAI reasoning_effort, Claude effort); null = model default */
  reasoning_effort: ReasoningEffort | null
  /** OpenAI endpoint; reasoning models such as gpt-6.1-sol need "responses" for tool calling */
  api: LLMApi
  /** Accepts images and PDFs: work order files are passed to agents on this model */
  vision: boolean
  created_at: string
  updated_at: string
}
export const REASONING_EFFORTS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const
export type ReasoningEffort = (typeof REASONING_EFFORTS)[number]
export type LLMApi = 'completions' | 'responses'
export type LLMInput = Omit<LLM, 'id' | 'created_at' | 'updated_at'>

// ---------------------------------------------------------------- Tools
/** What a tool does outside the run (runner/tools_catalog.json): agent chat gives an agent only tools that read */
export type ToolEffect = 'reads' | 'proposes' | 'changes'
/** What a tool needs besides keys: crew state, a tracker or KM2 connection, a schedule */
export type ToolNeed = 'crew' | 'tracker' | 'km2' | 'schedule'

export type Tool = {
  key: string
  name: string
  description: string
  requires_env: string[]
  available: boolean
  effect: ToolEffect
  needs: ToolNeed[]
}

// ---------------------------------------------------------------- Pricing
export type ModelPrice = {
  id: string
  model: string
  input_per_mtok: number
  cached_input_per_mtok: number
  /** Price of tokens written to the prompt cache (Anthropic charges extra); null means the input price */
  cache_write_per_mtok: number | null
  output_per_mtok: number
  notes: string
  created_at: string
  updated_at: string
}
export type ModelPriceInput = Omit<ModelPrice, 'id' | 'created_at' | 'updated_at'>

// ---------------------------------------------------------------- Settings
export type Settings = {
  default_budget_usd: number
  hard_stop_multiplier: number
  search_model: string
  grounded_search_cost_per_1000: number
  grounded_searches_free: number
  grounding_free_period: GroundingPeriod
  max_concurrent_runs: number
  /** An agent's context is compacted during a run once it passes this many tokens; 0 never */
  compact_context_tokens: number
}

export type GroundingPeriod = 'day' | 'month'

// ---------------------------------------------------------------- History
export const ENTITY_TYPES = [
  'llm', 'pricing', 'agent', 'task', 'crew', 'settings', 'work_order', 'schedule', 'trigger',
  'area', 'role', 'user', 'api_key', 'access_settings',
] as const
export type EntityType = (typeof ENTITY_TYPES)[number]
/** Revision types that also need admin.audit to read. */
export const ADMIN_ENTITY_TYPES: readonly EntityType[] = ['area', 'role', 'user', 'api_key', 'access_settings']

export type Revision = {
  id: number
  entity_type: EntityType
  entity_id: string
  action: 'create' | 'update' | 'delete'
  snapshot: Record<string, unknown>
  actor_id: string | null
  actor_name: string
  created_at: string
}

// ---------------------------------------------------------------- Agents
export type Agent = {
  id: string
  name: string
  role: string
  goal: string
  backstory: string
  llm_id: string | null
  tools: string[]
  max_iter: number
  allow_delegation: boolean
  verbose: boolean
  /** Functional areas; at least one */
  area_ids: string[]
  created_at: string
  updated_at: string
}
export type AgentInput = Omit<Agent, 'id' | 'created_at' | 'updated_at'>

// ---------------------------------------------------------------- Tasks
export type Task = {
  id: string
  name: string
  description: string
  expected_output: string
  agent_id: string | null
  context_task_ids: string[]
  markdown: boolean
  area_ids: string[]
  created_at: string
  updated_at: string
}
export type TaskInput = Omit<Task, 'id' | 'created_at' | 'updated_at'>

// ---------------------------------------------------------------- Crews
export type CrewInput = { key: string; label: string; default: string; required: boolean }
export type CrewProcess = 'sequential' | 'hierarchical'

export type Crew = {
  id: string
  name: string
  description: string
  process: CrewProcess
  manager_llm_id: string | null
  agent_ids: string[]
  task_ids: string[]
  inputs: CrewInput[]
  budget_usd: number
  area_ids: string[]
  created_at: string
  updated_at: string
}
export type CrewBody = Omit<Crew, 'id' | 'created_at' | 'updated_at'>

/** GET /tasks/{id}/crew-draft: an unsaved crew that runs the task and the tasks it takes context from */
export type CrewDraft = Omit<CrewBody, 'manager_llm_id'> & { manager_llm_id?: string | null }

/** warnings do not stop a run but it is likely to fail, such as a model whose provider key is missing */
export type CrewValidation = { valid: boolean; errors: string[]; warnings?: string[] }

// ---------------------------------------------------------------- Runs
export const RUN_STATUSES = ['queued', 'running', 'succeeded', 'failed', 'cancelled'] as const
export type RunStatus = (typeof RUN_STATUSES)[number]

export type Usage = {
  prompt_tokens: number
  completion_tokens: number
  cached_prompt_tokens: number
  /** Prompt tokens written to the provider's prompt cache (part of prompt_tokens) */
  cache_write_tokens?: number
  successful_requests: number
}

export type TaskOutput = {
  position: number
  task_name: string
  agent_role: string
  raw: string
  /** What the task cost when it ran; left out for older runs */
  cost_usd?: number
  /** Set when a resumed run copied this output from the run that produced it */
  reused_from_run_id?: string
  /** That run was deleted, or is not one you may open */
  reused_from_unavailable?: boolean
}

/** Where a failed or cancelled run could be resumed: the first task that would run and what is reused */
export type ResumeHint = {
  start_position: number
  task_name: string
  reused_tasks: number
  reused_cost_usd: number
  /** What the run already spent on the task that runs again (older servers leave it out) */
  spent_on_task_usd?: number
  /** Reasons the remaining tasks are likely to fail as they were, such as a missing provider key */
  warnings?: string[]
}

export type RunSpec = {
  crew: Crew
  agents: Agent[]
  tasks: Task[]
  llms: LLM[]
  pricing: ModelPrice[]
  settings: Settings
  inputs: Record<string, string>
  budget_usd: number
  work_order?: WorkOrderSpec
  /** A resumed run: the tasks before start_position are not run; their outputs are handed on */
  resume?: { from_run_id: string; start_position: number; outputs: TaskOutput[] }
}

export type WorkOrderSpec = {
  id: string
  number: number
  title: string
  brief: string
  attachments: { id: string; filename: string; content_type: string; size_bytes: number; path: string }[]
}

export const TRIGGER_TYPES = ['manual', 'api', 'schedule', 'webhook', 'work_order'] as const
export type TriggerType = (typeof TRIGGER_TYPES)[number]
/** 'crew' for crew runs, 'chat' for one turn of a crew chat */
export const RUN_KINDS = ['crew', 'chat', 'handoff'] as const
export type RunKind = (typeof RUN_KINDS)[number]

export type Run = {
  id: string
  crew_id: string | null
  crew_name: string
  status: RunStatus
  inputs: Record<string, string>
  budget_usd: number
  output: string | null
  error: string | null
  cost_usd: number | null
  usage: Usage | null
  area_ids: string[]
  created_by: string | null
  created_by_name: string
  trigger_type: TriggerType
  /** Older servers leave it out; treat that as 'crew' */
  kind?: RunKind
  work_order_id: string | null
  created_at: string
  started_at: string | null
  finished_at: string | null
  task_outputs?: TaskOutput[]
  spec?: RunSpec
  /** Run detail only: the run this one resumed, and the first task (from 0) it ran */
  resumed_from_run_id?: string
  resume_position?: number
  /** Run detail only: runs that resumed this one, newest first */
  resumed_by?: string[]
  /** Run detail only: where you could resume this run now; left out when it cannot be resumed (or not by you) */
  resumable?: ResumeHint
  /** Run detail only: what the run spent on each task it worked on, by task name, finished or not */
  task_costs?: Record<string, number>
}

export type StartRunBody = { inputs: Record<string, string>; budget_usd?: number; callback_url?: string }

// ---------------------------------------------------------------- Interactions
export type InteractionNodeKind = 'crew' | 'task' | 'agent' | 'tool' | 'model'
export type InteractionStatus = 'pending' | 'running' | 'done' | 'failed'

export type InteractionStats = {
  llm_calls: number
  tool_calls: number
  prompt_tokens: number
  completion_tokens: number
  cost_usd: number
  duration_ms: number
}

export type InteractionNode = {
  id: string
  kind: InteractionNodeKind
  label: string
  status?: InteractionStatus
  stats: InteractionStats
}

export type InteractionEdgeKind = 'runs' | 'assigned' | 'context' | 'tool_call' | 'llm_call' | 'delegation'

export type InteractionEdge = {
  id: string
  source: string
  target: string
  kind: InteractionEdgeKind
  count: number
  cost_usd: number
}

export type Interactions = { nodes: InteractionNode[]; edges: InteractionEdge[] }

export type ChatMessage = { role: string; content: unknown; [key: string]: unknown }

export type LlmCall = {
  seq: number
  agent_role: string
  task_name: string
  model: string
  usage: { prompt_tokens: number; completion_tokens: number; cached_prompt_tokens: number; cache_write_tokens?: number } | null
  cost_usd: number
  messages: ChatMessage[]
  response: string
  created_at: string
}

// ---------------------------------------------------------------- Events
export const RUN_EVENT_TYPES = [
  'run_status',
  'crew_started',
  'task_started',
  'agent_started',
  'tool_started',
  'tool_finished',
  'tool_error',
  'llm_call',
  'search',
  'video',
  'delegation',
  'task_completed',
  'task_failed',
  'crew_completed',
  'crew_failed',
  'budget',
  'artifacts',
  'state',
  'context_compacted',
  'log',
] as const
export type RunEventType = (typeof RUN_EVENT_TYPES)[number]

export type RunEvent = {
  seq: number
  type: RunEventType
  payload: Record<string, unknown>
  created_at: string
}

// ---------------------------------------------------------------- Overview
export type Overview = {
  counts: {
    work_orders: number
    schedules: number
    llms: number
    pricing: number
    agents: number
    tasks: number
    crews: number
    runs: number
  }
  recent_runs: Run[]
  recent_work_orders: WorkOrder[]
  spend_last_30d_usd: number
  /** Chat turns' share of spend_last_30d_usd (older servers leave it out) */
  chat_spend_last_30d_usd?: number
}

export * from './types.access'
export * from './types.workorders'
