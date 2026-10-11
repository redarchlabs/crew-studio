import type { APIRequestContext } from '@playwright/test'
import { LIB } from './accounts.ts'
import { apiAs, unwrap } from './api.ts'

type Named = { id: string; name: string }

async function list(api: APIRequestContext, path: string): Promise<Named[]> {
  const data = await unwrap<Named[] | { items: Named[] }>(await api.get(path))
  return Array.isArray(data) ? data : data.items
}

async function ensure(api: APIRequestContext, path: string, name: string, body: Record<string, unknown>): Promise<string> {
  const hit = (await list(api, path)).find((x) => x.name === name)
  if (hit) return hit.id
  return (await unwrap<Named>(await api.post(path, { data: body }), 201)).id
}

export type Library = { areaId: string; visionLlmId: string; plainLlmId: string; agentId: string; task1Id: string; task2Id: string; crewId: string }

/**
 * Makes sure the items the 05-library spec builds through the UI exist (same names and shapes), so the run,
 * work order and trigger specs also work when run on their own. A no-op after 05 has run.
 */
export async function ensureLibrary(): Promise<Library> {
  const api = await apiAs('admin')
  try {
    const areaId = (await list(api, '/api/areas')).find((a) => a.name === LIB.area)!.id
    const llm = (name: string, vision: boolean) =>
      ensure(api, '/api/llms', name, { name, model: LIB.model, temperature: null, max_completion_tokens: 4096, reasoning_effort: null, api: 'completions', vision })
    const visionLlmId = await llm(LIB.visionLlm, true)
    const plainLlmId = await llm(LIB.plainLlm, false)
    const agentId = await ensure(api, '/api/agents', LIB.agent, {
      name: LIB.agent, role: LIB.agentRole, goal: 'Find, verify and summarise facts about {topic}', backstory: 'A careful analyst who always cites sources.',
      llm_id: visionLlmId, tools: [], max_iter: 7, allow_delegation: false, verbose: false, area_ids: [areaId],
    })
    const task1Id = await ensure(api, '/api/tasks', LIB.task1, {
      name: LIB.task1, description: 'Research {topic} and list five key facts.', expected_output: 'A bullet list of five facts about {topic}.',
      agent_id: agentId, context_task_ids: [], markdown: true, area_ids: [areaId],
    })
    const task2Id = await ensure(api, '/api/tasks', LIB.task2, {
      name: LIB.task2, description: 'Write a two sentence summary of the findings about {topic}.', expected_output: 'Two sentences.',
      agent_id: agentId, context_task_ids: [task1Id], markdown: true, area_ids: [areaId],
    })
    const crewId = await ensure(api, '/api/crews', LIB.crew, {
      name: LIB.crew, description: 'Crew built by the e2e suite', process: 'sequential', manager_llm_id: null, agent_ids: [agentId],
      task_ids: [task1Id, task2Id], inputs: [{ key: 'topic', label: 'Topic', default: LIB.topicDefault, required: true }], budget_usd: 0.75, area_ids: [areaId],
    })
    return { areaId, visionLlmId, plainLlmId, agentId, task1Id, task2Id, crewId }
  } finally {
    await api.dispose()
  }
}

/** Saves the one-task crew the server drafts for a task (what the task page's Make a crew button does). */
export async function oneTaskCrew(api: APIRequestContext, taskId: string): Promise<Named> {
  const draft = await unwrap<Record<string, unknown>>(await api.get(`/api/tasks/${taskId}/crew-draft`))
  return unwrap<Named>(await api.post('/api/crews', { data: draft }), 201)
}
