import type { Schedule, ScheduleInput } from '../../api/types'
import { budgetProblem } from '../workorders/target/BudgetField'
import { missingRequired, type InputSpec } from '../workorders/target/targetInputs'
import { hasTarget } from '../workorders/composer/composerForm'
import { cronProblem } from './cron'

export const SCHEDULE_FIELDS = ['name', 'description', 'crew_id', 'cron', 'timezone', 'brief', 'inputs', 'budget_usd', 'enabled', 'overlap_policy'] as const

export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

/** IANA zones the browser knows, with UTC first. */
export function timeZones(): string[] {
  let zones: string[] = []
  try {
    zones = Intl.supportedValuesOf('timeZone')
  } catch {
    zones = []
  }
  return ['UTC', ...zones.filter((z) => z !== 'UTC')]
}

export function emptySchedule(): ScheduleInput {
  return {
    name: '',
    description: '',
    crew_id: null,
    cron: '0 9 * * 1-5',
    timezone: browserTimeZone(),
    brief: '',
    inputs: {},
    budget_usd: null,
    enabled: true,
    overlap_policy: 'skip',
  }
}

export function toScheduleInput(s: Schedule): ScheduleInput {
  return {
    name: s.name,
    description: s.description,
    crew_id: s.crew_id,
    cron: s.cron,
    timezone: s.timezone,
    brief: s.brief,
    inputs: { ...s.inputs },
    budget_usd: s.budget_usd,
    enabled: s.enabled,
    overlap_policy: s.overlap_policy,
  }
}

export function normalizeSchedule(v: ScheduleInput): ScheduleInput {
  return {
    ...v,
    name: v.name.trim(),
    cron: v.cron.trim().split(/\s+/).join(' '),
  }
}

/** Local checks; required inputs must be filled because nobody is there to fill them when it fires. */
export function validateSchedule(v: ScheduleInput, specs: readonly InputSpec[]): Record<string, string> {
  const errors: Record<string, string> = {}
  if (!v.name.trim()) errors.name = 'Give the schedule a name.'
  if (!hasTarget(v)) errors.target = 'Choose a crew to run.'
  const cron = cronProblem(v.cron)
  if (cron) errors.cron = cron
  if (!v.timezone) errors.timezone = 'Choose a time zone.'
  const budget = budgetProblem(v.budget_usd)
  if (budget) errors.budget_usd = budget
  for (const [key, message] of Object.entries(missingRequired(specs, v.inputs))) errors[`input:${key}`] = message
  return errors
}
