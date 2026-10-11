import { NumberField } from '../../../components/Field'
import { usd } from '../../../lib/format'

type BudgetFieldProps = {
  value: number | null
  onChange: (value: number | null) => void
  /** The target crew's own budget, once a crew is chosen */
  crewBudget?: number
  error?: string
}

/** Optional spend cap; empty uses the crew's budget. */
export function BudgetField({ value, onChange, crewBudget, error }: BudgetFieldProps) {
  const fallback = crewBudget !== undefined ? `the crew's budget of ${usd(crewBudget)}` : "the crew's budget"
  return (
    <NumberField
      label="Budget (USD)"
      value={value}
      onChange={onChange}
      nullable
      min={0}
      step="any"
      error={error}
      hint={<>Optional. Leave empty to use {fallback}. LLM calls stop at the hard-stop multiple of the budget.</>}
    />
  )
}

/** Local check: a budget, when set, must be a positive number. */
export function budgetProblem(value: number | null): string | null {
  if (value === null) return null
  return Number.isNaN(value) || value <= 0 ? 'Budget must be greater than 0, or empty for the default.' : null
}
