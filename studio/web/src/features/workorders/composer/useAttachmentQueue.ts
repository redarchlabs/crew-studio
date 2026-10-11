import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { errorMessage } from '../../../api/client'
import { workOrdersApi } from '../../../api/endpoints'
import type { Attachment } from '../../../api/types'
import { notifyError } from '../../../lib/notify'
import { checkAttachmentLimits, isImageType } from '../files/attachments'

export type PendingStatus = 'waiting' | 'uploading' | 'error'

export type PendingFile = {
  localId: string
  file: File
  /** Object URL for image thumbnails before upload */
  previewUrl: string | null
  status: PendingStatus
  error: string | null
}

export type AttachmentQueue = {
  uploaded: Attachment[]
  pending: PendingFile[]
  removing: ReadonlySet<string>
  takenNames: ReadonlySet<string>
  add: (files: File[]) => Promise<void>
  /** Uploads every waiting file to the work order; resolves true when nothing is left pending */
  flush: (workOrderId: string) => Promise<boolean>
  retry: (localId: string) => void
  removePending: (localId: string) => void
  removeUploaded: (attachment: Attachment) => Promise<void>
  replaceUploaded: (attachments: Attachment[]) => void
}

type QueueOptions = {
  workOrderId: string | null
  initial: readonly Attachment[]
  /** Creates the draft if needed; returns its id, or null when it cannot be created yet */
  ensureDraft: () => Promise<string | null>
}

let nextLocalId = 0

/**
 * Input files for a work order: files already stored plus a local queue. Uploads need a work order id, so files
 * wait until a draft exists (the composer creates one the first time a file is attached, when it can).
 */
export function useAttachmentQueue({ workOrderId, initial, ensureDraft }: QueueOptions): AttachmentQueue {
  const [uploaded, setUploaded] = useState<Attachment[]>(() => [...initial])
  const [pending, setPendingState] = useState<PendingFile[]>([])
  const [removing, setRemoving] = useState<ReadonlySet<string>>(new Set())
  const pendingRef = useRef<PendingFile[]>([])
  const uploadedRef = useRef<Attachment[]>(uploaded)
  uploadedRef.current = uploaded
  const inflight = useRef(new Map<string, Promise<void>>())
  const ensureRef = useRef(ensureDraft)
  ensureRef.current = ensureDraft

  const updatePending = useCallback((fn: (prev: PendingFile[]) => PendingFile[]) => {
    pendingRef.current = fn(pendingRef.current)
    setPendingState(pendingRef.current)
  }, [])

  // Release thumbnails on unmount
  useEffect(() => () => pendingRef.current.forEach((p) => p.previewUrl && URL.revokeObjectURL(p.previewUrl)), [])

  const uploadOne = useCallback(
    (id: string, item: PendingFile): Promise<void> => {
      updatePending((prev) => prev.map((p) => (p.localId === item.localId ? { ...p, status: 'uploading', error: null } : p)))
      const job = workOrdersApi
        .upload(id, [item.file])
        .then((stored) => {
          setUploaded((prev) => [...prev, ...stored])
          updatePending((prev) => prev.filter((p) => p.localId !== item.localId))
          if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
        })
        .catch((err: unknown) => {
          updatePending((prev) => prev.map((p) => (p.localId === item.localId ? { ...p, status: 'error', error: errorMessage(err) } : p)))
        })
        .finally(() => inflight.current.delete(item.localId))
      inflight.current.set(item.localId, job)
      return job
    },
    [updatePending],
  )

  const flush = useCallback(
    async (id: string) => {
      const waiting = pendingRef.current.filter((p) => p.status === 'waiting' && !inflight.current.has(p.localId))
      await Promise.all(waiting.map((p) => uploadOne(id, p)))
      await Promise.allSettled([...inflight.current.values()])
      return pendingRef.current.length === 0
    },
    [uploadOne],
  )

  const add = useCallback(
    async (files: File[]) => {
      const sizes = [...uploadedRef.current.map((a) => a.size_bytes), ...pendingRef.current.map((p) => p.file.size)]
      const { accepted, rejected } = checkAttachmentLimits(sizes, files)
      if (rejected.length > 0) {
        toast.error(rejected.length === 1 ? `${rejected[0].name} was not attached` : `${rejected.length} files were not attached`, {
          description: rejected.map((r) => `${r.name}: ${r.reason}`).join('\n'),
        })
      }
      if (accepted.length === 0) return
      const items: PendingFile[] = accepted.map((file) => ({
        localId: `local-${++nextLocalId}`,
        file,
        previewUrl: isImageType(file.type) ? URL.createObjectURL(file) : null,
        status: 'waiting',
        error: null,
      }))
      updatePending((prev) => [...prev, ...items])
      const id = workOrderId ?? (await ensureRef.current())
      if (id) await flush(id)
    },
    [workOrderId, flush, updatePending],
  )

  const retry = useCallback(
    (localId: string) => {
      updatePending((prev) => prev.map((p) => (p.localId === localId ? { ...p, status: 'waiting', error: null } : p)))
      if (workOrderId) void flush(workOrderId)
    },
    [workOrderId, flush, updatePending],
  )

  const removePending = useCallback(
    (localId: string) => {
      const item = pendingRef.current.find((p) => p.localId === localId)
      if (!item || item.status === 'uploading') return
      if (item.previewUrl) URL.revokeObjectURL(item.previewUrl)
      updatePending((prev) => prev.filter((p) => p.localId !== localId))
    },
    [updatePending],
  )

  const removeUploaded = useCallback(
    async (attachment: Attachment) => {
      if (!workOrderId) return
      setRemoving((prev) => new Set([...prev, attachment.id]))
      try {
        await workOrdersApi.removeAttachment(workOrderId, attachment.id)
        setUploaded((prev) => prev.filter((a) => a.id !== attachment.id))
      } catch (err) {
        notifyError(`Could not remove ${attachment.filename}`, err)
      } finally {
        setRemoving((prev) => new Set([...prev].filter((x) => x !== attachment.id)))
      }
    },
    [workOrderId],
  )

  const takenNames = useMemo(() => new Set([...uploaded.map((a) => a.filename), ...pending.map((p) => p.file.name)]), [uploaded, pending])

  return { uploaded, pending, removing, takenNames, add, flush, retry, removePending, removeUploaded, replaceUploaded: setUploaded }
}
