import type { WorkOrder, WorkOrderInput } from '../../../api/types'
import { budgetProblem } from '../target/BudgetField'
import { missingRequired, type InputSpec, type TargetRef } from '../target/targetInputs'

export const WORK_ORDER_FIELDS = ['title', 'brief', 'crew_id', 'inputs', 'budget_usd'] as const

export const UNTITLED = 'Untitled work order'

export function emptyWorkOrder(crewId: string | null = null): WorkOrderInput {
  return {
    title: '',
    brief: '',
    crew_id: crewId,
    inputs: {},
    budget_usd: null,
  }
}

export function toWorkOrderInput(wo: WorkOrder): WorkOrderInput {
  return { title: wo.title, brief: wo.brief, crew_id: wo.crew_id, inputs: { ...wo.inputs }, budget_usd: wo.budget_usd }
}

export function hasTarget(v: TargetRef): boolean {
  return Boolean(v.crew_id)
}

export function normalizeWorkOrder(v: WorkOrderInput): WorkOrderInput {
  return { ...v, title: v.title.trim() }
}

/**
 * Local checks before saving. Drafts may leave required inputs empty; submitting may not.
 * Input problems are keyed "input:<key>" so they can be shown on each field.
 */
export function validateWorkOrder(v: WorkOrderInput, specs: readonly InputSpec[], forSubmit: boolean): Record<string, string> {
  const errors: Record<string, string> = {}
  const title = v.title.trim()
  if (!title) errors.title = 'Give the work order a title.'
  else if (title.length > 200) errors.title = 'Keep the title to 200 characters or fewer.'
  if (!hasTarget(v)) errors.target = 'Choose a crew to run.'
  const budget = budgetProblem(v.budget_usd)
  if (budget) errors.budget_usd = budget
  if (v.brief.length > 100_000) errors.brief = 'The brief is limited to 100,000 characters.'
  if (forSubmit) {
    for (const [key, message] of Object.entries(missingRequired(specs, v.inputs))) errors[`input:${key}`] = message
  }
  return errors
}

/** Splits "input:<key>" errors out for the inputs panel. */
export function inputErrors(errors: Readonly<Record<string, string>>): Record<string, string> {
  return Object.fromEntries(Object.entries(errors).filter(([k]) => k.startsWith('input:')).map(([k, v]) => [k.slice(6), v]))
}
