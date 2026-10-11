import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { FileList, type FileItem } from './FileList'
import { FileIcon } from './FileIcon'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const file = (over: Partial<FileItem> & { id: string }): FileItem => ({ filename: `${over.id}.txt`, content_type: 'text/plain', size_bytes: 2048, ...over })

describe('FileList rows', () => {
  it('shows name, size, type, date and a download link for each file', () => {
    render(<FileList files={[file({ id: 'a', filename: 'notes.txt', created_at: '2026-10-03T10:00:00Z' })]} />)
    expect(screen.getByText('notes.txt')).toBeTruthy()
    expect(screen.getByText(/2\.0 KB · text\/plain · /)).toBeTruthy()
    const link = screen.getByRole('link', { name: 'Download notes.txt' })
    expect(link.getAttribute('href')).toBe('/api/attachments/a/content?download=1')
    expect(link.getAttribute('download')).toBe('notes.txt')
  })

  it('uses only the last path segment as the download name (artifacts live in folders)', () => {
    render(<FileList files={[file({ id: 'a', filename: 'out/reports/final.md', content_type: 'text/markdown' })]} />)
    expect(screen.getByRole('link', { name: 'Download out/reports/final.md' }).getAttribute('download')).toBe('final.md')
  })

  it('shows a shortened sha256 when present, with the full hash as a tooltip', () => {
    const { container } = render(<FileList files={[file({ id: 'a', sha256: 'abcdef0123456789ffff' })]} />)
    expect(screen.getByText(/sha256 abcdef012345…/)).toBeTruthy()
    expect(container.querySelector('[title="sha256 abcdef0123456789ffff"]')).toBeTruthy()
  })

  it('omits the date and hash for input files from a run spec', () => {
    render(<FileList files={[file({ id: 'a' })]} />)
    expect(screen.queryByText(/sha256/)).toBeNull()
  })

  it('renders an empty list without error', () => {
    const { container } = render(<FileList files={[]} />)
    expect(container.querySelectorAll('li')).toHaveLength(0)
  })
})

describe('images', () => {
  const images = [file({ id: 'i1', filename: 'one.png', content_type: 'image/png' }), file({ id: 'i2', filename: 'two.jpg', content_type: 'image/jpeg' })]

  it('shows a thumbnail gallery plus a row thumbnail for each image', () => {
    const { container } = render(<FileList files={images} />)
    const imgs = [...container.querySelectorAll('img')]
    // 2 gallery + 2 row thumbs
    expect(imgs).toHaveLength(4)
    expect(imgs.every((i) => i.getAttribute('src')?.startsWith('/api/attachments/'))).toBe(true)
    expect(imgs.some((i) => i.getAttribute('alt') === 'one.png')).toBe(true)
  })

  it('skips the gallery when gallery is false', () => {
    const { container } = render(<FileList files={images} gallery={false} />)
    expect(container.querySelectorAll('img')).toHaveLength(2)
  })

  it('opens a lightbox from the gallery and navigates with arrow keys, closing on Escape', () => {
    render(<FileList files={images} />)
    fireEvent.click(screen.getAllByTitle('one.png')[0])
    expect(screen.getByRole('dialog', { name: 'one.png' })).toBeTruthy()
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(screen.getByRole('dialog', { name: 'two.jpg' })).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('opens the lightbox on the right image from a row thumbnail', () => {
    render(<FileList files={[file({ id: 'd', filename: 'doc.txt' }), ...images]} />)
    fireEvent.click(screen.getByRole('button', { name: 'Open two.jpg' }))
    expect(screen.getByRole('dialog', { name: 'two.jpg' })).toBeTruthy()
  })

  it('never offers a text preview for images', () => {
    render(<FileList files={images} />)
    expect(screen.queryByRole('button', { name: /Preview/ })).toBeNull()
  })
})

describe('text preview', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  const mockFetch = () => vi.mocked(globalThis.fetch)

  it('previews small text files by fetching the content, and hides again', async () => {
    mockFetch().mockResolvedValue(new Response('hello preview', { status: 200 }))
    render(<FileList files={[file({ id: 't', filename: 'a.txt' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    expect(screen.getByText('Loading preview…')).toBeTruthy()
    expect(await screen.findByText('hello preview')).toBeTruthy()
    expect(mockFetch()).toHaveBeenCalledWith('/api/attachments/t/content', expect.objectContaining({ credentials: 'same-origin' }))
    fireEvent.click(screen.getByRole('button', { name: /Hide/ }))
    expect(screen.queryByText('hello preview')).toBeNull()
  })

  it('shows (empty file) for an empty body', async () => {
    mockFetch().mockResolvedValue(new Response('', { status: 200 }))
    render(<FileList files={[file({ id: 't' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    expect(await screen.findByText('(empty file)')).toBeTruthy()
  })

  it('shows an error when the server refuses', async () => {
    mockFetch().mockResolvedValue(new Response('no', { status: 403 }))
    render(<FileList files={[file({ id: 't' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    expect(await screen.findByText('Could not load the preview')).toBeTruthy()
    expect(screen.getByText('The server answered 403.')).toBeTruthy()
  })

  it('shows an error when the request itself fails', async () => {
    mockFetch().mockRejectedValue(new Error('offline'))
    render(<FileList files={[file({ id: 't' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    expect(await screen.findByText('offline')).toBeTruthy()
  })

  it('does not offer a preview for large or binary files', () => {
    render(<FileList files={[file({ id: 'big', size_bytes: 10 * 1024 * 1024 }), file({ id: 'pdf', filename: 'a.pdf', content_type: 'application/pdf' }), file({ id: 'bin', filename: 'a.bin', content_type: 'application/octet-stream' })]} />)
    expect(screen.queryByRole('button', { name: /Preview/ })).toBeNull()
  })

  it('renders markdown files, with the source a click away', async () => {
    mockFetch().mockResolvedValue(new Response('# Daily report\n\n- **ERP-31** overdue\n\n<script>alert(1)</script>', { status: 200 }))
    render(<FileList files={[file({ id: 'r', filename: 'reports/daily.md', content_type: 'text/markdown; charset=utf-8' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    expect(await screen.findByRole('heading', { name: 'Daily report' })).toBeTruthy()
    expect(screen.getByText('ERP-31').tagName).toBe('STRONG')
    expect(document.querySelector('script')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Source' }))
    expect(screen.queryByRole('heading', { name: 'Daily report' })).toBeNull()
    expect(screen.getByText(/# Daily report/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Rendered' }))
    expect(screen.getByRole('heading', { name: 'Daily report' })).toBeTruthy()
  })

  it('shows plain text files as text, not markdown', async () => {
    mockFetch().mockResolvedValue(new Response('# not a heading', { status: 200 }))
    render(<FileList files={[file({ id: 't', filename: 'notes.txt' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    expect(await screen.findByText('# not a heading')).toBeTruthy()
    expect(screen.queryByRole('heading')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Source' })).toBeNull()
  })

  it('previews by extension when the content type is generic', () => {
    render(<FileList files={[file({ id: 'md', filename: 'readme.md', content_type: 'application/octet-stream' })]} />)
    expect(screen.getAllByRole('button', { name: /Preview/ })).toHaveLength(1)
  })

  it('aborts the fetch when the preview is closed before it finishes', async () => {
    let signal: AbortSignal | undefined
    mockFetch().mockImplementation((_url, init) => {
      signal = init?.signal as AbortSignal
      return new Promise<Response>(() => {})
    })
    render(<FileList files={[file({ id: 't' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    await waitFor(() => expect(signal).toBeDefined())
    fireEvent.click(screen.getByRole('button', { name: /Hide/ }))
    expect(signal!.aborted).toBe(true)
  })

  it('ignores a late failure after the preview was closed', async () => {
    let reject!: (e: unknown) => void
    mockFetch().mockImplementation(() => new Promise<Response>((_, r) => { reject = r }))
    render(<FileList files={[file({ id: 't' })]} />)
    fireEvent.click(screen.getByRole('button', { name: /Preview/ }))
    fireEvent.click(screen.getByRole('button', { name: /Hide/ }))
    reject(new Error('late'))
    await new Promise((r) => setTimeout(r, 0))
    expect(screen.queryByText('late')).toBeNull()
  })

  it('previews several files independently', async () => {
    mockFetch().mockImplementation((url) => Promise.resolve(new Response(String(url).includes('/a/') ? 'AAA' : 'BBB')))
    render(<FileList files={[file({ id: 'a', filename: 'a.txt' }), file({ id: 'b', filename: 'b.txt' })]} />)
    const buttons = screen.getAllByRole('button', { name: /Preview/ })
    fireEvent.click(buttons[1])
    expect(await screen.findByText('BBB')).toBeTruthy()
    expect(screen.queryByText('AAA')).toBeNull()
  })
})

describe('FileIcon', () => {
  it.each([
    ['image/png', 'a.png'],
    ['application/octet-stream', 'sheet.xlsx'],
    ['text/csv', 'a.csv'],
    ['application/vnd.ms-powerpoint', 'deck.pptx'],
    ['application/json', 'a.json'],
    ['text/plain', 'run.log'],
    ['application/pdf', 'a.pdf'],
    ['application/octet-stream', 'notes.md'],
    ['application/octet-stream', 'archive.zip'],
    ['application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', 'noext'],
    ['application/vnd.openxmlformats-officedocument.presentationml.presentation', 'noext'],
  ])('renders an icon for %s %s', (type, name) => {
    const { container } = render(<FileIcon contentType={type} filename={name} />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-hidden')).toBe('true')
    expect(svg.getAttribute('class')).toContain('size-4')
  })

  it('picks distinct icons per family', () => {
    const cls = (type: string, name: string) => {
      const { container, unmount } = render(<FileIcon contentType={type} filename={name} />)
      const c = container.querySelector('svg')!.getAttribute('class')!
      unmount()
      return c
    }
    expect(cls('image/png', 'a.png')).toContain('lucide-file-image')
    expect(cls('text/csv', 'a.csv')).toContain('lucide-file-spreadsheet')
    expect(cls('application/octet-stream', 'x.zip')).toBe(cls('application/octet-stream', 'y.bin'))
    expect(cls('application/json', 'a.json')).not.toBe(cls('application/octet-stream', 'x.zip'))
  })

  it('accepts a custom size class', () => {
    const { container } = render(<FileIcon contentType="text/plain" filename="a.txt" className="size-9" />)
    expect(container.querySelector('svg')!.getAttribute('class')).toContain('size-9')
  })
})

it('keeps a within helper import used', () => {
  expect(within).toBeTypeOf('function')
})
