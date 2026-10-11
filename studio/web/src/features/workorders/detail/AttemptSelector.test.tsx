import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen } from '@testing-library/react'
import { makeRun, renderRoute, setPermissions } from '../testUtils'
import { AttemptSelector } from './AttemptSelector'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())
vi.mock('../../../lib/auth', async () => (await import('../testUtils')).authMock())

beforeEach(() => setPermissions('runs.view'))
afterEach(cleanup)

const runs = [
  makeRun({ id: 'r3', status: 'running', cost_usd: 0.12 }),
  makeRun({ id: 'r2', status: 'failed', cost_usd: 1 }),
  makeRun({ id: 'r1', status: 'succeeded', cost_usd: null }),
]

describe('AttemptSelector', () => {
  it('numbers attempts oldest first, newest on the left', () => {
    renderRoute(<AttemptSelector runs={runs} selectedId="r3" onSelect={vi.fn()} liveStatus={null} />)
    expect(screen.getByText('Attempt 3 of 3')).toBeTruthy()
    const tabs = screen.getAllByRole('tab')
    expect(tabs.map((t) => t.textContent?.match(/#\d/)?.[0])).toEqual(['#3', '#2', '#1'])
    expect(tabs.map((t) => t.getAttribute('aria-selected'))).toEqual(['true', 'false', 'false'])
  })

  it('numbers the selected attempt by position', () => {
    renderRoute(<AttemptSelector runs={runs} selectedId="r1" onSelect={vi.fn()} liveStatus={null} />)
    expect(screen.getByText('Attempt 1 of 3')).toBeTruthy()
  })

  it('treats an unknown selection as the latest', () => {
    renderRoute(<AttemptSelector runs={runs} selectedId={null} onSelect={vi.fn()} liveStatus={null} />)
    expect(screen.getByText('Attempt 3 of 3')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /Open run/ })).toBeNull()
  })

  it('selects an attempt on click', () => {
    const onSelect = vi.fn()
    renderRoute(<AttemptSelector runs={runs} selectedId="r3" onSelect={onSelect} liveStatus={null} />)
    fireEvent.click(screen.getAllByRole('tab')[1])
    expect(onSelect).toHaveBeenCalledWith('r2')
  })

  it('shows the live status for the selected attempt only', () => {
    renderRoute(<AttemptSelector runs={runs} selectedId="r2" onSelect={vi.fn()} liveStatus="queued" />)
    const tabs = screen.getAllByRole('tab')
    expect(tabs[1].textContent).toContain('queued')
    expect(tabs[0].textContent).toContain('running')
    expect(tabs[2].textContent).toContain('succeeded')
  })

  it('shows cost per attempt, with a dash when unknown', () => {
    renderRoute(<AttemptSelector runs={runs} selectedId="r3" onSelect={vi.fn()} liveStatus={null} />)
    const tabs = screen.getAllByRole('tab')
    expect(tabs[0].textContent).toContain('$0.12')
    expect(tabs[2].textContent).toContain('—')
  })

  it('links to the run for people who can view runs', () => {
    renderRoute(<AttemptSelector runs={runs} selectedId="r2" onSelect={vi.fn()} liveStatus={null} />)
    expect(screen.getByRole('link', { name: /Open run/ }).getAttribute('href')).toBe('/runs/r2')
  })

  it('hides the run link without permission', () => {
    setPermissions()
    renderRoute(<AttemptSelector runs={runs} selectedId="r2" onSelect={vi.fn()} liveStatus={null} />)
    expect(screen.queryByRole('link', { name: /Open run/ })).toBeNull()
  })
})
