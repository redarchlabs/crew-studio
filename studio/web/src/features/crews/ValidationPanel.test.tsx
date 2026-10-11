import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { ValidationPanel } from './ValidationPanel'

afterEach(cleanup)

const props = { validation: undefined, loading: false, error: undefined, onRefresh: () => {}, isNew: false, dirty: false, draftWarnings: [] as string[] }

describe('ValidationPanel', () => {
  it('asks to save a new crew and hides re-validate', () => {
    render(<ValidationPanel {...props} isNew />)
    expect(screen.getByText(/Save the crew to run server validation/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Re-validate' })).toBeNull()
  })

  it('shows checking while the result is pending', () => {
    render(<ValidationPanel {...props} loading />)
    expect(screen.getByText('Checking…')).toBeTruthy()
  })

  it('shows ready to run, noting a dirty draft', () => {
    const { rerender } = render(<ValidationPanel {...props} validation={{ valid: true, errors: [] }} />)
    expect(screen.getByText(/Ready to run/)).toBeTruthy()
    expect(screen.queryByText('(saved version)')).toBeNull()
    rerender(<ValidationPanel {...props} validation={{ valid: true, errors: [] }} dirty />)
    expect(screen.getByText('(saved version)')).toBeTruthy()
  })

  it('shows warnings such as a missing provider key, even when the crew can run', () => {
    render(<ValidationPanel {...props} validation={{ valid: true, errors: [], warnings: ['LLM Haiku: ANTHROPIC_API_KEY is not set'] }} />)
    expect(screen.getByText(/Ready to run/)).toBeTruthy()
    expect(screen.getByTestId('validation-warnings').textContent).toContain('ANTHROPIC_API_KEY is not set')
  })

  it('lists server errors', () => {
    render(<ValidationPanel {...props} validation={{ valid: false, errors: ['no tasks', 'no agents'] }} />)
    expect(screen.getByText('no tasks')).toBeTruthy()
    expect(screen.getByText('no agents')).toBeTruthy()
  })

  it('shows a request error', () => {
    render(<ValidationPanel {...props} error={new ApiError(500, { code: 'x', message: 'boom' })} />)
    expect(screen.getByText(/Could not validate: boom/)).toBeTruthy()
  })

  it('renders draft warnings and refreshes on demand', () => {
    const onRefresh = vi.fn()
    render(<ValidationPanel {...props} onRefresh={onRefresh} loading draftWarnings={['Task: No agent assigned']} />)
    expect(screen.getByText('Draft checks')).toBeTruthy()
    expect(screen.getByText('Task: No agent assigned')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Re-validate' }))
    expect(onRefresh).toHaveBeenCalledOnce()
  })
})
