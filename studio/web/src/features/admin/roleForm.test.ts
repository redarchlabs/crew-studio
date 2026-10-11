import { describe, expect, it } from 'vitest'
import { normalizeRole, roleLockReason, validateRole } from './roleForm'

describe('role form', () => {
  it('requires a name and trims text', () => {
    expect(validateRole({ name: ' ', description: '', permissions: [] }).name).toBeDefined()
    expect(normalizeRole({ name: ' Ops ', description: ' runs things ', permissions: ['crews.run'] })).toEqual({ name: 'Ops', description: 'runs things', permissions: ['crews.run'] })
  })

  it('locks the built-in role for everyone and other roles without admin.roles', () => {
    expect(roleLockReason({ builtin: true }, true)).toBe('builtin')
    expect(roleLockReason({ builtin: false }, false)).toBe('permission')
    expect(roleLockReason(null, false)).toBe('permission')
    expect(roleLockReason({ builtin: false }, true)).toBeNull()
  })
})
