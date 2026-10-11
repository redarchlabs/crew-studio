import { describe, expect, it } from 'vitest'
import { makeTask } from '../../lib/testFixtures'
import { EMPTY_TASK, newTaskInput, toTaskInput, validateTask } from './taskForm'

const ok = { ...EMPTY_TASK, name: 'N', description: 'D', expected_output: 'E', area_ids: ['a'] }

describe('taskForm', () => {
  it('accepts a valid task', () => expect(validateTask(ok)).toEqual({}))

  it('requires name, description and expected output', () => {
    expect(validateTask({ ...ok, name: ' ', description: '', expected_output: '\n' })).toEqual({
      name: 'Name is required',
      description: 'Description is required',
      expected_output: 'Expected output is required',
    })
  })

  it('requires an area', () => {
    expect(validateTask({ ...ok, area_ids: [] }).area_ids).toMatch(/at least one/)
  })

  it('markdown defaults to true and agent to null', () => {
    expect(EMPTY_TASK.markdown).toBe(true)
    expect(EMPTY_TASK.agent_id).toBeNull()
  })

  it('newTaskInput copies areas', () => {
    const src = ['x']
    const t = newTaskInput(src)
    t.area_ids.push('y')
    expect(src).toEqual(['x'])
    expect(EMPTY_TASK.area_ids).toEqual([])
  })

  it('toTaskInput keeps editable fields only and tolerates missing areas', () => {
    const t = { ...makeTask({ id: 'z', context_task_ids: ['q'], agent_id: 'a' }), area_ids: undefined } as unknown as Parameters<typeof toTaskInput>[0]
    const input = toTaskInput(t)
    expect(input).not.toHaveProperty('id')
    expect(input).toMatchObject({ context_task_ids: ['q'], agent_id: 'a', area_ids: [] })
  })
})
