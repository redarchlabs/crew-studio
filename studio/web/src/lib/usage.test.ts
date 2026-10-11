import { describe, expect, it } from 'vitest'
import { agentLabel, crewsUsingAgent, crewsUsingTask, inputKeysOf, tasksUsingAgent } from './usage'
import { makeAgent, makeCrew, makeTask } from './testFixtures'

describe('usage helpers', () => {
  const tasks = [makeTask({ id: 't1', agent_id: 'a1' }), makeTask({ id: 't2', agent_id: 'a2' }), makeTask({ id: 't3', agent_id: null })]
  const crews = [
    makeCrew({ id: 'c1', agent_ids: ['a1'], task_ids: [] }),
    makeCrew({ id: 'c2', agent_ids: [], task_ids: ['t1'] }),
    makeCrew({ id: 'c3', agent_ids: ['a2'], task_ids: ['t2', 't3'] }),
    makeCrew({ id: 'c4' }),
  ]

  it('crewsUsingAgent finds direct membership and membership via a task', () => {
    expect(crewsUsingAgent('a1', crews, tasks).map((c) => c.id)).toEqual(['c1', 'c2'])
    expect(crewsUsingAgent('a2', crews, tasks).map((c) => c.id)).toEqual(['c3'])
    expect(crewsUsingAgent('nobody', crews, tasks)).toEqual([])
  })
  it('crewsUsingAgent ignores task ids missing from the task list', () => {
    expect(crewsUsingAgent('a1', [makeCrew({ id: 'cx', task_ids: ['ghost'] })], tasks)).toEqual([])
  })
  it('tasksUsingAgent / crewsUsingTask filter correctly', () => {
    expect(tasksUsingAgent('a1', tasks).map((t) => t.id)).toEqual(['t1'])
    expect(crewsUsingTask('t3', crews).map((c) => c.id)).toEqual(['c3'])
    expect(crewsUsingTask('zzz', crews)).toEqual([])
  })
  it('inputKeysOf dedupes keys across crews', () => {
    const mk = (key: string) => ({ key, label: key, default: '', required: false }) as never
    const cs = [makeCrew({ id: 'a', inputs: [mk('topic'), mk('tone')] }), makeCrew({ id: 'b', inputs: [mk('topic')] })]
    expect(inputKeysOf(cs)).toEqual(['topic', 'tone'])
    expect(inputKeysOf([])).toEqual([])
  })
  it('agentLabel falls back to Unassigned', () => {
    expect(agentLabel(makeAgent({ id: 'a1', name: 'Writer' }))).toBe('Writer')
    expect(agentLabel(undefined)).toBe('Unassigned')
  })
})
