import { describe, expect, it } from 'vitest'
import { checkAttachmentLimits, clipboardImages, formatBytes, isAllowedFile, isMarkdownType, isTextType, pastedImageName, uniqueName } from './attachments'

const LIMITS = { maxFileBytes: 100, maxFiles: 3, maxTotalBytes: 250 }
const f = (name: string, size: number, type = '') => ({ name, size, type })

describe('pastedImageName', () => {
  it('formats local time as YYYYMMDD-HHMMSS', () => {
    expect(pastedImageName(new Date(2026, 9, 3, 14, 5, 9))).toBe('pasted-20261003-140509.png')
  })

  it('uses the image type for the extension', () => {
    expect(pastedImageName(new Date(2026, 0, 1, 0, 0, 0), 'image/jpeg')).toBe('pasted-20260101-000000.jpg')
    expect(pastedImageName(new Date(2026, 0, 1, 0, 0, 0), 'image/x-unknown')).toBe('pasted-20260101-000000.png')
  })
})

describe('uniqueName', () => {
  it('keeps a free name and suffixes taken ones', () => {
    expect(uniqueName('a.png', new Set())).toBe('a.png')
    expect(uniqueName('a.png', new Set(['a.png', 'a-2.png']))).toBe('a-3.png')
    expect(uniqueName('README', new Set(['README']))).toBe('README-2')
  })
})

describe('clipboardImages', () => {
  const item = (type: string, kind = 'file') => ({ kind, type, getAsFile: () => new File(['x'], 'image.png', { type }) })

  it('keeps only image files and renames them uniquely', () => {
    const at = new Date(2026, 9, 3, 9, 0, 0)
    const files = clipboardImages([item('text/plain', 'string'), item('image/png'), item('image/png'), item('application/pdf')], at, new Set(['pasted-20261003-090000.png']))
    expect(files.map((x) => x.name)).toEqual(['pasted-20261003-090000-2.png', 'pasted-20261003-090000-3.png'])
    expect(files[0].type).toBe('image/png')
  })

  it('skips items without a file', () => {
    expect(clipboardImages([{ kind: 'file', type: 'image/png', getAsFile: () => null }], new Date(), new Set())).toEqual([])
  })
})

describe('checkAttachmentLimits', () => {
  it('accepts files within every limit', () => {
    const r = checkAttachmentLimits([], [f('a.png', 10), f('b.pdf', 20)], LIMITS)
    expect(r.accepted.map((x) => x.name)).toEqual(['a.png', 'b.pdf'])
    expect(r.rejected).toEqual([])
  })

  it('rejects oversized, empty and unsupported files with reasons', () => {
    const r = checkAttachmentLimits([], [f('big.png', 101), f('empty.txt', 0), f('tool.exe', 5, 'application/x-msdownload')], LIMITS)
    expect(r.accepted).toEqual([])
    expect(r.rejected.map((x) => x.name)).toEqual(['big.png', 'empty.txt', 'tool.exe'])
    expect(r.rejected[0].reason).toContain('per file')
    expect(r.rejected[2].reason).toContain('not supported')
  })

  it('counts existing files towards the file and total limits', () => {
    const r = checkAttachmentLimits([100, 100], [f('a.md', 40), f('b.md', 40)], LIMITS)
    expect(r.accepted.map((x) => x.name)).toEqual(['a.md'])
    expect(r.rejected[0].reason).toContain('at most 3 files')
  })

  it('rejects a file that would exceed the total size', () => {
    const r = checkAttachmentLimits([100], [f('a.md', 100), f('b.md', 60)], LIMITS)
    expect(r.accepted.map((x) => x.name)).toEqual(['a.md'])
    expect(r.rejected[0].reason).toContain('total')
  })
})

describe('type helpers', () => {
  it('allows by extension or MIME type', () => {
    expect(isAllowedFile(f('Report.DOCX', 1))).toBe(true)
    expect(isAllowedFile(f('noext', 1, 'text/plain'))).toBe(true)
    expect(isAllowedFile(f('archive.zip', 1, 'application/zip'))).toBe(false)
  })

  it('recognises text for previews', () => {
    expect(isTextType('text/markdown; charset=utf-8')).toBe(true)
    expect(isTextType('application/octet-stream', 'console.log')).toBe(true)
    expect(isTextType('image/png', 'a.png')).toBe(false)
  })

  it('formats sizes', () => {
    expect(formatBytes(12)).toBe('12 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(25 << 20)).toBe('25 MB')
    expect(formatBytes(null)).toBe('—')
  })
})

describe('isMarkdownType', () => {
  it('knows markdown by content type or extension', () => {
    expect(isMarkdownType('text/markdown; charset=utf-8')).toBe(true)
    expect(isMarkdownType('text/x-markdown')).toBe(true)
    expect(isMarkdownType('application/octet-stream', 'reports/daily.md')).toBe(true)
    expect(isMarkdownType('text/plain', 'README.markdown')).toBe(true)
    expect(isMarkdownType('text/plain', 'notes.txt')).toBe(false)
    expect(isMarkdownType('application/json', 'data.json')).toBe(false)
  })
})
