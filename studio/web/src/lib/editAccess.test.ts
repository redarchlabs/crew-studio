import { describe, expect, it } from 'vitest'
import type { Permission } from '../api/types'
import { editAccess } from './editAccess'

const holding = (...perms: Permission[]) => (p: Permission) => perms.includes(p)

describe('editAccess', () => {
  it('needs create for a new item and never offers delete', () => {
    expect(editAccess('agents', true, holding('agents.create', 'agents.delete'))).toEqual({ canSave: true, canDelete: false, missing: null })
    expect(editAccess('agents', true, holding('agents.edit'))).toEqual({ canSave: false, canDelete: false, missing: 'agents.create' })
  })

  it('needs edit to save an existing item and delete to remove it', () => {
    expect(editAccess('crews', false, holding('crews.edit'))).toEqual({ canSave: true, canDelete: false, missing: null })
    expect(editAccess('crews', false, holding('crews.delete'))).toEqual({ canSave: false, canDelete: true, missing: 'crews.edit' })
  })

  it('reports the missing permission for read-only viewers', () => {
    expect(editAccess('tasks', false, holding('tasks.view')).missing).toBe('tasks.edit')
  })
})
