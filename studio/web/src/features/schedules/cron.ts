// Client-side helpers for 5-field cron expressions ("min hour day month weekday") and @-descriptors.
// The server is the authority (POST /schedules/preview); this only describes expressions in words.

export type CronPreset = { key: string; label: string; cron: string }

export const CRON_PRESETS: readonly CronPreset[] = [
  { key: 'hourly', label: 'Every hour', cron: '0 * * * *' },
  { key: 'daily', label: 'Every day at 09:00', cron: '0 9 * * *' },
  { key: 'weekdays', label: 'Weekdays at 09:00', cron: '0 9 * * 1-5' },
  { key: 'monday', label: 'Every Monday at 09:00', cron: '0 9 * * 1' },
  { key: 'monthly', label: 'First of the month at 09:00', cron: '0 9 1 * *' },
]

export const CUSTOM_PRESET = 'custom'

/** The preset an expression matches exactly (ignoring extra spaces), or "custom". */
export function presetFor(cron: string): string {
  const norm = cron.trim().split(/\s+/).join(' ')
  return CRON_PRESETS.find((p) => p.cron === norm)?.key ?? CUSTOM_PRESET
}

const DESCRIPTORS: Record<string, string> = {
  '@hourly': '0 * * * *',
  '@daily': '0 0 * * *',
  '@midnight': '0 0 * * *',
  '@weekly': '0 0 * * 0',
  '@monthly': '0 0 1 * *',
  '@yearly': '0 0 1 1 *',
  '@annually': '0 0 1 1 *',
}

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const MONTH_NAMES = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC']
const DAY_NAMES = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT']

/** One parsed field: every allowed value, plus the shape it was written in. */
export type CronField = {
  any: boolean
  values: number[]
  /** "* /n" or "a-b/n" */
  step: number | null
  /** "a-b" without a step */
  range: [number, number] | null
}

type FieldSpec = { min: number; max: number; names?: readonly string[] }

const SPECS: readonly FieldSpec[] = [
  { min: 0, max: 59 },
  { min: 0, max: 23 },
  { min: 1, max: 31 },
  { min: 1, max: 12, names: MONTH_NAMES },
  { min: 0, max: 7, names: DAY_NAMES },
]

function parseNumber(raw: string, spec: FieldSpec): number | null {
  const upper = raw.toUpperCase()
  const named = spec.names?.indexOf(upper) ?? -1
  if (named >= 0) return spec === SPECS[3] ? named + 1 : named
  if (!/^\d+$/.test(raw)) return null
  const n = Number(raw)
  return n >= spec.min && n <= spec.max ? n : null
}

/** Values for one comma-separated part ("*", "5", "1-5", "* /15", "9-17/2"). */
function parsePart(part: string, spec: FieldSpec): { values: number[]; step: number | null; range: [number, number] | null } | null {
  const [base, stepRaw, extra] = part.split('/')
  if (extra !== undefined) return null
  const step = stepRaw === undefined ? null : /^\d+$/.test(stepRaw) && Number(stepRaw) > 0 ? Number(stepRaw) : NaN
  if (Number.isNaN(step)) return null
  let lo: number
  let hi: number
  if (base === '*') {
    lo = spec.min
    hi = spec.max
  } else if (base.includes('-')) {
    const [a, b] = base.split('-')
    const pa = parseNumber(a, spec)
    const pb = parseNumber(b, spec)
    if (pa === null || pb === null || pa > pb) return null
    lo = pa
    hi = pb
  } else {
    const n = parseNumber(base, spec)
    if (n === null) return null
    lo = n
    hi = step === null ? n : spec.max
  }
  const values: number[] = []
  for (let v = lo; v <= hi; v += step ?? 1) values.push(v)
  const range: [number, number] | null = base.includes('-') && step === null ? [lo, hi] : null
  return { values, step, range }
}

export function parseField(raw: string, spec: FieldSpec): CronField | null {
  if (raw === '*' || raw === '*/1') return { any: true, values: [], step: null, range: null }
  const parts = raw.split(',').map((p) => parsePart(p, spec))
  if (parts.some((p) => p === null)) return null
  const ok = parts as { values: number[]; step: number | null; range: [number, number] | null }[]
  const values = [...new Set(ok.flatMap((p) => p.values))].sort((a, b) => a - b)
  // Step and range shapes only matter when the field is written as a single part
  const single = ok.length === 1 ? ok[0] : null
  return { any: false, values, step: single?.step ?? null, range: single?.range ?? null }
}

export type ParsedCron = { minute: CronField; hour: CronField; dom: CronField; month: CronField; dow: CronField }

/** Parses an expression or @-descriptor; null when it is not valid 5-field cron. */
export function parseCron(expr: string): ParsedCron | null {
  const trimmed = expr.trim().toLowerCase()
  const expanded = DESCRIPTORS[trimmed] ?? expr.trim()
  const fields = expanded.split(/\s+/)
  if (fields.length !== 5) return null
  const parsed = fields.map((f, i) => parseField(f, SPECS[i]))
  if (parsed.some((p) => p === null)) return null
  const [minute, hour, dom, month, dowRaw] = parsed as CronField[]
  // Sunday may be written as 7
  const dow = dowRaw.any ? dowRaw : { ...dowRaw, values: [...new Set(dowRaw.values.map((d) => d % 7))].sort((a, b) => a - b) }
  return { minute, hour, dom, month, dow }
}

const pad = (n: number) => String(n).padStart(2, '0')
const hhmm = (h: number, m: number) => `${pad(h)}:${pad(m)}`

/** "a, b and c" */
export function joinWords(items: readonly string[]): string {
  if (items.length <= 1) return items.join('')
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`
}

export function ordinal(n: number): string {
  const rem100 = n % 100
  if (rem100 >= 11 && rem100 <= 13) return `${n}th`
  return `${n}${({ 1: 'st', 2: 'nd', 3: 'rd' } as Record<number, string>)[n % 10] ?? 'th'}`
}

const fixed = (f: CronField) => !f.any && f.step === null && f.range === null

/** Time-of-day phrase, e.g. "at 09:00", "every 15 minutes", "every hour at 30 minutes past". */
function timePhrase(minute: CronField, hour: CronField): string {
  const minuteLabel = (m: number) => (m === 0 ? 'on the hour' : `at ${m} minutes past`)
  if (fixed(minute) && fixed(hour) && minute.values.length * hour.values.length <= 6) {
    const times = hour.values.flatMap((h) => minute.values.map((m) => hhmm(h, m)))
    return `at ${joinWords(times)}`
  }
  if (fixed(minute) && minute.values.length === 1) {
    const m = minute.values[0]
    if (hour.any) return `every hour, ${minuteLabel(m)}`
    if (hour.step !== null && hour.values[0] === 0) return `every ${hour.step} hours, ${minuteLabel(m)}`
    if (hour.range) return `every hour ${minuteLabel(m)}, ${hhmm(hour.range[0], 0)}–${hhmm(hour.range[1], 59)}`
    return `at ${joinWords(hour.values.map((h) => hhmm(h, m)))}`
  }
  const every = minute.any ? 'every minute' : minute.step !== null && minute.values[0] === 0 ? `every ${minute.step} minutes` : `at minutes ${joinWords(minute.values.map(String))}`
  if (hour.any) return every
  if (hour.range) return `${every}, ${hhmm(hour.range[0], 0)}–${hhmm(hour.range[1], 59)}`
  return `${every}, during hours ${joinWords(hour.values.map((h) => pad(h)))}`
}

function weekdayPhrase(dow: CronField): string {
  const v = dow.values.join(',')
  if (v === '1,2,3,4,5') return 'weekdays'
  if (v === '0,6') return 'weekends'
  if (dow.range) return `${DAYS[dow.range[0] % 7]} to ${DAYS[dow.range[1] % 7]}`
  return `every ${joinWords(dow.values.map((d) => DAYS[d]))}`
}

function monthPhrase(month: CronField): string {
  if (month.any) return 'of every month'
  if (month.range) return `of ${MONTHS[month.range[0] - 1]} to ${MONTHS[month.range[1] - 1]}`
  return `of ${joinWords(month.values.map((m) => MONTHS[m - 1]))}`
}

/** Day phrase, e.g. "every day", "weekdays", "on the 1st of every month". */
function dayPhrase(c: ParsedCron): string {
  const months = c.month.any ? '' : ` in ${joinWords(c.month.values.map((m) => MONTHS[m - 1]))}`
  if (c.dom.any && c.dow.any) return c.month.any ? 'every day' : `every day${months}`
  if (c.dom.any) return `${weekdayPhrase(c.dow)}${months}`
  const days = c.dom.step !== null ? `every ${c.dom.step} days` : c.dom.range ? `the ${ordinal(c.dom.range[0])} to ${ordinal(c.dom.range[1])}` : `the ${joinWords(c.dom.values.map(ordinal))}`
  const onDays = `on ${days} ${monthPhrase(c.month)}`
  // Cron runs when either the day of month or the weekday matches
  return c.dow.any ? onDays : `${onDays} or ${weekdayPhrase(c.dow)}`
}

const capitalise = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

/**
 * Describes a cron expression in plain English, e.g. "0 9 * * 1-5" -> "Weekdays at 09:00".
 * Returns null when the expression cannot be parsed.
 */
export function cronToWords(expr: string): string | null {
  const c = parseCron(expr)
  if (!c) return null
  const time = timePhrase(c.minute, c.hour)
  const day = dayPhrase(c)
  if (day === 'every day') return capitalise(time.startsWith('at ') ? `every day ${time}` : time)
  if (time.startsWith('at ')) return capitalise(`${day} ${time}`)
  return capitalise(`${time}, ${day}`)
}

/** Shortest gap between runs within an hour, in minutes (approximate: minute and hour fields only). */
export function minIntervalMinutes(expr: string): number | null {
  const c = parseCron(expr)
  if (!c) return null
  const minutes = c.minute.any ? Array.from({ length: 60 }, (_, i) => i) : c.minute.values
  const hourly = c.hour.any || (c.hour.values.length > 1 && c.hour.values.every((h, i) => i === 0 || h - c.hour.values[i - 1] === 1))
  let gap = Infinity
  for (let i = 1; i < minutes.length; i++) gap = Math.min(gap, minutes[i] - minutes[i - 1])
  if (hourly && minutes.length > 0) gap = Math.min(gap, 60 - minutes[minutes.length - 1] + minutes[0])
  return Number.isFinite(gap) ? gap : null
}

/** Local problem with an expression, or null. The server has the final say. */
export function cronProblem(expr: string): string | null {
  if (!expr.trim()) return 'Enter a cron expression.'
  if (!parseCron(expr)) return 'Use 5 fields (minute hour day month weekday) or @hourly, @daily, @weekly, @monthly.'
  const gap = minIntervalMinutes(expr)
  if (gap !== null && gap < 5) return 'Schedules may not fire more often than every 5 minutes.'
  return null
}
