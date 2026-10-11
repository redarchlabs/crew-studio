import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, screen, within } from '@testing-library/react'
import { makeWorkOrder, renderRoute } from './testUtils'
import { WorkOrdersTable } from './WorkOrdersTable'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock())

afterEach(cleanup)

const NOW = Date.parse('2026-10-03T12:00:00Z')

describe('WorkOrdersTable', () => {
  const items = [
    makeWorkOrder({ id: 'a', number: 1, title: 'First', status: 'running', cost_usd: 1.5, run_count: 3, source: 'schedule', source_id: 's1', created_by_name: 'Ada' }),
    makeWorkOrder({ id: 'b', number: 2, title: 'Second', status: 'draft', cost_usd: null, run_count: 1, created_by_name: '', crew_id: null, target_name: 'Old crew' }),
  ]

  it('renders a row per work order with links, badges and cost', () => {
    renderRoute(<WorkOrdersTable items={items} now={NOW} />)
    const table = screen.getByRole('table')
    const rows = within(table).getAllByRole('row')
    expect(rows).toHaveLength(3) // header + 2
    const first = within(rows[1])
    expect(first.getAllByRole('link').map((a) => a.getAttribute('href'))).toEqual(['/work-orders/a', '/crews/crew-1'])
    expect(first.getByText('WO-1')).toBeTruthy()
    expect(first.getByText('First')).toBeTruthy()
    expect(first.getByText('3 attempts')).toBeTruthy()
    expect(first.getByText('running')).toBeTruthy()
    expect(first.getByText('$1.50')).toBeTruthy()
    expect(first.getByText('Ada')).toBeTruthy()
    expect(first.getByText('2h ago')).toBeTruthy()
    const second = within(rows[2])
    expect(second.getByText('draft')).toBeTruthy()
    expect(second.queryByText(/attempts/)).toBeNull()
    expect(second.getAllByText('—').length).toBeGreaterThanOrEqual(2)
    // A deleted crew shows its old name without a link
    expect(second.getByText('Old crew')).toBeTruthy()
    expect(second.getAllByRole('link')).toHaveLength(1)
  })

  it('also renders stacked cards for narrow screens with a fallback creator', () => {
    renderRoute(<WorkOrdersTable items={items} now={NOW} />)
    expect(screen.getByText(/Unknown · updated/)).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /First/ })).toHaveLength(2)
    // Cards are links themselves, so the crew inside is plain text
    expect(screen.getAllByRole('link').filter((a) => a.getAttribute('href') === '/crews/crew-1')).toHaveLength(1)
  })

  it('compact mode drops secondary columns and the card list', () => {
    renderRoute(<WorkOrdersTable items={items} now={NOW} compact />)
    expect(screen.queryByText('Crew')).toBeNull()
    expect(screen.queryByText('Source')).toBeNull()
    expect(screen.queryByText('Created by')).toBeNull()
    expect(screen.getByText('Cost')).toBeTruthy()
    expect(screen.queryByRole('list')).toBeNull()
  })
})
