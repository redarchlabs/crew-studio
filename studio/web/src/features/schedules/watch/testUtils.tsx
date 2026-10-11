import type { CrewWatch, Watch, WatchCheck, WatchPreview, WatchView } from '../../../api/types.watch'

const TS = '2026-10-06T13:00:00Z'

export function makeWatch(over: Partial<Watch> = {}): Watch {
  return {
    id: 'w1', schedule_id: 's1', schedule_name: 'Weekly digest', crew_id: 'crew-1',
    connection_id: 'k1', connection_name: 'Tracker API',
    source: { kind: 'http_json', path: '/issues', items_path: 'items' },
    id_field: 'key', tracked_fields: ['status', 'due'], run_when: 'changed_or_followups',
    followups: { date_field: 'due', skip_when: { status: ['Done'] }, steps: [{ name: 'due_today', offset: '0d', notify: 'owner' }], quiet_days: ['Sat', 'Sun'], max_per_tick: 25 },
    dry_run: true, max_items: 2000,
    last_checked_at: TS, last_changed_at: '2026-10-05T13:00:00Z', last_error: null, snapshot_at: TS, snapshot_items: 3,
    created_at: TS, updated_at: TS, ...over,
  }
}

export function makeCheck(over: Partial<WatchCheck> = {}): WatchCheck {
  return { id: 'c1', checked_at: TS, outcome: 'no_changes', added: 0, changed: 0, removed: 0, unchanged: 3, followups_due: 0, work_order_id: null, message: 'checked, no changes', ...over }
}

export function makeView(over: Partial<WatchView> = {}): WatchView {
  return {
    watch: makeWatch(),
    stats: { days: 30, checks: 10, runs_started: 2, runs_skipped: 8, avg_run_tokens: 12000, est_tokens_saved: 96000, est_cost_saved_usd: 0.48 },
    checks: [makeCheck(), makeCheck({ id: 'c2', outcome: 'started', added: 1, unchanged: 2, work_order_id: 'wo-1', message: '1 added, 0 changed, 0 removed, 0 follow-ups due' })],
    source_kinds: [
      { kind: 'http_json', label: 'JSON over HTTP', connection: 'http', items: true, followups: true },
      { kind: 'web_page', label: 'Web page', connection: '', items: false, followups: false },
      { kind: 'workspace_file', label: 'Workspace file (CSV or JSON)', connection: '', items: true, followups: true },
    ],
    connections: [{ id: 'k1', name: 'Tracker API', kind: 'http' }],
    ...over,
  }
}

export function makePreview(over: Partial<WatchPreview> = {}): WatchPreview {
  return {
    changes: {
      source: 'Tracker API: /issues', checked_at: TS, previous_check_at: '2026-10-05T13:00:00Z',
      added: [{ id: 'PRJ-3', fields: { status: 'Open', due: '2026-10-20' } }],
      changed: [{ id: 'PRJ-1', fields: { due: { old: '2026-10-10', new: '2026-10-12' } } }],
      removed: [{ id: 'PRJ-2', reason: 'no longer matches the query' }],
      unchanged_count: 4,
    },
    followups_due: {
      due: [{ item: 'PRJ-1', subject: 'PRJ-1', step: 'due_today', action: 'followup:due_today', notify: 'owner', notify_value: 'sam', date: '2026-10-06', step_date: '2026-10-06', days_late: 0, last_sent_at: null, fields: { status: 'Open' } }],
      more_waiting: 2, dry_run: true, how_to_record: 'Record it.',
    },
    would_run: true, reason: '1 added, 1 changed, 1 removed, 1 follow-ups due', item_count: 6,
    sample: [{ id: 'PRJ-1', fields: { status: 'Open', due: '2026-10-12' } }],
    ...over,
  }
}

export function makeCrewWatch(over: Partial<CrewWatch> = {}): CrewWatch {
  return { ...makeWatch(), stats: makeView().stats!, ...over }
}
