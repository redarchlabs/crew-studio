import { describe, expect, it } from 'vitest'
import { accessSummary, limitableAreas, limitsProblems, OPEN_LIMITS, readOnly, scopeOfMe, scopeOfUser, toLimits, type KeyScope, type LimitsDraft } from './apiKeyLimits'
import { makeMe, makeUser } from './testUtils'

const scope: KeyScope = { permissions: ['crews.view', 'crews.edit', 'runs.view'], allAreas: false, areaIds: ['a-1', 'a-2'] }

describe('key scopes', () => {
  it('come from the signed-in person or from an account', () => {
    expect(scopeOfMe(makeMe({ permissions: ['crews.view'], all_areas: true, area_ids: [] }))).toEqual({ permissions: ['crews.view'], allAreas: true, areaIds: [] })
    expect(scopeOfUser(makeUser({ effective_permissions: ['runs.view'], area_ids: ['a-1'] }))).toEqual({ permissions: ['runs.view'], allAreas: false, areaIds: ['a-1'] })
  })
})

describe('limits', () => {
  it('start open: both channels, everything the account holds', () => {
    expect(toLimits(OPEN_LIMITS, scope)).toEqual({ allow_api: true, allow_mcp: true, permissions: null, area_ids: null })
    expect(limitsProblems(OPEN_LIMITS)).toEqual({})
  })

  it('send only the chosen permissions and areas the account holds', () => {
    const d: LimitsDraft = { ...OPEN_LIMITS, mcp: false, limitPermissions: true, permissions: ['crews.view', 'admin.users'], limitAreas: true, areaIds: ['a-2', 'a-9'] }
    expect(toLimits(d, scope)).toEqual({ allow_api: true, allow_mcp: false, permissions: ['crews.view'], area_ids: ['a-2'] })
    // An all-areas account may limit a key to any area
    expect(toLimits({ ...OPEN_LIMITS, limitAreas: true, areaIds: ['a-9'] }, { ...scope, allAreas: true }).area_ids).toEqual(['a-9'])
  })

  it('keep choices while a limit is off', () => {
    expect(toLimits({ ...OPEN_LIMITS, permissions: ['crews.view'], areaIds: ['a-1'] }, scope)).toMatchObject({ permissions: null, area_ids: null })
  })

  it('need a channel, and at least one permission or area when limited', () => {
    expect(limitsProblems({ ...OPEN_LIMITS, api: false, mcp: false, limitPermissions: true, limitAreas: true })).toEqual({
      channels: expect.stringMatching(/REST API, MCP or both/),
      permissions: 'Choose at least one permission',
      areas: 'Choose at least one area',
    })
  })

  it('offer a read-only preset and the areas the account is in', () => {
    expect(readOnly(scope)).toEqual(['crews.view', 'runs.view'])
    const areas = [{ id: 'a-1' }, { id: 'a-3' }]
    expect(limitableAreas(areas, scope)).toEqual([{ id: 'a-1' }])
    expect(limitableAreas(areas, { ...scope, allAreas: true })).toEqual(areas)
  })
})

describe('accessSummary', () => {
  const name = (id: string) => ({ 'a-1': 'IT' })[id] ?? 'Unknown area'
  it('says where a key works and what it keeps', () => {
    expect(accessSummary({ allow_api: true, allow_mcp: true, permissions: null, area_ids: null }, name)).toMatchObject({
      channels: 'API and MCP', permissions: 'All permissions', areas: 'All areas',
    })
    expect(accessSummary({ allow_api: true, allow_mcp: false, permissions: ['crews.view'], area_ids: ['a-1', 'a-2'] }, name)).toMatchObject({
      channels: 'API only', permissions: '1 permission', permissionsTitle: 'crews.view', areas: 'IT, Unknown area', areasTitle: undefined,
    })
    expect(accessSummary({ allow_api: false, allow_mcp: true, permissions: ['crews.view', 'runs.view'], area_ids: null }, name)).toMatchObject({
      channels: 'MCP only', permissions: '2 permissions',
    })
  })
})
