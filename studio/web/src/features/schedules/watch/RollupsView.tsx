import type { Rollup } from '../../../api/types.trackers'
import { Chip } from '../../../components/StatusBadge'
import { safeHref } from '../../../lib/safeLink'

function Keys({ label, keys, tone }: { label: string; keys: string[]; tone: 'danger' | 'warn' | 'neutral' }) {
  if (keys.length === 0) return null
  return (
    <div className="flex flex-wrap items-baseline gap-1.5">
      <Chip tone={tone}>{label} {keys.length}</Chip>
      <span className="font-mono text-[12px] text-muted">{keys.join(', ')}</span>
    </div>
  )
}

/** A bar of the children by status: done, in progress, to do (canceled ones are only counted). */
function Progress({ c }: { c: Rollup['children'] }) {
  const open = c.total - c.canceled
  const pct = (n: number) => (open > 0 ? `${(100 * n) / open}%` : '0%')
  return (
    <div className="flex flex-col gap-1">
      <div className="flex h-1.5 w-full overflow-hidden rounded bg-panel-2" aria-hidden>
        <div className="bg-ok" style={{ width: pct(c.done) }} />
        <div className="bg-accent" style={{ width: pct(c.in_progress) }} />
      </div>
      <div className="text-[12px] text-muted">
        {c.total} children: {c.done} done, {c.in_progress} in progress, {c.to_do} to do{c.canceled > 0 && `, ${c.canceled} canceled`}
      </div>
    </div>
  )
}

function RollupCard({ r }: { r: Rollup }) {
  return (
    <li className="flex flex-col gap-2 rounded-md border border-line bg-raised p-3" data-testid="rollup">
      <div className="flex flex-wrap items-center gap-2 text-[13px]">
        <span className="font-medium">{r.project}</span>
        <a className="font-mono text-accent" href={safeHref(r.url)} target="_blank" rel="noreferrer noopener">{r.item}</a>
        <span className="min-w-0 truncate text-muted">{r.title}</span>
        {r.changed_since_last_check ? <Chip tone="blue">changed</Chip> : <Chip>no change</Chip>}
        {r.new_comments > 0 && <Chip tone="accent">{r.new_comments} new comments</Chip>}
      </div>
      <div className="text-[12px] text-muted">
        {r.status}{r.due_date && <> · due {r.due_date}</>}{r.lead && <> · lead {r.lead.studio_user || r.lead.name || r.lead.account_id}</>}
      </div>
      <Progress c={r.children} />
      <Keys label="overdue" keys={r.overdue} tone="danger" />
      <Keys label="due after the parent" keys={r.due_after_parent.map((d) => `${d.id} (${d.due_date})`)} tone="warn" />
      <Keys label="blocked" keys={r.blocked} tone="danger" />
      <Keys label="unassigned" keys={r.unassigned} tone="warn" />
      <Keys label="stale" keys={r.stale.map((s) => `${s.id} (${s.last_update})`)} tone="neutral" />
    </li>
  )
}

/** Tracked items' roll-ups: progress of their children and what needs attention. */
export function RollupsView({ rollups, unchanged = 0 }: { rollups: Rollup[]; unchanged?: number }) {
  return (
    <div className="flex flex-col gap-2">
      {rollups.length === 0 ? <p className="text-[12.5px] text-faint">No tracked item changed.</p> : <ul className="flex flex-col gap-2">{rollups.map((r) => <RollupCard key={`${r.connection}-${r.item}`} r={r} />)}</ul>}
      {unchanged > 0 && <p className="text-[12px] text-muted">{unchanged} more tracked items had no changes and were not passed to the crew.</p>}
    </div>
  )
}
