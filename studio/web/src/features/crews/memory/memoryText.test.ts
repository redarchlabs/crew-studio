import { describe, expect, it } from 'vitest'
import { categoriesProblem, memoryOrigin, normalizeScope, plural, scopeProblem, scopeTree, splitCategories } from './memoryText'

describe('memoryText', () => {
  it.each([
    ['', '/'],
    ['/', '/'],
    ['people/sam/', '/people/sam'],
    [' //people//sam ', '/people/sam'],
  ])('normalizeScope(%j) is %s', (raw, want) => expect(normalizeScope(raw)).toBe(want))

  it.each([
    ['/', ''],
    ['/people/sam-lee_2', ''],
    ['/People', 'Use lowercase letters, digits, - and _ between the slashes, such as /people/sam.'],
    ['/a/b/c/d/e/f/g/h/i', 'Use at most 8 levels.'],
    [`/${'a'.repeat(64)}`.repeat(4), 'Use at most 200 characters.'],
    [`/${'a'.repeat(65)}`, 'Use lowercase letters, digits, - and _ between the slashes, such as /people/sam.'],
  ])('scopeProblem(%j)', (scope, want) => expect(scopeProblem(scope)).toBe(want))

  it('splits and checks categories', () => {
    expect(splitCategories(' people, , vendor status ')).toEqual(['people', 'vendor status'])
    expect(categoriesProblem(['people', 'vendor status'])).toBe('')
    expect(categoriesProblem(['People'])).toMatch(/lowercase/)
    expect(categoriesProblem(Array.from({ length: 11 }, (_, i) => `c${i}`))).toBe('Use at most 10 categories.')
  })

  it('tells where a memory came from', () => {
    expect(memoryOrigin({ source: 'run:r1', run_id: 'r1', written_by: 'run' })).toEqual({ kind: 'run', runId: 'r1' })
    expect(memoryOrigin({ source: 'run:r1', run_id: null, written_by: 'run' })).toEqual({ kind: 'deleted-run' })
    expect(memoryOrigin({ source: 'user:Ada', run_id: null, written_by: 'u1' })).toEqual({ kind: 'person', name: 'Ada' })
    expect(memoryOrigin({ source: 'user:', run_id: null, written_by: 'Bo' })).toEqual({ kind: 'person', name: 'Bo' })
    expect(memoryOrigin({ source: '', run_id: null, written_by: '' })).toEqual({ kind: 'person', name: 'someone' })
  })

  it('builds the scope tree with totals, keeping children under their parent', () => {
    const tree = scopeTree([
      { scope: '/projects', count: 1 },
      { scope: '/people-x', count: 2 },
      { scope: '/people/sam', count: 3 },
      { scope: '/', count: 4 },
      { scope: '/people/ada', count: 1 },
      { scope: '/people', count: 1 },
    ])
    expect(tree).toEqual([
      { path: '/people', name: 'people', depth: 0, count: 5 },
      { path: '/people/ada', name: 'ada', depth: 1, count: 1 },
      { path: '/people/sam', name: 'sam', depth: 1, count: 3 },
      { path: '/people-x', name: 'people-x', depth: 0, count: 2 },
      { path: '/projects', name: 'projects', depth: 0, count: 1 },
    ])
  })

  it('pluralizes', () => {
    expect(plural(1, 'memory', 'memories')).toBe('1 memory')
    expect(plural(2, 'memory', 'memories')).toBe('2 memories')
    expect(plural(0, 'run')).toBe('0 runs')
  })
})
