import { describe, expect, it } from 'vitest'
import { makeCrew } from '../../lib/testFixtures'
import { inputProblems, moveItem, newCrewBody, normalizeCrew, toCrewBody, validateCrew } from './crewForm'

const base = { ...newCrewBody(['a']), name: 'Crew' }

describe('crewForm', () => {
  it('copies default areas into a new body', () => {
    const areas = ['a']
    const body = newCrewBody(areas)
    expect(body.area_ids).toEqual(['a'])
    expect(body.area_ids).not.toBe(areas)
  })

  it('toCrewBody tolerates missing area_ids', () => {
    const crew = { ...makeCrew(), area_ids: undefined } as unknown as Parameters<typeof toCrewBody>[0]
    expect(toCrewBody(crew).area_ids).toEqual([])
  })

  it('validates name, budget, manager and inputs', () => {
    expect(validateCrew({ ...base, name: '   ' }).name).toBe('Name is required')
    expect(validateCrew({ ...base, budget_usd: 0 }).budget_usd).toMatch(/greater than 0/)
    expect(validateCrew({ ...base, budget_usd: -1 }).budget_usd).toBeDefined()
    expect(validateCrew({ ...base, budget_usd: Number.NaN }).budget_usd).toBeDefined()
    expect(validateCrew({ ...base, process: 'hierarchical' }).manager_llm_id).toMatch(/manager LLM/)
    expect(validateCrew({ ...base, process: 'hierarchical', manager_llm_id: 'l1' })).toEqual({})
    expect(validateCrew({ ...base, process: 'sequential' })).toEqual({})
    const errs = validateCrew({ ...base, inputs: [{ key: 'Bad', label: '', default: '', required: true }] })
    expect(errs.inputs).toMatch(/Input 1/)
  })

  it('reports invalid and duplicate input keys', () => {
    const mk = (key: string) => ({ key, label: '', default: '', required: false })
    expect(inputProblems([])).toEqual([])
    expect(inputProblems([mk('ok'), mk('ok_2')])).toEqual([])
    expect(inputProblems([mk('')])).toHaveLength(1)
    expect(inputProblems([mk('1a')])).toHaveLength(1)
    expect(inputProblems([mk('a-b')])).toHaveLength(1)
    const dup = inputProblems([mk('a'), mk('a')])
    expect(dup).toEqual(['Input 2: duplicate key "a"'])
  })

  it('normalizes: trims, drops manager for sequential, keeps for hierarchical', () => {
    const inputs = [{ key: ' k ', label: ' L ', default: ' d ', required: true }]
    const seq = normalizeCrew({ ...base, name: '  X  ', manager_llm_id: 'l1', inputs })
    expect(seq.name).toBe('X')
    expect(seq.manager_llm_id).toBeNull()
    expect(seq.inputs).toEqual([{ key: 'k', label: 'L', default: ' d ', required: true }])
    expect(normalizeCrew({ ...base, process: 'hierarchical', manager_llm_id: 'l1' }).manager_llm_id).toBe('l1')
  })

  it('moveItem moves, clamps and never mutates', () => {
    const src = ['a', 'b', 'c']
    expect(moveItem(src, 1, -1)).toEqual(['b', 'a', 'c'])
    expect(moveItem(src, 1, 1)).toEqual(['a', 'c', 'b'])
    expect(moveItem(src, 0, -1)).toEqual(src)
    expect(moveItem(src, 2, 1)).toEqual(src)
    expect(moveItem(src, 0, -1)).not.toBe(src)
    expect(src).toEqual(['a', 'b', 'c'])
    expect(moveItem([], 0, 1)).toEqual([])
  })
})
