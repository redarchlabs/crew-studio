import type { DuePerson, DueSnapshot } from '../../../api/types.delivery'

// Chart geometry, kept apart from rendering so it can be tested. Marks follow the dataviz skill: bars at most 24px
// thick with a 4px rounded data end and a square baseline end, and a 2px surface gap between touching segments.

export const BAR = 18
export const GAP = 2
const RADIUS = 4

const n1 = (v: number) => Math.round(v * 10) / 10

/** A horizontal bar from x0 to x1, its right end rounded when `round`. */
export function barPath(x0: number, x1: number, y: number, h: number, round: boolean): string {
  const w = x1 - x0
  if (w <= 0) return ''
  if (!round || w < RADIUS) return `M${n1(x0)} ${n1(y)}h${n1(w)}v${n1(h)}h${n1(-w)}z`
  return `M${n1(x0)} ${n1(y)}h${n1(w - RADIUS)}a${RADIUS} ${RADIUS} 0 0 1 ${RADIUS} ${RADIUS}v${n1(h - 2 * RADIUS)}a${RADIUS} ${RADIUS} 0 0 1 ${-RADIUS} ${RADIUS}h${n1(-(w - RADIUS))}z`
}

/** A column rising from the baseline y0 by h, with a rounded top. */
export function columnPath(x: number, y0: number, w: number, h: number): string {
  if (h <= 0) return ''
  if (h < RADIUS) return `M${n1(x)} ${n1(y0)}v${n1(-h)}h${n1(w)}v${n1(h)}z`
  return `M${n1(x)} ${n1(y0)}v${n1(-(h - RADIUS))}a${RADIUS} ${RADIUS} 0 0 1 ${RADIUS} ${-RADIUS}h${n1(w - 2 * RADIUS)}a${RADIUS} ${RADIUS} 0 0 1 ${RADIUS} ${RADIUS}v${n1(h - RADIUS)}z`
}

export type Segment = { key: string; label: string; value: number; x0: number; x1: number; round: boolean }

/** Lays values out left to right over width, with a 2px gap between segments and the last one rounded. */
export function stack(parts: { key: string; label: string; value: number }[], total: number, width: number, x = 0): Segment[] {
  const shown = parts.filter((p) => p.value > 0)
  const out: Segment[] = []
  let at = x
  shown.forEach((p, i) => {
    const w = total > 0 ? (width * p.value) / total : 0
    const last = i === shown.length - 1
    out.push({ key: p.key, label: p.label, value: p.value, x0: at, x1: last ? at + w : at + w - GAP, round: last })
    at += w
  })
  return out
}

/** People with overdue or soon-due work, busiest first; past `keep`, the tail folds into "Other". */
export function foldPeople(people: readonly DuePerson[], keep = 8): DuePerson[] {
  const busy = people.filter((p) => p.overdue + p.due_7 > 0)
  if (busy.length <= keep) return busy
  const other = busy.slice(keep - 1).reduce<DuePerson>((o, p) => ({ ...o, open: o.open + p.open, overdue: o.overdue + p.overdue, due_7: o.due_7 + p.due_7 }),
    { name: 'Other', open: 0, overdue: 0, due_7: 0 })
  return [...busy.slice(0, keep - 1), other]
}

export type Column = { date: string; count: number; x: number; y: number; w: number; h: number; peak: boolean; today: boolean }

/** Columns for the due timeline: one per day, scaled to the busiest day, which is marked as the peak. */
export function timelineColumns(days: DueSnapshot['timeline'], width: number, height: number, left: number): { columns: Column[]; max: number } {
  const max = Math.max(1, ...days.map((d) => d.count))
  const peakCount = Math.max(0, ...days.map((d) => d.count))
  const peakAt = peakCount > 0 ? days.findIndex((d) => d.count === peakCount) : -1
  const slot = days.length ? (width - left) / days.length : 0
  const w = Math.min(24, slot - 6)
  return {
    max,
    columns: days.map((d, i) => {
      const h = (height * d.count) / max
      return { date: d.date, count: d.count, x: left + slot * i + (slot - w) / 2, y: height - h, w, h, peak: i === peakAt, today: i === 0 }
    }),
  }
}

/** Points of a sparkline over a w×h box with `pad` around it, and its last point. */
export function sparkPoints(values: readonly number[], w: number, h: number, pad = 4): { points: string; last: { x: number; y: number } } | null {
  if (values.length < 2) return null
  const top = Math.max(1, ...values)
  const step = (w - 2 * pad) / (values.length - 1)
  const pts = values.map((v, i) => ({ x: pad + step * i, y: h - pad - ((h - 2 * pad) * v) / top }))
  return { points: pts.map((p) => `${n1(p.x)},${n1(p.y)}`).join(' '), last: pts[pts.length - 1] }
}

/** A date such as 2026-10-12 as "12 Oct" (the dates are calendar dates, read without a time zone). */
export function shortDate(date: string): string {
  const [y, m, d] = date.split('-').map(Number)
  if (!y || !m || !d) return date
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', timeZone: 'UTC' })
}
