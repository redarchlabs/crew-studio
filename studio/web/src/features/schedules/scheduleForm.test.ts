import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeSchedule } from '../crews/testUtils.tsx'
import type { ScheduleInput } from '../../api/types'
import type { InputSpec } from '../workorders/target/targetInputs'
import { browserTimeZone, emptySchedule, normalizeSchedule, SCHEDULE_FIELDS, timeZones, toScheduleInput, validateSchedule } from './scheduleForm'

afterEach(() => vi.restoreAllMocks())

const valid: ScheduleInput = { ...emptySchedule(), name: 'N', crew_id: 'c1', timezone: 'UTC' }
const spec = (key: string, over: Partial<InputSpec> = {}): InputSpec => ({ key, label: key, default: '', required: true, ...over })

describe('SCHEDULE_FIELDS', () => {
  it('lists every ScheduleInput field the server can reject', () => {
    expect([...SCHEDULE_FIELDS].sort()).toEqual(Object.keys(emptySchedule()).sort())
  })
})

describe('browserTimeZone', () => {
  it('returns the resolved zone', () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => ({ resolvedOptions: () => ({ timeZone: 'Europe/Paris' }) }) as Intl.DateTimeFormat)
    expect(browserTimeZone()).toBe('Europe/Paris')
  })
  it('falls back to UTC when the zone is empty', () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => ({ resolvedOptions: () => ({ timeZone: '' }) }) as Intl.DateTimeFormat)
    expect(browserTimeZone()).toBe('UTC')
  })
  it('falls back to UTC when Intl throws', () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(() => { throw new Error('nope') })
    expect(browserTimeZone()).toBe('UTC')
  })
})

describe('timeZones', () => {
  it('puts UTC first exactly once', () => {
    vi.spyOn(Intl, 'supportedValuesOf').mockReturnValue(['Africa/Cairo', 'UTC', 'Asia/Tokyo'])
    expect(timeZones()).toEqual(['UTC', 'Africa/Cairo', 'Asia/Tokyo'])
  })
  it('returns just UTC when the browser cannot list zones', () => {
    vi.spyOn(Intl, 'supportedValuesOf').mockImplementation(() => { throw new RangeError('x') })
    expect(timeZones()).toEqual(['UTC'])
  })
  it('works with the real implementation', () => {
    const zones = timeZones()
    expect(zones[0]).toBe('UTC')
    expect(zones.filter((z) => z === 'UTC')).toHaveLength(1)
  })
})

describe('emptySchedule', () => {
  it('has safe defaults', () => {
    expect(emptySchedule()).toMatchObject({ name: '', crew_id: null, cron: '0 9 * * 1-5', brief: '', inputs: {}, budget_usd: null, enabled: true, overlap_policy: 'skip' })
  })
  it('returns a fresh inputs object each time', () => {
    expect(emptySchedule().inputs).not.toBe(emptySchedule().inputs)
  })
})

describe('toScheduleInput', () => {
  it('keeps editable fields only and copies inputs', () => {
    const s = makeSchedule({ inputs: { a: '1' }, overlap_policy: 'allow', budget_usd: 3, enabled: false })
    const input = toScheduleInput(s)
    expect(input).not.toHaveProperty('id')
    expect(input).not.toHaveProperty('run_as')
    expect(input).toMatchObject({ name: s.name, cron: s.cron, timezone: 'UTC', overlap_policy: 'allow', budget_usd: 3, enabled: false, inputs: { a: '1' } })
    expect(Object.keys(input).sort()).toEqual([...SCHEDULE_FIELDS].sort())
    expect(input.inputs).not.toBe(s.inputs)
  })
})

describe('normalizeSchedule', () => {
  it('trims name and collapses cron whitespace', () => {
    expect(normalizeSchedule({ ...valid, name: '  X ', cron: '  0   9  *\t* 1-5  ' })).toMatchObject({ name: 'X', cron: '0 9 * * 1-5' })
  })
  it('keeps the crew and sends no task target', () => {
    const out = normalizeSchedule(valid)
    expect(out.crew_id).toBe('c1')
    expect(out).not.toHaveProperty('task_id')
    expect(out).not.toHaveProperty('target_type')
  })
  it('does not mutate its input', () => {
    const v = { ...valid, name: ' a ' }
    normalizeSchedule(v)
    expect(v.name).toBe(' a ')
  })
})

describe('validateSchedule', () => {
  it('accepts a valid schedule', () => expect(validateSchedule(valid, [])).toEqual({}))

  it('requires a name', () => {
    expect(validateSchedule({ ...valid, name: '  ' }, []).name).toBe('Give the schedule a name.')
  })

  it('requires a crew', () => {
    expect(validateSchedule({ ...valid, crew_id: null }, []).target).toBe('Choose a crew to run.')
  })

  it.each([
    ['', /Enter a cron/],
    ['   ', /Enter a cron/],
    ['* * *', /5 fields/],
    ['61 * * * *', /5 fields/],
    ['* * * * *', /every 5 minutes/],
    ['*/4 * * * *', /every 5 minutes/],
  ])('rejects cron %j', (cron, msg) => {
    expect(validateSchedule({ ...valid, cron }, []).cron).toMatch(msg)
  })

  it.each(['*/5 * * * *', '@daily', '0 9 * * 1-5'])('accepts cron %s', (cron) => {
    expect(validateSchedule({ ...valid, cron }, []).cron).toBeUndefined()
  })

  it('requires a time zone', () => {
    expect(validateSchedule({ ...valid, timezone: '' }, []).timezone).toBe('Choose a time zone.')
  })

  it.each([0, -1, Number.NaN])('rejects budget %s', (b) => {
    expect(validateSchedule({ ...valid, budget_usd: b }, []).budget_usd).toMatch(/greater than 0/)
  })

  it.each([null, 0.01, 5])('accepts budget %s', (b) => {
    expect(validateSchedule({ ...valid, budget_usd: b }, []).budget_usd).toBeUndefined()
  })

  it('requires inputs marked required, keyed input:<key>', () => {
    const errs = validateSchedule({ ...valid, inputs: { a: ' ', c: 'x' } }, [spec('a'), spec('b', { label: 'Bee' }), spec('c'), spec('d', { required: false })])
    expect(errs).toEqual({ 'input:a': 'a is required', 'input:b': 'Bee is required' })
  })

  it('reports several problems at once', () => {
    const errs = validateSchedule({ ...valid, name: '', crew_id: null, cron: '', timezone: '', budget_usd: 0 }, [spec('a')])
    expect(Object.keys(errs).sort()).toEqual(['budget_usd', 'cron', 'input:a', 'name', 'target', 'timezone'])
  })
})
