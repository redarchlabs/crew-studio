import type { FollowupLadder, Watch, WatchInput, WatchRunWhen, WatchSource, WatchSourceKindKey } from '../../../api/types.watch'

/** One step row in the ladder editor. */
export type StepRow = { name: string; offset: string; notify: string; repeat: string }
/** One "skip items whose field is one of these values" rule. */
export type SkipRow = { field: string; values: string }

export type LadderForm = {
  date_field: string
  skip: SkipRow[]
  steps: StepRow[]
  quiet_days: string[]
  timezone: string
  max_per_tick: number | null
}

/** The watch editor's state: every field as the person edits it (lists as text). */
export type WatchForm = {
  kind: WatchSourceKindKey
  connection_id: string
  path: string
  items_path: string
  url: string
  selector: string
  file: string
  format: '' | 'csv' | 'json'
  /** tracker and tracking_list sources */
  project: string
  jql: string
  include_done: boolean
  lead: string
  comments: boolean
  stale_days: number | null
  id_field: string
  tracked_fields: string
  run_when: WatchRunWhen
  dry_run: boolean
  max_items: number | null
  ladder_on: boolean
  ladder: LadderForm
  /** Safety caps on the crew's tracker tools in this schedule's runs (null: the default) */
  mention_cap: number | null
  comment_cap: number | null
}

/** Fields tracker items always have, and the ones tracked when none are listed (the server's lists). */
export const TRACKER_FIELDS = ['title', 'status', 'status_category', 'assignee', 'due_date', 'priority', 'updated_at', 'parent', 'labels']
export const DEFAULT_TRACKER_FIELDS = ['status', 'assignee', 'due_date', 'priority']
const PROJECT_KEY = /^[A-Za-z][A-Za-z0-9_]{0,49}$/

/** Whether a source reads a tracker (items are identified by their key, such as jira:PRJ-15). */
export function isTracker(kind: WatchSourceKindKey): boolean {
  return kind === 'tracker' || kind === 'tracking_list'
}

/** The fields a form tracks: as listed, or for trackers the default set when none are. */
export function effectiveTracked(f: Pick<WatchForm, 'kind' | 'tracked_fields'>): string[] {
  const listed = splitList(f.tracked_fields)
  return isTracker(f.kind) && listed.length === 0 ? [...DEFAULT_TRACKER_FIELDS] : listed
}

export const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const

export const RUN_WHEN: { value: WatchRunWhen; label: string; hint: string }[] = [
  { value: 'changed', label: 'Something changed', hint: 'Skip the run when nothing changed.' },
  { value: 'changed_or_followups', label: 'Changed, or a follow-up is due', hint: 'Also run when a ladder step is due.' },
  { value: 'always', label: 'Every time', hint: 'Always run, and pass the changes along.' },
]

const OFFSET = /^[+-]?\d{1,4}[dw]$/
const REPEAT = /^\d{1,4}[dw]$/
const STEP_NAME = /^[a-z0-9][a-z0-9_-]{0,49}$/

export function emptyStep(): StepRow {
  return { name: '', offset: '', notify: 'owner', repeat: '' }
}

export function defaultLadder(): LadderForm {
  return {
    date_field: '',
    skip: [{ field: '', values: '' }],
    steps: [
      { name: 'due_soon', offset: '-3d', notify: 'owner', repeat: '' },
      { name: 'due_today', offset: '0d', notify: 'owner', repeat: '' },
      { name: 'overdue', offset: '+2d', notify: 'lead', repeat: '7d' },
    ],
    quiet_days: ['Sat', 'Sun'],
    timezone: '',
    max_per_tick: null,
  }
}

export function emptyWatchForm(): WatchForm {
  return {
    kind: 'http_json', connection_id: '', path: '', items_path: '', url: '', selector: '', file: '', format: '',
    project: '', jql: '', include_done: false, lead: '', comments: true, stale_days: null,
    id_field: '', tracked_fields: '', run_when: 'changed', dry_run: true, max_items: null, ladder_on: false, ladder: defaultLadder(),
    mention_cap: null, comment_cap: null,
  }
}

/** Splits "a, b\nc" into trimmed, non-empty, unique entries. */
export function splitList(text: string): string[] {
  const out: string[] = []
  for (const part of text.split(/[,\n]/)) {
    const v = part.trim()
    if (v && !out.includes(v)) out.push(v)
  }
  return out
}

function ladderForm(l: FollowupLadder): LadderForm {
  const skip = Object.entries(l.skip_when ?? {}).map(([field, values]) => ({ field, values: values.join(', ') }))
  return {
    date_field: l.date_field,
    skip: skip.length > 0 ? skip : [{ field: '', values: '' }],
    steps: l.steps.map((s) => ({ name: s.name, offset: s.offset, notify: s.notify, repeat: s.repeat ?? '' })),
    quiet_days: [...(l.quiet_days ?? [])],
    timezone: l.timezone ?? '',
    max_per_tick: l.max_per_tick ?? null,
  }
}

export function toWatchForm(w: Watch): WatchForm {
  const s = w.source
  return {
    kind: s.kind,
    connection_id: w.connection_id ?? '',
    path: s.path ?? '',
    items_path: s.items_path ?? '',
    url: s.url ?? '',
    selector: s.selector ?? '',
    file: s.file ?? '',
    format: s.format ?? '',
    project: s.project ?? '',
    jql: s.jql ?? '',
    include_done: s.include_done ?? false,
    lead: s.lead ?? '',
    comments: s.comments ?? false,
    stale_days: s.stale_days ?? null,
    id_field: s.kind === 'web_page' ? '' : w.id_field,
    tracked_fields: s.kind === 'web_page' ? '' : w.tracked_fields.join(', '),
    run_when: w.run_when,
    dry_run: w.dry_run,
    max_items: w.max_items,
    ladder_on: w.followups !== null,
    ladder: w.followups ? ladderForm(w.followups) : defaultLadder(),
    mention_cap: w.mention_cap_per_day ?? null,
    comment_cap: w.comment_cap_per_run ?? null,
  }
}

function toLadder(l: LadderForm): FollowupLadder {
  const skip_when: Record<string, string[]> = {}
  for (const row of l.skip) {
    const values = splitList(row.values)
    if (row.field.trim() && values.length > 0) skip_when[row.field.trim()] = values
  }
  return {
    date_field: l.date_field.trim(),
    skip_when,
    steps: l.steps.map((s) => ({ name: s.name.trim(), offset: s.offset.trim(), notify: s.notify.trim(), ...(s.repeat.trim() ? { repeat: s.repeat.trim() } : {}) })),
    quiet_days: [...l.quiet_days],
    timezone: l.timezone.trim(),
    ...(l.max_per_tick ? { max_per_tick: l.max_per_tick } : {}),
  }
}

/** Whether the kind's source returns many items with fields (everything except web pages). */
export function hasItems(kind: WatchSourceKindKey): boolean {
  return kind !== 'web_page'
}

/** The format a workspace file is read as: the chosen one, or the file's extension. */
export function fileFormat(f: Pick<WatchForm, 'format' | 'file'>): 'csv' | 'json' {
  if (f.format) return f.format
  return f.file.trim().toLowerCase().endsWith('.csv') ? 'csv' : 'json'
}

function toSource(f: WatchForm): WatchSource {
  switch (f.kind) {
    case 'http_json':
      return { kind: f.kind, path: f.path.trim(), items_path: f.items_path.trim() }
    case 'web_page':
      return { kind: f.kind, url: f.url.trim(), selector: f.selector.trim() }
    case 'tracker':
      return { kind: f.kind, project: f.project.trim(), jql: f.jql.trim(), include_done: f.include_done, lead: f.lead.trim(), comments: f.comments, stale_days: f.stale_days ?? 0 }
    case 'tracking_list':
      return { kind: f.kind, comments: f.comments, stale_days: f.stale_days ?? 0 }
    default:
      return { kind: f.kind, file: f.file.trim(), format: f.format, ...(fileFormat(f) === 'csv' ? {} : { items_path: f.items_path.trim() }) }
  }
}

/** The request body for save and preview: only the fields the chosen kind uses. */
export function toWatchInput(f: WatchForm): WatchInput {
  const items = hasItems(f.kind)
  const usesConnection = f.kind === 'http_json' || f.kind === 'tracker'
  return {
    connection_id: usesConnection && f.connection_id ? f.connection_id : null,
    source: toSource(f),
    id_field: isTracker(f.kind) ? 'key' : items ? f.id_field.trim() : '',
    tracked_fields: items ? splitList(f.tracked_fields) : [],
    run_when: f.run_when,
    followups: items && f.ladder_on ? toLadder(f.ladder) : null,
    dry_run: f.dry_run,
    max_items: f.max_items ?? 0,
    ...(f.mention_cap ? { mention_cap_per_day: f.mention_cap } : {}),
    ...(f.comment_cap ? { comment_cap_per_run: f.comment_cap } : {}),
  }
}

function trackerErrors(f: WatchForm): Record<string, string> {
  const errors: Record<string, string> = {}
  if (f.kind === 'tracker') {
    if (!f.connection_id) errors.connection_id = 'Pick the tracker connection to read.'
    if (!PROJECT_KEY.test(f.project.trim())) errors.project = 'Enter the Jira project key, Linear team key or KM2 project key, such as PRJ.'
  }
  const unknown = splitList(f.tracked_fields).filter((t) => !TRACKER_FIELDS.includes(t))
  if (unknown.length > 0) errors.tracked_fields = `Not tracker fields: ${unknown.join(', ')}.`
  if (f.stale_days !== null && (Number.isNaN(f.stale_days) || f.stale_days < 1 || f.stale_days > 365)) errors.stale_days = 'Between 1 and 365 days.'
  return errors
}

function capErrors(f: WatchForm): Record<string, string> {
  const errors: Record<string, string> = {}
  if (f.mention_cap !== null && (Number.isNaN(f.mention_cap) || f.mention_cap < 1 || f.mention_cap > 50)) errors.mention_cap = 'Between 1 and 50.'
  if (f.comment_cap !== null && (Number.isNaN(f.comment_cap) || f.comment_cap < 1 || f.comment_cap > 500)) errors.comment_cap = 'Between 1 and 500.'
  return errors
}

function ladderErrors(l: LadderForm, tracked: string[]): Record<string, string> {
  const errors: Record<string, string> = {}
  if (!l.date_field.trim()) errors.date_field = 'Pick the date field each step counts from.'
  else if (!tracked.includes(l.date_field.trim())) errors.date_field = 'The date field must be one of the tracked fields.'
  if (l.steps.length === 0) errors.steps = 'Add at least one step.'
  const names = new Set<string>()
  l.steps.forEach((s, i) => {
    const name = s.name.trim()
    if (!STEP_NAME.test(name)) errors[`step:${i}`] = 'Name: lowercase letters, digits, _ or -, such as due_soon.'
    else if (names.has(name)) errors[`step:${i}`] = `The name ${name} is used twice.`
    else if (!OFFSET.test(s.offset.trim())) errors[`step:${i}`] = 'Offset: days or weeks, such as -3d, 0d, +2d or 1w.'
    else if (!s.notify.trim()) errors[`step:${i}`] = 'Say who to notify, such as owner or lead.'
    else if (s.repeat.trim() && (!REPEAT.test(s.repeat.trim()) || /^0+[dw]$/.test(s.repeat.trim()))) errors[`step:${i}`] = 'Repeat: such as 7d or 1w.'
    names.add(name)
  })
  if (l.quiet_days.length === 7) errors.quiet_days = 'Leave at least one working day.'
  const badSkip = l.skip.find((r) => r.field.trim() && !tracked.includes(r.field.trim()))
  if (badSkip) errors.skip = `Skip rules use tracked fields; ${badSkip.field.trim()} is not one.`
  return errors
}

/** Local checks before saving or previewing; the server checks everything again. */
export function validateWatchForm(f: WatchForm): Record<string, string> {
  const errors: Record<string, string> = {}
  if (f.kind === 'http_json') {
    if (!f.connection_id) errors.connection_id = 'Pick the HTTP connection to read through.'
    if (!f.path.trim().startsWith('/') || f.path.trim().startsWith('//')) errors.path = 'Start the path with a single /, such as /issues?state=open.'
  }
  if (f.kind === 'web_page' && !/^https?:\/\/\S+$/.test(f.url.trim())) errors.url = 'Enter the full http(s) address of the page.'
  if (f.kind === 'workspace_file' && !/^watch\/\S/.test(f.file.trim())) errors.file = "Enter a file in the workspace's watch/ folder, such as watch/courses.csv."
  if (isTracker(f.kind)) Object.assign(errors, trackerErrors(f))
  else if (hasItems(f.kind)) {
    if (!f.id_field.trim()) errors.id_field = 'Name the field that identifies each item.'
    if (splitList(f.tracked_fields).length === 0) errors.tracked_fields = 'List at least one field to compare.'
  }
  if (hasItems(f.kind) && f.ladder_on) {
    for (const [k, v] of Object.entries(ladderErrors(f.ladder, effectiveTracked(f)))) errors[`ladder:${k}`] = v
  }
  Object.assign(errors, capErrors(f))
  if (f.max_items !== null && (Number.isNaN(f.max_items) || f.max_items < 1 || f.max_items > 10000)) errors.max_items = 'Between 1 and 10,000.'
  return errors
}
