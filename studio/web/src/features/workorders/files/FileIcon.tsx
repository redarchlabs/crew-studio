import { File, FileBraces, FileImage, FileSpreadsheet, FileText, Presentation, ScrollText, type LucideIcon } from 'lucide-react'
import { extensionOf } from './attachments'

function iconFor(contentType: string, filename: string): LucideIcon {
  const ext = extensionOf(filename)
  if (contentType.startsWith('image/')) return FileImage
  if (['xls', 'xlsx', 'csv'].includes(ext) || contentType.includes('spreadsheet')) return FileSpreadsheet
  if (['ppt', 'pptx'].includes(ext) || contentType.includes('presentation')) return Presentation
  if (['json', 'jsonl', 'xml', 'yaml', 'yml', 'html', 'htm'].includes(ext) || contentType.includes('json')) return FileBraces
  if (ext === 'log') return ScrollText
  if (contentType.startsWith('text/') || contentType === 'application/pdf' || ['md', 'txt', 'pdf', 'doc', 'docx'].includes(ext)) return FileText
  return File
}

export function FileIcon({ contentType, filename, className = 'size-4' }: { contentType: string; filename: string; className?: string }) {
  const Icon = iconFor(contentType, filename)
  return <Icon className={`shrink-0 text-muted ${className}`} aria-hidden="true" />
}
