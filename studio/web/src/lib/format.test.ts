import { describe, expect, it } from 'vitest'
import { between, compact, dateTime, durationMs, int, relative, shortId, timeOfDay, truncate, usd } from './format'

describe('usd', () => {
  it('formats with two decimals', () => {
    expect(usd(1.5)).toBe('$1.50')
    expect(usd(0)).toBe('$0.00')
    expect(usd(1234567.891)).toBe('$1,234,567.89')
  })
  it('uses four decimals for sub-cent amounts (both signs)', () => {
    expect(usd(0.0042)).toBe('$0.0042')
    expect(usd(-0.0042)).toBe('-$0.0042')
  })
  it('switches precision exactly at one cent', () => {
    expect(usd(0.01)).toBe('$0.01')
  })
  it.each([null, undefined, Number.NaN])('renders a dash for %s', (v) => {
    expect(usd(v)).toBe('—')
  })
})

describe('int', () => {
  it('groups thousands', () => expect(int(1234567)).toBe('1,234,567'))
  it('renders zero', () => expect(int(0)).toBe('0'))
  it.each([null, undefined, Number.NaN])('renders a dash for %s', (v) => expect(int(v)).toBe('—'))
})

describe('compact', () => {
  it.each([
    [0, '0'],
    [999, '999'],
    [1000, '1.0k'],
    [1500, '1.5k'],
    [9949, '9.9k'],
    [9950, '10k'],
    [9999, '10k'],
    [999_499, '999k'],
    [999_500, '1.0M'],
    [-9999, '-10k'],
    [10_000, '10k'],
    [123_456, '123k'],
    [1_000_000, '1.0M'],
    [2_500_000, '2.5M'],
    [-2_500_000, '-2.5M'],
    [-1500, '-1.5k'],
  ])('compact(%d) = %s', (n, out) => expect(compact(n)).toBe(out))
})

describe('durationMs', () => {
  it.each([
    [0, '0ms'],
    [999.4, '999ms'],
    [1000, '1.0s'],
    [9500, '9.5s'],
    [10_000, '10s'],
    [59_000, '59s'],
    [60_000, '1m 0s'],
    [125_000, '2m 5s'],
    [3_600_000, '1h 0m'],
    [3_660_000, '1h 1m'],
  ])('durationMs(%d) = %s', (ms, out) => expect(durationMs(ms)).toBe(out))
  it.each([null, undefined, -1, Number.NaN])('renders a dash for %s', (v) => expect(durationMs(v)).toBe('—'))
})

describe('between', () => {
  it('returns null without a start', () => expect(between(null, null)).toBeNull())
  it('measures start to end', () => {
    expect(between('2026-01-01T00:00:00Z', '2026-01-01T00:00:05Z')).toBe(5000)
  })
  it('uses now when there is no end (running)', () => {
    const now = Date.parse('2026-01-01T00:01:00Z')
    expect(between('2026-01-01T00:00:00Z', null, now)).toBe(60_000)
  })
  it('defaults now to the current clock', () => {
    const start = new Date(Date.now() - 2000).toISOString()
    const d = between(start, null)
    expect(d).not.toBeNull()
    expect(d!).toBeGreaterThanOrEqual(1900)
  })
  it('returns null for unparseable dates', () => {
    expect(between('nope', null)).toBeNull()
    expect(between('2026-01-01T00:00:00Z', 'nope')).toBeNull()
  })
})

describe('dateTime / timeOfDay', () => {
  it('dash for empty, echoes unparseable input', () => {
    expect(dateTime(null)).toBe('—')
    expect(dateTime(undefined)).toBe('—')
    expect(dateTime('')).toBe('—')
    expect(dateTime('garbage')).toBe('garbage')
  })
  it('formats a valid date with the month and day', () => {
    expect(dateTime('2026-03-05T12:00:00Z')).toMatch(/Mar/)
  })
  it('timeOfDay is HH:MM:SS, empty for invalid input', () => {
    expect(timeOfDay('2026-03-05T12:34:56Z')).toMatch(/^\d{2}:\d{2}:\d{2}$/)
    expect(timeOfDay('garbage')).toBe('')
  })
})

describe('relative', () => {
  const now = Date.parse('2026-01-02T00:00:00Z')
  it.each([
    ['2026-01-02T00:00:00Z', 'just now'],
    ['2026-01-01T23:59:30Z', 'just now'],
    ['2026-01-01T23:59:00Z', '1m ago'],
    ['2026-01-01T23:00:00Z', '1h ago'],
    ['2025-12-31T00:00:00Z', '2d ago'],
  ])('%s -> %s', (iso, out) => expect(relative(iso, now)).toBe(out))
  it('dash for null, undefined and invalid dates', () => {
    expect(relative(null)).toBe('—')
    expect(relative(undefined)).toBe('—')
    expect(relative('nope')).toBe('—')
  })
  it('defaults now to the current time', () => {
    expect(relative(new Date().toISOString())).toBe('just now')
  })
})

describe('truncate / shortId', () => {
  it('leaves short text alone and cuts long text with an ellipsis inside the limit', () => {
    expect(truncate('abc', 3)).toBe('abc')
    expect(truncate('abcd', 3)).toBe('ab…')
    expect(truncate('', 3)).toBe('')
  })
  it('shortId keeps the first 8 characters', () => {
    expect(shortId('0123456789abcdef')).toBe('01234567')
    expect(shortId('abc')).toBe('abc')
  })
})
