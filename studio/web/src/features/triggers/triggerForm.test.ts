import { describe, expect, it } from 'vitest'
import type { TriggerInput } from '../../api/types'
import { makeTrigger } from '../crews/testUtils.tsx'
import { emptyTrigger, mapFromRows, normalizeTrigger, rowsFromMap, toTriggerInput, TRIGGER_FIELDS, validateTrigger } from './triggerForm'

const valid: TriggerInput = { ...emptyTrigger(), name: 'T', crew_id: 'c1' }

describe('TRIGGER_FIELDS', () => {
  it('matches every TriggerInput key', () => {
    expect([...TRIGGER_FIELDS].sort()).toEqual(Object.keys(emptyTrigger()).sort())
  })
})

describe('emptyTrigger', () => {
  it('has safe defaults and a fresh input_map per call', () => {
    expect(emptyTrigger()).toMatchObject({ name: '', crew_id: null, enabled: true, input_map: {}, include_payload: true, budget_usd: null, callback_url: null })
    expect(emptyTrigger().input_map).not.toBe(emptyTrigger().input_map)
    expect(emptyTrigger()).not.toHaveProperty('task_id')
    expect(emptyTrigger()).not.toHaveProperty('target_type')
  })
})

describe('toTriggerInput', () => {
  it('keeps editable fields only and copies the map', () => {
    const t = makeTrigger({ input_map: { a: 'x.y' }, callback_url: 'https://e.com', budget_usd: 2, include_payload: false, enabled: false })
    const input = toTriggerInput(t)
    expect(Object.keys(input).sort()).toEqual([...TRIGGER_FIELDS].sort())
    expect(input).toMatchObject({ input_map: { a: 'x.y' }, callback_url: 'https://e.com', budget_usd: 2, include_payload: false, enabled: false })
    expect(input.input_map).not.toBe(t.input_map)
    expect(input).not.toHaveProperty('token')
    expect(input).not.toHaveProperty('signing_secret')
  })
})

describe('rowsFromMap / mapFromRows', () => {
  it('round-trips', () => {
    const map = { a: 'x', b: 'y.z' }
    expect(mapFromRows(rowsFromMap(map))).toEqual(map)
  })
  it('handles the empty map', () => {
    expect(rowsFromMap({})).toEqual([])
    expect(mapFromRows([])).toEqual({})
  })
  it('drops rows without a key and trims keys and paths', () => {
    expect(mapFromRows([{ key: '  ', path: 'x' }, { key: ' k ', path: ' a.b ' }, { key: '', path: '' }])).toEqual({ k: 'a.b' })
  })
  it('lets later duplicates win', () => {
    expect(mapFromRows([{ key: 'k', path: 'one' }, { key: 'k', path: 'two' }])).toEqual({ k: 'two' })
  })
  it('keeps a key with an empty path so validation can flag it', () => {
    expect(mapFromRows([{ key: 'k', path: '' }])).toEqual({ k: '' })
  })
})

describe('normalizeTrigger', () => {
  it('trims the name', () => expect(normalizeTrigger({ ...valid, name: '  X ' }).name).toBe('X'))

  it.each([[null, null], ['', null], ['   ', null], [' https://a.b/c ', 'https://a.b/c']])('callback %j -> %j', (input, out) => {
    expect(normalizeTrigger({ ...valid, callback_url: input }).callback_url).toBe(out)
  })

  it('keeps the crew', () => {
    expect(normalizeTrigger(valid).crew_id).toBe('c1')
  })

  it('does not mutate its input', () => {
    const v = { ...valid, name: ' a ' }
    normalizeTrigger(v)
    expect(v.name).toBe(' a ')
  })
})

describe('validateTrigger', () => {
  it('accepts a valid trigger', () => expect(validateTrigger(valid)).toEqual({}))

  it('requires a name', () => expect(validateTrigger({ ...valid, name: ' ' }).name).toBe('Give the trigger a name.'))

  it('requires a crew', () => {
    expect(validateTrigger({ ...valid, crew_id: null }).target).toBe('Choose a crew to run.')
  })

  it.each([0, -3, Number.NaN])('rejects budget %s', (b) => expect(validateTrigger({ ...valid, budget_usd: b }).budget_usd).toMatch(/greater than 0/))
  it.each([null, 0.5])('accepts budget %s', (b) => expect(validateTrigger({ ...valid, budget_usd: b }).budget_usd).toBeUndefined())

  describe('input_map', () => {
    it.each(['Bad', '1a', 'a-b', 'a b', '_a'])('rejects key %j', (k) => {
      expect(validateTrigger({ ...valid, input_map: { [k]: 'x' } }).input_map).toBe(`"${k}" is not a valid input key (lowercase letters, digits and _).`)
    })
    it.each(['a', 'a_1', 'abc_def9'])('accepts key %s', (k) => {
      expect(validateTrigger({ ...valid, input_map: { [k]: 'x' } }).input_map).toBeUndefined()
    })
    it.each(['', ' ', 'a..b', '.a', 'a.', 'a b', 'a/b', 'a[0]', '$.a'])('rejects path %j', (p) => {
      expect(validateTrigger({ ...valid, input_map: { k: p } }).input_map).toBe('Give "k" a dotted payload path, e.g. customer.name.')
    })
    it.each(['a', 'customer.name', 'items.0.sku', 'A-b_c.d9'])('accepts path %s', (p) => {
      expect(validateTrigger({ ...valid, input_map: { k: p } }).input_map).toBeUndefined()
    })
    it('reports a bad key before a bad path', () => {
      expect(validateTrigger({ ...valid, input_map: { good: '', Bad: 'x' } }).input_map).toMatch(/"Bad" is not a valid input key/)
    })
  })

  describe('callback_url', () => {
    it.each([null, '', '   ', 'https://example.com/h', 'http://localhost:8080/x', ' https://example.com '])('accepts %j', (u) => {
      expect(validateTrigger({ ...valid, callback_url: u }).callback_url).toBeUndefined()
    })
    it.each(['example.com', 'not a url', '/relative', '//host/path'])('rejects non-URL %j', (u) => {
      expect(validateTrigger({ ...valid, callback_url: u }).callback_url).toMatch(/Enter a full URL/)
    })
    it.each(['ftp://example.com/x', 'javascript:alert(1)', 'file:///etc/passwd', 'mailto:a@b.c'])('rejects non-http protocol %j', (u) => {
      expect(validateTrigger({ ...valid, callback_url: u }).callback_url).toMatch(/^Use an https:\/\/ URL/)
    })
  })

  it('collects several problems at once', () => {
    const errs = validateTrigger({ ...valid, name: '', crew_id: null, budget_usd: 0, input_map: { Bad: 'x' }, callback_url: 'nope' })
    expect(Object.keys(errs).sort()).toEqual(['budget_usd', 'callback_url', 'input_map', 'name', 'target'])
  })
})
