import { useState } from 'react'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import type { InputSpec } from '../workorders/target/targetInputs'
import { InputMapEditor } from './InputMapEditor'

afterEach(cleanup)

const spec = (key: string, over: Partial<InputSpec> = {}): InputSpec => ({ key, label: key.toUpperCase(), default: '', required: true, ...over })

function Harness({ initial = {}, specs = [], error }: { initial?: Record<string, string>; specs?: InputSpec[]; error?: string }) {
  const [map, setMap] = useState(initial)
  return (
    <>
      <InputMapEditor value={map} onChange={setMap} specs={specs} error={error} />
      <pre data-testid="map">{JSON.stringify(map)}</pre>
    </>
  )
}
const map = () => JSON.parse(screen.getByTestId('map').textContent ?? '{}') as Record<string, string>

describe('InputMapEditor', () => {
  it('explains an empty mapping', () => {
    render(<Harness />)
    expect(screen.getByText(/No mapping/)).toBeTruthy()
  })

  it('renders existing rows', () => {
    render(<Harness initial={{ company: 'customer.name' }} />)
    expect((screen.getByLabelText('Input key') as HTMLInputElement).value).toBe('company')
    expect((screen.getByLabelText('Payload path') as HTMLInputElement).value).toBe('customer.name')
    expect(screen.queryByText(/No mapping/)).toBeNull()
  })

  it('adds a blank row; blank keys stay out of the emitted map', () => {
    render(<Harness />)
    fireEvent.click(screen.getByRole('button', { name: 'Add mapping' }))
    expect(screen.getAllByLabelText('Input key')).toHaveLength(1)
    expect(map()).toEqual({})
    fireEvent.change(screen.getByLabelText('Input key'), { target: { value: 'k' } })
    fireEvent.change(screen.getByLabelText('Payload path'), { target: { value: 'a.b' } })
    expect(map()).toEqual({ k: 'a.b' })
  })

  it('Add mapping suggests the first unmapped target input', () => {
    render(<Harness specs={[spec('company'), spec('region')]} initial={{ company: 'c' }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Add mapping' }))
    const keys = screen.getAllByLabelText('Input key') as HTMLInputElement[]
    expect(keys[1].value).toBe('region')
  })

  it('maps all unmapped crew inputs to a same-named path', () => {
    render(<Harness specs={[spec('company'), spec('region'), spec('topic')]} initial={{ company: 'customer.name' }} />)
    fireEvent.click(screen.getByRole('button', { name: 'Map all 2 crew inputs' }))
    expect(map()).toEqual({ company: 'customer.name', region: 'region', topic: 'topic' })
    expect(screen.queryByRole('button', { name: /Map all/ })).toBeNull()
  })

  it('has no map-all button without specs', () => {
    render(<Harness />)
    expect(screen.queryByRole('button', { name: /Map all/ })).toBeNull()
  })

  it('removes a row', () => {
    render(<Harness initial={{ a: 'x', b: 'y' }} />)
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove mapping' })[0])
    expect(map()).toEqual({ b: 'y' })
    expect(screen.getAllByLabelText('Input key')).toHaveLength(1)
  })

  it('edits only the targeted row', () => {
    render(<Harness initial={{ a: 'x', b: 'y' }} />)
    fireEvent.change(screen.getAllByLabelText('Payload path')[1], { target: { value: 'z.z' } })
    expect(map()).toEqual({ a: 'x', b: 'z.z' })
  })

  it('suggests keys from the target through a datalist', () => {
    const { container } = render(<Harness specs={[spec('company'), spec('region')]} />)
    const opts = Array.from(container.querySelectorAll('datalist option')).map((o) => o.getAttribute('value'))
    expect(opts).toEqual(['company', 'region'])
    expect(screen.queryByLabelText('Input key')).toBeNull()
  })

  it('warns about unmapped required inputs once something is mapped', () => {
    render(<Harness specs={[spec('company'), spec('opt', { required: false }), spec('region')]} initial={{ company: 'c' }} />)
    expect(screen.getByText(/Unmapped required inputs: region\./)).toBeTruthy()
  })

  it('does not warn when there are no rows yet or when only optional inputs are unmapped', () => {
    const { unmount } = render(<Harness specs={[spec('company')]} />)
    expect(screen.queryByText(/Unmapped required/)).toBeNull()
    unmount()
    render(<Harness specs={[spec('opt', { required: false })]} initial={{ x: 'y' }} />)
    expect(screen.queryByText(/Unmapped required/)).toBeNull()
  })

  it('shows an error instead of the warning', () => {
    render(<Harness specs={[spec('company'), spec('region')]} initial={{ company: 'c' }} error="bad path" />)
    expect(screen.getByRole('alert').textContent).toBe('bad path')
    expect(screen.queryByText(/Unmapped required/)).toBeNull()
  })
})
