import type { Crew, CrewBody, CrewDraft, CrewInput } from '../../api/types'
import { areaError } from '../../lib/areaFilter'

export const CREW_FIELDS = ['name', 'description', 'process', 'manager_llm_id', 'agent_ids', 'task_ids', 'inputs', 'budget_usd', 'area_ids'] as const

export const INPUT_KEY_RE = /^[a-z][a-z0-9_]*$/

export const EMPTY_CREW: CrewBody = {
  name: '',
  description: '',
  process: 'sequential',
  manager_llm_id: null,
  agent_ids: [],
  task_ids: [],
  inputs: [],
  budget_usd: 1,
  area_ids: [],
}

/** A blank crew placed in the given areas (the person's default areas for new items). */
export function newCrewBody(areaIds: readonly string[]): CrewBody {
  return { ...EMPTY_CREW, area_ids: [...areaIds] }
}

export function toCrewBody(c: Crew): CrewBody {
  const { name, description, process, manager_llm_id, agent_ids, task_ids, inputs, budget_usd, area_ids } = c
  return { name, description, process, manager_llm_id, agent_ids, task_ids, inputs, budget_usd, area_ids: area_ids ?? [] }
}

/** The unsaved crew the server drafts from a task (GET /tasks/{id}/crew-draft), as a form value. */
export function crewBodyFromDraft(d: CrewDraft): CrewBody {
  return {
    name: d.name ?? '',
    description: d.description ?? '',
    process: d.process ?? EMPTY_CREW.process,
    manager_llm_id: d.manager_llm_id ?? null,
    agent_ids: [...(d.agent_ids ?? [])],
    task_ids: [...(d.task_ids ?? [])],
    inputs: (d.inputs ?? []).map((i) => ({ ...i })),
    budget_usd: d.budget_usd ?? EMPTY_CREW.budget_usd,
    area_ids: [...(d.area_ids ?? [])],
  }
}

export function inputProblems(inputs: readonly CrewInput[]): string[] {
  const problems: string[] = []
  const seen = new Set<string>()
  inputs.forEach((i, idx) => {
    if (!INPUT_KEY_RE.test(i.key)) problems.push(`Input ${idx + 1}: key must match [a-z][a-z0-9_]*`)
    if (seen.has(i.key)) problems.push(`Input ${idx + 1}: duplicate key "${i.key}"`)
    seen.add(i.key)
  })
  return problems
}

export function validateCrew(v: CrewBody): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.name.trim()) errs.name = 'Name is required'
  if (v.process === 'hierarchical' && !v.manager_llm_id) errs.manager_llm_id = 'Hierarchical crews need a manager LLM'
  if (Number.isNaN(v.budget_usd) || v.budget_usd <= 0) errs.budget_usd = 'Budget must be greater than 0'
  const inputs = inputProblems(v.inputs)
  if (inputs.length > 0) errs.inputs = inputs.join('; ')
  const areas = areaError(v.area_ids)
  if (areas) errs.area_ids = areas
  return errs
}

export function normalizeCrew(v: CrewBody): CrewBody {
  return {
    ...v,
    name: v.name.trim(),
    manager_llm_id: v.process === 'hierarchical' ? v.manager_llm_id : null,
    inputs: v.inputs.map((i) => ({ ...i, key: i.key.trim(), label: i.label.trim() })),
  }
}

/** Moves an item one step up (-1) or down (+1), returning a new array. */
export function moveItem<T>(items: readonly T[], index: number, delta: -1 | 1): T[] {
  const target = index + delta
  if (target < 0 || target >= items.length) return [...items]
  const next = [...items]
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  return next
}
