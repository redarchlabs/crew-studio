import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { ApiError } from '../../../api/client'
import type { WorkOrderInput } from '../../../api/types'
import { workOrdersApi } from '../../../api/endpoints'
import { makeCrew } from '../../../lib/testFixtures'
import { makeAttachment, makeRun, makeWorkOrder } from '../testUtils'
import type { TargetCatalogData } from '../target/useTargetCatalog'
import { emptyWorkOrder } from './composerForm'
import { useComposer } from './useComposer'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const api = vi.mocked(workOrdersApi)

const input = (key: string, def = '') => ({ key, label: '', default: def, required: true })

const catalog: TargetCatalogData = {
  crews: [
    makeCrew({ id: 'c1', name: 'Research', inputs: [input('topic')] }),
    makeCrew({ id: 'c2', name: 'Regional', inputs: [input('region', 'EMEA')] }),
    makeCrew({ id: 'c3', name: 'Other', inputs: [input('other')] }),
    makeCrew({ id: 'c4', name: 'Shared', inputs: [input('topic'), input('extra')] }),
  ],
  tasks: [],
  forbidden: { crews: false, tasks: false },
}

const valid = (): WorkOrderInput => ({ ...emptyWorkOrder('c1'), title: 'My order', inputs: { topic: 'AI' } })

beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset()
  vi.mocked(toast.error).mockReset()
  vi.mocked(toast.success).mockReset()
  URL.createObjectURL = vi.fn(() => 'blob:x')
  URL.revokeObjectURL = vi.fn()
})

const setup = (workOrder = null as ReturnType<typeof makeWorkOrder> | null, initial = valid()) => renderHook(() => useComposer(workOrder, initial, catalog))

const file = (name = 'a.txt') => new File(['abc'], name, { type: 'text/plain' })

describe('initial state', () => {
  it('starts a new order clean with defaults of a preselected crew', () => {
    const { result } = setup(null, { ...emptyWorkOrder('c2'), title: '' })
    expect(result.current.value.inputs).toEqual({ region: 'EMEA' })
    expect(result.current.dirty).toBe(false)
    expect(result.current.saved).toBeNull()
    expect(result.current.busy).toBeNull()
    expect(result.current.derived.inputs.map((i) => i.key)).toEqual(['region'])
  })

  it('uses the given values as is when editing', () => {
    const wo = makeWorkOrder({ inputs: { stale: 'x' } })
    const { result } = setup(wo, { ...valid(), inputs: { stale: 'x' } })
    expect(result.current.value.inputs).toEqual({ stale: 'x' })
    expect(result.current.saved).toBe(wo)
  })

  it('seeds the file queue from the work order attachments', () => {
    const wo = makeWorkOrder({ attachments: [makeAttachment()] })
    const { result } = setup(wo)
    expect(result.current.queue.uploaded).toHaveLength(1)
  })
})

describe('editing', () => {
  it('set marks the form dirty, and returning to the original value cleans it', () => {
    const { result } = setup()
    act(() => result.current.set('title', 'Changed'))
    expect(result.current.dirty).toBe(true)
    act(() => result.current.set('title', 'My order'))
    expect(result.current.dirty).toBe(false)
  })

  it('setTarget swaps the crew, defaulting the new inputs and dropping unused ones', () => {
    const { result } = setup()
    act(() => result.current.setTarget({ crew_id: 'c2' }))
    expect(result.current.value.crew_id).toBe('c2')
    expect(result.current.value.inputs).toEqual({ region: 'EMEA' })
    act(() => result.current.setTarget({ crew_id: 'c3' }))
    expect(result.current.value.inputs).toEqual({ other: '' })
  })

  it('keeps typed input values across crews that share the key', () => {
    const { result } = setup()
    act(() => result.current.setTarget({ crew_id: 'c4' }))
    expect(result.current.value.inputs).toEqual({ topic: 'AI', extra: '' })
  })
})

describe('saveDraft', () => {
  it('validates locally without calling the API', async () => {
    const { result } = setup(null, { ...valid(), title: '' })
    await act(async () => {
      expect(await result.current.saveDraft()).toBeNull()
    })
    expect(api.create).not.toHaveBeenCalled()
    expect(result.current.errors.fields.title).toBeTruthy()
    expect(toast.error).toHaveBeenCalledWith('Check the work order', expect.anything())
  })

  it('allows empty required inputs in a draft', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'new' }))
    const { result } = setup(null, { ...valid(), inputs: {} })
    await act(async () => {
      expect(await result.current.saveDraft()).not.toBeNull()
    })
    expect(api.create).toHaveBeenCalled()
  })

  it('creates the work order with a normalised body, then clears dirty and errors', async () => {
    const wo = makeWorkOrder({ id: 'new', number: 9 })
    api.create.mockResolvedValue(wo)
    const { result } = setup(null, { ...valid(), title: '  Padded  ' })
    let saved = null as unknown
    await act(async () => {
      saved = await result.current.saveDraft()
    })
    expect(saved).toBe(wo)
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ title: 'Padded', crew_id: 'c1' }))
    expect(result.current.saved).toBe(wo)
    expect(toast.success).toHaveBeenCalledWith('Saved WO-9')
    expect(result.current.busy).toBeNull()
    expect(result.current.errors.general).toEqual([])
  })

  it('updates (not creates) once a work order exists', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'new' }))
    api.update.mockResolvedValue(makeWorkOrder({ id: 'new', title: 'v2' }))
    const { result } = setup()
    await act(async () => void (await result.current.saveDraft()))
    act(() => result.current.set('title', 'v2'))
    await act(async () => void (await result.current.saveDraft()))
    expect(api.create).toHaveBeenCalledTimes(1)
    expect(api.update).toHaveBeenCalledWith('new', expect.objectContaining({ title: 'v2' }))
    expect(result.current.dirty).toBe(false)
  })

  it('shows busy while saving', async () => {
    let finish!: (w: ReturnType<typeof makeWorkOrder>) => void
    api.create.mockReturnValue(new Promise((r) => { finish = r }))
    const { result } = setup()
    let p!: Promise<unknown>
    act(() => {
      p = result.current.saveDraft()
    })
    await waitFor(() => expect(result.current.busy).toBe('draft'))
    await act(async () => {
      finish(makeWorkOrder())
      await p
    })
    expect(result.current.busy).toBeNull()
  })

  it('maps 422 details onto fields, with unknown ones as general messages', async () => {
    api.create.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'invalid', details: ['title: is too long', 'budget_usd: must be positive', 'weird thing happened'] }))
    const { result } = setup()
    await act(async () => {
      expect(await result.current.saveDraft()).toBeNull()
    })
    expect(result.current.errors.fields).toEqual({ title: 'is too long', budget_usd: 'must be positive' })
    expect(result.current.errors.general).toEqual(['weird thing happened'])
    expect(toast.error).toHaveBeenCalledWith('Could not save the work order', { description: expect.stringContaining('title: is too long') })
    expect(result.current.busy).toBeNull()
  })

  it('shows a general message for non-validation failures', async () => {
    api.create.mockRejectedValue(new Error('network down'))
    const { result } = setup()
    await act(async () => void (await result.current.saveDraft()))
    expect(result.current.errors).toEqual({ fields: {}, general: ['network down'] })
  })

  it('uploads queued files after saving', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'new' }))
    api.upload.mockResolvedValue([makeAttachment({ id: 'f1' })])
    // No target at first, so the file waits
    const { result } = setup(null, { ...emptyWorkOrder(), title: 'T' })
    await act(() => result.current.queue.add([file()]))
    expect(api.upload).not.toHaveBeenCalled()
    act(() => result.current.setTarget({ crew_id: 'c1' }))
    await waitFor(() => expect(api.upload).toHaveBeenCalled())
  })
})

describe('submit', () => {
  it('requires inputs that are required', async () => {
    const { result } = setup(null, { ...valid(), inputs: { topic: '  ' } })
    await act(async () => {
      expect(await result.current.submit()).toBeNull()
    })
    expect(api.create).not.toHaveBeenCalled()
    expect(result.current.errors.fields['input:topic']).toBe('Topic is required')
  })

  it('creates, then submits, returning the work order and run', async () => {
    const wo = makeWorkOrder({ id: 'new' })
    const run = makeRun({ id: 'r1', status: 'queued' })
    api.create.mockResolvedValue(wo)
    api.submit.mockResolvedValue(run)
    const { result } = setup()
    let out = null as unknown
    await act(async () => {
      out = await result.current.submit()
    })
    expect(out).toEqual({ workOrder: wo, run })
    expect(api.submit).toHaveBeenCalledWith('new')
    expect(result.current.busy).toBeNull()
  })

  it('does not re-save an unchanged, already saved work order', async () => {
    const wo = makeWorkOrder({ id: 'w1', inputs: { topic: 'AI' } })
    api.submit.mockResolvedValue(makeRun())
    const { result } = setup(wo, valid())
    await act(async () => void (await result.current.submit()))
    expect(api.update).not.toHaveBeenCalled()
    expect(api.create).not.toHaveBeenCalled()
    expect(api.submit).toHaveBeenCalledWith('w1')
  })

  it('saves pending edits to an existing work order before submitting', async () => {
    const wo = makeWorkOrder({ id: 'w1' })
    api.update.mockResolvedValue(wo)
    api.submit.mockResolvedValue(makeRun())
    const { result } = setup(wo, valid())
    act(() => result.current.set('brief', 'new brief'))
    await act(async () => void (await result.current.submit()))
    expect(api.update).toHaveBeenCalledWith('w1', expect.objectContaining({ brief: 'new brief' }))
    expect(api.update.mock.invocationCallOrder[0]).toBeLessThan(api.submit.mock.invocationCallOrder[0])
  })

  it('refuses to submit while a file has failed to upload', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'new' }))
    api.upload.mockRejectedValue(new Error('quota'))
    const { result } = setup()
    await act(() => result.current.queue.add([file()]))
    await waitFor(() => expect(result.current.queue.pending[0]?.status).toBe('error'))
    await act(async () => {
      expect(await result.current.submit()).toBeNull()
    })
    expect(api.submit).not.toHaveBeenCalled()
    expect(result.current.errors.general).toEqual(['Some files have not uploaded. Retry or remove them, then submit again.'])
  })

  it('reports 422 problems from submit', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'new' }))
    api.submit.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'invalid', details: ['inputs: topic is required by agent', 'crew_id: crew was deleted'] }))
    const { result } = setup()
    await act(async () => {
      expect(await result.current.submit()).toBeNull()
    })
    expect(result.current.errors.fields).toEqual({ inputs: 'topic is required by agent', crew_id: 'crew was deleted' })
    expect(toast.error).toHaveBeenCalledWith('Could not submit the work order', expect.anything())
    expect(result.current.busy).toBeNull()
  })

  it('fails cleanly when saving before submit fails', async () => {
    api.create.mockRejectedValue(new Error('db'))
    const { result } = setup()
    await act(async () => void (await result.current.submit()))
    expect(api.submit).not.toHaveBeenCalled()
    expect(result.current.errors.general).toEqual(['db'])
  })
})

describe('draft creation for attachments', () => {
  it('creates a draft with the given title, then uploads', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'draft-1', number: 3 }))
    api.upload.mockResolvedValue([makeAttachment()])
    const { result } = setup()
    await act(() => result.current.queue.add([file()]))
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ title: 'My order' }))
    expect(api.upload).toHaveBeenCalledWith('draft-1', expect.any(Array))
    expect(toast.success).toHaveBeenCalledWith('Saved draft WO-3 so your files can upload')
    expect(result.current.saved?.id).toBe('draft-1')
  })

  it('names an untitled draft', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'draft-1' }))
    api.upload.mockResolvedValue([makeAttachment()])
    const { result } = setup(null, { ...valid(), title: '   ' })
    await act(() => result.current.queue.add([file()]))
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ title: 'Untitled work order' }))
    expect(result.current.value.title).toBe('Untitled work order')
  })

  it('creates only one draft for concurrent attachments', async () => {
    let finish!: (w: ReturnType<typeof makeWorkOrder>) => void
    api.create.mockReturnValue(new Promise((r) => { finish = r }))
    api.upload.mockResolvedValue([makeAttachment()])
    const { result } = setup()
    await act(async () => {
      void result.current.queue.add([file('a.txt')])
      void result.current.queue.add([file('b.txt')])
    })
    await act(async () => finish(makeWorkOrder({ id: 'd' })))
    await waitFor(() => expect(api.upload).toHaveBeenCalled())
    expect(api.create).toHaveBeenCalledTimes(1)
  })

  it('does not create a draft before a target is chosen', async () => {
    const { result } = setup(null, emptyWorkOrder())
    await act(() => result.current.queue.add([file()]))
    expect(api.create).not.toHaveBeenCalled()
    expect(result.current.queue.pending).toHaveLength(1)
  })

  it('reports a failed draft creation and leaves the files waiting', async () => {
    api.create.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['title: nope'] }))
    const { result } = setup()
    await act(() => result.current.queue.add([file()]))
    expect(toast.error).toHaveBeenCalledWith('Could not create a draft for the files', expect.anything())
    expect(result.current.errors.fields.title).toBe('nope')
    expect(api.upload).not.toHaveBeenCalled()
    expect(result.current.saved).toBeNull()
  })

  it('uses the existing work order when editing instead of creating a draft', async () => {
    api.upload.mockResolvedValue([makeAttachment({ id: 'new' })])
    const { result } = setup(makeWorkOrder({ id: 'w1' }))
    await act(() => result.current.queue.add([file()]))
    expect(api.create).not.toHaveBeenCalled()
    expect(api.upload).toHaveBeenCalledWith('w1', expect.any(Array))
  })
})
