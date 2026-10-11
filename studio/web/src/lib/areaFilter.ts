// Pure helpers for the "All areas / <area>" filter on the agents, tasks and crews lists.

/** Query-string key the list pages use for the selected area. */
export const AREA_PARAM = 'area'

/** Items that belong to the chosen area; an empty id means "All areas". */
export function filterByArea<T extends { area_ids: readonly string[] }>(items: readonly T[], areaId: string): T[] {
  if (!areaId) return [...items]
  return items.filter((item) => item.area_ids.includes(areaId))
}

/** Dropdown options: "All areas" first, then every area by name. */
export function areaFilterOptions(areas: readonly { id: string; name: string }[]): { value: string; label: string }[] {
  const sorted = [...areas].sort((a, b) => a.name.localeCompare(b.name))
  return [{ value: '', label: 'All areas' }, ...sorted.map((a) => ({ value: a.id, label: a.name }))]
}

/** Drops a stale area id (e.g. a deleted area left in the URL) so the list is not silently empty. */
export function knownAreaId(areaId: string | null, areas: readonly { id: string }[], loaded: boolean): string {
  if (!areaId) return ''
  if (!loaded) return areaId
  return areas.some((a) => a.id === areaId) ? areaId : ''
}

/** Client-side check shared by the agent, task and crew forms. */
export function areaError(areaIds: readonly string[]): string | undefined {
  return areaIds.length === 0 ? 'Choose at least one functional area' : undefined
}
