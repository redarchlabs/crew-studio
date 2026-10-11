import type { Area, AreaInput, AreaUsage } from '../../api/types'

export const AREA_FIELDS = ['name', 'description'] as const

/** Problems with an area draft; names must be unique (ignoring case) among the other areas. */
export function validateArea(v: AreaInput, others: readonly Pick<Area, 'name'>[]): Record<string, string> {
  const errs: Record<string, string> = {}
  const name = v.name.trim()
  if (!name) errs.name = 'Name is required'
  else if (others.some((a) => a.name.trim().toLowerCase() === name.toLowerCase())) errs.name = 'Another area already has this name'
  return errs
}

/** Items that stop an area from being deleted (people in it do not). */
export function itemsInArea(usage: AreaUsage | undefined): number {
  if (!usage) return 0
  return usage.crews + usage.agents + usage.tasks
}

export function usageSummary(usage: AreaUsage | undefined): string {
  if (!usage) return '—'
  const part = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`
  return [part(usage.crews, 'crew'), part(usage.agents, 'agent'), part(usage.tasks, 'task'), part(usage.users, 'account')].join(' · ')
}

export function sortAreas(areas: readonly Area[]): Area[] {
  return [...areas].sort((a, b) => a.name.localeCompare(b.name))
}
