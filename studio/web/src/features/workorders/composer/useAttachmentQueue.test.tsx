import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { toast } from 'sonner'
import { ApiError } from '../../../api/client'
import { workOrdersApi } from '../../../api/endpoints'
import type { Attachment } from '../../../api/types'
import { imageFile, makeAttachment } from '../testUtils'
import { useAttachmentQueue } from './useAttachmentQueue'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const upload = vi.mocked(workOrdersApi.upload)
const removeAttachment = vi.mocked(workOrdersApi.removeAttachment)

const textFile = (name: string, size = 10) => new File([new Uint8Array(size)], name, { type: 'text/plain' })

let urlCount = 0
beforeEach(() => {
  upload.mockReset()
  removeAttachment.mockReset()
  vi.mocked(toast.error).mockReset()
  urlCount = 0
  URL.createObjectURL = vi.fn(() => `blob:preview-${++urlCount}`)
  URL.revokeObjectURL = vi.fn()
})
afterEach(() => vi.restoreAllMocks())

type Opts = { workOrderId?: string | null; initial?: Attachment[]; ensureDraft?: () => Promise<string | null> }
function setup({ workOrderId = 'wo-1', initial = [], ensureDraft = () => Promise.resolve('wo-1') }: Opts = {}) {
  return renderHook((props: Opts) => useAttachmentQueue({ workOrderId: props.workOrderId ?? workOrderId, initial: props.initial ?? initial, ensureDraft: props.ensureDraft ?? ensureDraft }), { initialProps: {} as Opts })
}

describe('useAttachmentQueue', () => {
  it('starts with the stored attachments', () => {
    const a = makeAttachment()
    const { result } = setup({ initial: [a] })
    expect(result.current.uploaded).toEqual([a])
    expect(result.current.pending).toEqual([])
    expect([...result.current.takenNames]).toEqual(['notes.txt'])
  })

  it('uploads added files to the existing work order and moves them to uploaded', async () => {
    const stored = makeAttachment({ id: 'new', filename: 'a.txt' })
    upload.mockResolvedValue([stored])
    const { result } = setup()
    await act(() => result.current.add([textFile('a.txt')]))
    expect(upload).toHaveBeenCalledWith('wo-1', [expect.objectContaining({ name: 'a.txt' })])
    expect(result.current.uploaded).toEqual([stored])
    expect(result.current.pending).toEqual([])
  })

  it('creates a draft first when there is no work order yet', async () => {
    const ensureDraft = vi.fn().mockResolvedValue('wo-new')
    upload.mockResolvedValue([makeAttachment({ id: 'x' })])
    const { result } = setup({ workOrderId: null, ensureDraft })
    await act(() => result.current.add([textFile('a.txt')]))
    expect(ensureDraft).toHaveBeenCalledTimes(1)
    expect(upload).toHaveBeenCalledWith('wo-new', expect.any(Array))
  })

  it('keeps files waiting when no draft can be created', async () => {
    const { result } = setup({ workOrderId: null, ensureDraft: () => Promise.resolve(null) })
    await act(() => result.current.add([textFile('a.txt')]))
    expect(upload).not.toHaveBeenCalled()
    expect(result.current.pending).toHaveLength(1)
    expect(result.current.pending[0].status).toBe('waiting')
  })

  it('flush uploads waiting files later, and resolves true when nothing is left', async () => {
    upload.mockResolvedValue([makeAttachment({ id: 'x' })])
    const { result } = setup({ workOrderId: null, ensureDraft: () => Promise.resolve(null) })
    await act(() => result.current.add([textFile('a.txt')]))
    let ok = false
    await act(async () => {
      ok = await result.current.flush('wo-9')
    })
    expect(ok).toBe(true)
    expect(upload).toHaveBeenCalledWith('wo-9', expect.any(Array))
    expect(result.current.pending).toEqual([])
  })

  it('flush resolves true immediately with an empty queue', async () => {
    const { result } = setup()
    let ok = false
    await act(async () => {
      ok = await result.current.flush('wo-1')
    })
    expect(ok).toBe(true)
    expect(upload).not.toHaveBeenCalled()
  })

  it('marks a failed upload as error with the message, and flush then reports false', async () => {
    upload.mockRejectedValue(new ApiError(413, { code: 'quota', message: 'Storage quota exceeded' }))
    const { result } = setup()
    await act(() => result.current.add([textFile('big.txt')]))
    expect(result.current.pending[0]).toMatchObject({ status: 'error', error: 'Storage quota exceeded' })
    expect(result.current.uploaded).toEqual([])
    let ok = true
    await act(async () => {
      ok = await result.current.flush('wo-1')
    })
    // error items are not retried by flush and still count as pending
    expect(ok).toBe(false)
    expect(upload).toHaveBeenCalledTimes(1)
  })

  it('retry re-queues a failed file and uploads it', async () => {
    upload.mockRejectedValueOnce(new Error('flaky')).mockResolvedValueOnce([makeAttachment({ id: 'ok', filename: 'a.txt' })])
    const { result } = setup()
    await act(() => result.current.add([textFile('a.txt')]))
    const id = result.current.pending[0].localId
    expect(result.current.pending[0].error).toBe('flaky')
    await act(async () => {
      result.current.retry(id)
    })
    await vi.waitFor(() => expect(result.current.uploaded).toHaveLength(1))
    expect(result.current.pending).toEqual([])
    expect(upload).toHaveBeenCalledTimes(2)
  })

  it('retry without a work order leaves the file waiting', async () => {
    const { result } = setup({ workOrderId: null, ensureDraft: () => Promise.resolve(null) })
    await act(() => result.current.add([textFile('a.txt')]))
    await act(async () => result.current.retry(result.current.pending[0].localId))
    expect(upload).not.toHaveBeenCalled()
    expect(result.current.pending[0].status).toBe('waiting')
  })

  it('removePending drops a waiting or failed file and revokes its preview', async () => {
    const { result } = setup({ workOrderId: null, ensureDraft: () => Promise.resolve(null) })
    await act(() => result.current.add([imageFile('shot.png')]))
    expect(result.current.pending[0].previewUrl).toBe('blob:preview-1')
    act(() => result.current.removePending(result.current.pending[0].localId))
    expect(result.current.pending).toEqual([])
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
  })

  it('removePending ignores unknown ids and files that are uploading', async () => {
    let resolve!: (v: Attachment[]) => void
    upload.mockReturnValue(new Promise((r) => { resolve = r }))
    const { result } = setup()
    let adding!: Promise<void>
    await act(async () => {
      adding = result.current.add([textFile('a.txt')])
    })
    expect(result.current.pending[0].status).toBe('uploading')
    act(() => result.current.removePending(result.current.pending[0].localId))
    act(() => result.current.removePending('nope'))
    expect(result.current.pending).toHaveLength(1)
    await act(async () => {
      resolve([makeAttachment({ id: 'done' })])
      await adding
    })
    expect(result.current.pending).toEqual([])
  })

  it('only creates previews for images, and revokes after upload', async () => {
    upload.mockResolvedValue([makeAttachment()])
    const { result } = setup()
    await act(() => result.current.add([imageFile('a.png'), textFile('b.txt')]))
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
  })

  it('revokes outstanding previews on unmount', async () => {
    const { result, unmount } = setup({ workOrderId: null, ensureDraft: () => Promise.resolve(null) })
    await act(() => result.current.add([imageFile('a.png')]))
    unmount()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:preview-1')
  })

  it('rejects empty and unsupported files with a toast naming them', async () => {
    const { result } = setup()
    await act(() => result.current.add([textFile('empty.txt', 0)]))
    expect(toast.error).toHaveBeenCalledWith('empty.txt was not attached', { description: 'empty.txt: the file is empty' })
    expect(upload).not.toHaveBeenCalled()
    expect(result.current.pending).toEqual([])
  })

  it('summarises several rejections in one toast', async () => {
    const { result } = setup()
    await act(() => result.current.add([textFile('a.txt', 0), new File(['x'], 'run.exe', { type: 'application/x-msdownload' })]))
    expect(toast.error).toHaveBeenCalledTimes(1)
    expect(vi.mocked(toast.error).mock.calls[0][0]).toBe('2 files were not attached')
  })

  it('accepts the valid files of a mixed batch', async () => {
    upload.mockResolvedValue([makeAttachment()])
    const { result } = setup()
    await act(() => result.current.add([textFile('good.txt'), textFile('empty.txt', 0)]))
    expect(upload).toHaveBeenCalledWith('wo-1', [expect.objectContaining({ name: 'good.txt' })])
    expect(toast.error).toHaveBeenCalled()
  })

  it('enforces the file-count limit against stored files', async () => {
    const initial = Array.from({ length: 20 }, (_, i) => makeAttachment({ id: `a${i}`, filename: `f${i}.txt` }))
    const { result } = setup({ initial })
    await act(() => result.current.add([textFile('one-more.txt')]))
    expect(upload).not.toHaveBeenCalled()
    expect(vi.mocked(toast.error).mock.calls[0][1]).toMatchObject({ description: expect.stringContaining('at most 20 files') })
  })

  it('removeUploaded deletes on the server and drops the file', async () => {
    removeAttachment.mockResolvedValue(undefined)
    const a = makeAttachment({ id: 'a' })
    const b = makeAttachment({ id: 'b', filename: 'b.txt' })
    const { result } = setup({ initial: [a, b] })
    await act(() => result.current.removeUploaded(a))
    expect(removeAttachment).toHaveBeenCalledWith('wo-1', 'a')
    expect(result.current.uploaded.map((x) => x.id)).toEqual(['b'])
    expect(result.current.removing.size).toBe(0)
  })

  it('marks a file as removing while the delete is in flight', async () => {
    let done!: () => void
    removeAttachment.mockReturnValue(new Promise<void>((r) => { done = r }))
    const a = makeAttachment({ id: 'a' })
    const { result } = setup({ initial: [a] })
    let p!: Promise<void>
    act(() => {
      p = result.current.removeUploaded(a)
    })
    expect(result.current.removing.has('a')).toBe(true)
    await act(async () => {
      done()
      await p
    })
    expect(result.current.removing.has('a')).toBe(false)
  })

  it('keeps the file and toasts when the delete fails', async () => {
    removeAttachment.mockRejectedValue(new Error('denied'))
    const a = makeAttachment({ id: 'a', filename: 'keep.txt' })
    const { result } = setup({ initial: [a] })
    await act(() => result.current.removeUploaded(a))
    expect(result.current.uploaded).toHaveLength(1)
    expect(toast.error).toHaveBeenCalledWith('Could not remove keep.txt', { description: 'denied' })
    expect(result.current.removing.size).toBe(0)
  })

  it('removeUploaded does nothing without a work order', async () => {
    const a = makeAttachment()
    const { result } = setup({ workOrderId: null, initial: [a] })
    await act(() => result.current.removeUploaded(a))
    expect(removeAttachment).not.toHaveBeenCalled()
    expect(result.current.uploaded).toHaveLength(1)
  })

  it('replaceUploaded swaps the list', () => {
    const { result } = setup({ initial: [makeAttachment()] })
    act(() => result.current.replaceUploaded([makeAttachment({ id: 'z', filename: 'z.txt' })]))
    expect([...result.current.takenNames]).toEqual(['z.txt'])
  })

  it('does not upload the same waiting file twice when flush runs concurrently', async () => {
    let resolve!: (v: Attachment[]) => void
    upload.mockReturnValue(new Promise((r) => { resolve = r }))
    const { result } = setup({ workOrderId: null, ensureDraft: () => Promise.resolve(null) })
    await act(() => result.current.add([textFile('a.txt')]))
    let f1!: Promise<boolean>
    let f2!: Promise<boolean>
    await act(async () => {
      f1 = result.current.flush('wo-1')
      f2 = result.current.flush('wo-1')
    })
    await act(async () => {
      resolve([makeAttachment({ id: 'x' })])
      await Promise.all([f1, f2])
    })
    expect(upload).toHaveBeenCalledTimes(1)
  })
})
