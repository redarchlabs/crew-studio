import { ATTACHMENT_LIMITS } from '../../../api/types'

// Pure helpers for input files: naming pasted images, client-side limit checks and display formatting.
// The server decides the real type from the content; these checks only give early, clear messages.

/** holder names what the files go with in messages ("a work order" unless set) */
export type Limits = { maxFileBytes: number; maxFiles: number; maxTotalBytes: number; holder?: string }

/** Minimal shape of a file for limit checks (File satisfies it). */
export type FileInfo = { name: string; size: number; type: string }

export type Rejection = { name: string; reason: string }

export type LimitCheck<F extends FileInfo> = { accepted: F[]; rejected: Rejection[] }

const IMAGE_EXT: Record<string, string> = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/gif': 'gif', 'image/webp': 'webp' }

/** Extensions the server accepts (images, PDF, text formats, Office documents). */
const ALLOWED_EXTENSIONS = new Set([
  'png', 'jpg', 'jpeg', 'gif', 'webp',
  'pdf',
  'txt', 'md', 'markdown', 'csv', 'json', 'xml', 'yaml', 'yml', 'html', 'htm', 'log',
  'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx',
])

const ALLOWED_MIME_PREFIXES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'application/pdf', 'text/']

const pad = (n: number) => String(n).padStart(2, '0')

/** "pasted-20261003-142501.png" in local time. */
export function pastedImageName(at: Date, contentType = 'image/png'): string {
  const ext = IMAGE_EXT[contentType] ?? 'png'
  const date = `${at.getFullYear()}${pad(at.getMonth() + 1)}${pad(at.getDate())}`
  const time = `${pad(at.getHours())}${pad(at.getMinutes())}${pad(at.getSeconds())}`
  return `pasted-${date}-${time}.${ext}`
}

/** Adds "-2", "-3"… before the extension until the name is not taken. */
export function uniqueName(name: string, taken: ReadonlySet<string>): string {
  if (!taken.has(name)) return name
  const dot = name.lastIndexOf('.')
  const stem = dot > 0 ? name.slice(0, dot) : name
  const ext = dot > 0 ? name.slice(dot) : ''
  for (let i = 2; ; i++) {
    const candidate = `${stem}-${i}${ext}`
    if (!taken.has(candidate)) return candidate
  }
}

/** Minimal clipboard item shape (DataTransferItem satisfies it). */
export type ClipboardItemLike = { kind: string; type: string; getAsFile: () => File | null }

/** Image files from a paste event, renamed to pasted-<time>.<ext> (clipboard images are all called "image.png"). */
export function clipboardImages(items: readonly ClipboardItemLike[], at: Date, taken: ReadonlySet<string>): File[] {
  const names = new Set(taken)
  const out: File[] = []
  for (const item of items) {
    if (item.kind !== 'file' || !item.type.startsWith('image/')) continue
    const file = item.getAsFile()
    if (!file) continue
    const name = uniqueName(pastedImageName(at, item.type), names)
    names.add(name)
    out.push(new File([file], name, { type: item.type, lastModified: at.getTime() }))
  }
  return out
}

export function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : ''
}

/** Best-effort type check by extension or MIME type. */
export function isAllowedFile(file: FileInfo): boolean {
  if (ALLOWED_EXTENSIONS.has(extensionOf(file.name))) return true
  return ALLOWED_MIME_PREFIXES.some((p) => file.type.startsWith(p))
}

/**
 * Splits incoming files into accepted and rejected against the per-file, per-order count and total-size limits,
 * given the sizes of files already attached (or uploading). Files are taken in order until a limit is hit.
 */
export function checkAttachmentLimits<F extends FileInfo>(
  existingSizes: readonly number[],
  incoming: readonly F[],
  limits: Limits = ATTACHMENT_LIMITS,
): LimitCheck<F> {
  let count = existingSizes.length
  let total = existingSizes.reduce((a, b) => a + b, 0)
  const accepted: F[] = []
  const rejected: Rejection[] = []
  for (const file of incoming) {
    const reason = rejectionReason(file, count, total, limits)
    if (reason) {
      rejected.push({ name: file.name, reason })
      continue
    }
    accepted.push(file)
    count += 1
    total += file.size
  }
  return { accepted, rejected }
}

function rejectionReason(file: FileInfo, count: number, total: number, limits: Limits): string | null {
  if (file.size === 0) return 'the file is empty'
  if (!isAllowedFile(file)) return 'this file type is not supported (images, PDF, text, Word, Excel and PowerPoint only)'
  if (file.size > limits.maxFileBytes) return `it is ${formatBytes(file.size)}; the limit is ${formatBytes(limits.maxFileBytes)} per file`
  if (count >= limits.maxFiles) return `${limits.holder ?? 'a work order'} can hold at most ${limits.maxFiles} files`
  if (total + file.size > limits.maxTotalBytes) return `the files would total more than ${formatBytes(limits.maxTotalBytes)}`
  return null
}

/** "1.4 MB", "820 KB", "12 B". */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || Number.isNaN(bytes) || bytes < 0) return '—'
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value >= 10 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`
}

export function isImageType(contentType: string): boolean {
  return Object.hasOwn(IMAGE_EXT, contentType)
}

const TEXT_TYPES = ['application/json', 'application/xml', 'application/yaml', 'application/x-yaml', 'application/x-ndjson', 'application/jsonl']

/** Text formats worth previewing inline. */
export function isTextType(contentType: string, filename = ''): boolean {
  const base = contentType.split(';')[0].trim()
  if (base.startsWith('text/') || TEXT_TYPES.includes(base)) return true
  return ['md', 'txt', 'log', 'csv', 'json', 'jsonl', 'yaml', 'yml', 'xml'].includes(extensionOf(filename))
}

/** Markdown files, which the preview renders instead of showing as plain text. */
export function isMarkdownType(contentType: string, filename = ''): boolean {
  const base = contentType.split(';')[0].trim().toLowerCase()
  if (base === 'text/markdown' || base === 'text/x-markdown') return true
  return ['md', 'markdown'].includes(extensionOf(filename))
}

/** Largest file fetched for an inline text preview. */
export const TEXT_PREVIEW_MAX_BYTES = 512 * 1024
