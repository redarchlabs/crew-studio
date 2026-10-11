import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { Field, FormErrors, NumberField, SelectField, TextAreaField, TextField, Toggle } from './Field'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('Field', () => {
  it('links the label to the control via a generated id', () => {
    render(<Field label="Name">{(id) => <input id={id} />}</Field>)
    const input = screen.getByLabelText('Name')
    expect(input.tagName).toBe('INPUT')
  })
  it('gives each field a unique id', () => {
    render(
      <>
        <Field label="A">{(id) => <input id={id} />}</Field>
        <Field label="B">{(id) => <input id={id} />}</Field>
      </>,
    )
    expect(screen.getByLabelText('A').id).not.toBe(screen.getByLabelText('B').id)
  })
  it('shows a hint, and an error alert in place of the hint', () => {
    const { rerender } = render(<Field label="X" hint="a hint">{(id) => <input id={id} />}</Field>)
    expect(screen.getByText('a hint')).toBeTruthy()
    expect(screen.queryByRole('alert')).toBeNull()
    rerender(<Field label="X" hint="a hint" error="it broke">{(id) => <input id={id} />}</Field>)
    expect(screen.getByRole('alert').textContent).toBe('it broke')
    expect(screen.queryByText('a hint')).toBeNull()
  })
  it('passes invalid to the render prop and renders the aside', () => {
    const seen: boolean[] = []
    render(
      <Field label="X" error="bad" aside={<span>aside!</span>} className="custom">
        {(id, invalid) => {
          seen.push(invalid)
          return <input id={id} />
        }}
      </Field>,
    )
    expect(seen).toEqual([true])
    expect(screen.getByText('aside!')).toBeTruthy()
  })
  it('renders neither hint nor error when none given', () => {
    const { container } = render(<Field label="X">{(id) => <input id={id} />}</Field>)
    expect(container.querySelector('.text-faint')).toBeNull()
  })
})

describe('TextField', () => {
  it('is controlled and reports string changes', () => {
    const onChange = vi.fn()
    render(<TextField label="Name" value="abc" onChange={onChange} placeholder="p" />)
    const input = screen.getByLabelText('Name') as HTMLInputElement
    expect(input.value).toBe('abc')
    expect(input.placeholder).toBe('p')
    fireEvent.change(input, { target: { value: 'abcd' } })
    expect(onChange).toHaveBeenCalledWith('abcd')
  })
  it('marks invalid and shows the error', () => {
    render(<TextField label="Name" value="" onChange={() => undefined} error="Required" />)
    expect(screen.getByLabelText('Name').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByRole('alert').textContent).toBe('Required')
  })
  it('is valid without an error', () => {
    render(<TextField label="Name" value="" onChange={() => undefined} />)
    expect(screen.getByLabelText('Name').getAttribute('aria-invalid')).toBe('false')
  })
  it('forwards native attributes like disabled and type', () => {
    render(<TextField label="Pw" type="password" disabled value="" onChange={() => undefined} />)
    const input = screen.getByLabelText('Pw') as HTMLInputElement
    expect(input.type).toBe('password')
    expect(input.disabled).toBe(true)
  })
})

describe('NumberField', () => {
  function Harness({ nullable, initial }: { nullable?: boolean; initial: number | null }) {
    const [v, setV] = useState<number | null>(initial)
    return (
      <>
        <NumberField label="Temp" value={v} onChange={setV} nullable={nullable} min={0} max={2} step={0.1} />
        <output data-testid="out">{v === null ? 'null' : Number.isNaN(v) ? 'NaN' : String(v)}</output>
      </>
    )
  }
  const input = () => screen.getByLabelText('Temp') as HTMLInputElement

  it('shows the number and forwards min/max/step', () => {
    render(<Harness initial={0.5} />)
    expect(input().value).toBe('0.5')
    expect(input().type).toBe('number')
    expect(input().min).toBe('0')
    expect(input().max).toBe('2')
    expect(input().step).toBe('0.1')
  })
  it('emits numbers', () => {
    render(<Harness initial={1} />)
    fireEvent.change(input(), { target: { value: '1.5' } })
    expect(screen.getByTestId('out').textContent).toBe('1.5')
  })
  it('clearing a nullable field emits null and shows the "default" placeholder', () => {
    render(<Harness initial={1} nullable />)
    expect(input().placeholder).toBe('default')
    fireEvent.change(input(), { target: { value: '' } })
    expect(screen.getByTestId('out').textContent).toBe('null')
    expect(input().value).toBe('')
  })
  it('clearing a required field emits NaN and renders empty', () => {
    render(<Harness initial={1} />)
    expect(input().placeholder).toBe('')
    fireEvent.change(input(), { target: { value: '' } })
    expect(screen.getByTestId('out').textContent).toBe('NaN')
    expect(input().value).toBe('')
  })
  it('renders null as empty', () => {
    render(<Harness initial={null} nullable />)
    expect(input().value).toBe('')
  })
  it('accepts zero as a real value', () => {
    render(<Harness initial={0} />)
    expect(input().value).toBe('0')
  })
})

describe('TextAreaField', () => {
  it('defaults to 4 rows, accepts rows override, and reports changes', () => {
    const onChange = vi.fn()
    const { rerender } = render(<TextAreaField label="Body" value="hello" onChange={onChange} />)
    const ta = screen.getByLabelText('Body') as HTMLTextAreaElement
    expect(ta.rows).toBe(4)
    expect(ta.value).toBe('hello')
    fireEvent.change(ta, { target: { value: 'line1\nline2' } })
    expect(onChange).toHaveBeenCalledWith('line1\nline2')
    rerender(<TextAreaField label="Body" value="" onChange={onChange} rows={9} error="e" />)
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).rows).toBe(9)
    expect(screen.getByLabelText('Body').getAttribute('aria-invalid')).toBe('true')
  })
})

describe('SelectField', () => {
  const options = [
    { value: 'a', label: 'Alpha' },
    { value: 'b', label: 'Beta', disabled: true },
    { value: 'c', label: 'Gamma' },
  ]
  it('renders options, selects the current value, and reports changes', () => {
    const onChange = vi.fn()
    render(<SelectField label="Pick" value="a" onChange={onChange} options={options} hint="choose" aside={<span>side</span>} />)
    const sel = screen.getByLabelText('Pick') as HTMLSelectElement
    expect(sel.value).toBe('a')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Alpha', 'Beta', 'Gamma'])
    expect((screen.getByRole('option', { name: 'Beta' }) as HTMLOptionElement).disabled).toBe(true)
    expect(screen.getByText('choose')).toBeTruthy()
    expect(screen.getByText('side')).toBeTruthy()
    fireEvent.change(sel, { target: { value: 'c' } })
    expect(onChange).toHaveBeenCalledWith('c')
  })
  it('shows an error', () => {
    render(<SelectField label="Pick" value="a" onChange={() => undefined} options={options} error="Pick one" />)
    expect(screen.getByRole('alert').textContent).toBe('Pick one')
  })
})

describe('Toggle', () => {
  it('exposes a switch with aria-checked and toggles', () => {
    const onChange = vi.fn()
    const { rerender } = render(<Toggle label="Verbose" checked={false} onChange={onChange} hint="Logs more" />)
    const sw = screen.getByRole('switch')
    expect(sw.getAttribute('aria-checked')).toBe('false')
    expect(screen.getByText('Logs more')).toBeTruthy()
    fireEvent.click(sw)
    expect(onChange).toHaveBeenLastCalledWith(true)
    rerender(<Toggle label="Verbose" checked onChange={onChange} />)
    expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('true')
    fireEvent.click(screen.getByRole('switch'))
    expect(onChange).toHaveBeenLastCalledWith(false)
  })
  it('does not toggle when disabled', () => {
    const onChange = vi.fn()
    render(<Toggle label="X" checked={false} onChange={onChange} disabled />)
    const sw = screen.getByRole('switch') as HTMLButtonElement
    expect(sw.disabled).toBe(true)
    fireEvent.click(sw)
    expect(onChange).not.toHaveBeenCalled()
  })
})

describe('FormErrors', () => {
  it('renders nothing for an empty list', () => {
    const { container } = render(<FormErrors messages={[]} />)
    expect(container.firstChild).toBeNull()
  })
  it('renders each message in an alert list', () => {
    render(<FormErrors messages={['one', 'two']} />)
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.getAllByRole('listitem').map((l) => l.textContent)).toEqual(['one', 'two'])
  })
})
