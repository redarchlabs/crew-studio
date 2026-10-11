import { describe, expect, it } from 'vitest'
import type { Permission, PermissionDef } from '../../api/types'
import { effectivePermissions, groupPermissions, groupState, setGroup, sortByCatalog, summarizeByFeature, togglePermission } from './permissions'

const def = (key: Permission, feature: string, label: string): PermissionDef => ({ key, feature, label, description: '' })

const DEFS: PermissionDef[] = [
  def('crews.view', 'Crews', 'View'),
  def('crews.edit', 'Crews', 'Edit'),
  def('agents.view', 'Agents', 'View'),
  def('crews.run', 'Crews', 'Run'),
  def('admin.users', 'Admin', 'People'),
]
const CATALOG = DEFS.map((d) => d.key)

describe('groupPermissions', () => {
  it('groups by feature in first-seen order and keeps permission order', () => {
    const groups = groupPermissions(DEFS)
    expect(groups.map((g) => g.feature)).toEqual(['Crews', 'Agents', 'Admin'])
    expect(groups[0].defs.map((d) => d.key)).toEqual(['crews.view', 'crews.edit', 'crews.run'])
  })
})

describe('group selection', () => {
  const crews = groupPermissions(DEFS)[0]

  it('reports none / some / all', () => {
    expect(groupState([], crews)).toBe('none')
    expect(groupState(['crews.view'], crews)).toBe('some')
    expect(groupState(['crews.view', 'crews.edit', 'crews.run'], crews)).toBe('all')
  })

  it('selects a whole group without touching others or duplicating', () => {
    expect(setGroup(['agents.view', 'crews.view'], crews, true)).toEqual(['agents.view', 'crews.view', 'crews.edit', 'crews.run'])
  })

  it('clears a whole group', () => {
    expect(setGroup(['agents.view', 'crews.view', 'crews.run'], crews, false)).toEqual(['agents.view'])
  })

  it('does not mutate its input', () => {
    const selected: Permission[] = ['crews.view']
    setGroup(selected, crews, true)
    togglePermission(selected, 'crews.view')
    expect(selected).toEqual(['crews.view'])
  })

  it('toggles a single permission', () => {
    expect(togglePermission(['crews.view'], 'crews.edit')).toEqual(['crews.view', 'crews.edit'])
    expect(togglePermission(['crews.view', 'crews.edit'], 'crews.view')).toEqual(['crews.edit'])
  })
})

describe('effectivePermissions', () => {
  const roles = [
    { id: 'admin', builtin: true, permissions: [] as Permission[] },
    { id: 'ops', builtin: false, permissions: ['crews.run', 'crews.view'] as Permission[] },
  ]

  it('unions role and direct permissions in catalog order without duplicates', () => {
    expect(effectivePermissions(roles, ['ops'], ['agents.view', 'crews.view'], CATALOG)).toEqual(['crews.view', 'agents.view', 'crews.run'])
  })

  it('gives every permission to holders of the built-in role', () => {
    expect(effectivePermissions(roles, ['admin'], [], CATALOG)).toEqual(CATALOG)
  })

  it('ignores roles the person does not hold', () => {
    expect(effectivePermissions(roles, [], ['admin.users'], CATALOG)).toEqual(['admin.users'])
  })
})

describe('sortByCatalog and summarizeByFeature', () => {
  it('puts unknown keys last', () => {
    expect(sortByCatalog(['overview.view', 'crews.edit', 'crews.view'], CATALOG)).toEqual(['crews.view', 'crews.edit', 'overview.view'])
  })

  it('summarises held permissions per feature', () => {
    expect(summarizeByFeature(['crews.view', 'crews.edit', 'crews.run', 'admin.users'], DEFS)).toEqual([
      { feature: 'Crews', labels: ['View', 'Edit', 'Run'], all: true },
      { feature: 'Admin', labels: ['People'], all: true },
    ])
  })
})

describe('fallbackDefs', () => {
  it('derives a feature and label from the key', async () => {
    const { fallbackDefs } = await import('./permissions')
    expect(fallbackDefs(['work_orders.view', 'admin.service_accounts'])).toEqual([
      { key: 'work_orders.view', feature: 'Work orders', label: 'View', description: 'work_orders.view' },
      { key: 'admin.service_accounts', feature: 'Admin', label: 'Service accounts', description: 'admin.service_accounts' },
    ])
  })
})
