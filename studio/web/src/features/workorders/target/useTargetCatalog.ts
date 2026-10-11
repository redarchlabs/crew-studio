import { ApiError } from '../../../api/client'
import { crewsApi, tasksApi } from '../../../api/endpoints'
import type { Crew, Task } from '../../../api/types'
import { sortByName } from '../../../lib/catalog'
import { useAsync, type AsyncState } from '../../../lib/useAsync'

export type TargetCatalogData = {
  crews: Crew[]
  /** For naming each crew's tasks in the picker */
  tasks: Task[]
  /** Collections the person may not read (403); the picker explains instead of failing */
  forbidden: { crews: boolean; tasks: boolean }
}

/** A list the person lacks the permission for is empty rather than an error. */
async function orForbidden<T>(load: Promise<T[]>): Promise<{ items: T[]; forbidden: boolean }> {
  try {
    return { items: await load, forbidden: false }
  } catch (err) {
    if (err instanceof ApiError && err.status === 403) return { items: [], forbidden: true }
    throw err
  }
}

export async function loadTargetCatalog(signal?: AbortSignal): Promise<TargetCatalogData> {
  const [crews, tasks] = await Promise.all([orForbidden(crewsApi.list(signal)), orForbidden(tasksApi.list(signal))])
  return {
    crews: sortByName(crews.items),
    tasks: tasks.items,
    forbidden: { crews: crews.forbidden, tasks: tasks.forbidden },
  }
}

/** Crews visible to the person (and their tasks' names), for target pickers and input derivation. */
export function useTargetCatalog(): AsyncState<TargetCatalogData> {
  return useAsync((signal) => loadTargetCatalog(signal), [])
}
