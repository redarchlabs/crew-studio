import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { InputsFields } from './InputsFields'
import type { DerivedInputs, InputSpec } from './targetInputs'

afterEach(cleanup)

const spec = (over: Partial<InputSpec> & { key: string }): InputSpec => ({ label: over.key, default: '', required: false, ...over })
const derived = (over: Partial<DerivedInputs> = {}): DerivedInputs => ({ inputs: [], ...over })

describe('InputsFields', () => {
  it('prompts for a crew when none is chosen', () => {
    render(<InputsFields derived={derived()} values={{}} onChange={vi.fn()} noTarget />)
    expect(screen.getByText('Choose a crew to see the inputs it needs.')).toBeTruthy()
  })

  it('says when the crew takes no inputs', () => {
    render(<InputsFields derived={derived()} values={{}} onChange={vi.fn()} />)
    expect(screen.getByText('This crew takes no inputs.')).toBeTruthy()
  })

  it('renders a field per input with required marker and placeholder key', () => {
    const d = derived({ inputs: [spec({ key: 'topic', label: 'Topic', required: true }), spec({ key: 'tone', label: 'Tone' })] })
    render(<InputsFields derived={d} values={{ topic: 'AI', tone: '' }} onChange={vi.fn()} />)
    expect((screen.getByLabelText(/Topic/) as HTMLTextAreaElement).value).toBe('AI')
    expect(screen.getByText('{topic}')).toBeTruthy()
    expect(screen.getByText('*')).toBeTruthy()
  })

  it('merges edits into the full value map', () => {
    const onChange = vi.fn()
    const d = derived({ inputs: [spec({ key: 'a', label: 'Alpha' }), spec({ key: 'b', label: 'Beta' })] })
    render(<InputsFields derived={d} values={{ a: '1', b: '2' }} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText(/Beta/), { target: { value: 'two' } })
    expect(onChange).toHaveBeenCalledWith({ a: '1', b: 'two' })
  })

  it('uses a taller textarea for long values', () => {
    const d = derived({ inputs: [spec({ key: 'a', label: 'Alpha' })] })
    const { rerender } = render(<InputsFields derived={d} values={{ a: 'short' }} onChange={vi.fn()} />)
    expect((screen.getByLabelText(/Alpha/) as HTMLTextAreaElement).rows).toBe(1)
    rerender(<InputsFields derived={d} values={{ a: 'x'.repeat(81) }} onChange={vi.fn()} />)
    expect((screen.getByLabelText(/Alpha/) as HTMLTextAreaElement).rows).toBe(3)
  })

  it('shows per-field errors and disables fields', () => {
    const d = derived({ inputs: [spec({ key: 'a', label: 'Alpha', required: true })] })
    render(<InputsFields derived={d} values={{ a: '' }} onChange={vi.fn()} errors={{ a: 'Alpha is required' }} disabled />)
    expect(screen.getByText('Alpha is required')).toBeTruthy()
    expect((screen.getByLabelText(/Alpha/) as HTMLTextAreaElement).disabled).toBe(true)
  })

  it('lists saved inputs the crew no longer uses and removes them on request', () => {
    const onChange = vi.fn()
    const d = derived({ inputs: [spec({ key: 'a', label: 'Alpha' })] })
    render(<InputsFields derived={d} values={{ a: '1', old: 'stale', blank: '' }} onChange={onChange} />)
    expect(screen.getByText('stale')).toBeTruthy()
    expect(screen.getByText('""')).toBeTruthy()
    expect(screen.queryByText('This crew takes no inputs.')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Remove old' }))
    expect(onChange).toHaveBeenCalledWith({ a: '1', blank: '' })
  })

  it('hides the remove buttons while disabled', () => {
    render(<InputsFields derived={derived()} values={{ old: 'x' }} onChange={vi.fn()} disabled />)
    expect(screen.queryByRole('button', { name: 'Remove old' })).toBeNull()
  })
})
