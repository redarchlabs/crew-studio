import { describe, expect, it } from 'vitest'
import type { Journal, JournalAgent, JournalEntry } from '../../../api/types'
import { arrowPath, CHIP_INSET, laneCenter, layoutSequence, rowTop, SEQ, sequenceLanes } from './sequenceLayout'

let seq = 0
function entry(over: Partial<JournalEntry>): JournalEntry {
  seq += 1
  return { seq, at: '2026-10-03T10:00:00Z', agent_role: '', task_name: '', kind: 'run', title: '', body: '', model: '', cost_usd: 0, tokens: 0, ...over }
}

function agent(role: string): JournalAgent {
  return { role, tasks: [], entries: 0, llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, first_at: null, last_at: null }
}

const journal: Pick<Journal, 'agents' | 'entries' | 'tasks'> = {
  agents: [agent('Manager'), agent('Analyst'), agent('Writer')],
  tasks: [
    { name: 'research', agent_role: 'Analyst', status: 'done', started_at: null, finished_at: null, output: null },
    { name: 'write', agent_role: 'Writer', status: 'running', started_at: null, finished_at: null, output: null },
  ],
  entries: [
    entry({ kind: 'run', title: 'Run started' }),
    entry({ agent_role: 'Analyst', task_name: 'research', kind: 'task_started' }),
    entry({ agent_role: 'Analyst', task_name: 'research', kind: 'llm_call' }),
    entry({ agent_role: 'Analyst', task_name: 'research', kind: 'delegation', title: 'Delegated to Writer' }),
    entry({ agent_role: 'Writer', task_name: 'write', kind: 'llm_call' }),
    entry({ agent_role: 'Ghost', task_name: 'write', kind: 'tool_call' }),
    entry({ agent_role: '', task_name: '', kind: 'artifacts' }),
  ],
}

describe('sequenceLanes', () => {
  it('puts Crew first, then journal agents, then roles seen only in entries', () => {
    expect(sequenceLanes(journal).map((l) => l.label)).toEqual(['Crew', 'Manager', 'Analyst', 'Writer', 'Ghost'])
  })
})

describe('layoutSequence', () => {
  const layout = layoutSequence(journal)

  it('gives each entry its own row in its agent lane', () => {
    expect(layout.rows).toBe(7)
    expect(layout.items.map((i) => i.lane)).toEqual([0, 2, 2, 2, 3, 4, 0])
    expect(layout.items.map((i) => i.row)).toEqual([0, 1, 2, 3, 4, 5, 6])
  })

  it('resolves delegation arrows to the target lane', () => {
    expect(layout.items[3].toLane).toBe(3)
    expect(layout.items.filter((i) => i.toLane !== null)).toHaveLength(1)
  })

  it('draws task bands over contiguous rows with the task status', () => {
    expect(layout.bands).toEqual([
      { task: 'research', startRow: 1, endRow: 3, status: 'done' },
      { task: 'write', startRow: 4, endRow: 5, status: 'running' },
    ])
  })

  it('caps the number of rows', () => {
    const capped = layoutSequence(journal, journal.entries, 3)
    expect(capped.rows).toBe(3)
    expect(capped.bands).toEqual([{ task: 'research', startRow: 1, endRow: 2, status: 'done' }])
  })

  it('lays out a filtered subset', () => {
    const only = layoutSequence(journal, journal.entries.filter((e) => e.kind === 'llm_call'))
    expect(only.items.map((i) => i.row)).toEqual([0, 1])
    expect(only.bands.map((b) => b.task)).toEqual(['research', 'write'])
  })
})

describe('geometry', () => {
  it('computes lane centres, row tops and arrows', () => {
    expect(laneCenter(0)).toBe(SEQ.timeColumn + SEQ.laneWidth / 2)
    expect(rowTop(2)).toBe(2 * SEQ.rowHeight)
    expect(arrowPath({ lane: 2, toLane: 3, row: 3 })).toEqual({ x1: laneCenter(2) + SEQ.laneWidth / 2 - CHIP_INSET, x2: laneCenter(3), y: rowTop(3) + SEQ.rowHeight / 2 })
    expect(arrowPath({ lane: 3, toLane: 1, row: 0 })?.x1).toBe(laneCenter(3) - SEQ.laneWidth / 2 + CHIP_INSET)
    expect(arrowPath({ lane: 2, toLane: null, row: 0 })).toBeNull()
    expect(arrowPath({ lane: 2, toLane: 2, row: 0 })).toBeNull()
  })
})
