import { describe, expect, it } from 'vitest'
import { safeHref } from './safeLink'

describe('safeHref', () => {
  it('keeps http(s) links and drops everything else', () => {
    expect(safeHref('https://acme.atlassian.net/browse/PRJ-1')).toBe('https://acme.atlassian.net/browse/PRJ-1')
    expect(safeHref('http://127.0.0.1:8593/browse/PRJ-1')).toBe('http://127.0.0.1:8593/browse/PRJ-1')
    expect(safeHref('javascript:alert(1)')).toBeUndefined()
    expect(safeHref('data:text/html,x')).toBeUndefined()
    expect(safeHref('not a url')).toBeUndefined()
    expect(safeHref('')).toBeUndefined()
    expect(safeHref(null)).toBeUndefined()
  })
})
