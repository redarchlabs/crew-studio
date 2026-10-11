import { useEffect, useState } from 'react'
import { Download, Eye, EyeOff } from 'lucide-react'
import { attachmentsApi } from '../../../api/endpoints'
import { buttonClass, Button } from '../../../components/Button'
import { ImageLightbox, type LightboxImage } from '../../../components/ImageLightbox'
import { Markdown } from '../../../components/Markdown'
import { ErrorState, Loading } from '../../../components/States'
import { dateTime } from '../../../lib/format'
import { formatBytes, isImageType, isMarkdownType, isTextType, TEXT_PREVIEW_MAX_BYTES } from './attachments'
import { FileIcon } from './FileIcon'

/** The fields every file view needs; input files from a run spec have no hash. */
export type FileItem = {
  id: string
  filename: string
  content_type: string
  size_bytes: number
  sha256?: string
  created_at?: string
}

const previewBox = 'max-h-96 overflow-auto rounded-md border border-line bg-raised px-2.5 py-2'

/** Fetches a small text file through the authenticated content URL. Markdown is rendered, with its source a click away. */
function TextPreview({ file }: { file: FileItem }) {
  const [state, setState] = useState<{ text?: string; error?: unknown }>({})
  const markdown = isMarkdownType(file.content_type, file.filename)
  const [source, setSource] = useState(false)
  useEffect(() => {
    const ctrl = new AbortController()
    fetch(attachmentsApi.contentUrl(file.id), { credentials: 'same-origin', signal: ctrl.signal })
      .then(async (res) => {
        if (!res.ok) throw new Error(`The server answered ${res.status}.`)
        setState({ text: await res.text() })
      })
      .catch((error: unknown) => {
        if (!ctrl.signal.aborted) setState({ error })
      })
    return () => ctrl.abort()
  }, [file.id])
  if (state.error) return <ErrorState error={state.error} title="Could not load the preview" />
  if (state.text === undefined) return <Loading label="Loading preview…" />
  const sourceView = <pre className={`${previewBox} whitespace-pre-wrap break-words font-mono text-[11.5px] leading-relaxed`}>{state.text || '(empty file)'}</pre>
  if (!markdown || !state.text) return sourceView
  return (
    <div>
      <div className="mb-1.5 flex justify-end">
        <Button size="sm" variant="ghost" aria-pressed={source} onClick={() => setSource((s) => !s)}>
          {source ? 'Rendered' : 'Source'}
        </Button>
      </div>
      {source ? sourceView : <div className={previewBox}><Markdown className="text-[13px]">{state.text}</Markdown></div>}
    </div>
  )
}

function FileRow({ file, onOpenImage }: { file: FileItem; onOpenImage?: () => void }) {
  const [preview, setPreview] = useState(false)
  const image = isImageType(file.content_type)
  const previewable = !image && isTextType(file.content_type, file.filename) && file.size_bytes <= TEXT_PREVIEW_MAX_BYTES
  const meta = [formatBytes(file.size_bytes), file.content_type, file.created_at ? dateTime(file.created_at) : null].filter(Boolean).join(' · ')
  return (
    <li className="border-b border-line last:border-0">
      <div className="flex items-center gap-2.5 px-3 py-2" title={file.sha256 ? `sha256 ${file.sha256}` : undefined}>
        {image ? (
          <button type="button" onClick={onOpenImage} className="size-9 shrink-0 overflow-hidden rounded bg-panel-2" aria-label={`Open ${file.filename}`}>
            <img src={attachmentsApi.contentUrl(file.id)} alt="" loading="lazy" className="size-full object-cover" />
          </button>
        ) : (
          <span className="flex size-9 shrink-0 items-center justify-center rounded bg-panel-2"><FileIcon contentType={file.content_type} filename={file.filename} /></span>
        )}
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-[12px]" title={file.filename}>{file.filename}</div>
          <div className="truncate text-[11px] text-faint">{meta}{file.sha256 && <span className="hidden sm:inline"> · sha256 {file.sha256.slice(0, 12)}…</span>}</div>
        </div>
        {previewable && (
          <Button size="sm" variant="ghost" icon={preview ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />} onClick={() => setPreview((p) => !p)}>
            <span className="hidden sm:inline">{preview ? 'Hide' : 'Preview'}</span>
          </Button>
        )}
        <a href={attachmentsApi.contentUrl(file.id, true)} download={file.filename.split('/').pop()} className={buttonClass('ghost', 'sm')} aria-label={`Download ${file.filename}`}>
          <Download className="size-3.5" /><span className="hidden sm:inline">Download</span>
        </a>
      </div>
      {preview && <div className="px-3 pb-3"><TextPreview file={file} /></div>}
    </li>
  )
}

/** Images as a thumbnail gallery (opening a lightbox) and every file as a row with preview and download. */
export function FileList({ files, gallery = true }: { files: readonly FileItem[]; gallery?: boolean }) {
  const [open, setOpen] = useState<number | null>(null)
  const images = files.filter((f) => isImageType(f.content_type))
  const lightbox: LightboxImage[] = images.map((f) => ({ src: attachmentsApi.contentUrl(f.id), title: f.filename, downloadUrl: attachmentsApi.contentUrl(f.id, true) }))
  const openFile = (f: FileItem) => setOpen(images.findIndex((x) => x.id === f.id))
  return (
    <div className="flex flex-col gap-2">
      {gallery && images.length > 0 && (
        <ul className="grid grid-cols-3 gap-2 sm:grid-cols-4 lg:grid-cols-6">
          {images.map((f, i) => (
            <li key={f.id}>
              <button type="button" onClick={() => setOpen(i)} className="group block w-full overflow-hidden rounded-md border border-line bg-panel-2" title={f.filename}>
                <img src={attachmentsApi.contentUrl(f.id)} alt={f.filename} loading="lazy" className="aspect-square w-full object-cover transition-transform group-hover:scale-105" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <ul className="overflow-hidden rounded-lg border border-line bg-panel">
        {files.map((f) => <FileRow key={f.id} file={f} onOpenImage={() => openFile(f)} />)}
      </ul>
      {open !== null && <ImageLightbox images={lightbox} index={open} onIndex={setOpen} onClose={() => setOpen(null)} />}
    </div>
  )
}
