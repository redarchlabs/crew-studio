import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, screen } from '@testing-library/react'
import { endpointsMock, renderRoute } from './testUtils'
import { CrewLabel, isActive, SourceChip, WORK_ORDER_SOURCES, WORK_ORDER_STATUSES, WoBadge, woNumber, WorkOrderStatusBadge } from './workOrderLabels'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock())
void endpointsMock

afterEach(cleanup)

describe('woNumber / isActive', () => {
  it('formats the number', () => {
    expect(woNumber(123)).toBe('WO-123')
  })

  it.each([
    ['queued', true],
    ['running', true],
    ['draft', false],
    ['succeeded', false],
    ['failed', false],
    ['cancelled', false],
    [null, false],
    [undefined, false],
  ] as const)('isActive(%s) = %s', (status, expected) => {
    expect(isActive(status)).toBe(expected)
  })

  it('lists draft first in the status filter and every source', () => {
    expect(WORK_ORDER_STATUSES[0]).toBe('draft')
    expect(WORK_ORDER_STATUSES).toContain('running')
    expect([...WORK_ORDER_SOURCES].sort()).toEqual(['api', 'chat', 'manual', 'schedule', 'webhook'])
  })
})

describe('badges', () => {
  it('shows a neutral badge for drafts and the run badge otherwise', () => {
    renderRoute(<><WorkOrderStatusBadge status="draft" /><WorkOrderStatusBadge status="running" /></>)
    expect(screen.getByText('draft')).toBeTruthy()
    expect(screen.getByText('running')).toBeTruthy()
  })

  it('renders WO badge with custom class', () => {
    renderRoute(<WoBadge number={4} className="extra" />)
    expect(screen.getByText('WO-4').className).toContain('extra')
  })
})

describe('SourceChip', () => {
  it('shows the source label without a link by default', () => {
    renderRoute(<SourceChip source="schedule" sourceId="s1" />)
    expect(screen.getByText('schedule')).toBeTruthy()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('links schedules, webhooks and chats when asked', () => {
    renderRoute(<><SourceChip source="schedule" sourceId="s1" link /><SourceChip source="webhook" sourceId="w1" link /><SourceChip source="chat" sourceId="c1" link /></>)
    const hrefs = screen.getAllByRole('link').map((a) => a.getAttribute('href'))
    expect(hrefs).toEqual(['/schedules/s1', '/triggers/w1', '/chat/c1'])
  })

  it('does not link manual/api sources or ones without an id', () => {
    renderRoute(<><SourceChip source="manual" sourceId="x" link /><SourceChip source="api" link /><SourceChip source="schedule" sourceId={null} link /></>)
    expect(screen.queryByRole('link')).toBeNull()
  })
})

describe('CrewLabel', () => {
  it('shows name with an accessible icon label', () => {
    renderRoute(<CrewLabel name="Research crew" crewId="c1" />)
    expect(screen.getByText('Research crew')).toBeTruthy()
    expect(screen.getByLabelText('crew')).toBeTruthy()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('links to the crew when link is set', () => {
    renderRoute(<CrewLabel name="C" crewId="c1" link />)
    expect(screen.getByRole('link').getAttribute('href')).toBe('/crews/c1')
  })

  it('shows the old name as plain text once the crew was deleted', () => {
    renderRoute(<CrewLabel name="Old crew" crewId={null} link />)
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.getByText('Old crew').parentElement?.getAttribute('title')).toBe('This crew was deleted')
  })

  it('marks a target with no name', () => {
    renderRoute(<CrewLabel name="" crewId={null} link />)
    expect(screen.getByText('(deleted)')).toBeTruthy()
  })
})
