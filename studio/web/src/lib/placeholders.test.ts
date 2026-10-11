import { describe, expect, it } from 'vitest'
import { detectCrewInputs, findPlaceholders, labelFromKey, splitPlaceholders } from './placeholders'
import { makeAgent, makeTask } from './testFixtures'

describe('findPlaceholders', () => {
  it('finds unique keys in order', () => {
    expect(findPlaceholders('Research {company} in {region}, then {company} again')).toEqual(['company', 'region'])
  })

  it('ignores keys that do not match [a-z][a-z0-9_]*', () => {
    expect(findPlaceholders('{Company} {1abc} { spaced } {ok_1} {a-b} {}')).toEqual(['ok_1'])
  })

  it('returns empty for plain text', () => {
    expect(findPlaceholders('no placeholders here')).toEqual([])
  })
})

describe('splitPlaceholders', () => {
  it('splits text into plain and placeholder segments', () => {
    expect(splitPlaceholders('Hi {name}!')).toEqual([
      { text: 'Hi ', placeholder: null },
      { text: '{name}', placeholder: 'name' },
      { text: '!', placeholder: null },
    ])
  })

  it('handles adjacent placeholders and empty text', () => {
    expect(splitPlaceholders('{a}{b}')).toEqual([
      { text: '{a}', placeholder: 'a' },
      { text: '{b}', placeholder: 'b' },
    ])
    expect(splitPlaceholders('')).toEqual([])
  })
})

describe('labelFromKey', () => {
  it('humanises snake_case keys', () => {
    expect(labelFromKey('target_company')).toBe('Target company')
  })
})

describe('detectCrewInputs', () => {
  const agents = [
    makeAgent({ id: 'a1', role: '{industry} analyst', goal: 'Study {company}' }),
    makeAgent({ id: 'a2', backstory: 'Knows {region}' }),
    makeAgent({ id: 'a3', goal: 'Unrelated {unused}' }),
  ]
  const tasks = [
    makeTask({ id: 't1', description: 'Research {company}', expected_output: 'Report on {topic}', agent_id: 'a2' }),
    makeTask({ id: 't2', description: 'Not in crew {other}' }),
  ]

  it('adds missing keys from crew tasks, crew agents and task agents', () => {
    const existing = [{ key: 'company', label: 'Company', default: 'Acme', required: true }]
    const { inputs, added } = detectCrewInputs(existing, ['t1'], ['a1'], tasks, agents)
    expect(added).toEqual(['topic', 'industry', 'region'])
    expect(inputs[0]).toBe(existing[0])
    expect(inputs.map((i) => i.key)).toEqual(['company', 'topic', 'industry', 'region'])
    expect(inputs[1]).toEqual({ key: 'topic', label: 'Topic', default: '', required: true })
  })

  it('does not mutate the existing list', () => {
    const existing = [{ key: 'company', label: 'Company', default: '', required: false }]
    const copy = structuredClone(existing)
    detectCrewInputs(existing, ['t1'], ['a1'], tasks, agents)
    expect(existing).toEqual(copy)
  })

  it('ignores unknown ids', () => {
    expect(detectCrewInputs([], ['missing'], ['nope'], tasks, agents)).toEqual({ inputs: [], added: [] })
  })
})
