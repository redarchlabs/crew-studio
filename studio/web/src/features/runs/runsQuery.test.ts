import { describe, expect, it } from 'vitest'
import { isFiltered, parseRunsFilter, runsFilterToParams } from './runsQuery'

describe('parseRunsFilter', () => {
  it('reads known values', () => {
    const f = parseRunsFilter(new URLSearchParams('crew_id=c1&status=failed&trigger_type=webhook&kind=chat&offset=50'))
    expect(f).toEqual({ crew_id: 'c1', status: 'failed', trigger_type: 'webhook', kind: 'chat', offset: 50 })
    expect(parseRunsFilter(new URLSearchParams('kind=handoff')).kind).toBe('handoff')
  })

  it('ignores unknown statuses, trigger types and bad offsets', () => {
    const f = parseRunsFilter(new URLSearchParams('status=exploded&trigger_type=telepathy&kind=dream&offset=-4'))
    expect(f).toEqual({ crew_id: '', status: '', trigger_type: '', kind: '', offset: 0 })
  })
})

describe('runsFilterToParams', () => {
  it('omits empty values and round-trips', () => {
    const f = { crew_id: '', status: 'running' as const, trigger_type: '' as const, kind: 'crew' as const, offset: 0 }
    const qs = runsFilterToParams(f)
    expect(qs.toString()).toBe('status=running&kind=crew')
    expect(parseRunsFilter(qs)).toEqual(f)
  })
})

describe('isFiltered', () => {
  it('is false only when nothing narrows the list', () => {
    expect(isFiltered({ crew_id: '', status: '', trigger_type: '', kind: '', offset: 25 })).toBe(false)
    expect(isFiltered({ crew_id: '', status: '', trigger_type: 'api', kind: '', offset: 0 })).toBe(true)
    expect(isFiltered({ crew_id: '', status: '', trigger_type: '', kind: 'chat', offset: 0 })).toBe(true)
  })
})
