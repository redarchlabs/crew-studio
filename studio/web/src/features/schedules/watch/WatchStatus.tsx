import { Link } from 'react-router'
import { TriangleAlert } from 'lucide-react'
import type { Watch, WatchCheck, WatchCheckOutcome, WatchStats } from '../../../api/types.watch'
import { Chip, type Tone } from '../../../components/StatusBadge'
import { Stat } from '../../../components/misc'
import { compact, dateTime, relative, usd } from '../../../lib/format'

const OUTCOMES: Record<WatchCheckOutcome, { label: string; tone: Tone }> = {
  started: { label: 'started a run', tone: 'ok' },
  no_changes: { label: 'no changes', tone: 'neutral' },
  busy: { label: 'skipped: busy', tone: 'warn' },
  error: { label: 'failed', tone: 'danger' },
}

export function OutcomeChip({ outcome }: { outcome: WatchCheckOutcome }) {
  const o = OUTCOMES[outcome] ?? { label: outcome, tone: 'neutral' as Tone }
  return <Chip tone={o.tone}>{o.label}</Chip>
}

/** "Last checked 2h ago · last changed 1d ago", plus the last problem. */
export function WatchStatusLine({ watch, now = Date.now() }: { watch: Watch; now?: number }) {
  return (
    <div className="flex flex-col gap-1 text-[12.5px]">
      <div className="text-muted">
        {watch.last_checked_at ? <>Last checked <span className="text-fg" title={dateTime(watch.last_checked_at)}>{relative(watch.last_checked_at, now)}</span></> : 'Not checked yet'}
        {' · '}
        {watch.last_changed_at ? <>last changed <span className="text-fg" title={dateTime(watch.last_changed_at)}>{relative(watch.last_changed_at, now)}</span></> : 'no changes seen yet'}
        {' · '}
        {watch.snapshot_at ? `${watch.snapshot_items} items in the snapshot` : 'the next check is the first'}
        {watch.dry_run && <Chip tone="warn" className="ml-2">dry run</Chip>}
      </div>
      {watch.last_error && (
        <div className="flex items-start gap-1 text-warn" role="alert"><TriangleAlert className="mt-0.5 size-3.5 shrink-0" /><span>{watch.last_error}</span></div>
      )}
    </div>
  )
}

/** Runs skipped and what that saved, over the stats period. */
export function WatchSavings({ stats }: { stats: WatchStats }) {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <Stat label={`Checks (${stats.days}d)`} value={stats.checks} />
      <Stat label="Runs started" value={stats.runs_started} />
      <Stat label="Runs skipped" value={stats.runs_skipped} />
      <Stat
        label="Est. tokens saved"
        value={stats.avg_run_tokens > 0 ? compact(stats.est_tokens_saved) : '—'}
        sub={stats.avg_run_tokens > 0 ? `~${usd(stats.est_cost_saved_usd)} at ${compact(stats.avg_run_tokens)} tokens a run` : 'no finished runs to average yet'}
      />
    </div>
  )
}

/** Recent checks, newest first: the schedule's history, including the ticks that started nothing. */
export function WatchChecks({ checks }: { checks: WatchCheck[] }) {
  if (checks.length === 0) return <p className="text-[12.5px] text-faint">No checks yet.</p>
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-left text-[12.5px]">
        <thead className="border-b border-line">
          <tr className="eyebrow [&>th]:px-2 [&>th]:py-1.5 [&>th]:font-semibold"><th>When</th><th>Outcome</th><th>Added</th><th>Changed</th><th>Removed</th><th>Follow-ups</th><th>Details</th></tr>
        </thead>
        <tbody>
          {checks.map((c) => (
            <tr key={c.id} className="border-b border-line align-top last:border-0 [&>td]:px-2 [&>td]:py-1.5">
              <td className="whitespace-nowrap text-muted">{dateTime(c.checked_at)}</td>
              <td><OutcomeChip outcome={c.outcome} /></td>
              <td className="tabular">{c.added}</td>
              <td className="tabular">{c.changed}</td>
              <td className="tabular">{c.removed}</td>
              <td className="tabular">{c.followups_due}</td>
              <td className="max-w-sm">
                <span className="text-muted">{c.message}</span>
                {c.work_order_id && <> · <Link className="text-accent" to={`/work-orders/${c.work_order_id}`}>work order</Link></>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
