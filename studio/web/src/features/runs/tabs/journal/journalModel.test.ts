import { describe, expect, it } from 'vitest'
import type { JournalEntry } from '../../../../api/types'
import { avatarClass, colorSlot, delegationTarget, filterEntries, groupByTask, initials, NO_JOURNAL_FILTER } from './journalModel'

let seq = 0
function entry(over: Partial<JournalEntry>): JournalEntry {
  seq += 1
  return { seq, at: '2026-10-03T10:00:00Z', agent_role: 'Analyst', task_name: 'research', kind: 'llm_call', title: 'Called model', body: '', model: '', cost_usd: 0, tokens: 0, ...over }
}

const entries = [
  entry({ agent_role: '', task_name: '', kind: 'run', title: 'Run started' }),
  entry({ kind: 'task_started', title: 'Started research' }),
  entry({ kind: 'llm_call', cost_usd: 0.01, tokens: 100 }),
  entry({ kind: 'tool_call', title: 'Called Google Search', body: 'query: acme' }),
  entry({ kind: 'delegation', title: 'Delegated to Writer', agent_role: 'Analyst' }),
  entry({ agent_role: 'Writer', task_name: 'write', kind: 'llm_call', cost_usd: 0.02, tokens: 50 }),
  entry({ agent_role: 'Analyst', task_name: 'research', kind: 'llm_call', cost_usd: 0.03, tokens: 10 }),
]

describe('groupByTask', () => {
  it('groups consecutive entries and keeps re-entered tasks separate', () => {
    const sections = groupByTask(entries)
    expect(sections.map((s) => [s.task, s.entries.length])).toEqual([
      ['', 1],
      ['research', 4],
      ['write', 1],
      ['research', 1],
    ])
    expect(new Set(sections.map((s) => s.key)).size).toBe(4)
    expect(sections[1].cost).toBeCloseTo(0.01)
    expect(sections[1].tokens).toBe(100)
  })

  it('returns nothing for no entries', () => {
    expect(groupByTask([])).toEqual([])
  })
})

describe('filterEntries', () => {
  it('filters by agent, including delegations that name the agent', () => {
    const r = filterEntries(entries, { ...NO_JOURNAL_FILTER, agent: 'Writer' })
    expect(r.map((e) => e.kind)).toEqual(['delegation', 'llm_call'])
  })

  it('filters by task, hidden kinds and text query', () => {
    expect(filterEntries(entries, { ...NO_JOURNAL_FILTER, task: 'write' })).toHaveLength(1)
    expect(filterEntries(entries, { ...NO_JOURNAL_FILTER, hidden: new Set(['llm_call']) }).some((e) => e.kind === 'llm_call')).toBe(false)
    expect(filterEntries(entries, { ...NO_JOURNAL_FILTER, query: 'ACME' }).map((e) => e.kind)).toEqual(['tool_call'])
  })

  it('returns everything with no filter', () => {
    expect(filterEntries(entries, NO_JOURNAL_FILTER)).toHaveLength(entries.length)
  })
})

describe('labels', () => {
  it('builds initials', () => {
    expect(initials('Market research analyst')).toBe('MR')
    expect(initials('writer')).toBe('W')
    expect(initials('')).toBe('C')
  })

  it('assigns stable colour slots', () => {
    expect(colorSlot('Writer')).toBe(colorSlot('Writer'))
    expect(colorSlot('Writer')).toBeGreaterThanOrEqual(0)
    expect(colorSlot('Writer')).toBeLessThan(6)
    expect(avatarClass('')).toContain('bg-panel-2')
  })

  it('finds the delegation target by the longest role in the title', () => {
    const roles = ['', 'Analyst', 'Senior Analyst', 'Writer']
    expect(delegationTarget({ title: 'Delegated to Senior Analyst', body: '', agent_role: 'Writer' }, roles)).toBe('Senior Analyst')
    expect(delegationTarget({ title: 'Asked a question', body: '', agent_role: 'Writer' }, roles)).toBeNull()
    expect(delegationTarget({ title: 'Delegated to Writer', body: '', agent_role: 'Writer' }, roles)).toBeNull()
  })
})
