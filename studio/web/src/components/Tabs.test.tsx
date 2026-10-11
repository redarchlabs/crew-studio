import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Tabs } from './Tabs'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()
const tabs = [
  { key: 'one', label: 'One' },
  { key: 'two', label: 'Two', badge: 3 },
  { key: 'three', label: 'Three', badge: 0 },
] as const

describe('Tabs', () => {
  it('renders a tablist with the active tab selected', () => {
    render(<Tabs tabs={tabs} active="two" onChange={() => undefined} />)
    expect(screen.getByRole('tablist')).toBeTruthy()
    const all = screen.getAllByRole('tab')
    expect(all.map((t) => t.getAttribute('aria-selected'))).toEqual(['false', 'true', 'false'])
  })

  it('reports the clicked tab key, including the already-active one', () => {
    const onChange = vi.fn()
    render(<Tabs tabs={tabs} active="one" onChange={onChange} />)
    fireEvent.click(screen.getByRole('tab', { name: /Three/ }))
    expect(onChange).toHaveBeenLastCalledWith('three')
    fireEvent.click(screen.getByRole('tab', { name: 'One' }))
    expect(onChange).toHaveBeenLastCalledWith('one')
  })

  it('shows badges, including a zero badge, but not when undefined', () => {
    render(<Tabs tabs={tabs} active="one" onChange={() => undefined} />)
    expect(screen.getByRole('tab', { name: /^Two\s*3$/ })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /^Three\s*0$/ })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'One' }).querySelector('.font-mono')).toBeNull()
  })

  it('renders an empty tablist for no tabs', () => {
    render(<Tabs tabs={[]} active={'x' as never} onChange={() => undefined} />)
    expect(screen.queryAllByRole('tab')).toHaveLength(0)
  })
})
