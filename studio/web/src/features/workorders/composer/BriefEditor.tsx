import { useId, useRef, useState, type ClipboardEvent, type DragEvent, type ReactNode } from 'react'
import { Eye, Paperclip, Pencil, Upload } from 'lucide-react'
import { Button } from '../../../components/Button'
import { Markdown } from '../../../components/Markdown'
import { clipboardImages } from '../files/attachments'

type BriefEditorProps = {
  value: string
  onChange: (value: string) => void
  label?: ReactNode
  error?: string
  rows?: number
  placeholder?: string
  /** Enables pasting images, dropping files and the "Attach files" button */
  onFiles?: (files: File[]) => void
  /** Names already used, so pasted images get unique names */
  takenNames?: ReadonlySet<string>
  /** Rendered under the editor (the attachment tray) */
  footer?: ReactNode
  hint?: ReactNode
}

const ACCEPT = 'image/png,image/jpeg,image/gif,image/webp,application/pdf,text/*,.md,.csv,.json,.xml,.yaml,.yml,.html,.doc,.docx,.xls,.xlsx,.ppt,.pptx'

const MAX_BRIEF = 100_000

/**
 * Markdown brief with a Write/Preview toggle. Text pastes normally; a clipboard image (with no text alongside it)
 * becomes an attachment named pasted-<time>.png. Files can also be dropped onto the editor or picked.
 */
export function BriefEditor({ value, onChange, label = 'Brief', error, rows = 12, placeholder, onFiles, takenNames = new Set(), footer, hint }: BriefEditorProps) {
  const id = useId()
  const [mode, setMode] = useState<'write' | 'preview'>('write')
  const [dragging, setDragging] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (!onFiles) return
    const items = Array.from(e.clipboardData.items)
    // Office apps put an image rendition next to copied text; only attach when there is no text to paste
    if (items.some((i) => i.kind === 'string' && i.type === 'text/plain')) return
    const images = clipboardImages(items, new Date(), takenNames)
    if (images.length === 0) return
    e.preventDefault()
    onFiles(images)
  }

  const dragProps = onFiles
    ? {
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
          onFiles(Array.from(e.dataTransfer.files))
        },
      }
    : {}

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <label htmlFor={id} className="text-[12px] font-medium text-muted">{label}</label>
        <div className="flex items-center gap-1">
          {onFiles && (
            <>
              <Button size="sm" variant="ghost" icon={<Paperclip className="size-3.5" />} onClick={() => fileInput.current?.click()}>Attach files</Button>
              <input
                ref={fileInput}
                type="file"
                multiple
                accept={ACCEPT}
                className="hidden"
                onChange={(e) => {
                  const files = Array.from(e.target.files ?? [])
                  e.target.value = ''
                  if (files.length > 0) onFiles(files)
                }}
              />
            </>
          )}
          <div role="tablist" aria-label="Brief mode" className="inline-flex rounded-md border border-line bg-raised p-0.5">
            {(['write', 'preview'] as const).map((m) => (
              <button key={m} type="button" role="tab" aria-selected={mode === m} onClick={() => setMode(m)} className={`inline-flex items-center gap-1 rounded px-2 py-0.5 text-[12px] ${mode === m ? 'bg-panel-2 text-fg' : 'text-muted hover:text-fg'}`}>
                {m === 'write' ? <Pencil className="size-3" /> : <Eye className="size-3" />}
                {m === 'write' ? 'Write' : 'Preview'}
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className={`relative rounded-md ${dragging ? 'outline-2 outline-offset-2 outline-dashed outline-accent' : ''}`} {...dragProps}>
        {mode === 'write' ? (
          <textarea
            id={id}
            rows={rows}
            className="field-input min-h-40 resize-y font-mono text-[12.5px] leading-relaxed"
            aria-invalid={Boolean(error)}
            value={value}
            maxLength={MAX_BRIEF}
            placeholder={placeholder ?? (onFiles ? 'Describe the work. Markdown works. Paste screenshots or drop files here.' : 'Describe the work. Markdown works.')}
            onChange={(e) => onChange(e.target.value)}
            onPaste={onPaste}
          />
        ) : (
          <div className="min-h-40 overflow-auto rounded-md border border-line bg-raised px-3 py-2" style={{ maxHeight: `${rows * 1.9}rem` }}>
            {value.trim() ? <Markdown>{value}</Markdown> : <span className="text-faint">Nothing to preview yet.</span>}
          </div>
        )}
        {dragging && (
          <div className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-md bg-bg/70 text-[13px] font-medium text-accent">
            <Upload className="mr-2 size-4" /> Drop files to attach
          </div>
        )}
      </div>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        {error ? <div className="text-[12px] text-danger" role="alert">{error}</div> : <div className="text-[11.5px] text-faint">{hint}</div>}
        <span className={`font-mono text-[10.5px] ${value.length > MAX_BRIEF * 0.9 ? 'text-warn' : 'text-faint'}`}>{value.length.toLocaleString()} / {MAX_BRIEF.toLocaleString()}</span>
      </div>
      {footer}
    </div>
  )
}
