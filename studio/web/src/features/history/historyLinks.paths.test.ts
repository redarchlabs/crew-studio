import { describe, expect, it } from 'vitest'
import { ENTITY_TYPES, type EntityType, type Revision } from '../../api/types'
import { ENTITY_LABELS, entityPath, snapshotName, visibleEntityTypes } from './historyLinks'

const rev = (entity_type: EntityType, over: Partial<Pick<Revision, 'entity_id' | 'action' | 'snapshot'>> = {}) => ({
  entity_type, entity_id: 'id-1', action: 'update' as const, snapshot: {}, ...over,
})

describe('entityPath', () => {
  it.each<[EntityType, string]>([
    ['agent', '/agents/id-1'], ['task', '/tasks/id-1'], ['crew', '/crews/id-1'], ['llm', '/llms?edit=id-1'], ['pricing', '/pricing'],
    ['settings', '/settings'], ['work_order', '/work-orders/id-1'], ['schedule', '/schedules/id-1'], ['trigger', '/triggers/id-1'],
    ['area', '/admin/areas'], ['role', '/admin/roles/id-1'], ['access_settings', '/admin/access'],
  ])('%s opens %s', (type, path) => {
    expect(entityPath(rev(type))).toBe(path)
  })

  it('opens an API key\'s service account when the snapshot names it, else nothing', () => {
    expect(entityPath(rev('api_key', { snapshot: { user_id: 'svc-1' } }))).toBe('/admin/service-accounts/svc-1')
    expect(entityPath(rev('api_key', { snapshot: {} }))).toBeNull()
    expect(entityPath(rev('api_key', { snapshot: { user_id: '' } }))).toBeNull()
  })

  it('has no page for deleted items, except singletons and lists', () => {
    expect(entityPath(rev('agent', { action: 'delete' }))).toBeNull()
    for (const type of ['pricing', 'settings', 'area', 'access_settings'] as const) {
      expect(entityPath(rev(type, { action: 'delete' }))).not.toBeNull()
    }
  })

  it('tolerates a missing snapshot and an unknown type', () => {
    expect(entityPath({ ...rev('user'), snapshot: undefined as never })).toBe('/admin/users/id-1')
    expect(entityPath(rev('mystery' as never))).toBeNull()
  })
})

describe('snapshotName', () => {
  it('names settings singletons by type', () => {
    expect(snapshotName({ entity_type: 'settings', entity_id: 'x', snapshot: { name: 'ignored' } })).toBe('Settings')
    expect(snapshotName({ entity_type: 'access_settings', entity_id: 'x', snapshot: {} })).toBe('Sign-in settings')
  })

  it('formats work orders with and without a title', () => {
    expect(snapshotName({ entity_type: 'work_order', entity_id: 'x', snapshot: { number: 3, title: 'T' } })).toBe('WO-3 · T')
    expect(snapshotName({ entity_type: 'work_order', entity_id: 'x', snapshot: { number: 3 } })).toBe('WO-3')
  })

  it('formats API keys with their prefix, defaulting the name', () => {
    expect(snapshotName({ entity_type: 'api_key', entity_id: 'x', snapshot: { name: 'CI', prefix: 'abc' } })).toBe('CI (csk_abc_…)')
    expect(snapshotName({ entity_type: 'api_key', entity_id: 'x', snapshot: { prefix: 'abc' } })).toBe('Key (csk_abc_…)')
    expect(snapshotName({ entity_type: 'api_key', entity_id: 'abcdefghij', snapshot: {} })).toBe('abcdefgh')
  })

  it('falls back from name to model to email to a short id', () => {
    expect(snapshotName({ entity_type: 'llm', entity_id: 'x', snapshot: { model: 'gpt' } })).toBe('gpt')
    expect(snapshotName({ entity_type: 'user', entity_id: 'x', snapshot: { email: 'a@b.c' } })).toBe('a@b.c')
    expect(snapshotName({ entity_type: 'agent', entity_id: '1234567890', snapshot: { name: '' } })).toBe('12345678')
    expect(snapshotName({ entity_type: 'agent', entity_id: '1234567890', snapshot: undefined as never })).toBe('12345678')
  })
})

describe('visibleEntityTypes / labels', () => {
  it('splits business and admin types by permission', () => {
    expect(visibleEntityTypes(false, false)).toEqual([])
    expect(visibleEntityTypes(true, false)).toContain('agent')
    expect(visibleEntityTypes(true, false)).not.toContain('role')
    expect(visibleEntityTypes(false, true)).toEqual(['area', 'role', 'user', 'api_key', 'access_settings'])
    expect(visibleEntityTypes(true, true)).toEqual([...ENTITY_TYPES])
  })

  it('labels every entity type', () => {
    for (const t of ENTITY_TYPES) expect(ENTITY_LABELS[t]).toBeTruthy()
  })
})
