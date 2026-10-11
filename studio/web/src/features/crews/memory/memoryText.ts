// Small helpers shared by the Memory tab: scope paths, categories and where a memory came from.
// The server applies the same rules (design section 19, plan section 2); these give early, friendly errors.

import type { MemoryMode, MemoryRecord, MemoryScopeCount } from '../../../api/types.memory'

export const MAX_CONTENT = 2000
export const MAX_CATEGORIES = 10
const SEGMENT = /^[a-z0-9_-]{1,64}$/
const CATEGORY = /^[a-z0-9 _-]{1,40}$/
const MAX_SEGMENTS = 8
const MAX_SCOPE = 200

export const MODE_LABELS: Record<MemoryMode, string> = {
  off: 'Off',
  recall_only: 'Recall only',
  explicit: 'Explicit saves',
  automatic: 'Automatic',
}

/** "people/sam/" and "//people//sam" both become "/people/sam"; empty becomes "/". */
export function normalizeScope(raw: string): string {
  const parts = raw.trim().split('/').filter(Boolean)
  return `/${parts.join('/')}`
}

/** A problem with a scope path, or '' when it is fine. Expects a normalized path. */
export function scopeProblem(scope: string): string {
  if (scope.length > MAX_SCOPE) return `Use at most ${MAX_SCOPE} characters.`
  const parts = scope.split('/').filter(Boolean)
  if (parts.length > MAX_SEGMENTS) return `Use at most ${MAX_SEGMENTS} levels.`
  if (parts.some((p) => !SEGMENT.test(p))) return 'Use lowercase letters, digits, - and _ between the slashes, such as /people/sam.'
  return ''
}

/** "vendor, People" → ["vendor", "People"]; blanks dropped. */
export function splitCategories(raw: string): string[] {
  return raw.split(',').map((c) => c.trim()).filter(Boolean)
}

export function categoriesProblem(categories: readonly string[]): string {
  if (categories.length > MAX_CATEGORIES) return `Use at most ${MAX_CATEGORIES} categories.`
  if (categories.some((c) => !CATEGORY.test(c))) return 'Categories use lowercase letters, digits, spaces, - and _ (up to 40 each).'
  return ''
}

export type MemoryOrigin = { kind: 'run'; runId: string } | { kind: 'deleted-run' } | { kind: 'person'; name: string }

/** Who wrote a memory: a run (perhaps since deleted) or a person. */
export function memoryOrigin(m: Pick<MemoryRecord, 'source' | 'run_id' | 'written_by'>): MemoryOrigin {
  if (m.run_id) return { kind: 'run', runId: m.run_id }
  if (m.source.startsWith('run:')) return { kind: 'deleted-run' }
  const name = m.source.startsWith('user:') ? m.source.slice('user:'.length) : ''
  return { kind: 'person', name: name || m.written_by || 'someone' }
}

export type ScopeNode = { path: string; name: string; depth: number; count: number }

/** The scopes as a tree in display order, each with the memories in it and below it. The root "/" is left out. */
export function scopeTree(scopes: readonly MemoryScopeCount[]): ScopeNode[] {
  const totals = new Map<string, { parts: string[]; count: number }>()
  for (const { scope, count } of scopes) {
    const parts = scope.split('/').filter(Boolean)
    for (let i = 1; i <= parts.length; i++) {
      const path = `/${parts.slice(0, i).join('/')}`
      const prev = totals.get(path)
      totals.set(path, { parts: parts.slice(0, i), count: (prev?.count ?? 0) + count })
    }
  }
  // Sort by segments, not by text, so /people/sam stays under /people even when /people-x exists
  const key = (parts: string[]) => parts.join('\u0000')
  return [...totals.entries()]
    .sort(([, a], [, b]) => (key(a.parts) < key(b.parts) ? -1 : 1))
    .map(([path, { parts, count }]) => ({ path, name: parts[parts.length - 1], depth: parts.length - 1, count }))
}

export const plural = (n: number, one: string, many = `${one}s`): string => `${n} ${n === 1 ? one : many}`
