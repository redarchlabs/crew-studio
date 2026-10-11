import type { JournalEntry, JournalEntryKind } from '../../../../api/types'

// Pure helpers behind the Journal tab and the sequence diagram: filtering, grouping by task and labels.

export const CREW_LANE = ''

export type JournalFilter = {
  agent: string | null
  task: string | null
  /** Entry kinds hidden from the list */
  hidden: ReadonlySet<JournalEntryKind>
  query: string
}

export const NO_JOURNAL_FILTER: JournalFilter = { agent: null, task: null, hidden: new Set(), query: '' }

/** Kinds offered as filter chips, in a sensible reading order. */
export const KIND_GROUPS: readonly { key: string; label: string; kinds: readonly JournalEntryKind[] }[] = [
  { key: 'llm', label: 'LLM calls', kinds: ['llm_call'] },
  { key: 'tool', label: 'Tools', kinds: ['tool_call', 'tool_result', 'tool_error', 'search', 'video', 'state'] },
  { key: 'delegation', label: 'Delegations', kinds: ['delegation', 'consultation'] },
  { key: 'task', label: 'Tasks', kinds: ['task_started', 'task_completed', 'task_failed', 'agent_started'] },
  { key: 'run', label: 'Run', kinds: ['run', 'budget', 'artifacts', 'compaction'] },
]

export function filterEntries(entries: readonly JournalEntry[], filter: JournalFilter): JournalEntry[] {
  const q = filter.query.trim().toLowerCase()
  return entries.filter(
    (e) =>
      !filter.hidden.has(e.kind) &&
      (filter.agent === null || e.agent_role === filter.agent || (e.kind === 'delegation' && e.title.includes(filter.agent))) &&
      (filter.task === null || e.task_name === filter.task) &&
      (q === '' || `${e.title}\n${e.body}\n${e.agent_role}`.toLowerCase().includes(q)),
  )
}

export type JournalSection = {
  /** Unique within the list (a task can appear in several runs of entries, e.g. hierarchical crews) */
  key: string
  task: string
  entries: JournalEntry[]
  cost: number
  tokens: number
}

/** Groups consecutive entries that share a task; crew-level entries (no task) form their own sections. */
export function groupByTask(entries: readonly JournalEntry[]): JournalSection[] {
  const sections: JournalSection[] = []
  for (const e of entries) {
    const last = sections[sections.length - 1]
    if (last && last.task === e.task_name) {
      sections[sections.length - 1] = { ...last, entries: [...last.entries, e], cost: last.cost + e.cost_usd, tokens: last.tokens + e.tokens }
    } else {
      sections.push({ key: `${sections.length}:${e.task_name}`, task: e.task_name, entries: [e], cost: e.cost_usd, tokens: e.tokens })
    }
  }
  return sections
}

/** "Market Research Analyst" -> "MR"; "" -> "C" (crew). */
export function initials(role: string): string {
  const words = role.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return 'C'
  return words.slice(0, 2).map((w) => w.charAt(0).toUpperCase()).join('')
}

/** Stable colour slot (0-5) for an agent role, so avatars and lanes match across views. */
export function colorSlot(role: string): number {
  let h = 0
  for (let i = 0; i < role.length; i++) h = (h * 31 + role.charCodeAt(i)) >>> 0
  return h % 6
}

/** Tailwind classes per colour slot (tokens from index.css). */
export const SLOT_CLASSES: readonly string[] = [
  'bg-teal-soft text-teal',
  'bg-blue-soft text-blue',
  'bg-violet-soft text-violet',
  'bg-accent-soft text-accent',
  'bg-ok-soft text-ok',
  'bg-danger-soft text-danger',
]

export function avatarClass(role: string): string {
  return role === CREW_LANE ? 'bg-panel-2 text-muted' : SLOT_CLASSES[colorSlot(role)]
}

/**
 * The agent a delegation entry hands work to. The journal title reads "Delegated to <role>", so the longest
 * known role (other than the delegating agent) mentioned in the title wins.
 */
export function delegationTarget(entry: Pick<JournalEntry, 'title' | 'body' | 'agent_role'>, roles: readonly string[]): string | null {
  const candidates = roles.filter((r) => r && r !== entry.agent_role && entry.title.includes(r))
  if (candidates.length === 0) return null
  return candidates.reduce((a, b) => (b.length > a.length ? b : a))
}

export function taskLabel(task: string): string {
  return task || 'Crew'
}
