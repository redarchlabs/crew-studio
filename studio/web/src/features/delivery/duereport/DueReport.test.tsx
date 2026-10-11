import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { makeSnapshot } from '../testUtils'
import { Delta, DueReport, initials, KeyChip } from './DueReport'

afterEach(cleanup)

const report = { markdown: '**On track** apart from the vendor.', dry_run: false }

describe('DueReport', () => {
  it('shows the header, headline counts with their change, the summary, charts and sections', () => {
    render(<DueReport report={{ ...report, dry_run: true }} snapshot={makeSnapshot()} />)
    expect(screen.getByText('ERP')).toBeTruthy()
    expect(screen.getByText('Daily')).toBeTruthy()
    expect(screen.getByText('Data as of 2026-10-06 (America/Denver)')).toBeTruthy()
    expect(screen.getByText('dry run')).toBeTruthy()
    const overdue = screen.getByTestId('kpi-overdue')
    expect(overdue.textContent).toContain('1')
    expect(within(overdue).getByText('+1 since last report')).toBeTruthy()
    expect(within(overdue).getByRole('img', { name: 'Overdue over the last 2 reports: 0, 1' })).toBeTruthy()
    expect(within(screen.getByTestId('kpi-due_7')).getByText('-1 since last report')).toBeTruthy()
    expect(within(screen.getByTestId('kpi-blocked')).getByText('no change')).toBeTruthy()
    const done = screen.getByTestId('kpi-done')
    expect(done.textContent).toContain('2')
    expect(within(done).getByText('finished items')).toBeTruthy()
    // only one report has a done count, so there is no trend line
    expect(within(done).queryByRole('img')).toBeNull()
    expect(within(screen.getByRole('complementary', { name: 'Summary' })).getByText('On track')).toBeTruthy()
    for (const id of ['chart-workload', 'chart-timeline', 'chart-status']) expect(screen.getByTestId(id)).toBeTruthy()

    const late = screen.getByTestId('section-overdue')
    expect(within(late).getByText('3 days late')).toBeTruthy()
    expect(within(late).getByText('blocked')).toBeTruthy()
    expect(within(late).getByText('In progress')).toBeTruthy()
    expect(within(late).getByRole('link').getAttribute('href')).toBe('https://km2.example.com/r?id=ERP-2')
    expect(within(late).getByText('SL')).toBeTruthy()
    // a javascript: link is not a link
    const today = screen.getByTestId('section-due-today')
    expect(within(today).queryByRole('link')).toBeNull()
    expect(within(today).getByText('ERP-3')).toBeTruthy()
    expect(within(today).queryByText(/days? late/)).toBeNull()
    expect(within(screen.getByTestId('section-unassigned')).getByText('Unassigned')).toBeTruthy()
    const changes = screen.getByTestId('section-changes').textContent
    expect(changes).toContain('Newly overdue: ERP-2')
    expect(changes).toContain('Finished: ERP-9')
    expect(changes).toContain('Slipped: ERP-4 from 2026-10-07 to 2026-10-09')
    expect(changes).toContain('Reassigned: ERP-3 from nobody to Kim')
    expect(changes).toContain('New: ERP-4')
    expect(screen.queryByText(/Some items were left out/)).toBeNull()
  })

  it('handles a first report, empty sections, hostile text and a truncated snapshot', () => {
    const s = makeSnapshot({
      cadence: 'weekly', deltas: null, changes: { newly_overdue: [], finished: [], slipped: [], reassigned: [{ key: 'ERP-4', from: 'Kim', to: '' }], new: [] },
      kpis: { ...makeSnapshot().kpis, done_since_prev: null }, truncated: true, trend: [],
      items: [{ key: 'ERP-2', title: '<img src=x onerror="alert(1)">', status: '', status_category: 'unknown', labels: [], blocked: false, due_date: '' }],
      sections: { overdue: [{ key: 'ERP-2', days_late: 1 }, { key: 'GONE-1', days_late: 1 }], due_today: [], due_next_7: [], blocked: [], unassigned_soon: [], by_person: [] },
    })
    const { container } = render(<DueReport report={{ ...report, markdown: '  ' }} snapshot={s} />)
    expect(screen.getByText('Weekly')).toBeTruthy()
    expect(within(screen.getByTestId('kpi-overdue')).getByText('first report')).toBeTruthy()
    expect(screen.getByTestId('kpi-done').textContent).toContain('—')
    expect(screen.queryByRole('complementary')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByText('<img src=x onerror="alert(1)">')).toBeTruthy()
    expect(screen.getAllByText('1 day late')).toHaveLength(2)
    expect(screen.getByText('GONE-1')).toBeTruthy()
    expect(screen.getByText('unknown')).toBeTruthy()
    expect(screen.queryByTestId('section-due-today')).toBeNull()
    expect(screen.getByTestId('section-changes').textContent).toContain('Reassigned: ERP-4 from Kim to nobody')
    expect(screen.getByText(/Some items were left out/)).toBeTruthy()
    cleanup()
    render(<DueReport report={report} snapshot={makeSnapshot({ changes: { newly_overdue: [], finished: [], slipped: [], reassigned: [], new: [] } })} />)
    expect(screen.getByText('Nothing changed.')).toBeTruthy()
    cleanup()
    render(<DueReport report={report} snapshot={makeSnapshot({ changes: null })} />)
    expect(screen.queryByTestId('section-changes')).toBeNull()
  })

  it('makes initials, deltas and key chips', () => {
    expect(initials('sam lee jones')).toBe('SL')
    expect(initials('  ')).toBe('?')
    expect(initials('Émile')).toBe('É')
    render(<><Delta value={undefined} /><KeyChip item={{ key: 'A-1', url: 'https://x.example/a' }} /><KeyChip item={{ key: 'A-2' }} /></>)
    expect(screen.getByText('first report')).toBeTruthy()
    expect(screen.getAllByRole('link')).toHaveLength(1)
  })
})
