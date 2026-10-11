// Fixtures for the Memory tab tests (excluded from coverage like the other testUtils files).
import type { MemoryRecord, MemorySettings, MemorySettingsView } from '../../../api/types.memory'

export const TS = '2026-10-03T07:00:00Z'

export function makeMemory(over: Partial<MemoryRecord> = {}): MemoryRecord {
  return {
    id: 'm1aaaaaaaa',
    content: 'Sam usually replies late on Fridays',
    scope: '/people/sam',
    categories: ['people'],
    metadata: {},
    importance: 0.5,
    source: 'run:r1',
    private: false,
    pinned: false,
    status: 'active',
    run_id: 'r1bbbbbbbbbb',
    agent_role: 'Coordinator',
    task_name: 'Daily sweep',
    written_by: 'run',
    created_at: TS,
    last_accessed: TS,
    updated_at: TS,
    km2_document_id: null,
    ...over,
  }
}

export function makeSettings(over: Partial<MemorySettings> = {}): MemorySettings {
  return {
    mode: 'off',
    llm_id: null,
    recall_depth: 'shallow',
    recall_limit: 5,
    saves_per_run: 20,
    retention_days: 365,
    review_run_saves: true,
    recency_weight: 0.3,
    semantic_weight: 0.5,
    importance_weight: 0.2,
    recency_half_life_days: 30,
    updated_by_name: '',
    updated_at: null,
    ...over,
  }
}

export function makeSettingsView(over: Partial<MemorySettingsView> = {}, settings: Partial<MemorySettings> = {}): MemorySettingsView {
  return {
    settings: makeSettings(settings),
    available: true,
    unavailable_reason: '',
    embed_model: 'openai/text-embedding-3-small:768',
    suggested_llm_id: 'l1',
    reembed_pending: 0,
    ...over,
  }
}
