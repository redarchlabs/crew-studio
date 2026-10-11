const usdFmt = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 })
const smallUsdFmt = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 4, maximumFractionDigits: 4 })
const intFmt = new Intl.NumberFormat('en-US')

/** Dollars with extra precision for sub-cent amounts. */
export function usd(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  if (value !== 0 && Math.abs(value) < 0.01) return smallUsdFmt.format(value)
  return usdFmt.format(value)
}

export function int(value: number | null | undefined): string {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  return intFmt.format(value)
}

export function compact(value: number): string {
  // Thresholds sit where rounding would otherwise print "10.0k" or "1000k"
  const abs = Math.abs(value)
  if (abs >= 999_500) return `${(value / 1_000_000).toFixed(1)}M`
  if (abs >= 9_950) return `${Math.round(value / 1000)}k`
  if (abs >= 1000) return `${(value / 1000).toFixed(1)}k`
  return String(value)
}

export function durationMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || ms < 0 || Number.isNaN(ms)) return '—'
  if (ms < 1000) return `${Math.round(ms)}ms`
  const s = ms / 1000
  if (s < 60) return `${s.toFixed(s < 10 ? 1 : 0)}s`
  const m = Math.floor(s / 60)
  const rem = Math.round(s % 60)
  if (m < 60) return `${m}m ${rem}s`
  return `${Math.floor(m / 60)}h ${m % 60}m`
}

export function between(start: string | null, end: string | null, now: number = Date.now()): number | null {
  if (!start) return null
  const a = Date.parse(start)
  const b = end ? Date.parse(end) : now
  if (Number.isNaN(a) || Number.isNaN(b)) return null
  return b - a
}

export function dateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function timeOfDay(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
}

export function relative(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return '—'
  const s = Math.round((now - t) / 1000)
  if (s < 45) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)}m ago`
  if (s < 86400) return `${Math.round(s / 3600)}h ago`
  return `${Math.round(s / 86400)}d ago`
}

export function truncate(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}

export function shortId(id: string): string {
  return id.slice(0, 8)
}
