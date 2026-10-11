import { describe, expect, it } from 'vitest'
import type { RunEvent, RunEventType } from '../../../api/types'
import {
  CREW_END,
  CREW_START,
  filterGroups,
  initialTimeline,
  orderedGroups,
  timelineReducer,
  type TimelineState,
} from './timelineReducer'

let seq = 0
function ev(type: RunEventType, payload: Record<string, unknown> = {}, at = seq + 1): RunEvent {
  seq = at
  return { seq: at, type, payload, created_at: `2026-01-01T00:00:${String(at % 60).padStart(2, '0')}Z` }
}

function run(events: RunEvent[], state: TimelineState = initialTimeline): TimelineState {
  return timelineReducer(state, { type: 'append', events })
}

function scenario(): RunEvent[] {
  seq = 0
  return [
    ev('run_status', { status: 'running' }),
    ev('crew_started', { crew_name: 'Crew' }),
    ev('task_started', { task_name: 'research', agent_role: 'Researcher' }),
    ev('agent_started', { agent_role: 'Researcher', task_name: 'research' }),
    ev('tool_started', { agent_role: 'Researcher', task_name: 'research', tool_name: 'search', tool_args: { q: 'x' } }),
    ev('log', { line: 'thinking...' }),
    ev('tool_finished', { agent_role: 'Researcher', task_name: 'research', tool_name: 'search', output: 'found' }),
    ev('llm_call', { agent_role: 'Researcher', task_name: 'research', model: 'm1', cost_usd: 0.01, spent_usd: 0.01 }),
    ev('budget', { spent_usd: 0.02, budget_usd: 1, state: 'searches_stopped' }),
    ev('delegation', { from_agent: 'Researcher', to_agent: 'Writer', task_name: 'research', question: 'help?' }),
    ev('task_completed', { task_name: 'research', agent_role: 'Researcher', output: 'done' }),
    ev('task_started', { task_name: 'write', agent_role: 'Writer' }),
    ev('task_failed', { task_name: 'write', agent_role: 'Writer', error: 'boom' }),
    ev('crew_failed', { error: 'boom', budget_stopped: false }),
    ev('run_status', { status: 'failed', error: 'boom' }),
  ]
}

describe('timelineReducer', () => {
  it('groups events by task with crew start and outcome groups', () => {
    const s = run(scenario())
    expect(orderedGroups(s.groups).map((g) => g.key)).toEqual([CREW_START, 'task:research', 'task:write', CREW_END])
    expect(s.groups.find((g) => g.key === 'task:research')?.status).toBe('done')
    expect(s.groups.find((g) => g.key === 'task:write')?.status).toBe('failed')
  })

  it('merges tool_finished into the matching tool_started item', () => {
    const s = run(scenario())
    const research = s.groups.find((g) => g.key === 'task:research')
    const tool = research?.items.find((i) => i.event.type === 'tool_started')
    expect(tool?.result?.payload.output).toBe('found')
    expect(research?.items.some((i) => i.event.type === 'tool_finished')).toBe(false)
  })

  it('attaches task-less budget events to the active task', () => {
    const s = run(scenario())
    const research = s.groups.find((g) => g.key === 'task:research')
    expect(research?.items.some((i) => i.event.type === 'budget')).toBe(true)
  })

  it('tracks spend, budget state, agents, logs and terminal status', () => {
    const s = run(scenario())
    expect(s.spentUsd).toBe(0.02)
    expect(s.budgetUsd).toBe(1)
    expect(s.budgetState).toBe('searches_stopped')
    expect(s.agents).toEqual(['Researcher', 'Writer'])
    expect(s.logs).toEqual([{ seq: 6, line: 'thinking...', at: expect.any(String) }])
    expect(s.runStatus).toBe('failed')
    expect(s.runError).toBe('boom')
    expect(s.lastSeq).toBe(15)
    expect(s.activeTask).toBeNull()
  })

  it('ignores replayed events and sorts out-of-order batches', () => {
    const events = scenario()
    const first = run(events.slice(0, 8))
    const again = run([...events.slice(5, 8)].reverse(), first)
    expect(again).toBe(first)
    const resumed = run([...events.slice(6)].reverse(), first)
    expect(resumed).toEqual(run(events))
  })

  it('does not mutate the previous state', () => {
    const events = scenario()
    const first = run(events.slice(0, 5))
    const snapshot = structuredClone(first)
    run(events.slice(5), first)
    expect(first).toEqual(snapshot)
  })

  it('resets', () => {
    expect(timelineReducer(run(scenario()), { type: 'reset' })).toBe(initialTimeline)
  })
})

describe('filterGroups', () => {
  const state = run(scenario())

  it('hides categories', () => {
    const groups = filterGroups(state.groups, { hidden: new Set(['crew', 'task', 'budget']), agent: null, focus: null })
    expect(groups.map((g) => g.key)).toEqual(['task:research'])
    expect(groups[0].items.map((i) => i.event.type)).toEqual(['agent_started', 'tool_started', 'llm_call', 'delegation'])
  })

  it('filters by agent, including delegation targets', () => {
    const groups = filterGroups(state.groups, { hidden: new Set(), agent: 'Writer', focus: null })
    expect(groups.flatMap((g) => g.items.map((i) => i.event.type))).toEqual(['delegation', 'task_started', 'task_failed'])
  })

  it('filters by graph focus', () => {
    const byTool = filterGroups(state.groups, { hidden: new Set(), agent: null, focus: { kind: 'tool', value: 'search' } })
    expect(byTool.flatMap((g) => g.items.map((i) => i.seq))).toEqual([5])
    const byModel = filterGroups(state.groups, { hidden: new Set(), agent: null, focus: { kind: 'model', value: 'm1' } })
    expect(byModel.flatMap((g) => g.items.map((i) => i.seq))).toEqual([8])
    const byTask = filterGroups(state.groups, { hidden: new Set(), agent: null, focus: { kind: 'task', value: 'write' } })
    expect(byTask.map((g) => g.key)).toEqual(['task:write'])
  })

  it('focuses a tool whichever way its name is spelled', () => {
    seq = 0
    const s = run([
      ev('task_started', { task_name: 'research', agent_role: 'R' }),
      ev('tool_started', { agent_role: 'R', task_name: 'research', tool_name: 'google_search' }),
      ev('search', { agent_role: 'R', task_name: 'research', tool_name: 'Google Search', cost_usd: 0.01 }),
    ])
    const items = filterGroups(s.groups, { hidden: new Set(), agent: null, focus: { kind: 'tool', value: 'Google Search' } })
    expect(items.flatMap((g) => g.items.map((i) => i.seq))).toEqual([2, 3])
  })
})

describe('human input', () => {
  it('counts ask_human journal lines only', () => {
    seq = 0
    const s = run([
      ev('state', { tool: 'ask_human', message: 'Asked a human: x' }),
      ev('state', { tool: 'notes_get', message: 'Read a note' }),
      ev('state', { tool: 'ask_human', message: 'Sam answered: y' }),
    ])
    expect(s.humanEvents).toBe(2)
  })
})

describe('spend tally', () => {
  it('accumulates LLM and search spend from events', () => {
    seq = 0
    const s = run([
      ev('llm_call', { model: 'm', cost_usd: 0.001, usage: { prompt_tokens: 300, completion_tokens: 20 } }),
      ev('search', { tool_name: 'Google Search', cost_usd: 0.006, input_tokens: 40, output_tokens: 1500 }),
      ev('llm_call', { model: 'm', cost_usd: 0.002, usage: { prompt_tokens: 900, completion_tokens: 30, cached_prompt_tokens: 300 } }),
    ])
    expect(s.tally).toMatchObject({ llmCalls: 2, searches: 1, promptTokens: 1200, completionTokens: 50, cachedTokens: 300 })
    expect(s.tally.searchUsd).toBeCloseTo(0.006)
  })
})
