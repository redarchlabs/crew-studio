import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { BudgetField, budgetProblem } from './BudgetField'

afterEach(cleanup)

describe('budgetProblem', () => {
  it.each([
    [null, null],
    [0.01, null],
    [5, null],
    [0, 'Budget must be greater than 0, or empty for the default.'],
    [-1, 'Budget must be greater than 0, or empty for the default.'],
    [Number.NaN, 'Budget must be greater than 0, or empty for the default.'],
  ])('budgetProblem(%s)', (value, expected) => {
    expect(budgetProblem(value)).toBe(expected)
  })
})

describe('BudgetField', () => {
  it('explains the crew budget with its amount, or without when unknown', () => {
    const { rerender } = render(<BudgetField value={null} onChange={vi.fn()} crewBudget={2.5} />)
    expect(screen.getByText(/the crew's budget of \$2\.50/)).toBeTruthy()
    rerender(<BudgetField value={null} onChange={vi.fn()} />)
    expect(screen.getByText(/Leave empty to use the crew's budget\./)).toBeTruthy()
  })

  it('emits numbers, and null when cleared', () => {
    const onChange = vi.fn()
    render(<BudgetField value={1} onChange={onChange} />)
    const input = screen.getByLabelText('Budget (USD)') as HTMLInputElement
    expect(input.value).toBe('1')
    fireEvent.change(input, { target: { value: '3.5' } })
    expect(onChange).toHaveBeenLastCalledWith(3.5)
    fireEvent.change(input, { target: { value: '' } })
    expect(onChange).toHaveBeenLastCalledWith(null)
  })

  it('shows an error', () => {
    render(<BudgetField value={0} onChange={vi.fn()} error="bad budget" />)
    expect(screen.getByText('bad budget')).toBeTruthy()
  })
})
