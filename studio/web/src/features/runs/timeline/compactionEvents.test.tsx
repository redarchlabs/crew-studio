import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { makeEvent } from '../testUtils'
import { eventCategory } from './payload'
import { TimelineItemRow } from './TimelineItemRow'
import { initialTimeline, timelineReducer } from './timelineReducer'

afterEach(cleanup)

describe('context compactions in the timeline', () => {
  it('counts as LLM activity and is kept', () => {
    expect(eventCategory('context_compacted')).toBe('llm')
    const state = timelineReducer(initialTimeline, { type: 'append', events: [makeEvent(1, 'context_compacted', { agent_role: 'Analyst' })] })
    expect(state.groups.flatMap((g) => g.items).map((i) => i.category)).toEqual(['llm'])
  })

  it('says whose context shrank, by how much and why', () => {
    const event = makeEvent(1, 'context_compacted', { agent_role: 'Analyst', reason: 'auto', tokens_before: 48210, tokens_after: 5900 })
    render(<ol><TimelineItemRow item={{ seq: 1, event, result: null, category: 'llm', agents: [], task: null }} /></ol>)
    expect(screen.getByText('Analyst')).toBeTruthy()
    expect(screen.getByText((48210).toLocaleString())).toBeTruthy()
    expect(screen.getByText((5900).toLocaleString())).toBeTruthy()
    expect(screen.getByText(/over the limit in Settings/)).toBeTruthy()
    cleanup()
    const asked = makeEvent(2, 'context_compacted', { agent_role: 'Analyst', reason: 'asked' })
    render(<ol><TimelineItemRow item={{ seq: 2, event: asked, result: null, category: 'llm', agents: [], task: null }} /></ol>)
    expect(screen.getByText(/\(asked\)/)).toBeTruthy()
  })

  it('says when a compaction failed', () => {
    const event = makeEvent(3, 'context_compacted', { agent_role: 'Analyst', failed: true, error: 'RuntimeError: provider down' })
    render(<ol><TimelineItemRow item={{ seq: 3, event, result: null, category: 'llm', agents: [], task: null }} /></ol>)
    expect(screen.getByText(/Could not compact/)).toBeTruthy()
    expect(screen.getByText(/provider down/)).toBeTruthy()
  })
})
