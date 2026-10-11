import { CircleAlert, Clock, LoaderCircle, RotateCcw, X } from 'lucide-react'
import { attachmentsApi } from '../../../api/endpoints'
import type { Attachment } from '../../../api/types'
import { ATTACHMENT_LIMITS } from '../../../api/types'
import { IconButton } from '../../../components/Button'
import { formatBytes, isImageType } from '../files/attachments'
import { FileIcon } from '../files/FileIcon'
import type { AttachmentQueue, PendingFile } from './useAttachmentQueue'

type TileProps = {
  name: string
  size: number
  contentType: string
  thumbUrl: string | null
  status?: PendingFile['status']
  error?: string | null
  busy?: boolean
  waitingHint?: string
  onRemove?: () => void
  onRetry?: () => void
}

function Tile({ name, size, contentType, thumbUrl, status, error, busy, waitingHint, onRemove, onRetry }: TileProps) {
  const failed = status === 'error'
  return (
    <li className={`flex min-w-0 items-center gap-2 rounded-md border bg-raised p-1.5 ${failed ? 'border-danger/60' : 'border-line'}`}>
      <div className="relative flex size-11 shrink-0 items-center justify-center overflow-hidden rounded bg-panel-2">
        {thumbUrl ? <img src={thumbUrl} alt="" className="size-full object-cover" /> : <FileIcon contentType={contentType} filename={name} className="size-5" />}
        {(status === 'uploading' || busy) && (
          <span className="absolute inset-0 flex items-center justify-center bg-bg/60"><LoaderCircle className="size-4 animate-spin text-accent" /></span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-[12.5px]" title={name}>{name}</div>
        <div className="truncate font-mono text-[10.5px] text-faint">
          {formatBytes(size)}
          {status === 'uploading' && <span className="text-accent"> · uploading…</span>}
          {status === 'waiting' && <span className="inline-flex items-center gap-0.5 text-warn"> · <Clock className="size-3" /> {waitingHint ?? 'waiting'}</span>}
        </div>
        {failed && error && <div className="flex items-center gap-1 text-[11px] text-danger" role="alert"><CircleAlert className="size-3 shrink-0" /><span className="truncate" title={error}>{error}</span></div>}
      </div>
      {failed && onRetry && <IconButton label={`Retry ${name}`} icon={<RotateCcw className="size-3.5" />} onClick={onRetry} />}
      {onRemove && <IconButton label={`Remove ${name}`} tone="danger" icon={<X className="size-3.5" />} onClick={onRemove} disabled={status === 'uploading' || busy} />}
    </li>
  )
}

type AttachmentTrayProps = {
  queue: AttachmentQueue
  /** Why waiting files have not uploaded yet (e.g. no target chosen) */
  waitingHint?: string
  readOnly?: boolean
}

/** Input files: thumbnails for images, icon + name + size for the rest, with upload state and remove buttons. */
export function AttachmentTray({ queue, waitingHint, readOnly }: AttachmentTrayProps) {
  const { uploaded, pending } = queue
  if (uploaded.length === 0 && pending.length === 0) return null
  const total = uploaded.reduce((a, x) => a + x.size_bytes, 0) + pending.reduce((a, x) => a + x.file.size, 0)
  const count = uploaded.length + pending.length
  return (
    <div className="mt-1 flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between text-[11.5px] text-faint">
        <span className="eyebrow">Input files</span>
        <span className="font-mono">
          {count}/{ATTACHMENT_LIMITS.maxFiles} files · {formatBytes(total)} of {formatBytes(ATTACHMENT_LIMITS.maxTotalBytes)}
        </span>
      </div>
      <ul className="grid gap-1.5 sm:grid-cols-2">
        {uploaded.map((a: Attachment) => (
          <Tile
            key={a.id}
            name={a.filename}
            size={a.size_bytes}
            contentType={a.content_type}
            thumbUrl={isImageType(a.content_type) ? attachmentsApi.contentUrl(a.id) : null}
            busy={queue.removing.has(a.id)}
            onRemove={readOnly ? undefined : () => void queue.removeUploaded(a)}
          />
        ))}
        {pending.map((p) => (
          <Tile
            key={p.localId}
            name={p.file.name}
            size={p.file.size}
            contentType={p.file.type}
            thumbUrl={p.previewUrl}
            status={p.status}
            error={p.error}
            waitingHint={waitingHint}
            onRemove={() => queue.removePending(p.localId)}
            onRetry={() => queue.retry(p.localId)}
          />
        ))}
      </ul>
    </div>
  )
}
