import type { Agent, Crew, LLM, ModelPrice, Task, Tool } from '../api/types'

const TS = '2026-01-01T00:00:00Z'

export function makeAgent(over: Partial<Agent> & { id: string }): Agent {
  return {
    name: over.id,
    role: 'Role',
    goal: 'Goal',
    backstory: '',
    llm_id: null,
    tools: [],
    max_iter: 20,
    allow_delegation: false,
    verbose: false,
    area_ids: ['area-1'],
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makeTask(over: Partial<Task> & { id: string }): Task {
  return {
    name: over.id,
    description: '',
    expected_output: '',
    agent_id: null,
    context_task_ids: [],
    markdown: true,
    area_ids: ['area-1'],
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makeLlm(over: Partial<LLM> & { id: string }): LLM {
  return {
    name: over.id,
    model: 'openai/gpt-x',
    temperature: null,
    reasoning_effort: null,
    api: 'completions',
    vision: false,
    max_completion_tokens: null,
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makePrice(over: Partial<ModelPrice> & { id: string; model: string }): ModelPrice {
  return {
    input_per_mtok: 1,
    cached_input_per_mtok: 0.1,
    cache_write_per_mtok: null,
    output_per_mtok: 4,
    notes: '',
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makeCrew(over: Partial<Crew> = {}): Crew {
  return {
    id: 'crew-1',
    name: 'Research crew',
    description: '',
    process: 'sequential',
    manager_llm_id: null,
    agent_ids: [],
    task_ids: [],
    inputs: [],
    budget_usd: 1,
    area_ids: ['area-1'],
    created_at: TS,
    updated_at: TS,
    ...over,
  }
}

export function makeTool(over: Partial<Tool> & { key: string }): Tool {
  return { name: over.key, description: '', requires_env: [], available: true, effect: 'reads', needs: [], ...over }
}
