import { useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { CrewInput } from '../../api/types'
import { InputsEditor } from './InputsEditor'

afterEach(cleanup)

const inp = (key: string, over: Partial<CrewInput> = {}): CrewInput => ({ key, label: '', default: '', required: true, ...over })

function Harness({ initial, used = new Set<string>(), onDetect = () => {}, error }: { initial: CrewInput[]; used?: Set<string>; onDetect?: () => void; error?: string }) {
  const [inputs, setInputs] = useState(initial)
  return (
    <>
      <InputsEditor inputs={inputs} onChange={setInputs} onDetect={onDetect} usedKeys={used} error={error} />
      <pre data-testid="state">{JSON.stringify(inputs)}</pre>
    </>
  )
}
const state = () => JSON.parse(screen.getByTestId('state').textContent ?? '[]') as CrewInput[]

describe('InputsEditor', () => {
  it('shows an empty state with no inputs', () => {
    render(<Harness initial={[]} />)
    expect(screen.getByText(/No inputs/)).toBeTruthy()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('adds a blank required row', () => {
    render(<Harness initial={[]} />)
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(state()).toEqual([{ key: '', label: '', default: '', required: true }])
    expect(screen.getByLabelText('Input 1 key')).toBeTruthy()
  })

  it('edits key, label, default and required of the right row only', () => {
    render(<Harness initial={[inp('a'), inp('b')]} used={new Set(['a', 'b', 'company'])} />)
    fireEvent.change(screen.getByLabelText('Input 2 key'), { target: { value: 'company' } })
    fireEvent.change(screen.getByLabelText('Input 2 label'), { target: { value: 'Company' } })
    fireEvent.change(screen.getByLabelText('Input 2 default'), { target: { value: 'ACME' } })
    fireEvent.click(screen.getByLabelText('Input 2 required'))
    expect(state()).toEqual([inp('a'), { key: 'company', label: 'Company', default: 'ACME', required: false }])
  })

  it('removes a row', () => {
    render(<Harness initial={[inp('a'), inp('b')]} />)
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove input' })[0])
    expect(state().map((i) => i.key)).toEqual(['b'])
  })

  it('flags invalid and duplicate keys with aria-invalid', () => {
    render(<Harness initial={[inp('Bad'), inp('dup'), inp('dup'), inp('fine')]} />)
    expect(screen.getByLabelText('Input 1 key').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByLabelText('Input 2 key').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByLabelText('Input 3 key').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByLabelText('Input 4 key').getAttribute('aria-invalid')).toBe('false')
  })

  it('lists unused keys and hints on the field', () => {
    render(<Harness initial={[inp('used'), inp('spare'), inp('')]} used={new Set(['used'])} />)
    expect(screen.getByText('Unused keys: spare')).toBeTruthy()
    expect(screen.getByLabelText('Input 2 key').getAttribute('title')).toMatch(/Not referenced/)
    expect(screen.getByLabelText('Input 1 key').getAttribute('title')).toBeNull()
  })

  it('omits the unused note when every key is used', () => {
    render(<Harness initial={[inp('used')]} used={new Set(['used'])} />)
    expect(screen.queryByText(/Unused keys/)).toBeNull()
  })

  it('calls onDetect and renders the error', () => {
    const onDetect = vi.fn()
    render(<Harness initial={[]} onDetect={onDetect} error="Input 1: bad" />)
    fireEvent.click(screen.getByRole('button', { name: /Detect from placeholders/ }))
    expect(onDetect).toHaveBeenCalledOnce()
    expect(screen.getByText('Input 1: bad')).toBeTruthy()
  })
})
