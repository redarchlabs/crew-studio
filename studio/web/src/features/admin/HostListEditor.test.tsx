import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { HostListEditor } from './HostListEditor'
import './testUtils'

function setup(hosts: string[] = [], disabled = false) {
  const onChange = vi.fn()
  render(<HostListEditor hosts={hosts} onChange={onChange} disabled={disabled} />)
  const input = screen.getByLabelText('Host to allow') as HTMLInputElement
  return { onChange, input }
}
const type = (input: HTMLInputElement, v: string) => fireEvent.change(input, { target: { value: v } })

describe('HostListEditor', () => {
  it('adds a normalised host with the Add button', () => {
    const { onChange, input } = setup(['a.example'])
    type(input, '  ERP.Internal. ')
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(onChange).toHaveBeenCalledWith(['a.example', 'erp.internal'])
    expect(input.value).toBe('')
  })

  it.each(['Enter', ','])('adds on %s', (key) => {
    const { onChange, input } = setup()
    type(input, '10.0.0.5')
    fireEvent.keyDown(input, { key })
    expect(onChange).toHaveBeenCalledWith(['10.0.0.5'])
  })

  it('ignores other keys', () => {
    const { onChange, input } = setup()
    type(input, '10.0.0.5')
    fireEvent.keyDown(input, { key: 'a' })
    expect(onChange).not.toHaveBeenCalled()
  })

  it('keeps Add disabled for a blank draft', () => {
    const { input } = setup()
    expect((screen.getByRole('button', { name: 'Add' }) as HTMLButtonElement).disabled).toBe(true)
    type(input, '   ')
    expect((screen.getByRole('button', { name: 'Add' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it.each([
    ['https://evil.example', /without http/],
    ['evil.example/path', /without a path/],
    ['evil.example:8080', /Leave out the port/],
    ['two words', /cannot contain spaces/],
    ['[::1]', /without brackets/],
    ['999.1.1.1', /Not a valid host/],
    ['bad_host!', /Not a valid host/],
    ['a.example', /Already on the list/],
  ])('rejects %s with a message and does not change the list', (value, message) => {
    const { onChange, input } = setup(['a.example'])
    type(input, value)
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(screen.getByRole('alert').textContent).toMatch(message)
    expect(onChange).not.toHaveBeenCalled()
    expect(input.getAttribute('aria-invalid')).toBe('true')
    type(input, 'ok.example')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('accepts IPv6 without brackets', () => {
    const { onChange, input } = setup()
    type(input, '::1')
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(onChange).toHaveBeenCalledWith(['::1'])
  })

  it('removes a host', () => {
    const { onChange } = setup(['a.example', 'b.example'])
    fireEvent.click(screen.getByRole('button', { name: 'Remove a.example' }))
    expect(onChange).toHaveBeenCalledWith(['b.example'])
  })

  it('is read-only when disabled', () => {
    render(<HostListEditor hosts={['a.example']} onChange={vi.fn()} disabled />)
    expect(screen.getByText('a.example')).toBeTruthy()
    expect(screen.queryByLabelText('Host to allow')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Remove a.example' })).toBeNull()
  })

  it('explains an empty list', () => {
    setup()
    expect(screen.getByText(/No hosts: callbacks may only reach public https/)).toBeTruthy()
  })
})
