import type { ReactNode } from 'react'
import { ArrowDown, ArrowUp, Minus } from 'lucide-react'
import type { DueItem, DueKPIs, DueSnapshot, Report } from '../../../api/types.delivery'
import { Markdown } from '../../../components/Markdown'
import { Chip, type Tone } from '../../../components/StatusBadge'
import { safeHref } from '../../../lib/safeLink'
import { Sparkline, StatusMixChart, TimelineChart, WorkloadChart } from './charts'

// The designed daily or weekly report: Crew Studio's own reading of the tracker (headline counts with their change
// since the previous report, sections, charts) around the crew's narrative. Every string from the tracker is
// untrusted: React escapes it, and links pass safeHref.

type KpiKey = Exclude<keyof DueKPIs, 'open' | 'due_today' | 'done_since_prev'>

const TILES: { key: KpiKey; label: string }[] = [
  { key: 'overdue', label: 'Overdue' },
  { key: 'due_7', label: 'Due within 7 days' },
  { key: 'blocked', label: 'Blocked' },
  { key: 'unassigned_soon', label: 'Unassigned, due soon' },
]

/** The change since the previous report; for these counts a rise is bad news. */
export function Delta({ value }: { value: number | undefined }) {
  if (value === undefined) return <span className="text-[12px] text-faint">first report</span>
  const Icon = value > 0 ? ArrowUp : value < 0 ? ArrowDown : Minus
  const tone = value > 0 ? 'text-danger' : value < 0 ? 'text-ok' : 'text-muted'
  const text = value === 0 ? 'no change' : `${value > 0 ? '+' : ''}${value} since last report`
  return (
    <span className={`inline-flex items-center gap-1 text-[12px] ${tone}`}>
      <Icon aria-hidden className="size-3" />{text}
    </span>
  )
}

function Tile({ label, value, delta, trend, testId }: { label: string; value: ReactNode; delta: ReactNode; trend: number[]; testId: string }) {
  return (
    <div className="rounded-lg border border-line bg-panel px-3 py-2.5" data-testid={testId}>
      <div className="text-[12px] text-muted">{label}</div>
      <div className="mt-0.5 text-[24px] font-semibold leading-tight tabular-nums">{value}</div>
      {delta}
      <Sparkline values={trend} label={label} />
    </div>
  )
}

function Kpis({ s }: { s: DueSnapshot }) {
  const trend = s.trend ?? []
  const done = s.kpis.done_since_prev
  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5" aria-label="Headline counts">
      {TILES.map((t) => (
        <Tile key={t.key} testId={`kpi-${t.key}`} label={t.label} value={s.kpis[t.key]} delta={<Delta value={s.deltas?.[t.key]} />}
          trend={trend.map((p) => p[t.key])} />
      ))}
      <Tile testId="kpi-done" label="Done since last report" value={done ?? '—'}
        delta={done === null ? <span className="text-[12px] text-faint">first report</span> : <span className="text-[12px] text-muted">finished items</span>}
        trend={trend.filter((p) => p.done_since_prev !== null).map((p) => p.done_since_prev as number)} />
    </div>
  )
}

/** The first letters of up to two words of a name. */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean)
  return words.slice(0, 2).map((w) => Array.from(w)[0]?.toUpperCase() ?? '').join('') || '?'
}

function Assignee({ name }: { name?: string }) {
  if (!name) return <span className="text-faint">Unassigned</span>
  return (
    <span className="inline-flex items-center gap-1.5">
      <span aria-hidden className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-raised text-[10px] font-medium text-muted">{initials(name)}</span>
      {name}
    </span>
  )
}

const STATUS_TONE: Record<string, Tone> = { to_do: 'neutral', in_progress: 'blue', done: 'ok', canceled: 'neutral' }

/** An issue key as a chip, linked to the tracker only when the link is http(s). */
export function KeyChip({ item }: { item: Pick<DueItem, 'key' | 'url'> }) {
  const href = safeHref(item.url)
  const chip = <span className="inline-block rounded border border-line bg-raised px-1.5 py-px font-mono text-[11.5px]">{item.key}</span>
  return href ? <a href={href} target="_blank" rel="noopener noreferrer nofollow" className="hover:underline">{chip}</a> : chip
}

type Row = { item: DueItem; daysLate?: number }

function ItemTable({ title, rows, testId }: { title: string; rows: Row[]; testId: string }) {
  if (rows.length === 0) return null
  return (
    <section data-testid={testId}>
      <h4 className="mb-1 text-[13px] font-medium">{title} <span className="text-muted">({rows.length})</span></h4>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[640px] table-fixed text-left text-[12.5px]">
          <colgroup><col className="w-24" /><col /><col className="w-40" /><col className="w-24" /><col className="w-28" /><col className="w-44" /></colgroup>
          <thead className="text-muted">
            <tr><th className="py-1 pr-3 font-medium">Item</th><th className="py-1 pr-3 font-medium">Title</th><th className="py-1 pr-3 font-medium">Assignee</th>
              <th className="py-1 pr-3 font-medium">Due</th><th className="py-1 pr-3 font-medium">Late</th><th className="py-1 font-medium">Status</th></tr>
          </thead>
          <tbody>
            {rows.map(({ item, daysLate }) => (
              <tr key={item.key} className="border-t border-line align-top">
                <td className="py-1.5 pr-3"><KeyChip item={item} /></td>
                <td className="py-1.5 pr-3">{item.title}</td>
                <td className="py-1.5 pr-3 whitespace-nowrap"><Assignee name={item.assignee} /></td>
                <td className="py-1.5 pr-3 whitespace-nowrap tabular-nums">{item.due_date || '—'}</td>
                <td className="py-1.5 pr-3">{daysLate !== undefined && <Chip tone="danger">{daysLate} {daysLate === 1 ? 'day' : 'days'} late</Chip>}</td>
                <td className="py-1.5">
                  <span className="inline-flex flex-wrap gap-1">
                    <Chip tone={STATUS_TONE[item.status_category] ?? 'neutral'}>{item.status || item.status_category}</Chip>
                    {item.blocked && <Chip tone="warn">blocked</Chip>}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function Sections({ s, items }: { s: DueSnapshot; items: Map<string, DueItem> }) {
  const of = (key: string): DueItem => items.get(key) ?? { key, title: '', status: '', status_category: '', labels: [], blocked: false }
  const rows = (keys: string[] | undefined): Row[] => (keys ?? []).map((k) => ({ item: of(k) }))
  return (
    <div className="flex flex-col gap-4">
      <ItemTable testId="section-overdue" title="Overdue" rows={(s.sections.overdue ?? []).map((l) => ({ item: of(l.key), daysLate: l.days_late }))} />
      <ItemTable testId="section-due-today" title="Due today" rows={rows(s.sections.due_today)} />
      <ItemTable testId="section-due-next-7" title="Due in the next 7 days" rows={rows(s.sections.due_next_7)} />
      <ItemTable testId="section-blocked" title="Blocked" rows={rows(s.sections.blocked)} />
      <ItemTable testId="section-unassigned" title="Unassigned and due within 14 days" rows={rows(s.sections.unassigned_soon)} />
    </div>
  )
}

function Changes({ c }: { c: NonNullable<DueSnapshot['changes']> }) {
  const lines: { label: string; text: string }[] = []
  if (c.newly_overdue.length) lines.push({ label: 'Newly overdue', text: c.newly_overdue.join(', ') })
  if (c.finished.length) lines.push({ label: 'Finished', text: c.finished.map((f) => f.key).join(', ') })
  for (const m of c.slipped) lines.push({ label: 'Slipped', text: `${m.key} from ${m.from} to ${m.to}` })
  for (const m of c.reassigned) lines.push({ label: 'Reassigned', text: `${m.key} from ${m.from || 'nobody'} to ${m.to || 'nobody'}` })
  if (c.new.length) lines.push({ label: 'New', text: c.new.join(', ') })
  return (
    <section data-testid="section-changes">
      <h4 className="mb-1 text-[13px] font-medium">Since the last report</h4>
      {lines.length === 0 ? <p className="text-[12.5px] text-faint">Nothing changed.</p> : (
        <ul className="flex flex-col gap-0.5 text-[12.5px]">
          {lines.map((l, i) => <li key={i}><span className="text-muted">{l.label}:</span> {l.text}</li>)}
        </ul>
      )}
    </section>
  )
}

/** The designed report for a report whose snapshot was taken. */
export function DueReport({ report, snapshot: s }: { report: Pick<Report, 'markdown' | 'dry_run'>; snapshot: DueSnapshot }) {
  const items = new Map((s.items ?? []).map((i) => [i.key, i]))
  return (
    <div className="flex flex-col gap-4" data-testid="due-report">
      <div className="flex flex-wrap items-center gap-2 text-[12.5px]">
        <span className="text-[15px] font-semibold">{s.project}</span>
        <Chip tone={s.cadence === 'daily' ? 'blue' : 'neutral'}>{s.cadence === 'daily' ? 'Daily' : 'Weekly'}</Chip>
        <span className="text-muted">Data as of {s.today} ({s.timezone})</span>
        {report.dry_run && <Chip tone="warn">dry run</Chip>}
      </div>
      <Kpis s={s} />
      {report.markdown.trim() !== '' && (
        <aside className="rounded-lg border border-accent/40 bg-accent-soft px-4 py-3" aria-label="Summary">
          <div className="mb-1 text-[12px] font-medium text-muted">Summary from the crew</div>
          <Markdown>{report.markdown}</Markdown>
        </aside>
      )}
      <div className="grid gap-3 lg:grid-cols-2">
        <WorkloadChart people={s.sections.by_person ?? []} />
        <TimelineChart days={s.timeline ?? []} />
      </div>
      <StatusMixChart mix={s.status_mix} />
      <Sections s={s} items={items} />
      {s.changes && <Changes c={s.changes} />}
      {s.truncated && <p className="text-[12px] text-faint">Some items were left out to keep the report short; the counts cover every item.</p>}
    </div>
  )
}
