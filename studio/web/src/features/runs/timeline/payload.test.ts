import { describe, expect, it } from 'vitest'
import { eventAgents, eventCategory, parseRunEvent } from './payload'

describe('parseRunEvent', () => {
  it('parses a well-formed event', () => {
    expect(parseRunEvent('{"seq":3,"type":"log","payload":{"line":"x"},"created_at":"t"}')).toEqual({ seq: 3, type: 'log', payload: { line: 'x' }, created_at: 't' })
  })
  it('rejects malformed data', () => {
    expect(parseRunEvent('not json')).toBeNull()
    expect(parseRunEvent('{"type":"log"}')).toBeNull()
    expect(parseRunEvent('null')).toBeNull()
  })
  it('defaults a missing payload to an empty object', () => {
    expect(parseRunEvent('{"seq":1,"type":"crew_started","created_at":"t"}')?.payload).toEqual({})
  })
})

describe('event helpers', () => {
  it('categorises event types', () => {
    expect(eventCategory('tool_finished')).toBe('tool')
    expect(eventCategory('video')).toBe('tool')
    expect(eventCategory('log')).toBeNull()
  })
  it('lists agents for delegations', () => {
    expect(eventAgents({ seq: 1, type: 'delegation', payload: { from_agent: 'A', to_agent: 'B' }, created_at: '' })).toEqual(['A', 'B'])
  })
})
