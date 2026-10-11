import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))

import { ApiError } from '../api/client'
import { notifyError, notifySuccess } from './notify'
import { useEntityForm } from './useEntityForm'
import { autoCleanup } from './testUtils'

autoCleanup()
beforeEach(() => vi.clearAllMocks())

type Form = { name: string; count: number }
const initial: Form = { name: 'a', count: 1 }

function setup(over: Partial<Parameters<typeof useEntityForm<Form, { id: string }>>[0]> = {}) {
  const create = vi.fn((b: Form) => Promise.resolve({ id: `new:${b.name}` }))
  const update = vi.fn((id: string, b: Form) => Promise.resolve({ id: `${id}:${b.name}` }))
  const validate = vi.fn((v: Form): Record<string, string> => (v.name ? {} : { name: 'Name is required' }))
  const hook = renderHook(() =>
    useEntityForm<Form, { id: string }>({ entityId: null, initial, fields: ['name', 'count'], label: 'agent', validate, create, update, ...over }),
  )
  return { ...hook, create, update, validate }
}

describe('useEntityForm', () => {
  it('starts clean', () => {
    const { result } = setup()
    expect(result.current.value).toEqual(initial)
    expect(result.current.dirty).toBe(false)
    expect(result.current.saving).toBe(false)
    expect(result.current.errors).toEqual({ fields: {}, general: [] })
  })

  it('set and update edit the value immutably and mark it dirty', () => {
    const { result } = setup()
    act(() => result.current.set('name', 'b'))
    expect(result.current.value).toEqual({ name: 'b', count: 1 })
    expect(initial.name).toBe('a')
    expect(result.current.dirty).toBe(true)
    act(() => result.current.update((p) => ({ ...p, count: 5 })))
    expect(result.current.value.count).toBe(5)
    act(() => result.current.set('name', 'a'))
    act(() => result.current.set('count', 1))
    expect(result.current.dirty).toBe(false)
  })

  it('blocks save and reports local validation errors without calling the API', async () => {
    const { result, create } = setup()
    act(() => result.current.set('name', ''))
    let out: unknown = 'unset'
    await act(async () => {
      out = await result.current.save()
    })
    expect(out).toBeNull()
    expect(create).not.toHaveBeenCalled()
    expect(result.current.errors.fields).toEqual({ name: 'Name is required' })
    expect(notifyError).toHaveBeenCalledWith('Check the agent form', expect.objectContaining({ message: 'Name is required' }))
    expect(result.current.saving).toBe(false)
  })

  it('creates when there is no entity id, resets the baseline, and toasts', async () => {
    const { result, create, update } = setup()
    act(() => result.current.set('name', 'zed'))
    let out: unknown
    await act(async () => {
      out = await result.current.save()
    })
    expect(out).toEqual({ id: 'new:zed' })
    expect(create).toHaveBeenCalledWith({ name: 'zed', count: 1 })
    expect(update).not.toHaveBeenCalled()
    expect(result.current.dirty).toBe(false)
    expect(notifySuccess).toHaveBeenCalledWith('Created agent')
  })

  it('updates when an entity id is present', async () => {
    const { result, create, update } = setup({ entityId: 'e1' })
    act(() => result.current.set('name', 'q'))
    await act(async () => {
      await result.current.save()
    })
    expect(update).toHaveBeenCalledWith('e1', { name: 'q', count: 1 })
    expect(create).not.toHaveBeenCalled()
    expect(notifySuccess).toHaveBeenCalledWith('Saved agent')
  })

  it('applies normalize to what is sent and to the saved value', async () => {
    const { result, create } = setup({ normalize: (v) => ({ ...v, name: v.name.trim() }) })
    act(() => result.current.set('name', '  padded  '))
    await act(async () => {
      await result.current.save()
    })
    expect(create).toHaveBeenCalledWith({ name: 'padded', count: 1 })
    expect(result.current.value.name).toBe('padded')
    expect(result.current.dirty).toBe(false)
  })

  it('maps 422 details to field errors and keeps the form dirty', async () => {
    const err = new ApiError(422, { code: 'validation_failed', message: 'bad', details: ['name: taken', 'weird thing'] })
    const { result } = setup({ create: () => Promise.reject(err) })
    act(() => result.current.set('name', 'dup'))
    let out: unknown = 'unset'
    await act(async () => {
      out = await result.current.save()
    })
    expect(out).toBeNull()
    expect(result.current.errors).toEqual({ fields: { name: 'taken' }, general: ['weird thing'] })
    expect(result.current.dirty).toBe(true)
    expect(result.current.saving).toBe(false)
    expect(notifyError).toHaveBeenCalledWith('Could not save agent', err)
  })

  it('clears earlier errors after a successful save', async () => {
    let fail = true
    const create = vi.fn(() => (fail ? Promise.reject(new ApiError(422, { code: 'v', message: 'm', details: ['name: bad'] })) : Promise.resolve({ id: 'ok' })))
    const { result } = setup({ create })
    await act(async () => {
      await result.current.save()
    })
    expect(result.current.errors.fields.name).toBe('bad')
    fail = false
    await act(async () => {
      await result.current.save()
    })
    expect(result.current.errors.fields).toEqual({})
  })

  it('sets saving while the request is in flight', async () => {
    let release!: () => void
    const create = vi.fn(() => new Promise<{ id: string }>((res) => (release = () => res({ id: 'x' }))))
    const { result } = setup({ create })
    let p!: Promise<unknown>
    act(() => {
      p = result.current.save()
    })
    expect(result.current.saving).toBe(true)
    await act(async () => {
      release()
      await p
    })
    expect(result.current.saving).toBe(false)
  })

  it('reset replaces value and baseline and clears errors', async () => {
    const { result } = setup()
    act(() => result.current.set('name', ''))
    await act(async () => {
      await result.current.save()
    })
    expect(result.current.errors.fields.name).toBeDefined()
    act(() => result.current.reset({ name: 'loaded', count: 9 }))
    expect(result.current.value).toEqual({ name: 'loaded', count: 9 })
    expect(result.current.dirty).toBe(false)
    expect(result.current.errors.fields).toEqual({})
  })
})
