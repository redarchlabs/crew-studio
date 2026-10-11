import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { makeEvent } from '../testUtils'
import { eventCategory } from './payload'
import { TimelineItemRow } from './TimelineItemRow'
import { initialTimeline, timelineReducer } from './timelineReducer'

afterEach(cleanup)

// The run-state API records each notes or action log call as a "state" event
describe('state events in the timeline', () => {
  it('count as tool activity', () => {
    expect(eventCategory('state')).toBe('tool')
    const state = timelineReducer(initialTimeline, { type: 'append', events: [makeEvent(1, 'state', { message: 'Notes: listed 2' })] })
    expect(state.groups.flatMap((g) => g.items).map((i) => i.category)).toEqual(['tool'])
  })

  it('show the agent and the message, with the detail on expand', () => {
    const event = makeEvent(1, 'state', { agent_role: 'PMO Coordinator', message: 'Notes: read weekly-report v12', detail: 'Subject: project:atlas' })
    render(<ol><TimelineItemRow item={{ seq: 1, event, result: null, category: 'tool', agents: [], task: null }} /></ol>)
    expect(screen.getByText('PMO Coordinator')).toBeTruthy()
    expect(screen.getByText(/Notes: read weekly-report v12/)).toBeTruthy()
    fireEvent.click(screen.getByText(/Notes: read weekly-report v12/))
    expect(screen.getByText('Subject: project:atlas')).toBeTruthy()
  })

  it('fall back to Crew without an agent and have no detail without one', () => {
    const event = makeEvent(1, 'state', { message: 'Log: recorded report:weekly for project:atlas' })
    render(<ol><TimelineItemRow item={{ seq: 1, event, result: null, category: 'tool', agents: [], task: null }} /></ol>)
    expect(screen.getByText('Crew')).toBeTruthy()
  })
})
