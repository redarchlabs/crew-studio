import { describe, expect, it } from 'vitest'
import { AUDIT_ACTION_LABELS, AUDIT_ACTION_TONES, detailPairs } from './auditFormat'

describe('detailPairs', () => {
  it('sorts keys and formats scalar values', () => {
    expect(detailPairs({ provider: 'microsoft', attempts: 3, ok: false, reason: null })).toEqual([
      { key: 'attempts', value: '3' },
      { key: 'ok', value: 'false' },
      { key: 'provider', value: 'microsoft' },
      { key: 'reason', value: '—' },
    ])
  })

  it('shortens long nested values', () => {
    const [pair] = detailPairs({ big: { list: 'x'.repeat(200) } })
    expect(pair.value.length).toBe(80)
    expect(pair.value.endsWith('…')).toBe(true)
  })

  it('handles missing details', () => {
    expect(detailPairs(null)).toEqual([])
  })
})

describe('audit action labels', () => {
  it('names and colours the API key events', () => {
    expect(AUDIT_ACTION_LABELS.api_key_created).toBe('API key created')
    expect(AUDIT_ACTION_LABELS.api_key_revoked).toBe('API key revoked')
    expect(AUDIT_ACTION_LABELS.api_key_rejected).toBe('API key refused')
    expect(AUDIT_ACTION_TONES.api_key_rejected).toBe('danger')
  })

  it('names changes made through MCP', () => {
    expect(AUDIT_ACTION_LABELS.mcp_change).toBe('Changed through MCP')
  })

  it('labels and colours the same set of actions', () => {
    expect(Object.keys(AUDIT_ACTION_TONES).sort()).toEqual(Object.keys(AUDIT_ACTION_LABELS).sort())
  })
})
