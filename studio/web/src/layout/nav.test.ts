import { describe, expect, it } from 'vitest'
import { PERMISSIONS } from '../api/types'
import { NAV } from './nav'

const items = NAV.flatMap((s) => s.items)

describe('NAV', () => {
  it('has titled, non-empty sections in a stable order', () => {
    expect(NAV.map((s) => s.title)).toEqual(['Console', 'Build', 'Automate', 'Configure', 'Developers', 'Admin', 'Support'])
    for (const s of NAV) expect(s.items.length).toBeGreaterThan(0)
  })

  it('has unique routes, all absolute', () => {
    const tos = items.map((i) => i.to)
    expect(new Set(tos).size).toBe(tos.length)
    for (const to of tos) expect(to.startsWith('/')).toBe(true)
  })

  it('has unique labels and an icon for every item', () => {
    expect(new Set(items.map((i) => i.label)).size).toBe(items.length)
    for (const i of items) expect(i.icon).toBeTruthy()
  })

  it('only references permissions that exist', () => {
    const known = new Set<string>(PERMISSIONS)
    for (const i of items) {
      const perms = i.perm === undefined ? [] : Array.isArray(i.perm) ? i.perm : [i.perm]
      for (const p of perms) expect(known.has(p), `${i.label}: ${p}`).toBe(true)
    }
  })

  it('every item requires a permission except the API explorer and Help', () => {
    expect(items.filter((i) => !i.perm).map((i) => i.to)).toEqual(['/api-docs', '/help'])
  })

  it('only the overview link matches exactly (so it is not always active)', () => {
    expect(items.filter((i) => i.end).map((i) => i.to)).toEqual(['/'])
  })

  it('admin screens need admin.* permissions', () => {
    const admin = NAV.find((s) => s.title === 'Admin')!
    for (const i of admin.items) {
      const perms = Array.isArray(i.perm) ? i.perm : [i.perm]
      expect(perms.every((p) => p?.startsWith('admin.'))).toBe(true)
    }
  })

  it('History and Roles accept either of two permissions', () => {
    expect(items.find((i) => i.to === '/history')!.perm).toEqual(['history.view', 'admin.audit'])
    expect(items.find((i) => i.to === '/admin/roles')!.perm).toEqual(['admin.roles', 'admin.users'])
  })
})
