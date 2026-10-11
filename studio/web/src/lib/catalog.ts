import { agentsApi, crewsApi, llmsApi, pricingApi, tasksApi, toolsApi } from '../api/endpoints'
import type { Agent, Crew, LLM, ModelPrice, Task, Tool } from '../api/types'

export type Catalog = {
  agents: Agent[]
  tasks: Task[]
  crews: Crew[]
  llms: LLM[]
  tools: Tool[]
  pricing: ModelPrice[]
}

/** Loads every editable collection at once; used by editors that cross-reference entities. */
export async function loadCatalog(signal?: AbortSignal): Promise<Catalog> {
  const [agents, tasks, crews, llms, tools, pricing] = await Promise.all([
    agentsApi.list(signal),
    tasksApi.list(signal),
    crewsApi.list(signal),
    llmsApi.list(signal),
    toolsApi.list(signal),
    pricingApi.list(signal),
  ])
  return { agents, tasks, crews, llms, tools, pricing }
}

export function byId<T extends { id: string }>(items: readonly T[]): Map<string, T> {
  return new Map(items.map((i) => [i.id, i]))
}

export function sortByName<T extends { name: string }>(items: readonly T[]): T[] {
  return [...items].sort((a, b) => a.name.localeCompare(b.name))
}
