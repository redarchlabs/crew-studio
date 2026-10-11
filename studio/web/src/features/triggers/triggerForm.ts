import type { Trigger, TriggerInput } from '../../api/types'
import { budgetProblem } from '../workorders/target/BudgetField'
import { hasTarget } from '../workorders/composer/composerForm'

export const TRIGGER_FIELDS = ['name', 'description', 'crew_id', 'enabled', 'input_map', 'brief', 'include_payload', 'budget_usd', 'callback_url'] as const

export type MapRow = { key: string; path: string }

export function emptyTrigger(): TriggerInput {
  return {
    name: '',
    description: '',
    crew_id: null,
    enabled: true,
    input_map: {},
    brief: '',
    include_payload: true,
    budget_usd: null,
    callback_url: null,
  }
}

export function toTriggerInput(t: Trigger): TriggerInput {
  return {
    name: t.name,
    description: t.description,
    crew_id: t.crew_id,
    enabled: t.enabled,
    input_map: { ...t.input_map },
    brief: t.brief,
    include_payload: t.include_payload,
    budget_usd: t.budget_usd,
    callback_url: t.callback_url,
  }
}

export function rowsFromMap(map: Readonly<Record<string, string>>): MapRow[] {
  return Object.entries(map).map(([key, path]) => ({ key, path }))
}

/** Rows with a key become the map; later duplicates win, as they would in JSON. */
export function mapFromRows(rows: readonly MapRow[]): Record<string, string> {
  return Object.fromEntries(rows.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.path.trim()]))
}

export function normalizeTrigger(v: TriggerInput): TriggerInput {
  const url = v.callback_url?.trim() ?? ''
  return {
    ...v,
    name: v.name.trim(),
    callback_url: url === '' ? null : url,
  }
}

const KEY_RE = /^[a-z][a-z0-9_]*$/
const PATH_RE = /^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$/

export function validateTrigger(v: TriggerInput): Record<string, string> {
  const errors: Record<string, string> = {}
  if (!v.name.trim()) errors.name = 'Give the trigger a name.'
  if (!hasTarget(v)) errors.target = 'Choose a crew to run.'
  const budget = budgetProblem(v.budget_usd)
  if (budget) errors.budget_usd = budget
  const badKey = Object.keys(v.input_map).find((k) => !KEY_RE.test(k))
  const badPath = Object.entries(v.input_map).find(([, p]) => !PATH_RE.test(p))
  if (badKey) errors.input_map = `"${badKey}" is not a valid input key (lowercase letters, digits and _).`
  else if (badPath) errors.input_map = `Give "${badPath[0]}" a dotted payload path, e.g. customer.name.`
  const url = v.callback_url?.trim()
  if (url) {
    try {
      const parsed = new URL(url)
      if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') errors.callback_url = 'Use an https:// URL (http:// works only for hosts an administrator has allowed).'
    } catch {
      errors.callback_url = 'Enter a full URL, e.g. https://example.com/hooks/crew-studio.'
    }
  }
  return errors
}
