import { relative } from '../../lib/format'

/** Relative time in either direction: "in 5m", "in 2h", "due now", or "3m ago" for the past. */
export function fromNow(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  const s = Math.round((t - now) / 1000)
  if (s <= 0) return s > -45 ? 'due now' : relative(iso, now)
  if (s < 60) return `in ${s}s`
  if (s < 3600) return `in ${Math.round(s / 60)}m`
  if (s < 86400) return `in ${Math.round(s / 3600)}h`
  return `in ${Math.round(s / 86400)}d`
}

/** Absolute date and time in a given IANA zone, e.g. "Mon 6 Oct, 09:00 BST". */
export function inZone(iso: string, timeZone: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  try {
    return d.toLocaleString(undefined, { timeZone, weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', timeZoneName: 'short' })
  } catch {
    return d.toLocaleString()
  }
}
