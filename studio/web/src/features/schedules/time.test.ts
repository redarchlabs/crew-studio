import { describe, expect, it } from 'vitest'
import { fromNow, inZone } from './time'

const NOW = Date.parse('2026-10-03T12:00:00Z')
const at = (sec: number) => new Date(NOW + sec * 1000).toISOString()

describe('fromNow', () => {
  it.each([null, undefined, '', 'garbage'])('returns a dash for %j', (v) => expect(fromNow(v, NOW)).toBe('—'))

  it.each([
    [1, 'in 1s'],
    [59, 'in 59s'],
    [60, 'in 1m'],
    [89, 'in 1m'],
    [90, 'in 2m'],
    [3599, 'in 60m'],
    [3600, 'in 1h'],
    [86399, 'in 24h'],
    [86400, 'in 1d'],
    [3 * 86400, 'in 3d'],
  ])('future %ds -> %s', (s, text) => expect(fromNow(at(s), NOW)).toBe(text))

  it('says due now for the present and the last 44s', () => {
    expect(fromNow(at(0), NOW)).toBe('due now')
    expect(fromNow(at(-44), NOW)).toBe('due now')
  })

  it('falls back to a relative past time beyond 45s', () => {
    expect(fromNow(at(-45), NOW)).toBe('1m ago')
    expect(fromNow(at(-120), NOW)).toBe('2m ago')
    expect(fromNow(at(-7200), NOW)).toBe('2h ago')
    expect(fromNow(at(-3 * 86400), NOW)).toBe('3d ago')
  })

  it('uses Date.now by default', () => {
    expect(fromNow(new Date(Date.now() + 3 * 3600 * 1000).toISOString())).toMatch(/^in [23]h$/)
  })
})

describe('inZone', () => {
  it('formats in the requested zone', () => {
    const iso = '2026-10-05T08:00:00Z'
    expect(inZone(iso, 'UTC')).toMatch(/0?8:00/)
    expect(inZone(iso, 'Asia/Tokyo')).toMatch(/(17:00|0?5:00 PM)/)
  })
  it('returns the input for an invalid date', () => expect(inZone('nope', 'UTC')).toBe('nope'))
  it('falls back to a local string for an unknown zone', () => {
    const out = inZone('2026-10-05T08:00:00Z', 'Not/AZone')
    expect(out).toBe(new Date('2026-10-05T08:00:00Z').toLocaleString())
  })
})
