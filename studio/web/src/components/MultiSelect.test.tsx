import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { MultiSelect, type MultiOption } from './MultiSelect'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

const opts: MultiOption[] = [
  { value: 'a', label: 'Alpha', hint: 'first' },
  { value: 'b', label: 'Beta' },
  { value: 'c', label: 'Gamma', disabled: true, disabledReason: 'Locked by admin' },
]

describe('MultiSelect', () => {
  it('lists options as a multiselectable listbox with selection state', () => {
    render(<MultiSelect label="Agents" options={opts} value={['b']} onChange={() => undefined} />)
    expect(screen.getByRole('listbox').getAttribute('aria-multiselectable')).toBe('true')
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(3)
    expect(options.map((o) => o.getAttribute('aria-selected'))).toEqual(['false', 'true', 'false'])
    expect(screen.getByText('first')).toBeTruthy()
    expect(screen.getByText('1/3')).toBeTruthy()
  })

  it('adds and removes values preserving insertion order', () => {
    const onChange = vi.fn()
    const { rerender } = render(<MultiSelect label="L" options={opts} value={['b']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('option', { name: /Alpha/ }))
    expect(onChange).toHaveBeenLastCalledWith(['b', 'a'])
    rerender(<MultiSelect label="L" options={opts} value={['b', 'a']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('option', { name: /Beta/ }))
    expect(onChange).toHaveBeenLastCalledWith(['a'])
  })

  it('disables unselected disabled options and explains why', () => {
    const onChange = vi.fn()
    render(<MultiSelect label="L" options={opts} value={[]} onChange={onChange} />)
    const gamma = screen.getByRole('option', { name: /Gamma/ }) as HTMLButtonElement
    expect(gamma.disabled).toBe(true)
    expect(gamma.title).toBe('Locked by admin')
    expect(screen.getAllByText('Locked by admin').length).toBeGreaterThan(0)
    fireEvent.click(gamma)
    expect(onChange).not.toHaveBeenCalled()
  })

  it('keeps an already-selected disabled option enabled so it can be removed', () => {
    const onChange = vi.fn()
    render(<MultiSelect label="L" options={opts} value={['c']} onChange={onChange} />)
    const gamma = screen.getByRole('option', { name: /Gamma/ }) as HTMLButtonElement
    expect(gamma.disabled).toBe(false)
    fireEvent.click(gamma)
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('shows the empty text when there are no options (default and custom)', () => {
    const { rerender } = render(<MultiSelect label="L" options={[]} value={[]} onChange={() => undefined} />)
    expect(screen.getByText('Nothing to choose from yet.')).toBeTruthy()
    expect(screen.getByText('0/0')).toBeTruthy()
    rerender(<MultiSelect label="L" options={[]} value={[]} onChange={() => undefined} emptyText="No tools" />)
    expect(screen.getByText('No tools')).toBeTruthy()
  })

  it('shows an error message', () => {
    render(<MultiSelect label="L" options={opts} value={[]} onChange={() => undefined} error="Pick at least one" />)
    expect(screen.getByText('Pick at least one')).toBeTruthy()
  })

  describe('filtering', () => {
    const many: MultiOption[] = Array.from({ length: 8 }, (_, i) => ({ value: `id-${i}`, label: i % 2 ? `Odd ${i}` : `Even ${i}` }))
    function Harness() {
      const [v, setV] = useState<string[]>([])
      return <MultiSelect label="Many" options={many} value={v} onChange={setV} />
    }
    it('only shows the filter box for long lists (>6)', () => {
      const { rerender } = render(<MultiSelect label="L" options={many.slice(0, 6)} value={[]} onChange={() => undefined} />)
      expect(screen.queryByPlaceholderText('Filter…')).toBeNull()
      rerender(<MultiSelect label="L" options={many} value={[]} onChange={() => undefined} />)
      expect(screen.getByPlaceholderText('Filter…')).toBeTruthy()
    })
    it('filters case-insensitively by label and by value, ignoring surrounding spaces', () => {
      render(<Harness />)
      const box = screen.getByPlaceholderText('Filter…')
      fireEvent.change(box, { target: { value: '  ODD ' } })
      expect(screen.getAllByRole('option')).toHaveLength(4)
      fireEvent.change(box, { target: { value: 'id-3' } })
      expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Odd 3'])
      fireEvent.change(box, { target: { value: 'zzz' } })
      expect(screen.queryAllByRole('option')).toHaveLength(0)
      fireEvent.change(box, { target: { value: '' } })
      expect(screen.getAllByRole('option')).toHaveLength(8)
    })
    it('keeps the selection count over the full list while filtered', () => {
      render(<Harness />)
      fireEvent.click(screen.getByRole('option', { name: /Even 0/ }))
      fireEvent.change(screen.getByPlaceholderText('Filter…'), { target: { value: 'Odd' } })
      expect(screen.getByText('1/8')).toBeTruthy()
    })
  })
})
