import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyText, downloadText, safeFilename } from './download'

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('safeFilename', () => {
  it.each([
    ['Weekly Report', 'weekly-report'],
    ['  --Hello, World!--  ', 'hello-world'],
    ['a/b\\c:d', 'a-b-c-d'],
    ['UPPER_case 123', 'upper-case-123'],
    ['', 'output'],
    ['!!!', 'output'],
    ['日本語', 'output'],
    ['😀 smile', 'smile'],
  ])('%j -> %j', (input, out) => expect(safeFilename(input)).toBe(out))
})

describe('downloadText', () => {
  it('clicks a temporary anchor with the filename and revokes the URL later', () => {
    vi.useFakeTimers()
    const create = vi.fn(() => 'blob:fake')
    const revoke = vi.fn()
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke }))
    let clicked: HTMLAnchorElement | null = null
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked = this
      expect(document.body.contains(this)).toBe(true)
    })
    downloadText('out.md', '# hi')
    expect(clicked).not.toBeNull()
    const a = clicked as unknown as HTMLAnchorElement
    expect(a.download).toBe('out.md')
    expect(a.getAttribute('href')).toBe('blob:fake')
    expect(document.body.contains(a)).toBe(false)
    const blob = (create.mock.calls[0] as unknown as [Blob])[0]
    expect(blob.type).toBe('text/markdown;charset=utf-8')
    expect(blob.size).toBe(4)
    expect(revoke).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1000)
    expect(revoke).toHaveBeenCalledWith('blob:fake')
  })

  it('honours a custom mime type', () => {
    const create = vi.fn(() => 'blob:x')
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() }))
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    downloadText('a.json', '{}', 'application/json')
    expect((create.mock.calls[0] as unknown as [Blob])[0].type).toBe('application/json;charset=utf-8')
  })
})

describe('copyText', () => {
  it('writes to the clipboard', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    await copyText('hello')
    expect(writeText).toHaveBeenCalledWith('hello')
  })
  it('propagates clipboard failures', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: () => Promise.reject(new Error('denied')) } })
    await expect(copyText('x')).rejects.toThrow('denied')
  })
})
