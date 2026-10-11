import { agentsApi, crewsApi, llmsApi, pricingApi, tasksApi, toolsApi } from '../api/endpoints'
import type { Permission } from '../api/types'
import type { Catalog } from './catalog'

/** Which permission each catalog collection needs to be listed (tools are part of the agents feature). */
const NEEDS: Record<keyof Catalog, Permission> = {
  agents: 'agents.view',
  tasks: 'tasks.view',
  crews: 'crews.view',
  llms: 'llms.view',
  tools: 'agents.view',
  pricing: 'pricing.view',
}

/** Collections the person may list; the rest load as empty instead of failing the page with a 403. */
export function visibleCollections(can: (p: Permission) => boolean): Record<keyof Catalog, boolean> {
  return {
    agents: can(NEEDS.agents),
    tasks: can(NEEDS.tasks),
    crews: can(NEEDS.crews),
    llms: can(NEEDS.llms),
    tools: can(NEEDS.tools),
    pricing: can(NEEDS.pricing),
  }
}

/**
 * Like loadCatalog, but only asks for collections the signed-in person can read, so someone with
 * crews.view but not llms.view still gets a working crew page.
 */
export async function loadVisibleCatalog(can: (p: Permission) => boolean, signal?: AbortSignal): Promise<Catalog> {
  const show = visibleCollections(can)
  const empty = <T,>(): Promise<T[]> => Promise.resolve([])
  const [agents, tasks, crews, llms, tools, pricing] = await Promise.all([
    show.agents ? agentsApi.list(signal) : empty<Catalog['agents'][number]>(),
    show.tasks ? tasksApi.list(signal) : empty<Catalog['tasks'][number]>(),
    show.crews ? crewsApi.list(signal) : empty<Catalog['crews'][number]>(),
    show.llms ? llmsApi.list(signal) : empty<Catalog['llms'][number]>(),
    show.tools ? toolsApi.list(signal) : empty<Catalog['tools'][number]>(),
    show.pricing ? pricingApi.list(signal) : empty<Catalog['pricing'][number]>(),
  ])
  return { agents, tasks, crews, llms, tools, pricing }
}
