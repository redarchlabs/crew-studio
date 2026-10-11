import { describe, expect, it } from 'vitest'
import { createPermission } from './editAccess'

describe('createPermission', () => {
  it.each([
    ['agents', 'agents.create'],
    ['tasks', 'tasks.create'],
    ['crews', 'crews.create'],
  ] as const)('%s -> %s', (feature, perm) => expect(createPermission(feature)).toBe(perm))
})
