import { describe, expect, it } from 'vitest'
import { makeWorkOrder } from '../testUtils'
import type { InputSpec } from '../target/targetInputs'
import { emptyWorkOrder, hasTarget, inputErrors, normalizeWorkOrder, toWorkOrderInput, UNTITLED, validateWorkOrder, WORK_ORDER_FIELDS } from './composerForm'

const spec = (key: string, required: boolean): InputSpec => ({ key, label: key.toUpperCase(), default: '', required })
const valid = () => ({ ...emptyWorkOrder('c1'), title: 'Hello' })

describe('emptyWorkOrder', () => {
  it('defaults to no crew', () => {
    expect(emptyWorkOrder()).toEqual({ title: '', brief: '', crew_id: null, inputs: {}, budget_usd: null })
  })

  it('applies a preselected crew', () => {
    expect(emptyWorkOrder('c1')).toMatchObject({ crew_id: 'c1' })
  })

  it('has a field for every key the server can reject', () => {
    expect([...WORK_ORDER_FIELDS].sort()).toEqual(Object.keys(emptyWorkOrder()).filter((k) => k !== 'title' && k !== 'brief').concat('title', 'brief').sort())
  })
})

describe('toWorkOrderInput', () => {
  it('copies editable fields only and does not alias inputs', () => {
    const wo = makeWorkOrder({ inputs: { a: '1' }, budget_usd: 3, title: 'T', brief: 'B' })
    const input = toWorkOrderInput(wo)
    expect(input).toEqual({ title: 'T', brief: 'B', crew_id: 'crew-1', inputs: { a: '1' }, budget_usd: 3 })
    input.inputs.a = 'changed'
    expect(wo.inputs.a).toBe('1')
    expect('status' in input).toBe(false)
  })
})

describe('hasTarget', () => {
  it('needs a crew id', () => {
    expect(hasTarget({ crew_id: 'c' })).toBe(true)
    expect(hasTarget({ crew_id: null })).toBe(false)
    expect(hasTarget({ crew_id: '' })).toBe(false)
  })
})

describe('normalizeWorkOrder', () => {
  it('trims the title and keeps the crew', () => {
    const out = normalizeWorkOrder({ ...valid(), title: '  Hi  ' })
    expect(out.title).toBe('Hi')
    expect(out.crew_id).toBe('c1')
    expect(out).not.toHaveProperty('task_id')
  })

  it('does not mutate its input', () => {
    const v = { ...valid(), title: ' x ' }
    normalizeWorkOrder(v)
    expect(v.title).toBe(' x ')
  })
})

describe('validateWorkOrder', () => {
  it('accepts a valid order', () => {
    expect(validateWorkOrder(valid(), [], true)).toEqual({})
  })

  it('requires a title (whitespace does not count)', () => {
    expect(validateWorkOrder({ ...valid(), title: '   ' }, [], false).title).toBe('Give the work order a title.')
  })

  it('limits the title to 200 characters after trimming', () => {
    expect(validateWorkOrder({ ...valid(), title: 'x'.repeat(200) }, [], false).title).toBeUndefined()
    expect(validateWorkOrder({ ...valid(), title: ' ' + 'x'.repeat(200) + ' ' }, [], false).title).toBeUndefined()
    expect(validateWorkOrder({ ...valid(), title: 'x'.repeat(201) }, [], false).title).toMatch(/200 characters/)
  })

  it('requires a crew', () => {
    expect(validateWorkOrder({ ...valid(), crew_id: null }, [], false).target).toBe('Choose a crew to run.')
  })

  it('validates the budget', () => {
    expect(validateWorkOrder({ ...valid(), budget_usd: 0 }, [], false).budget_usd).toMatch(/greater than 0/)
    expect(validateWorkOrder({ ...valid(), budget_usd: 1 }, [], false).budget_usd).toBeUndefined()
  })

  it('limits the brief to 100,000 characters', () => {
    expect(validateWorkOrder({ ...valid(), brief: 'x'.repeat(100_000) }, [], false).brief).toBeUndefined()
    expect(validateWorkOrder({ ...valid(), brief: 'x'.repeat(100_001) }, [], false).brief).toMatch(/100,000/)
  })

  it('checks required inputs only when submitting', () => {
    const specs = [spec('a', true), spec('b', false)]
    expect(validateWorkOrder(valid(), specs, false)).toEqual({})
    expect(validateWorkOrder(valid(), specs, true)).toEqual({ 'input:a': 'A is required' })
    expect(validateWorkOrder({ ...valid(), inputs: { a: 'ok' } }, specs, true)).toEqual({})
  })

  it('reports several problems at once', () => {
    const errs = validateWorkOrder({ ...valid(), title: '', crew_id: null, budget_usd: -1 }, [], false)
    expect(Object.keys(errs).sort()).toEqual(['budget_usd', 'target', 'title'])
  })
})

describe('inputErrors', () => {
  it('splits input: keys and strips the prefix', () => {
    expect(inputErrors({ title: 'x', 'input:a': 'A bad', 'input:b.c': 'B bad' })).toEqual({ a: 'A bad', 'b.c': 'B bad' })
    expect(inputErrors({})).toEqual({})
  })
})

it('exposes the untitled placeholder', () => {
  expect(UNTITLED).toBe('Untitled work order')
})
