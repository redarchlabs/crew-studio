import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { PlaceholderTextarea } from './PlaceholderTextarea'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('PlaceholderTextarea', () => {
  it('is a labelled textarea wired to its value and onChange', () => {
    const onChange = vi.fn()
    render(<PlaceholderTextarea label="Description" value="hello" onChange={onChange} placeholder="Describe" />)
    const ta = screen.getByLabelText('Description') as HTMLTextAreaElement
    expect(ta.value).toBe('hello')
    expect(ta.placeholder).toBe('Describe')
    expect(ta.rows).toBe(5)
    fireEvent.change(ta, { target: { value: 'hello {topic}' } })
    expect(onChange).toHaveBeenCalledWith('hello {topic}')
  })

  it('honours rows, hint and error (aria-invalid)', () => {
    const { rerender } = render(<PlaceholderTextarea label="D" value="" onChange={() => undefined} rows={8} hint="use {braces}" />)
    expect((screen.getByLabelText('D') as HTMLTextAreaElement).rows).toBe(8)
    expect(screen.getByText('use {braces}')).toBeTruthy()
    rerender(<PlaceholderTextarea label="D" value="" onChange={() => undefined} error="Required" />)
    expect(screen.getByLabelText('D').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByRole('alert').textContent).toBe('Required')
  })

  it('mirrors the text, highlighting each placeholder in a <mark> (aria-hidden)', () => {
    const { container } = render(<PlaceholderTextarea label="D" value="Hi {name}, about {topic}!" onChange={() => undefined} />)
    const mirror = container.querySelector('.hl-mirror')!
    expect(mirror.getAttribute('aria-hidden')).toBe('true')
    expect(Array.from(mirror.querySelectorAll('mark')).map((m) => m.textContent)).toEqual(['{name}', '{topic}'])
    expect(mirror.textContent).toContain('Hi {name}, about {topic}!')
  })

  it('shows no chip list when there are no placeholders', () => {
    const { container } = render(<PlaceholderTextarea label="D" value="plain text" onChange={() => undefined} />)
    expect(container.querySelector('.font-mono')).toBeNull()
  })

  it('lists each distinct key found as a chip', () => {
    render(<PlaceholderTextarea label="D" value="{a} and {b} and {a}" onChange={() => undefined} />)
    const chips = screen.getAllByTitle('Filled from run inputs')
    expect(chips.map((c) => c.textContent).sort()).toEqual(['{a}', '{b}'])
  })

  it('flags keys missing from knownKeys and treats known ones normally', () => {
    render(<PlaceholderTextarea label="D" value="{known} {unknown}" onChange={() => undefined} knownKeys={['known']} />)
    expect(screen.getByTitle('Filled from run inputs').textContent).toBe('{known}')
    const missing = screen.getByTitle('No crew input defines this key yet')
    expect(missing.textContent).toBe('{unknown}')
    expect(missing.className).toContain('border-dashed')
  })

  it('an empty knownKeys list flags every key', () => {
    render(<PlaceholderTextarea label="D" value="{x}" onChange={() => undefined} knownKeys={[]} />)
    expect(screen.getByTitle('No crew input defines this key yet')).toBeTruthy()
  })

  it('keeps the mirror scroll in sync with the textarea', () => {
    const { container } = render(<PlaceholderTextarea label="D" value={'line\n'.repeat(50)} onChange={() => undefined} />)
    const ta = screen.getByLabelText('D')
    const mirror = container.querySelector('.hl-mirror') as HTMLDivElement
    ta.scrollTop = 42
    fireEvent.scroll(ta)
    expect(mirror.scrollTop).toBe(42)
  })
})
