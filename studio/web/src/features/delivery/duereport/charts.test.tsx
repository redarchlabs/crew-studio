import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { makeSnapshot } from '../testUtils'
import { Sparkline, StatusMixChart, TimelineChart, WorkloadChart } from './charts'

afterEach(cleanup)

describe('report charts', () => {
  it('draws workload as stacked bars, reads a mark on hover and focus, and has a table', () => {
    render(<WorkloadChart people={makeSnapshot().sections.by_person} />)
    const chart = screen.getByTestId('chart-workload')
    expect(within(chart).getByRole('group', { name: /Workload by person/ })).toBeTruthy()
    const mark = within(chart).getByRole('img', { name: 'Sam Lee: 1 overdue' })
    expect(mark.getAttribute('fill')).toBe('var(--chart-critical)')
    expect(within(chart).getByRole('img', { name: 'Kim: 1 due within 7 days' }).getAttribute('fill')).toBe('var(--chart-warning)')
    fireEvent.pointerEnter(mark)
    expect(within(chart).getByText('Sam Lee: 1 overdue', { selector: 'span' })).toBeTruthy()
    fireEvent.pointerLeave(mark)
    fireEvent.focus(mark)
    expect(within(chart).getByText('Sam Lee: 1 overdue', { selector: 'span' })).toBeTruthy()
    fireEvent.blur(mark)
    expect(within(chart).queryByText('Sam Lee: 1 overdue', { selector: 'span' })).toBeNull()
    expect(within(chart).getByRole('list', { name: 'Legend' }).textContent).toContain('Overdue')
    const rows = within(chart).getAllByRole('row')
    expect(rows.map((r) => r.textContent)).toEqual(['PersonOverdueDue within 7 days', 'Sam Lee10', 'Kim01', 'Unassigned01'])
  })

  it('shortens long names and says when nobody has work due', () => {
    const { unmount } = render(<WorkloadChart people={[{ name: 'Alexandra Featherstonehaugh', open: 1, overdue: 2, due_7: 0 }]} />)
    expect(screen.getByText('Alexandra Feather…')).toBeTruthy()
    unmount()
    const { container } = render(<WorkloadChart people={[{ name: 'Idle', open: 4, overdue: 0, due_7: 0 }]} />)
    expect(screen.getByText('Nobody has overdue work or work due this week.')).toBeTruthy()
    expect(container.querySelector('svg')).toBeNull()
  })

  it('draws the due timeline with the peak labeled, today marked and a table', () => {
    render(<TimelineChart days={makeSnapshot().timeline} />)
    const chart = screen.getByTestId('chart-timeline')
    expect(within(chart).getByRole('group', { name: 'Items due on each of the next 14 days' })).toBeTruthy()
    expect(within(chart).getByText('Today')).toBeTruthy()
    const peak = within(chart).getByRole('img', { name: '9 Oct: 2 due' })
    fireEvent.pointerEnter(peak)
    expect(within(chart).getByText('9 Oct: 2 due', { selector: 'span' })).toBeTruthy()
    // the peak's value is written over it; the axis says 0 and the top
    expect(within(chart).getAllByText('2').length).toBeGreaterThanOrEqual(2)
    expect(within(chart).getAllByRole('row')).toHaveLength(15)
    cleanup()
    render(<TimelineChart days={[{ date: '2026-10-06', count: 0 }]} />)
    expect(screen.getByText('Nothing is due in the next 14 days.')).toBeTruthy()
  })

  it('draws the status mix on one bar with counts in the legend', () => {
    render(<StatusMixChart mix={makeSnapshot().status_mix} />)
    const chart = screen.getByTestId('chart-status')
    expect(within(chart).getByRole('img', { name: 'To do: 2 of 5' }).getAttribute('fill')).toBe('var(--chart-ord-1)')
    expect(within(chart).getByRole('img', { name: 'Done: 2 of 5' }).getAttribute('fill')).toBe('var(--chart-ord-3)')
    expect(within(chart).queryByRole('img', { name: /Canceled/ })).toBeNull()
    expect(within(chart).getByRole('list', { name: 'Legend' }).textContent).toBe('To do 2In progress 1Done 2Canceled 0')
    cleanup()
    render(<StatusMixChart mix={{ to_do: 0, in_progress: 0, done: 0, canceled: 0 }} />)
    expect(screen.getByText('No tracked items.')).toBeTruthy()
  })

  it('draws a sparkline with its values as text, and nothing for one value', () => {
    render(<Sparkline values={[0, 2, 1]} label="Overdue" />)
    expect(screen.getByRole('img', { name: 'Overdue over the last 3 reports: 0, 2, 1' })).toBeTruthy()
    cleanup()
    const { container } = render(<Sparkline values={[1]} label="Overdue" />)
    expect(container.innerHTML).toBe('')
  })
})
