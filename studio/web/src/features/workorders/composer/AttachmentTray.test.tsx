import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { makeAttachment } from '../testUtils'
import { AttachmentTray } from './AttachmentTray'
import type { AttachmentQueue, PendingFile } from './useAttachmentQueue'

afterEach(cleanup)

const pending = (over: Partial<PendingFile> & { localId: string }): PendingFile => ({
  file: new File([new Uint8Array(2048)], 'draft.txt', { type: 'text/plain' }),
  previewUrl: null,
  status: 'waiting',
  error: null,
  ...over,
})

function queue(over: Partial<AttachmentQueue> = {}): AttachmentQueue {
  return {
    uploaded: [],
    pending: [],
    removing: new Set(),
    takenNames: new Set(),
    add: vi.fn(),
    flush: vi.fn(),
    retry: vi.fn(),
    removePending: vi.fn(),
    removeUploaded: vi.fn().mockResolvedValue(undefined),
    replaceUploaded: vi.fn(),
    ...over,
  }
}

describe('AttachmentTray', () => {
  it('renders nothing for an empty queue', () => {
    const { container } = render(<AttachmentTray queue={queue()} />)
    expect(container.firstChild).toBeNull()
  })

  it('shows counts and total size against the limits', () => {
    render(<AttachmentTray queue={queue({ uploaded: [makeAttachment({ size_bytes: 1024 })], pending: [pending({ localId: 'l1' })] })} />)
    expect(screen.getByText(/2\/20 files · 3\.0 KB of 100 MB/)).toBeTruthy()
  })

  it('shows uploaded files with remove buttons that call removeUploaded', () => {
    const a = makeAttachment({ id: 'a', filename: 'report.pdf', content_type: 'application/pdf' })
    const q = queue({ uploaded: [a] })
    render(<AttachmentTray queue={q} />)
    expect(screen.getByText('report.pdf')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Remove report.pdf' }))
    expect(q.removeUploaded).toHaveBeenCalledWith(a)
  })

  it('uses a thumbnail from the content URL for uploaded images only', () => {
    const { container } = render(<AttachmentTray queue={queue({ uploaded: [makeAttachment({ id: 'img', filename: 'p.png', content_type: 'image/png' }), makeAttachment({ id: 'doc', filename: 'd.txt' })] })} />)
    const imgs = container.querySelectorAll('img')
    expect(imgs).toHaveLength(1)
    expect(imgs[0].getAttribute('src')).toBe('/api/attachments/img/content')
  })

  it('hides remove buttons when read-only', () => {
    render(<AttachmentTray queue={queue({ uploaded: [makeAttachment()] })} readOnly />)
    expect(screen.queryByRole('button', { name: /Remove/ })).toBeNull()
  })

  it('disables removal and shows a spinner while an uploaded file is being removed', () => {
    render(<AttachmentTray queue={queue({ uploaded: [makeAttachment({ id: 'a' })], removing: new Set(['a']) })} />)
    expect((screen.getByRole('button', { name: 'Remove notes.txt' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows the waiting hint for queued files, with a default', () => {
    const q = queue({ pending: [pending({ localId: 'l1' })] })
    const { rerender } = render(<AttachmentTray queue={q} />)
    expect(screen.getByText(/waiting/)).toBeTruthy()
    rerender(<AttachmentTray queue={q} waitingHint="uploads once a target is chosen" />)
    expect(screen.getByText(/uploads once a target is chosen/)).toBeTruthy()
  })

  it('shows uploading state with removal disabled', () => {
    render(<AttachmentTray queue={queue({ pending: [pending({ localId: 'l1', status: 'uploading' })] })} />)
    expect(screen.getByText(/uploading…/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Remove draft.txt' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByRole('button', { name: /Retry/ })).toBeNull()
  })

  it('shows failures with the message, a retry and a remove action', () => {
    const q = queue({ pending: [pending({ localId: 'l1', status: 'error', error: 'Storage quota exceeded' })] })
    render(<AttachmentTray queue={q} />)
    expect(screen.getByRole('alert').textContent).toContain('Storage quota exceeded')
    fireEvent.click(screen.getByRole('button', { name: 'Retry draft.txt' }))
    expect(q.retry).toHaveBeenCalledWith('l1')
    fireEvent.click(screen.getByRole('button', { name: 'Remove draft.txt' }))
    expect(q.removePending).toHaveBeenCalledWith('l1')
  })

  it('does not show an error line for a failed file without a message', () => {
    render(<AttachmentTray queue={queue({ pending: [pending({ localId: 'l1', status: 'error', error: null })] })} />)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows pending image previews', () => {
    const { container } = render(<AttachmentTray queue={queue({ pending: [pending({ localId: 'l1', file: new File(['x'], 'p.png', { type: 'image/png' }), previewUrl: 'blob:abc' })] })} />)
    expect(container.querySelector('img')!.getAttribute('src')).toBe('blob:abc')
  })

  it('lists uploaded before pending', () => {
    render(<AttachmentTray queue={queue({ uploaded: [makeAttachment({ filename: 'first.txt' })], pending: [pending({ localId: 'l1' })] })} />)
    const items = within(screen.getByRole('list')).getAllByRole('listitem')
    expect(items[0].textContent).toContain('first.txt')
    expect(items[1].textContent).toContain('draft.txt')
  })
})
