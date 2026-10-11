import { describe, expect, it } from 'vitest'
import { barPath, columnPath, foldPeople, GAP, shortDate, sparkPoints, stack, timelineColumns } from './geometry'

describe('chart geometry', () => {
  it('draws bars and columns with a rounded data end, and nothing for empty marks', () => {
    expect(barPath(0, 0, 0, 18, true)).toBe('')
    expect(barPath(10, 12, 0, 18, true)).toBe('M10 0h2v18h-2z')
    expect(barPath(0, 50, 6, 18, false)).toBe('M0 6h50v18h-50z')
    expect(barPath(0, 50, 0, 18, true)).toBe('M0 0h46a4 4 0 0 1 4 4v10a4 4 0 0 1 -4 4h-46z')
    expect(columnPath(0, 100, 20, 0)).toBe('')
    expect(columnPath(0, 100, 20, 2)).toBe('M0 100v-2h20v2z')
    expect(columnPath(5, 100, 20, 40)).toBe('M5 100v-36a4 4 0 0 1 4 -4h12a4 4 0 0 1 4 4v36z')
  })

  it('stacks segments with a gap between them, rounding only the last, and skips zeros', () => {
    const segs = stack([{ key: 'a', label: 'A', value: 1 }, { key: 'z', label: 'Z', value: 0 }, { key: 'b', label: 'B', value: 3 }], 4, 100, 10)
    expect(segs).toEqual([
      { key: 'a', label: 'A', value: 1, x0: 10, x1: 35 - GAP, round: false },
      { key: 'b', label: 'B', value: 3, x0: 35, x1: 110, round: true },
    ])
    expect(stack([{ key: 'a', label: 'A', value: 1 }], 0, 100)[0].x1).toBe(0)
  })

  it('keeps busy people and folds the tail into Other', () => {
    const people = Array.from({ length: 10 }, (_, i) => ({ name: `P${i}`, open: 1, overdue: 1, due_7: i }))
    expect(foldPeople([{ name: 'Idle', open: 3, overdue: 0, due_7: 0 }])).toEqual([])
    const folded = foldPeople(people)
    expect(folded).toHaveLength(8)
    expect(folded[7]).toEqual({ name: 'Other', open: 3, overdue: 3, due_7: 7 + 8 + 9 })
    expect(foldPeople(people.slice(0, 3))).toHaveLength(3)
  })

  it('scales timeline columns to the busiest day and marks it and today', () => {
    const { columns, max } = timelineColumns([{ date: '2026-10-06', count: 1 }, { date: '2026-10-07', count: 4 }, { date: '2026-10-08', count: 4 }], 330, 100, 30)
    expect(max).toBe(4)
    expect(columns.map((c) => [c.h, c.peak, c.today])).toEqual([[25, false, true], [100, true, false], [100, false, false]])
    expect(columns[0].w).toBe(24)
    expect(columns[1].x).toBe(30 + 100 + 38)
    const empty = timelineColumns([{ date: '2026-10-06', count: 0 }], 100, 50, 0)
    expect(empty.max).toBe(1)
    expect(empty.columns[0].peak).toBe(false)
    expect(timelineColumns([], 100, 50, 0).columns).toEqual([])
  })

  it('places sparkline points and needs two of them', () => {
    expect(sparkPoints([3], 100, 20)).toBeNull()
    expect(sparkPoints([0, 2], 108, 24)).toEqual({ points: '4,20 104,4', last: { x: 104, y: 4 } })
    expect(sparkPoints([0, 0], 108, 24)?.points).toBe('4,20 104,20')
  })

  it('writes calendar dates without shifting them by a time zone', () => {
    expect(shortDate('2026-10-12')).toBe('12 Oct')
    expect(shortDate('soon')).toBe('soon')
  })
})
