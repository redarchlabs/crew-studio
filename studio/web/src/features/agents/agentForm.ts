import type { Agent, AgentInput } from '../../api/types'
import { areaError } from '../../lib/areaFilter'

export const AGENT_FIELDS = ['name', 'role', 'goal', 'backstory', 'llm_id', 'tools', 'max_iter', 'allow_delegation', 'verbose', 'area_ids'] as const

export const EMPTY_AGENT: AgentInput = {
  name: '',
  role: '',
  goal: '',
  backstory: '',
  llm_id: null,
  tools: [],
  max_iter: 20,
  allow_delegation: false,
  verbose: false,
  area_ids: [],
}

/** A blank agent placed in the given areas (the person's default areas for new items). */
export function newAgentInput(areaIds: readonly string[]): AgentInput {
  return { ...EMPTY_AGENT, area_ids: [...areaIds] }
}

export function toAgentInput(a: Agent): AgentInput {
  const { name, role, goal, backstory, llm_id, tools, max_iter, allow_delegation, verbose, area_ids } = a
  return { name, role, goal, backstory, llm_id, tools, max_iter, allow_delegation, verbose, area_ids: area_ids ?? [] }
}

export function validateAgent(v: AgentInput): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.name.trim()) errs.name = 'Name is required'
  if (!v.role.trim()) errs.role = 'Role is required'
  if (!v.goal.trim()) errs.goal = 'Goal is required'
  if (Number.isNaN(v.max_iter) || !Number.isInteger(v.max_iter) || v.max_iter < 1 || v.max_iter > 100) errs.max_iter = 'Whole number from 1 to 100'
  const areas = areaError(v.area_ids)
  if (areas) errs.area_ids = areas
  return errs
}
