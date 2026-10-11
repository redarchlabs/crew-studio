import { describe, expect, it } from 'vitest'
import { areaError, areaFilterOptions, filterByArea, knownAreaId } from './areaFilter'

const items = [
  { id: 'a', area_ids: ['fin'] },
  { id: 'b', area_ids: ['fin', 'hr'] },
  { id: 'c', area_ids: ['it'] },
]

describe('filterByArea', () => {
  it('returns every item for "All areas"', () => {
    expect(filterByArea(items, '').map((i) => i.id)).toEqual(['a', 'b', 'c'])
  })

  it('keeps items that belong to the area, including multi-area items', () => {
    expect(filterByArea(items, 'fin').map((i) => i.id)).toEqual(['a', 'b'])
    expect(filterByArea(items, 'hr').map((i) => i.id)).toEqual(['b'])
  })

  it('returns nothing for an area no item uses', () => {
    expect(filterByArea(items, 'legal')).toEqual([])
  })

  it('does not mutate the input', () => {
    const copy = [...items]
    filterByArea(items, '')
    expect(items).toEqual(copy)
  })
})

describe('areaFilterOptions', () => {
  it('puts "All areas" first and sorts areas by name', () => {
    expect(areaFilterOptions([{ id: '2', name: 'IT' }, { id: '1', name: 'Finance' }])).toEqual([
      { value: '', label: 'All areas' },
      { value: '1', label: 'Finance' },
      { value: '2', label: 'IT' },
    ])
  })
})

describe('knownAreaId', () => {
  const areas = [{ id: 'fin' }]
  it('keeps a known id and drops an unknown one once areas are loaded', () => {
    expect(knownAreaId('fin', areas, true)).toBe('fin')
    expect(knownAreaId('gone', areas, true)).toBe('')
  })
  it('keeps the id while areas are still loading', () => {
    expect(knownAreaId('gone', [], false)).toBe('gone')
  })
  it('treats null as all areas', () => {
    expect(knownAreaId(null, areas, true)).toBe('')
  })
})

describe('areaError', () => {
  it('requires at least one area', () => {
    expect(areaError([])).toMatch(/at least one/)
    expect(areaError(['fin'])).toBeUndefined()
  })
})
