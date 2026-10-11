import { describe, expect, it } from 'vitest'
import type { Revision } from '../../api/types'
import { entityPath, snapshotName, visibleEntityTypes } from './historyLinks'

function rev(over: Partial<Revision>): Revision {
  return { id: 1, entity_type: 'agent', entity_id: 'id-1', action: 'update', snapshot: {}, actor_id: null, actor_name: '', created_at: '', ...over }
}

describe('entityPath', () => {
  it('links the new entity types to their pages', () => {
    expect(entityPath(rev({ entity_type: 'work_order' }))).toBe('/work-orders/id-1')
    expect(entityPath(rev({ entity_type: 'schedule' }))).toBe('/schedules/id-1')
    expect(entityPath(rev({ entity_type: 'trigger' }))).toBe('/triggers/id-1')
    expect(entityPath(rev({ entity_type: 'role' }))).toBe('/admin/roles/id-1')
    expect(entityPath(rev({ entity_type: 'user' }))).toBe('/admin/users/id-1')
  })

  it('sends service accounts and their keys to the service account screen', () => {
    expect(entityPath(rev({ entity_type: 'user', snapshot: { kind: 'service' } }))).toBe('/admin/service-accounts/id-1')
    expect(entityPath(rev({ entity_type: 'api_key', snapshot: { user_id: 'sa-9' } }))).toBe('/admin/service-accounts/sa-9')
    expect(entityPath(rev({ entity_type: 'api_key' }))).toBeNull()
  })

  it('has no link for deleted items except list pages', () => {
    expect(entityPath(rev({ entity_type: 'crew', action: 'delete' }))).toBeNull()
    expect(entityPath(rev({ entity_type: 'area', action: 'delete' }))).toBe('/admin/areas')
  })
})

describe('visibleEntityTypes', () => {
  it('shows admin types only with admin.audit', () => {
    const plain = visibleEntityTypes(true, false)
    expect(plain).toContain('work_order')
    expect(plain).not.toContain('role')
    expect(visibleEntityTypes(true, true)).toContain('access_settings')
  })

  it('shows only admin types to auditors without history.view', () => {
    expect(visibleEntityTypes(false, true)).toEqual(['area', 'role', 'user', 'api_key', 'access_settings'])
  })
})

describe('snapshotName', () => {
  it('names work orders and api keys helpfully', () => {
    expect(snapshotName(rev({ entity_type: 'work_order', snapshot: { number: 42, title: 'Q3 report' } }))).toBe('WO-42 · Q3 report')
    expect(snapshotName(rev({ entity_type: 'api_key', snapshot: { name: 'CI', prefix: 'ab12' } }))).toBe('CI (csk_ab12_…)')
  })

  it('falls back to a short id', () => {
    expect(snapshotName(rev({ entity_id: 'abcdef123456' }))).toBe('abcdef12')
  })
})
