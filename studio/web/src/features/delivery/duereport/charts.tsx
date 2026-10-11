import { useState, type ReactNode } from 'react'
import type { DuePerson, DueSnapshot } from '../../../api/types.delivery'
import { BAR, barPath, columnPath, foldPeople, shortDate, sparkPoints, stack, timelineColumns } from './geometry'

// Hand-written SVG charts for the report page (dataviz skill): thin marks, hairline axes, text in text colors (never
// the series color), a legend for two or more series, selective direct labels, a hover/focus readout on every mark,
// and a table view so no value depends on color or hover. Colors are the --chart-* tokens of the app's themes.

type Readout = { text: string } | null

/** A chart's frame: title, the hovered mark's value, the legend, the chart, and its table. */
function ChartCard({ title, legend, readout, table, children, testId }: {
  title: string; legend?: ReactNode; readout: Readout; table: ReactNode; children: ReactNode; testId: string
}) {
  return (
    <section className="rounded-lg border border-line bg-panel p-3" data-testid={testId}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h4 className="text-[13px] font-medium">{title}</h4>
        <span className="text-[12px] text-muted" aria-live="polite">{readout?.text ?? ''}</span>
      </div>
      {legend}
      {children}
      <details className="mt-2 text-[12px]">
        <summary className="cursor-pointer text-muted">Table</summary>
        {table}
      </details>
    </section>
  )
}

function Legend({ entries }: { entries: { color: string; label: string }[] }) {
  return (
    <ul className="my-1.5 flex flex-wrap gap-x-4 gap-y-1 text-[12px] text-muted" aria-label="Legend">
      {entries.map((e) => (
        <li key={e.label} className="inline-flex items-center gap-1.5">
          <span aria-hidden className="inline-block size-2.5 rounded-[2px]" style={{ background: e.color }} />{e.label}
        </li>
      ))}
    </ul>
  )
}

/** Props that make a mark show its value on hover and on keyboard focus. */
function markProps(text: string, set: (r: Readout) => void) {
  return {
    tabIndex: 0, role: 'img', 'aria-label': text,
    onPointerEnter: () => set({ text }), onPointerLeave: () => set(null), onFocus: () => set({ text }), onBlur: () => set(null),
    className: 'outline-none hover:opacity-80 focus-visible:opacity-80',
  }
}

const tableClass = 'mt-1 w-full text-left [&_td]:py-0.5 [&_td]:pr-3 [&_th]:pr-3 [&_th]:font-medium [&_th]:text-muted'

/** Workload by person: overdue (critical) then due within 7 days (warning), a horizontal stacked bar each. */
export function WorkloadChart({ people }: { people: readonly DuePerson[] }) {
  const [readout, setReadout] = useState<Readout>(null)
  const rows = foldPeople(people)
  const width = 560
  const labelW = 130
  const endW = 34
  const rowH = BAR + 10
  const max = Math.max(1, ...rows.map((p) => p.overdue + p.due_7))
  const table = (
    <table className={tableClass}><thead><tr><th>Person</th><th>Overdue</th><th>Due within 7 days</th></tr></thead>
      <tbody>{rows.map((p) => <tr key={p.name}><td>{p.name}</td><td>{p.overdue}</td><td>{p.due_7}</td></tr>)}</tbody></table>
  )
  return (
    <ChartCard title="Workload by person" testId="chart-workload" readout={readout} table={table}
      legend={<Legend entries={[{ color: 'var(--chart-critical)', label: 'Overdue' }, { color: 'var(--chart-warning)', label: 'Due within 7 days' }]} />}>
      {rows.length === 0 ? <p className="text-[12.5px] text-faint">Nobody has overdue work or work due this week.</p> : (
        <svg viewBox={`0 0 ${width} ${rows.length * rowH + 6}`} className="w-full" role="group" aria-label="Workload by person: overdue and due within 7 days">
          <line x1={labelW} x2={labelW} y1={0} y2={rows.length * rowH + 6} stroke="var(--chart-axis)" strokeWidth={1} />
          {rows.map((p, i) => {
            const y = 6 + i * rowH
            const segs = stack([{ key: 'o', label: 'overdue', value: p.overdue }, { key: 'd', label: 'due within 7 days', value: p.due_7 }],
              max, width - labelW - endW, labelW)
            const total = p.overdue + p.due_7
            return (
              <g key={p.name}>
                <text x={labelW - 8} y={y + BAR - 5} fontSize={12} textAnchor="end" fill="var(--fg)">{p.name.length > 18 ? `${p.name.slice(0, 17)}…` : p.name}</text>
                {segs.map((s) => (
                  <path key={s.key} d={barPath(s.x0, s.x1, y, BAR, s.round)} fill={s.key === 'o' ? 'var(--chart-critical)' : 'var(--chart-warning)'}
                    {...markProps(`${p.name}: ${s.value} ${s.label}`, setReadout)} />
                ))}
                <text x={labelW + ((width - labelW - endW) * total) / max + 6} y={y + BAR - 5} fontSize={12} fill="var(--fg-muted)">{total}</text>
              </g>
            )
          })}
        </svg>
      )}
    </ChartCard>
  )
}

/** Items due on each of the next 14 days: one series, the peak labeled, today marked. */
export function TimelineChart({ days }: { days: DueSnapshot['timeline'] }) {
  const [readout, setReadout] = useState<Readout>(null)
  const width = 560
  const plotH = 120
  const left = 26
  const { columns, max } = timelineColumns(days, width, plotH, left)
  const top = 18
  const table = (
    <table className={tableClass}><thead><tr><th>Date</th><th>Due</th></tr></thead>
      <tbody>{days.map((d) => <tr key={d.date}><td>{d.date}</td><td>{d.count}</td></tr>)}</tbody></table>
  )
  return (
    <ChartCard title="Due in the next 14 days" testId="chart-timeline" readout={readout} table={table}>
      {!columns.some((c) => c.count > 0) ? <p className="text-[12.5px] text-faint">Nothing is due in the next 14 days.</p> : (
        <svg viewBox={`0 0 ${width} ${plotH + top + 22}`} className="w-full" role="group" aria-label={`Items due on each of the next ${days.length} days`}>
          {[0, max].map((v) => {
            const y = top + plotH - (plotH * v) / max
            return (
              <g key={v}>
                <line x1={left} x2={width} y1={y} y2={y} stroke="var(--chart-grid)" strokeWidth={1} />
                <text x={left - 6} y={y + 4} fontSize={11} textAnchor="end" fill="var(--fg-muted)">{v}</text>
              </g>
            )
          })}
          {columns.map((c, i) => (
            <g key={c.date}>
              <path d={columnPath(c.x, top + plotH, c.w, c.h)} fill="var(--chart-series)" {...markProps(`${shortDate(c.date)}: ${c.count} due`, setReadout)} />
              {(i % 2 === 0 || c.peak) && (
                <text x={c.x + c.w / 2} y={top + plotH + 16} fontSize={11} textAnchor="middle" fill="var(--fg-muted)" fontWeight={c.today ? 600 : 400}>
                  {c.today ? 'Today' : shortDate(c.date)}
                </text>
              )}
              {c.peak && <text x={c.x + c.w / 2} y={top + plotH - c.h - 6} fontSize={12} fontWeight={600} textAnchor="middle" fill="var(--fg)">{c.count}</text>}
            </g>
          ))}
        </svg>
      )}
    </ChartCard>
  )
}

const MIX = [
  { key: 'to_do', label: 'To do', color: 'var(--chart-ord-1)' },
  { key: 'in_progress', label: 'In progress', color: 'var(--chart-ord-2)' },
  { key: 'done', label: 'Done', color: 'var(--chart-ord-3)' },
  { key: 'canceled', label: 'Canceled', color: 'var(--chart-other)' },
] as const

/** Every tracked item by status: one 100% stacked bar on an ordinal ramp (to do, in progress, done), canceled in gray. */
export function StatusMixChart({ mix }: { mix: DueSnapshot['status_mix'] }) {
  const [readout, setReadout] = useState<Readout>(null)
  // Drawn at about the width it is shown at, full width, so the bar stays thin
  const width = 1100
  const total = mix.to_do + mix.in_progress + mix.done + mix.canceled
  const segs = stack(MIX.map((m) => ({ key: m.key, label: m.label, value: mix[m.key] })), total, width)
  const table = (
    <table className={tableClass}><thead><tr>{MIX.map((m) => <th key={m.key}>{m.label}</th>)}</tr></thead>
      <tbody><tr>{MIX.map((m) => <td key={m.key}>{mix[m.key]}</td>)}</tr></tbody></table>
  )
  return (
    <ChartCard title="Status mix" testId="chart-status" readout={readout} table={table}
      legend={<Legend entries={MIX.map((m) => ({ color: m.color, label: `${m.label} ${mix[m.key]}` }))} />}>
      {total === 0 ? <p className="text-[12.5px] text-faint">No tracked items.</p> : (
        <svg viewBox={`0 0 ${width} ${BAR}`} className="w-full" role="group" aria-label="Status mix of every tracked item">
          {segs.map((s) => (
            <path key={s.key} d={barPath(s.x0, s.x1, 0, BAR, s.round)} fill={MIX.find((m) => m.key === s.key)!.color}
              {...markProps(`${s.label}: ${s.value} of ${total}`, setReadout)} />
          ))}
        </svg>
      )}
    </ChartCard>
  )
}

/** A KPI's last reports as a 2px line in the de-emphasis gray, today's point in the series blue. */
export function Sparkline({ values, label }: { values: readonly number[]; label: string }) {
  const w = 110
  const h = 26
  const geo = sparkPoints(values, w, h)
  if (!geo) return null
  return (
    <svg width={w} height={h} className="mt-1.5 block" role="img" aria-label={`${label} over the last ${values.length} reports: ${values.join(', ')}`}>
      <polyline points={geo.points} fill="none" stroke="var(--chart-muted)" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={geo.last.x} cy={geo.last.y} r={4} fill="var(--chart-series)" stroke="var(--panel)" strokeWidth={2} />
    </svg>
  )
}
