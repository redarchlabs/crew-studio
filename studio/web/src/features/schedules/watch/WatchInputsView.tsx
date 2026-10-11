import type { ReactNode } from 'react'
import type { RollupsInput } from '../../../api/types.trackers'
import type { WatchChanges, WatchFollowups } from '../../../api/types.watch'
import { Chip } from '../../../components/StatusBadge'
import { dateTime } from '../../../lib/format'
import { RollupsView } from './RollupsView'

/** The keys a watch adds to a work order's inputs. */
export const WATCH_INPUT_KEYS = ['changes', 'followups_due', 'dry_run', 'rollups'] as const

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function parse(raw: string | undefined): unknown {
  if (!raw) return undefined
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

/** Reads a work order's watch inputs; anything that is not the expected JSON is left out. */
export function parseWatchInputs(inputs: Record<string, string>): { changes?: WatchChanges; followups?: WatchFollowups; rollups?: RollupsInput; dryRun?: boolean } {
  const changes = parse(inputs.changes)
  const followups = parse(inputs.followups_due)
  const rollups = parse(inputs.rollups)
  return {
    rollups: isRecord(rollups) && Array.isArray(rollups.items) ? (rollups as RollupsInput) : undefined,
    changes: isRecord(changes) && Array.isArray(changes.added) && Array.isArray(changes.changed) && Array.isArray(changes.removed) ? (changes as WatchChanges) : undefined,
    followups: isRecord(followups) && Array.isArray(followups.due) ? (followups as WatchFollowups) : undefined,
    dryRun: inputs.dry_run === undefined ? undefined : inputs.dry_run === 'true',
  }
}

/** A field value as short text. */
export function valueText(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'string') return v === '' ? '""' : v
  return JSON.stringify(v)
}

function Fields({ fields }: { fields: Record<string, unknown> | undefined }) {
  const entries = Object.entries(fields ?? {})
  if (entries.length === 0) return null
  return (
    <span className="text-muted">
      {entries.map(([k, v], i) => (
        <span key={k}>{i > 0 && ' · '}<span className="font-mono text-faint">{k}</span> {valueText(v)}</span>
      ))}
    </span>
  )
}

function Group({ title, tone, count, children }: { title: string; tone: 'ok' | 'blue' | 'danger' | 'accent'; count: number; children: ReactNode }) {
  if (count === 0) return null
  return (
    <div>
      <div className="mb-1 flex items-center gap-2"><Chip tone={tone}>{title}</Chip><span className="text-[11.5px] text-faint">{count}</span></div>
      <ul className="flex flex-col gap-1 text-[12.5px]">{children}</ul>
    </div>
  )
}

/** The change list a watch found, grouped as added, changed and removed. */
export function ChangesView({ changes }: { changes: WatchChanges }) {
  const total = changes.added.length + changes.changed.length + changes.removed.length + (changes.new_comments?.length ?? 0)
  return (
    <div className="flex flex-col gap-3">
      <div className="text-[12px] text-muted">
        <span className="font-medium text-fg">{changes.source}</span> · checked {dateTime(changes.checked_at)} ·{' '}
        {changes.previous_check_at ? <>since {dateTime(changes.previous_check_at)}</> : <Chip tone="accent">first check</Chip>}
        {' '}· {changes.unchanged_count} unchanged
      </div>
      {total === 0 && <p className="text-[12.5px] text-faint">No changes.</p>}
      <Group title="added" tone="ok" count={changes.added.length}>
        {changes.added.map((a) => <li key={a.id}><span className="font-mono">{a.id}</span> <Fields fields={a.fields} /></li>)}
        {changes.added_fields_omitted && <li className="text-faint">Fields left out: the list was too long to pass on in full.</li>}
      </Group>
      <Group title="changed" tone="blue" count={changes.changed.length}>
        {changes.changed.map((c) => (
          <li key={c.id}>
            <span className="font-mono">{c.id}</span>{' '}
            {Object.entries(c.fields).map(([field, ch], i) => (
              <span key={field} className="text-muted">{i > 0 && ' · '}<span className="font-mono text-faint">{field}</span> {valueText(ch.old)} → <span className="text-fg">{valueText(ch.new)}</span></span>
            ))}
          </li>
        ))}
      </Group>
      <Group title="removed" tone="danger" count={changes.removed.length}>
        {changes.removed.map((r) => <li key={r.id}><span className="font-mono">{r.id}</span> <span className="text-faint">{r.reason}</span></li>)}
      </Group>
      <Group title="new comments" tone="accent" count={changes.new_comments?.length ?? 0}>
        {(changes.new_comments ?? []).map((c, i) => <li key={`${c.id}-${i}`}><span className="font-mono">{c.id}</span> {c.author}: <span className="text-muted">{c.excerpt}</span></li>)}
        {(changes.new_comments_omitted ?? 0) > 0 && <li className="text-faint">{changes.new_comments_omitted} more new comments were left out (50 per check).</li>}
      </Group>
    </div>
  )
}

/** The follow-up steps due, as a table. */
export function FollowupsView({ followups }: { followups: WatchFollowups }) {
  return (
    <div className="flex flex-col gap-2">
      {followups.due.length === 0 ? (
        <p className="text-[12.5px] text-faint">No follow-ups due.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[520px] text-left text-[12.5px]">
            <thead className="border-b border-line">
              <tr className="eyebrow [&>th]:px-2 [&>th]:py-1.5 [&>th]:font-semibold"><th>Item</th><th>Step</th><th>Notify</th><th>Date</th><th>Last sent</th></tr>
            </thead>
            <tbody>
              {followups.due.map((d) => (
                <tr key={`${d.item}-${d.step}`} className="border-b border-line last:border-0 [&>td]:px-2 [&>td]:py-1.5">
                  <td className="font-mono">{d.item}</td>
                  <td className="font-mono">{d.step}{d.repeat && <span className="text-faint"> (every {d.repeat})</span>}</td>
                  <td>{d.notify}{d.notify_person ? <span className="text-muted"> ({d.notify_person.studio_user || d.notify_person.name || d.notify_person.account_id})</span>
                    : d.notify_value !== undefined && d.notify_value !== null && <span className="text-muted"> ({valueText(d.notify_value)})</span>}</td>
                  <td className="whitespace-nowrap">{d.step_date}{d.days_late > 0 && <span className="text-warn"> · {d.days_late}d late</span>}</td>
                  <td className="whitespace-nowrap text-muted">{d.last_sent_at ? dateTime(d.last_sent_at) : 'never'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {followups.more_waiting > 0 && <p className="text-[12px] text-muted">{followups.more_waiting} more wait for the next check (per-check cap).</p>}
      {(followups.undated_items ?? 0) > 0 && (
        <p className="text-[12px] text-warn">{followups.undated_items} items have no usable date (empty, or not like 2026-10-20), so no step can be due for them.</p>
      )}
    </div>
  )
}

/** A watched work order's inputs, readable: the changes, the follow-ups due, and whether it was a dry run. */
export function WatchInputsSection({ inputs }: { inputs: Record<string, string> }) {
  const { changes, followups, rollups, dryRun } = parseWatchInputs(inputs)
  if (!changes && !followups && !rollups) return null
  return (
    <div className="flex flex-col gap-3" data-testid="watch-inputs">
      {dryRun && <div><Chip tone="warn">dry run</Chip> <span className="text-[12px] text-muted">Nothing should have been sent; look for “Would post” in the output.</span></div>}
      {changes && <ChangesView changes={changes} />}
      {rollups && <div><div className="eyebrow mb-1">Roll-ups</div><RollupsView rollups={rollups.items} unchanged={rollups.unchanged_items} /></div>}
      {followups && <div><div className="eyebrow mb-1">Follow-ups due</div><FollowupsView followups={followups} /></div>}
    </div>
  )
}
