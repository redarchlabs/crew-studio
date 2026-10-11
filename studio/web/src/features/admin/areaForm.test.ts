import { describe, expect, it } from 'vitest'
import { itemsInArea, usageSummary, validateArea } from './areaForm'

describe('validateArea', () => {
  it('requires a unique name, ignoring case and spaces', () => {
    expect(validateArea({ name: ' ', description: '' }, []).name).toBe('Name is required')
    expect(validateArea({ name: ' finance ', description: '' }, [{ name: 'Finance' }]).name).toMatch(/already/)
    expect(validateArea({ name: 'HR', description: '' }, [{ name: 'Finance' }])).toEqual({})
  })
})

describe('area usage', () => {
  const usage = { crews: 1, agents: 0, tasks: 2, users: 5 }
  it('counts only crews, agents and tasks as blocking deletion', () => {
    expect(itemsInArea(usage)).toBe(3)
    expect(itemsInArea({ crews: 0, agents: 0, tasks: 0, users: 9 })).toBe(0)
    expect(itemsInArea(undefined)).toBe(0)
  })
  it('summarises usage with plurals', () => {
    expect(usageSummary(usage)).toBe('1 crew · 0 agents · 2 tasks · 5 accounts')
  })
})
