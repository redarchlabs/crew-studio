import type { Journal, JournalEntry } from '../../../api/types'
import { CREW_LANE, delegationTarget } from '../../runs/tabs/journal/journalModel'

// Lays journal entries out as a swimlane (sequence) diagram: one lane per agent plus a Crew lane, one row per
// entry with time flowing downward, horizontal bands for task boundaries and arrows for delegations.

export type SeqLane = { role: string; label: string; index: number }

export type SeqItem = {
  entry: JournalEntry
  lane: number
  row: number
  /** Delegations: the lane the work was handed to */
  toLane: number | null
}

export type SeqBand = { task: string; startRow: number; endRow: number; status: 'pending' | 'running' | 'done' | 'failed' | null }

export type SequenceLayout = { lanes: SeqLane[]; items: SeqItem[]; bands: SeqBand[]; rows: number }

/** Lane order: Crew first, then agents in the journal's order, then any role seen only in entries. */
export function sequenceLanes(journal: Pick<Journal, 'agents' | 'entries'>): SeqLane[] {
  const roles = [CREW_LANE, ...journal.agents.map((a) => a.role)]
  for (const e of journal.entries) if (!roles.includes(e.agent_role)) roles.push(e.agent_role)
  return [...new Set(roles)].map((role, index) => ({ role, label: role || 'Crew', index }))
}

/** Contiguous runs of rows that share a task name (crew-level rows have no band). */
export function taskBands(items: readonly SeqItem[], statusOf: (task: string) => SeqBand['status']): SeqBand[] {
  const bands: SeqBand[] = []
  for (const it of items) {
    const task = it.entry.task_name
    const last = bands[bands.length - 1]
    if (last && last.task === task && last.endRow === it.row - 1) {
      bands[bands.length - 1] = { ...last, endRow: it.row }
    } else if (task) {
      bands.push({ task, startRow: it.row, endRow: it.row, status: statusOf(task) })
    }
  }
  return bands
}

/** Builds the full layout; `limit` caps the number of rows drawn (very long runs). */
export function layoutSequence(journal: Pick<Journal, 'agents' | 'entries' | 'tasks'>, entries: readonly JournalEntry[] = journal.entries, limit = Infinity): SequenceLayout {
  const lanes = sequenceLanes(journal)
  const laneOf = new Map(lanes.map((l) => [l.role, l.index]))
  const roles = lanes.map((l) => l.role)
  const shown = entries.slice(0, limit)
  const items = shown.map((entry, row) => {
    const target = entry.kind === 'delegation' ? delegationTarget(entry, roles) : null
    return { entry, lane: laneOf.get(entry.agent_role) ?? 0, row, toLane: target === null ? null : (laneOf.get(target) ?? null) }
  })
  const status = new Map(journal.tasks.map((t) => [t.name, t.status]))
  return { lanes, items, bands: taskBands(items, (t) => status.get(t) ?? null), rows: shown.length }
}

/** Geometry shared by the diagram and its tests. */
export const SEQ = { laneWidth: 196, rowHeight: 34, timeColumn: 64, header: 44 } as const

export function laneCenter(lane: number): number {
  return SEQ.timeColumn + lane * SEQ.laneWidth + SEQ.laneWidth / 2
}

export function rowTop(row: number): number {
  return row * SEQ.rowHeight
}

/** Inset of an entry chip from its lane edges. */
export const CHIP_INSET = 8

/**
 * Horizontal arrow for a delegation, at the middle of its row: from the edge of the entry's chip (on the side
 * facing the target) to the target lane's lifeline.
 */
export function arrowPath(item: Pick<SeqItem, 'lane' | 'toLane' | 'row'>): { x1: number; x2: number; y: number } | null {
  if (item.toLane === null || item.toLane === item.lane) return null
  const half = SEQ.laneWidth / 2 - CHIP_INSET
  const x1 = laneCenter(item.lane) + (item.toLane > item.lane ? half : -half)
  return { x1, x2: laneCenter(item.toLane), y: rowTop(item.row) + SEQ.rowHeight / 2 }
}
