import { describe, expect, it } from 'vitest'
import type { Crew } from '../../../api/types'
import { makeCrew } from '../../../lib/testFixtures'
import { deriveCrewInputs, deriveTargetInputs, extraKeys, missingRequired, valuesForInputs } from './targetInputs'

describe('deriveCrewInputs / deriveTargetInputs', () => {
  const crew = { id: 'c1', inputs: [{ key: 'company', label: '', default: 'Acme', required: true }, { key: 'depth', label: 'Depth', default: '', required: false }] } as unknown as Crew

  it('uses the crew inputs with labels and defaults', () => {
    expect(deriveCrewInputs(crew).inputs).toEqual([
      { key: 'company', label: 'Company', default: 'Acme', required: true },
      { key: 'depth', label: 'Depth', default: '', required: false },
    ])
  })

  it('derives labels from multi-word keys when blank', () => {
    const c = makeCrew({ inputs: [{ key: 'a_b', label: '', default: 'd', required: true }] })
    expect(deriveCrewInputs(c).inputs[0]).toMatchObject({ key: 'a_b', label: 'A b' })
  })

  it('derives from the crew found in the catalog', () => {
    expect(deriveTargetInputs({ crew_id: 'c1' }, { crews: [crew] }).inputs).toHaveLength(2)
  })

  it('returns no inputs for an unknown crew or no crew', () => {
    expect(deriveTargetInputs({ crew_id: 'x' }, { crews: [crew] }).inputs).toEqual([])
    expect(deriveTargetInputs({ crew_id: null }, { crews: [crew] }).inputs).toEqual([])
  })
})

describe('input values', () => {
  const specs = [
    { key: 'a', label: 'Alpha', default: 'da', required: true },
    { key: 'b', label: 'Beta', default: '', required: false },
  ]

  it('keeps typed values, fills defaults and drops unused keys', () => {
    expect(valuesForInputs(specs, { b: 'typed', stale: '1' })).toEqual({ a: 'da', b: 'typed' })
  })

  it('valuesForInputs keeps an explicit empty string rather than the default', () => {
    expect(valuesForInputs(specs, { a: '' })).toEqual({ a: '', b: '' })
    expect(valuesForInputs(specs, {})).toEqual({ a: 'da', b: '' })
    expect(valuesForInputs([], { a: '1' })).toEqual({})
  })

  it('missingRequired treats whitespace as empty and ignores optional inputs', () => {
    expect(missingRequired(specs, { a: '   ', b: '' })).toEqual({ a: 'Alpha is required' })
    expect(missingRequired(specs, {})).toEqual({ a: 'Alpha is required' })
    expect(missingRequired(specs, { a: 'x' })).toEqual({})
  })

  it('extraKeys lists unknown keys only', () => {
    expect(extraKeys(specs, { a: '1', z: '2', y: '' })).toEqual(['z', 'y'])
    expect(extraKeys(specs, {})).toEqual([])
  })
})
