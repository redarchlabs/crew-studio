import type { Task, TaskInput } from '../../api/types'
import { areaError } from '../../lib/areaFilter'

export const TASK_FIELDS = ['name', 'description', 'expected_output', 'agent_id', 'context_task_ids', 'markdown', 'area_ids'] as const

export const EMPTY_TASK: TaskInput = { name: '', description: '', expected_output: '', agent_id: null, context_task_ids: [], markdown: true, area_ids: [] }

/** A blank task placed in the given areas (the person's default areas for new items). */
export function newTaskInput(areaIds: readonly string[]): TaskInput {
  return { ...EMPTY_TASK, area_ids: [...areaIds] }
}

export function toTaskInput(t: Task): TaskInput {
  const { name, description, expected_output, agent_id, context_task_ids, markdown, area_ids } = t
  return { name, description, expected_output, agent_id, context_task_ids, markdown, area_ids: area_ids ?? [] }
}

export function validateTask(v: TaskInput): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.name.trim()) errs.name = 'Name is required'
  if (!v.description.trim()) errs.description = 'Description is required'
  if (!v.expected_output.trim()) errs.expected_output = 'Expected output is required'
  const areas = areaError(v.area_ids)
  if (areas) errs.area_ids = areas
  return errs
}
