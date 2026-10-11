import { useEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent } from 'react'
import { Paperclip, X } from 'lucide-react'
import { IconButton } from '../../components/Button'
import { checkAttachmentLimits, clipboardImages, formatBytes, isImageType, type Limits } from '../workorders/files/attachments'
import { FileIcon } from '../workorders/files/FileIcon'

/** What may go with a chat message (the server's limits in api/chatfiles.go) */
export const CHAT_FILE_LIMITS: Limits = { maxFiles: 4, maxFileBytes: 10 * 1024 * 1024, maxTotalBytes: 20 * 1024 * 1024, holder: 'a message' }
export const CHAT_FILE_ACCEPT = 'image/png,image/jpeg,image/gif,image/webp,application/pdf'
const CHAT_TYPES = new Set(CHAT_FILE_ACCEPT.split(','))
const CHAT_EXTENSIONS = /\.(png|jpe?g|gif|webp|pdf)$/i

/** A chat message only carries images and PDFs (what models can look at). */
export function chatFileReason(file: File): string | null {
  return CHAT_TYPES.has(file.type) || CHAT_EXTENSIONS.test(file.name) ? null : 'only images and PDFs can go with a message'
}

type Options = {
  limits: Limits
  /** Extra check for each file; returns why it is refused */
  refuse?: (file: File) => string | null
}

export type PendingFiles = ReturnType<typeof usePendingFiles>

/** Files waiting to go with a message or feedback: pasted screenshots, dropped files and picked files. */
export function usePendingFiles({ limits, refuse }: Options) {
  const [files, setFiles] = useState<File[]>([])
  const [notice, setNotice] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)

  const add = (incoming: readonly File[]) => {
    const refused = incoming.flatMap((f) => {
      const reason = refuse?.(f)
      return reason ? [`${f.name}: ${reason}`] : []
    })
    const candidates = incoming.filter((f) => !refuse?.(f))
    const { accepted, rejected } = checkAttachmentLimits(files.map((f) => f.size), candidates, limits)
    setFiles((prev) => [...prev, ...accepted])
    const problems = [...refused, ...rejected.map((r) => `${r.name}: ${r.reason}`)]
    setNotice(problems.length > 0 ? `Not added: ${problems.join('; ')}.` : null)
  }

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const items = Array.from(e.clipboardData.items)
    // Office apps put an image rendition next to copied text; only attach when there is no text to paste
    if (items.some((i) => i.kind === 'string' && i.type === 'text/plain')) return
    const images = clipboardImages(items, new Date(), new Set(files.map((f) => f.name)))
    if (images.length === 0) return
    e.preventDefault()
    add(images)
  }

  const dragProps = {
    onDragOver: (e: DragEvent) => {
      if (!e.dataTransfer.types.includes('Files')) return
      e.preventDefault()
      setDragging(true)
    },
    onDragLeave: (e: DragEvent) => {
      if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false)
    },
    onDrop: (e: DragEvent) => {
      if (e.dataTransfer.files.length === 0) return
      e.preventDefault()
      setDragging(false)
      add(Array.from(e.dataTransfer.files))
    },
  }

  return {
    files,
    notice,
    dragging,
    add,
    onPaste,
    dragProps,
    remove: (index: number) => setFiles((prev) => prev.filter((_, i) => i !== index)),
    clear: () => {
      setFiles([])
      setNotice(null)
    },
  }
}

/** The paperclip button that picks files. */
export function AttachButton({ pending, accept, disabled }: { pending: PendingFiles; accept: string; disabled?: boolean }) {
  const input = useRef<HTMLInputElement>(null)
  return (
    <>
      <IconButton label="Attach files" icon={<Paperclip className="size-4" />} disabled={disabled} onClick={() => input.current?.click()} />
      <input
        ref={input}
        type="file"
        multiple
        accept={accept}
        className="hidden"
        data-testid="attach-input"
        onChange={(e) => {
          const picked = Array.from(e.target.files ?? [])
          e.target.value = ''
          if (picked.length > 0) pending.add(picked)
        }}
      />
    </>
  )
}

function Chip({ file, onRemove, disabled }: { file: File; onRemove: () => void; disabled?: boolean }) {
  const image = isImageType(file.type)
  const url = useMemo(() => (image ? URL.createObjectURL(file) : null), [file, image])
  useEffect(() => () => { if (url) URL.revokeObjectURL(url) }, [url])
  return (
    <li className="flex min-w-0 items-center gap-2 rounded-md border border-line bg-raised p-1 pr-1.5">
      <span className="flex size-8 shrink-0 items-center justify-center overflow-hidden rounded bg-panel-2">
        {url ? <img src={url} alt="" className="size-full object-cover" /> : <FileIcon contentType={file.type} filename={file.name} className="size-4" />}
      </span>
      <span className="min-w-0">
        <span className="block max-w-[180px] truncate text-[12px]" title={file.name}>{file.name}</span>
        <span className="block font-mono text-[10.5px] text-faint">{formatBytes(file.size)}</span>
      </span>
      <IconButton label={`Remove ${file.name}`} tone="danger" icon={<X className="size-3.5" />} onClick={onRemove} disabled={disabled} />
    </li>
  )
}

/** The files waiting to be sent, and why any were not added. */
export function PendingFileList({ pending, disabled }: { pending: PendingFiles; disabled?: boolean }) {
  if (pending.files.length === 0 && !pending.notice) return null
  return (
    <div className="flex flex-col gap-1">
      {pending.files.length > 0 && (
        <ul className="flex flex-wrap gap-1.5" aria-label="Files to send">
          {pending.files.map((f, i) => <Chip key={`${f.name}-${i}`} file={f} disabled={disabled} onRemove={() => pending.remove(i)} />)}
        </ul>
      )}
      {pending.notice && <p className="text-[11.5px] text-warn" role="alert">{pending.notice}</p>}
    </div>
  )
}
