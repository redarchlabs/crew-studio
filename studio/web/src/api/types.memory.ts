// Crew memory: soft lessons a crew keeps between runs, shown and managed on the crew's Memory tab.
// See docs/crew-state-and-pmo-design.md section 19. Keep field names in snake_case.

/** off: no memory; recall_only: runs read memories, never add; explicit: agents save with the Remember tool; automatic: extracted after each task */
export type MemoryMode = 'off' | 'recall_only' | 'explicit' | 'automatic'

export type RecallDepth = 'shallow' | 'deep'

export type MemoryStatus = 'active' | 'pending'

export type MemorySettings = {
  crew_id?: string
  mode: MemoryMode
  /** The model that extracts memories in automatic mode (and deep recall); null uses the crew's cheapest agent model */
  llm_id: string | null
  recall_depth: RecallDepth
  /** Memories recalled per task, 1–20 */
  recall_limit: number
  /** Memories one run may save, 0–100 */
  saves_per_run: number
  /** Unpinned memories older than this are deleted; null keeps them */
  retention_days: number | null
  /** Every memory a run saves (Remember tool or automatic extraction) waits as pending until a person approves it; memories people add stay active */
  review_run_saves: boolean
  recency_weight: number
  semantic_weight: number
  importance_weight: number
  recency_half_life_days: number
  updated_by_name?: string
  updated_at?: string | null
}

export type MemorySettingsInput = Pick<
  MemorySettings,
  | 'mode'
  | 'llm_id'
  | 'recall_depth'
  | 'recall_limit'
  | 'saves_per_run'
  | 'retention_days'
  | 'review_run_saves'
  | 'recency_weight'
  | 'semantic_weight'
  | 'importance_weight'
  | 'recency_half_life_days'
>

/** GET /crews/{id}/memory/settings (and the answer to PUT) */
export type MemorySettingsView = {
  settings: MemorySettings
  /** False when the server has no embedder, so memory cannot be turned on */
  available: boolean
  unavailable_reason: string
  embed_model: string
  /** The model used when llm_id is null: the crew's cheapest agent model */
  suggested_llm_id: string | null
  /** Memories still embedded with an older embedding model; they are re-embedded in the background and recall may miss them until then */
  reembed_pending: number
}

export type MemoryRecord = {
  id: string
  content: string
  /** A path relative to the crew, such as / or /people/sam */
  scope: string
  categories: string[]
  metadata: Record<string, unknown>
  importance: number
  /** run:<id> or user:<name> */
  source: string
  private: false
  pinned: boolean
  status: MemoryStatus
  run_id: string | null
  agent_role: string
  task_name: string
  written_by: string
  created_at: string
  last_accessed: string
  updated_at: string
  km2_document_id: string | null
}

export type MemoryQuery = {
  q?: string
  scope?: string
  status?: MemoryStatus | ''
  pinned?: boolean
  run_id?: string
  limit: number
  offset: number
}

export type MemoryScopeCount = { scope: string; count: number }

export type MemoryInput = { content: string; scope: string; categories?: string[]; importance?: number; pinned?: boolean }

/** status "active" approves a pending memory */
export type MemoryPatch = Partial<Omit<MemoryInput, 'scope'>> & { scope?: string; status?: 'active' }

export type MemoryDeleteRequest = { scope_prefix?: string; contains?: string; ids?: string[]; dry_run: boolean }

export type MemoryDeleteResult = { matched: number; deleted: number }

export type MemoryPromoteRequest = { memory_ids: string[]; title: string; folder?: string; external_ref?: string }

export type MemoryPromoteResult = { outcome: string; document_id: string; external_ref: string; folder_id: string }
