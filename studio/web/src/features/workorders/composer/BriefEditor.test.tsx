import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, createEvent, fireEvent, render, screen } from '@testing-library/react'
import { imageFile } from '../testUtils'
import { BriefEditor } from './BriefEditor'

afterEach(cleanup)

const textbox = () => screen.getByRole('textbox') as HTMLTextAreaElement

type Item = { kind: string; type: string; getAsFile: () => File | null }
const fileItem = (file: File | null, type = file?.type ?? 'image/png'): Item => ({ kind: 'file', type, getAsFile: () => file })
const textItem = (type = 'text/plain'): Item => ({ kind: 'string', type, getAsFile: () => null })

function paste(items: Item[]) {
  // fireEvent returns false when preventDefault was called
  return !fireEvent.paste(textbox(), { clipboardData: { items } })
}

describe('BriefEditor basics', () => {
  it('shows the label, value and character counter', () => {
    render(<BriefEditor value="hello" onChange={vi.fn()} />)
    expect(screen.getByLabelText('Brief')).toBe(textbox())
    expect(textbox().value).toBe('hello')
    expect(screen.getByText('5 / 100,000')).toBeTruthy()
  })

  it('reports typing', () => {
    const onChange = vi.fn()
    render(<BriefEditor value="" onChange={onChange} />)
    fireEvent.change(textbox(), { target: { value: 'new text' } })
    expect(onChange).toHaveBeenCalledWith('new text')
  })

  it('uses a custom label, rows, placeholder and caps the length', () => {
    render(<BriefEditor value="" onChange={vi.fn()} label="Notes" rows={4} placeholder="Say it" />)
    expect(textbox().rows).toBe(4)
    expect(textbox().placeholder).toBe('Say it')
    expect(textbox().maxLength).toBe(100_000)
    expect(screen.getByLabelText('Notes')).toBe(textbox())
  })

  it('mentions pasting only when files are enabled', () => {
    const { rerender } = render(<BriefEditor value="" onChange={vi.fn()} />)
    expect(textbox().placeholder).toBe('Describe the work. Markdown works.')
    expect(screen.queryByRole('button', { name: /Attach files/ })).toBeNull()
    rerender(<BriefEditor value="" onChange={vi.fn()} onFiles={vi.fn()} />)
    expect(textbox().placeholder).toMatch(/Paste screenshots/)
  })

  it('shows an error in place of the hint and flags the field', () => {
    const { rerender } = render(<BriefEditor value="" onChange={vi.fn()} hint="a hint" />)
    expect(screen.getByText('a hint')).toBeTruthy()
    rerender(<BriefEditor value="" onChange={vi.fn()} hint="a hint" error="Too long" />)
    expect(screen.getByRole('alert').textContent).toBe('Too long')
    expect(screen.queryByText('a hint')).toBeNull()
    expect(textbox().getAttribute('aria-invalid')).toBe('true')
  })

  it('warns when the brief nears the limit', () => {
    render(<BriefEditor value={'x'.repeat(95_000)} onChange={vi.fn()} />)
    expect(screen.getByText('95,000 / 100,000').className).toContain('text-warn')
  })

  it('renders the footer', () => {
    render(<BriefEditor value="" onChange={vi.fn()} footer={<div>tray</div>} />)
    expect(screen.getByText('tray')).toBeTruthy()
  })
})

describe('preview mode', () => {
  it('toggles between write and preview, rendering markdown', () => {
    render(<BriefEditor value={'# Title\n\nSome **bold** text'} onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('tab', { name: /Preview/ }))
    expect(screen.queryByRole('textbox')).toBeNull()
    expect(screen.getByRole('heading', { name: 'Title' })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /Preview/ }).getAttribute('aria-selected')).toBe('true')
    fireEvent.click(screen.getByRole('tab', { name: /Write/ }))
    expect(textbox().value).toContain('Some **bold** text')
  })

  it('shows a hint when there is nothing to preview', () => {
    render(<BriefEditor value="   " onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('tab', { name: /Preview/ }))
    expect(screen.getByText('Nothing to preview yet.')).toBeTruthy()
  })
})

describe('clipboard paste', () => {
  const NOW = new Date(2026, 9, 3, 14, 25, 1)

  it('ignores pastes when files are not enabled', () => {
    render(<BriefEditor value="" onChange={vi.fn()} />)
    expect(paste([fileItem(imageFile())])).toBe(false)
  })

  it('turns a pasted image into a named attachment and cancels the default paste', () => {
    vi.useFakeTimers({ now: NOW })
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    const prevented = paste([fileItem(imageFile('image.png', 'image/png', 4))])
    vi.useRealTimers()
    expect(prevented).toBe(true)
    expect(onFiles).toHaveBeenCalledTimes(1)
    const files = onFiles.mock.calls[0][0] as File[]
    expect(files).toHaveLength(1)
    expect(files[0].name).toBe('pasted-20261003-142501.png')
    expect(files[0].type).toBe('image/png')
    expect(files[0].size).toBe(4)
  })

  it('names pasted jpegs with .jpg and de-duplicates against taken names', () => {
    vi.useFakeTimers({ now: NOW })
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} takenNames={new Set(['pasted-20261003-142501.jpg'])} />)
    paste([fileItem(imageFile('x.jpg', 'image/jpeg'))])
    vi.useRealTimers()
    expect((onFiles.mock.calls[0][0] as File[])[0].name).toBe('pasted-20261003-142501-2.jpg')
  })

  it('attaches several pasted images with distinct names', () => {
    vi.useFakeTimers({ now: NOW })
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    paste([fileItem(imageFile()), fileItem(imageFile())])
    vi.useRealTimers()
    const names = (onFiles.mock.calls[0][0] as File[]).map((f) => f.name)
    expect(new Set(names).size).toBe(2)
  })

  it('leaves text pastes alone', () => {
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    expect(paste([textItem()])).toBe(false)
    expect(onFiles).not.toHaveBeenCalled()
  })

  it('prefers the text when an office app puts an image next to copied text', () => {
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    expect(paste([textItem('text/plain'), textItem('text/html'), fileItem(imageFile())])).toBe(false)
    expect(onFiles).not.toHaveBeenCalled()
  })

  it('still attaches an image that comes with html only (no plain text)', () => {
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    expect(paste([textItem('text/html'), fileItem(imageFile())])).toBe(true)
    expect(onFiles).toHaveBeenCalledTimes(1)
  })

  it('ignores non-image files, unreadable items and empty clipboards', () => {
    const onFiles = vi.fn()
    render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    expect(paste([fileItem(new File(['x'], 'a.pdf', { type: 'application/pdf' }))])).toBe(false)
    expect(paste([fileItem(null, 'image/png')])).toBe(false)
    expect(paste([])).toBe(false)
    expect(onFiles).not.toHaveBeenCalled()
  })
})

describe('attach button and file picker', () => {
  it('opens the picker and forwards chosen files, then resets the input', () => {
    const onFiles = vi.fn()
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    const input = container.querySelector('input[type=file]') as HTMLInputElement
    const click = vi.spyOn(input, 'click').mockImplementation(() => {})
    fireEvent.click(screen.getByRole('button', { name: /Attach files/ }))
    expect(click).toHaveBeenCalled()
    const f = new File(['x'], 'a.txt', { type: 'text/plain' })
    fireEvent.change(input, { target: { files: [f] } })
    expect(onFiles).toHaveBeenCalledWith([f])
  })

  it('does nothing when the picker is dismissed without files', () => {
    const onFiles = vi.fn()
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    const input = container.querySelector('input[type=file]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [] } })
    expect(onFiles).not.toHaveBeenCalled()
  })

  it('restricts the picker to supported types', () => {
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={vi.fn()} />)
    const input = container.querySelector('input[type=file]') as HTMLInputElement
    expect(input.multiple).toBe(true)
    expect(input.accept).toContain('image/png')
    expect(input.accept).toContain('.docx')
  })
})

describe('drag and drop', () => {
  const zone = (c: HTMLElement) => c.querySelector('textarea')!.parentElement as HTMLElement

  it('shows a drop overlay while files are dragged over and hides it on leave', () => {
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={vi.fn()} />)
    fireEvent.dragOver(zone(container), { dataTransfer: { types: ['Files'] } })
    expect(screen.getByText('Drop files to attach')).toBeTruthy()
    fireEvent.dragLeave(zone(container), { relatedTarget: null })
    expect(screen.queryByText('Drop files to attach')).toBeNull()
  })

  it('keeps the overlay while moving between children of the zone', () => {
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={vi.fn()} />)
    fireEvent.dragOver(zone(container), { dataTransfer: { types: ['Files'] } })
    // jsdom's generic Event ignores relatedTarget in the init dict, so define it by hand
    const leave = createEvent.dragLeave(zone(container))
    Object.defineProperty(leave, 'relatedTarget', { value: container.querySelector('textarea') })
    fireEvent(zone(container), leave)
    expect(screen.getByText('Drop files to attach')).toBeTruthy()
  })

  it('ignores drags that are not files (such as selected text)', () => {
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={vi.fn()} />)
    fireEvent.dragOver(zone(container), { dataTransfer: { types: ['text/plain'] } })
    expect(screen.queryByText('Drop files to attach')).toBeNull()
  })

  it('forwards dropped files and clears the overlay', () => {
    const onFiles = vi.fn()
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    const f = new File(['x'], 'a.txt', { type: 'text/plain' })
    fireEvent.dragOver(zone(container), { dataTransfer: { types: ['Files'] } })
    const notPrevented = fireEvent.drop(zone(container), { dataTransfer: { files: [f], types: ['Files'] } })
    expect(notPrevented).toBe(false)
    expect(onFiles).toHaveBeenCalledWith([f])
    expect(screen.queryByText('Drop files to attach')).toBeNull()
  })

  it('ignores a drop with no files', () => {
    const onFiles = vi.fn()
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} onFiles={onFiles} />)
    fireEvent.drop(zone(container), { dataTransfer: { files: [], types: ['text/plain'] } })
    expect(onFiles).not.toHaveBeenCalled()
  })

  it('has no drop handling without onFiles', () => {
    const onFiles = vi.fn()
    const { container } = render(<BriefEditor value="" onChange={vi.fn()} />)
    fireEvent.drop(zone(container), { dataTransfer: { files: [new File(['x'], 'a.txt')], types: ['Files'] } })
    expect(onFiles).not.toHaveBeenCalled()
  })
})
