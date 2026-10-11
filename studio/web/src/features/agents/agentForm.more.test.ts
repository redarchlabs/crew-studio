import { describe, expect, it } from 'vitest'
import { makeAgent } from '../../lib/testFixtures'
import { EMPTY_AGENT, newAgentInput, toAgentInput, validateAgent } from './agentForm'

const ok = { ...EMPTY_AGENT, name: 'N', role: 'R', goal: 'G', area_ids: ['a'] }

describe('agentForm', () => {
  it('accepts a valid agent', () => expect(validateAgent(ok)).toEqual({}))

  it('requires name, role and goal (whitespace counts as empty)', () => {
    expect(validateAgent({ ...ok, name: ' ', role: '\t', goal: '\n' })).toEqual({ name: 'Name is required', role: 'Role is required', goal: 'Goal is required' })
  })

  it.each([0, -1, 101, 1.5, Number.NaN, Infinity])('rejects max_iter %s', (n) => {
    expect(validateAgent({ ...ok, max_iter: n }).max_iter).toBe('Whole number from 1 to 100')
  })

  it.each([1, 20, 100])('accepts max_iter %s', (n) => {
    expect(validateAgent({ ...ok, max_iter: n }).max_iter).toBeUndefined()
  })

  it('does not share the area array with the caller or the template', () => {
    const src = ['x']
    const a = newAgentInput(src)
    a.area_ids.push('y')
    expect(src).toEqual(['x'])
    expect(EMPTY_AGENT.area_ids).toEqual([])
  })

  it('toAgentInput drops server fields and tolerates missing area_ids', () => {
    const agent = { ...makeAgent({ id: 'z', tools: ['t'] }), area_ids: undefined } as unknown as Parameters<typeof toAgentInput>[0]
    const input = toAgentInput(agent)
    expect(input).not.toHaveProperty('id')
    expect(input).not.toHaveProperty('created_at')
    expect(input.area_ids).toEqual([])
    expect(input.tools).toEqual(['t'])
  })
})
