import { describe, expect, it } from 'vitest'
import { makeAgent } from '../../lib/testFixtures'
import { newAgentInput, toAgentInput, validateAgent } from './agentForm'

describe('agent form areas', () => {
  it('starts new agents in the given areas', () => {
    expect(newAgentInput(['fin']).area_ids).toEqual(['fin'])
    expect(newAgentInput([]).area_ids).toEqual([])
  })

  it('requires at least one area', () => {
    const base = { ...newAgentInput([]), name: 'A', role: 'R', goal: 'G' }
    expect(validateAgent(base).area_ids).toMatch(/at least one/)
    expect(validateAgent({ ...base, area_ids: ['fin'] }).area_ids).toBeUndefined()
  })

  it('copies areas from an existing agent', () => {
    expect(toAgentInput(makeAgent({ id: 'a', area_ids: ['hr', 'it'] })).area_ids).toEqual(['hr', 'it'])
  })
})
