import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { messageBody } from '../../api/endpoints.chat'
import { Composer } from './Composer'

afterEach(cleanup)
globalThis.URL.createObjectURL ??= () => 'blob:x'
globalThis.URL.revokeObjectURL ??= () => undefined

const png = (name = 'image.png', size = 10) => new File([new Uint8Array(size)], name, { type: 'image/png' })
const box = () => screen.getByLabelText('Message')
const paste = (items: { kind: string; type: string; getAsFile: () => File | null }[]) =>
  fireEvent.paste(box(), { clipboardData: { items } })

describe('Composer files', () => {
  it('turns a pasted screenshot into a file and sends it, even without text', async () => {
    const onSend = vi.fn().mockResolvedValue(true)
    render(<Composer onSend={onSend} />)
    paste([{ kind: 'file', type: 'image/png', getAsFile: () => png() }])
    const name = (await screen.findByLabelText('Files to send')).textContent ?? ''
    expect(name).toMatch(/pasted-\d{8}-\d{6}\.png/)
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1))
    const [text, files] = onSend.mock.calls[0]
    expect(text).toBe('')
    expect(files).toHaveLength(1)
    // Sent: the box and its files are cleared
    await waitFor(() => expect(screen.queryByLabelText('Files to send')).toBeNull())
  })

  it('leaves pasted text alone, even when an image comes with it', () => {
    render(<Composer onSend={vi.fn()} />)
    paste([{ kind: 'string', type: 'text/plain', getAsFile: () => null }, { kind: 'file', type: 'image/png', getAsFile: () => png() }])
    expect(screen.queryByLabelText('Files to send')).toBeNull()
  })

  it('attaches picked and dropped files, refusing what a message cannot carry', async () => {
    render(<Composer onSend={vi.fn()} />)
    fireEvent.change(screen.getByTestId('attach-input'), { target: { files: [png('a.png'), new File(['x'], 'notes.txt', { type: 'text/plain' })] } })
    expect(await screen.findByText('a.png')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('notes.txt: only images and PDFs can go with a message')
    const drop = { dataTransfer: { types: ['Files'], files: [png('b.png'), png('c.png'), png('d.png'), png('e.png')] } }
    fireEvent.drop(box().parentElement as HTMLElement, drop)
    expect(await screen.findByText('d.png')).toBeTruthy()
    expect(screen.queryByText('e.png')).toBeNull()
    expect(screen.getByRole('alert').textContent).toContain('a message can hold at most 4 files')
    fireEvent.click(screen.getByRole('button', { name: 'Remove a.png' }))
    expect(screen.queryByText('a.png')).toBeNull()
  })

  it('keeps the files when sending fails', async () => {
    const onSend = vi.fn().mockResolvedValue(false)
    render(<Composer onSend={onSend} />)
    fireEvent.change(screen.getByTestId('attach-input'), { target: { files: [png('a.png')] } })
    fireEvent.change(box(), { target: { value: 'look' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(onSend).toHaveBeenCalled())
    expect(screen.getByText('a.png')).toBeTruthy()
  })
})

describe('messageBody', () => {
  it('is JSON without files and a form with them', () => {
    expect(messageBody('hi', [], { crew_id: 'c1' })).toEqual({ crew_id: 'c1', content: 'hi' })
    const form = messageBody('look', [png('a.png')], { crew_id: 'c1' }) as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.get('crew_id')).toBe('c1')
    expect(form.get('content')).toBe('look')
    expect((form.get('file') as File).name).toBe('a.png')
  })
})

describe('Composer text', () => {
  it('speaks of the crew by default', () => {
    render(<Composer onSend={vi.fn()} />)
    expect(screen.getByPlaceholderText('Message the crew…')).toBeTruthy()
    expect(screen.getByText(/Changes the crew proposes wait for your Confirm\./)).toBeTruthy()
  })

  it('takes the placeholder and the confirm hint it is given', () => {
    render(<Composer onSend={vi.fn()} placeholder="Message Ada…" confirmHint="Runs Ada proposes wait for your Confirm." />)
    expect(screen.getByPlaceholderText('Message Ada…')).toBeTruthy()
    expect(screen.getByText(/Runs Ada proposes wait for your Confirm\./)).toBeTruthy()
    expect(screen.queryByText(/Changes the crew proposes/)).toBeNull()
  })
})
