import { describe, expect, it } from 'vitest'
import { cronProblem, cronToWords, minIntervalMinutes, ordinal, parseCron, presetFor } from './cron'

describe('cronToWords', () => {
  it.each([
    ['* * * * *', 'Every minute'],
    ['0 * * * *', 'Every hour, on the hour'],
    ['30 * * * *', 'Every hour, at 30 minutes past'],
    ['*/15 * * * *', 'Every 15 minutes'],
    ['0 */2 * * *', 'Every 2 hours, on the hour'],
    ['0 9 * * *', 'Every day at 09:00'],
    ['0 9,17 * * *', 'Every day at 09:00 and 17:00'],
    ['0 9 * * 1-5', 'Weekdays at 09:00'],
    ['0 9 * * MON-FRI', 'Weekdays at 09:00'],
    ['0 10 * * 6,0', 'Weekends at 10:00'],
    ['0 9 * * 1', 'Every Monday at 09:00'],
    ['30 8 * * 1,3,5', 'Every Monday, Wednesday and Friday at 08:30'],
    ['0 9 * * 2-4', 'Tuesday to Thursday at 09:00'],
    ['0 9 1 * *', 'On the 1st of every month at 09:00'],
    ['0 9 1,15 * *', 'On the 1st and 15th of every month at 09:00'],
    ['0 0 1 1 *', 'On the 1st of January at 00:00'],
    ['*/30 9-17 * * 1-5', 'Every 30 minutes, 09:00–17:59, weekdays'],
    ['0 9 * 6 *', 'Every day in June at 09:00'],
    ['0 9 13 * 5', 'On the 13th of every month or every Friday at 09:00'],
    ['0 9 * * 7', 'Every Sunday at 09:00'],
  ])('%s -> %s', (expr, words) => {
    expect(cronToWords(expr)).toBe(words)
  })

  it.each([
    ['@hourly', 'Every hour, on the hour'],
    ['@daily', 'Every day at 00:00'],
    ['@weekly', 'Every Sunday at 00:00'],
    ['@monthly', 'On the 1st of every month at 00:00'],
    ['@yearly', 'On the 1st of January at 00:00'],
  ])('describes %s', (expr, words) => {
    expect(cronToWords(expr)).toBe(words)
  })

  it.each(['', 'foo', '* * *', '61 * * * *', '* 24 * * *', '0 9 * * 8', '5-1 * * * *', '*/0 * * * *', '@every 5m'])('rejects %j', (expr) => {
    expect(cronToWords(expr)).toBeNull()
  })

  it('tolerates extra whitespace', () => {
    expect(cronToWords('  0   9 * *  1  ')).toBe('Every Monday at 09:00')
  })
})

describe('parseCron', () => {
  it('expands steps within ranges', () => {
    expect(parseCron('0 9-17/4 * * *')?.hour.values).toEqual([9, 13, 17])
  })

  it('treats */1 as any', () => {
    expect(parseCron('*/1 * * * *')?.minute.any).toBe(true)
  })
})

describe('helpers', () => {
  it('finds presets', () => {
    expect(presetFor('0 9 * * 1-5')).toBe('weekdays')
    expect(presetFor(' 0  9 * * 1 ')).toBe('monday')
    expect(presetFor('5 4 * * *')).toBe('custom')
  })

  it('formats ordinals', () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 31].map(ordinal)).toEqual(['1st', '2nd', '3rd', '4th', '11th', '12th', '13th', '21st', '22nd', '31st'])
  })

  it('estimates the shortest interval', () => {
    expect(minIntervalMinutes('* * * * *')).toBe(1)
    expect(minIntervalMinutes('*/5 * * * *')).toBe(5)
    expect(minIntervalMinutes('0,58 * * * *')).toBe(2)
    expect(minIntervalMinutes('0 9 * * *')).toBeNull()
  })

  it('reports local problems', () => {
    expect(cronProblem('')).toMatch(/Enter/)
    expect(cronProblem('nope')).toMatch(/5 fields/)
    expect(cronProblem('*/2 * * * *')).toMatch(/5 minutes/)
    expect(cronProblem('0 9 * * 1-5')).toBeNull()
  })
})
