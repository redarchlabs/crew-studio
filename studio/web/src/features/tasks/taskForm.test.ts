import { describe, expect, it } from 'vitest'
import { makeTask } from '../../lib/testFixtures'
import { newTaskInput, toTaskInput, validateTask } from './taskForm'

describe('task form', () => {
  it('requires name, description, expected output and an area', () => {
    expect(Object.keys(validateTask(newTaskInput([]))).sort()).toEqual(['area_ids', 'description', 'expected_output', 'name'])
  })

  it('accepts a complete task', () => {
    const v = { ...newTaskInput(['fin']), name: 't', description: 'd', expected_output: 'o' }
    expect(validateTask(v)).toEqual({})
  })

  it('round-trips an existing task without server-only fields', () => {
    const input = toTaskInput(makeTask({ id: 't1', area_ids: ['fin'] }))
    expect(input).not.toHaveProperty('id')
    expect(input.area_ids).toEqual(['fin'])
  })
})
