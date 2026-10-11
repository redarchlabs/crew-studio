import type { DestinationsView, DueSnapshot, Report, ReportDelivery, ReportDestination } from '../../api/types.delivery'

const TS = '2026-10-05T15:00:00Z'

export function makeDestination(over: Partial<ReportDestination> = {}): ReportDestination {
  return { id: 'd1', schedule_id: 's1', position: 0, kind: 'slack', connection_id: 'c-slack', connection_name: 'PMO Slack', recipients: [], folder: '', created_at: TS, ...over }
}

export function makeDestinationsView(over: Partial<DestinationsView> = {}): DestinationsView {
  return {
    destinations: [makeDestination()],
    connections: [
      { id: 'c-slack', name: 'PMO Slack', kind: 'slack' },
      { id: 'c-mail', name: 'PMO Mail', kind: 'email' },
      { id: 'c-km2', name: 'KM2', kind: 'km2' },
    ],
    kinds: ['slack', 'teams', 'email', 'km2'],
    ...over,
  }
}

export function makeDelivery(over: Partial<ReportDelivery> = {}): ReportDelivery {
  return {
    id: 'v1', report_id: 'r1', position: 0, kind: 'work_order', connection_id: null, connection_name: '', recipients: [], folder: '',
    status: 'sent', message: 'Kept with the work order.', attempts: 0, sent_at: TS, updated_at: TS, ...over,
  }
}

export function makeReport(over: Partial<Report> = {}): Report {
  return {
    id: 'r1', crew_id: 'crew-1', schedule_id: 's1', run_id: 'run-1', work_order_id: 'wo-1', project: 'Atlas', project_slug: 'atlas', week: '2026-W41',
    title: 'Atlas status, 2026-W41', markdown: '# Atlas\nOn track', bytes: 17, dry_run: false, created_at: TS,
    deliveries: [
      makeDelivery(),
      makeDelivery({ id: 'v2', position: 1, kind: 'teams', connection_id: 'c-teams', connection_name: 'PMO Teams', status: 'failed', message: 'Teams had a problem (HTTP 502).', attempts: 2 }),
      makeDelivery({ id: 'v3', position: 2, kind: 'email', connection_id: 'c-mail', connection_name: 'PMO Mail', recipients: ['a@example.com'], message: 'Sent.' }),
    ],
    retryable: 1,
    ...over,
  }
}

const ZERO = { open: 0, overdue: 0, due_today: 0, due_7: 0, blocked: 0, unassigned_soon: 0, done_since_prev: null }

/** A daily ERP snapshot: one late blocked item, one due today, one unassigned soon, with a previous report. */
export function makeSnapshot(over: Partial<DueSnapshot> = {}): DueSnapshot {
  const days = Array.from({ length: 14 }, (_, i) => ({ date: `2026-10-${String(6 + i).padStart(2, '0')}`, count: i === 0 ? 1 : i === 3 ? 2 : 0 }))
  return {
    version: 1, generated_at: TS, timezone: 'America/Denver', today: '2026-10-06', cadence: 'daily', project: 'ERP', previous_report_id: 'r0', truncated: false,
    items: [
      { key: 'ERP-2', title: 'Vendor access', url: 'https://km2.example.com/r?id=ERP-2', assignee: 'Sam Lee', status: 'In progress', status_category: 'in_progress',
        due_date: '2026-10-03', labels: ['blocked'], blocked: true, epic: 'ERP-1' },
      { key: 'ERP-3', title: 'Cutover plan', url: 'javascript:alert(1)', assignee: 'Kim', status: 'To do', status_category: 'to_do', due_date: '2026-10-06', labels: [], blocked: false },
      { key: 'ERP-4', title: 'Data load', status: 'To do', status_category: 'to_do', due_date: '2026-10-09', labels: [], blocked: false },
    ],
    kpis: { open: 3, overdue: 1, due_today: 1, due_7: 3, blocked: 1, unassigned_soon: 1, done_since_prev: 2 },
    deltas: { ...ZERO, overdue: 1, due_7: -1, done_since_prev: 0 },
    sections: {
      overdue: [{ key: 'ERP-2', days_late: 3 }], due_today: ['ERP-3'], due_next_7: ['ERP-4'], blocked: ['ERP-2'], unassigned_soon: ['ERP-4'],
      by_person: [{ name: 'Sam Lee', open: 1, overdue: 1, due_7: 0 }, { name: 'Kim', open: 1, overdue: 0, due_7: 1 }, { name: 'Unassigned', open: 1, overdue: 0, due_7: 1 }],
    },
    changes: { newly_overdue: ['ERP-2'], finished: [{ key: 'ERP-9', title: 'Kickoff' }], slipped: [{ key: 'ERP-4', from: '2026-10-07', to: '2026-10-09' }],
      reassigned: [{ key: 'ERP-3', from: '', to: 'Kim' }], new: ['ERP-4'] },
    timeline: days,
    status_mix: { to_do: 2, in_progress: 1, done: 2, canceled: 0 },
    trend: [{ date: '2026-10-05', ...ZERO, overdue: 0, due_7: 4 }, { date: '2026-10-06', ...ZERO, overdue: 1, due_7: 3, done_since_prev: 2 }],
    ...over,
  }
}
