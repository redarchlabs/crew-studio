import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { workOrdersApi } from '../../../api/endpoints'
import { followUpApi } from '../../../api/endpoints.followup'
import type { WorkOrderFeedback } from '../../../api/types.followup'
import { msg, view } from '../../chat/testUtils'
import { makeAttachment, makeRun, renderRoute, setPermissions } from '../testUtils'
import { ConversationTab } from './ConversationTab'

vi.mock('../../../api/endpoints.followup')
globalThis.URL.createObjectURL ??= () => 'blob:x'
globalThis.URL.revokeObjectURL ??= () => undefined
vi.mock('../../../lib/auth', async () => (await import('../testUtils')).authMock())

const note = (over: Partial<WorkOrderFeedback> = {}): WorkOrderFeedback => ({
  id: 'f1', work_order_id: 'wo-1', run_id: 'r1', revision_run_id: 'r2', feedback: 'Add Claude Haiku.', with_conversation: false,
  created_by_name: 'Ada', created_at: '2026-10-06T13:00:00Z', ...over,
})

beforeEach(() => {
  setPermissions('work_orders.view', 'work_orders.create', 'crews.chat')
  vi.mocked(followUpApi.conversation).mockReset().mockResolvedValue(null)
  vi.mocked(followUpApi.feedback).mockReset().mockResolvedValue([])
})
afterEach(cleanup)

const render = (props: Partial<Parameters<typeof ConversationTab>[0]> = {}) => {
  const onRevised = vi.fn()
  renderRoute(<ConversationTab workOrderId="wo-1" crewId="crew-1" active={false} onRevised={onRevised} {...props} />)
  return onRevised
}

describe('ConversationTab', () => {
  it('starts a conversation about the work order', async () => {
    vi.mocked(followUpApi.send).mockResolvedValue(view({
      messages: [msg({ id: 'm1', role: 'user', content: 'Why Gemini?' }), msg({ id: 'm2', role: 'assistant', content: 'Its list price is lowest.' })],
    }))
    render()
    expect(await screen.findByText(/Ask the crew about this work order/)).toBeTruthy()
    fireEvent.change(screen.getByPlaceholderText('Ask the crew about this work order…'), { target: { value: 'Why Gemini?' } })
    fireEvent.keyDown(screen.getByPlaceholderText('Ask the crew about this work order…'), { key: 'Enter' })
    await waitFor(() => expect(followUpApi.send).toHaveBeenCalledWith('wo-1', 'Why Gemini?', []))
    expect(await screen.findByText('Its list price is lowest.')).toBeTruthy()
  })

  it('compacts the conversation', async () => {
    const before = view({ messages: [msg({ id: 'm1', role: 'user', content: 'Why Gemini?' }), msg({ id: 'm2', content: 'Lowest price.' })] })
    vi.mocked(followUpApi.conversation).mockResolvedValue(before)
    vi.mocked(followUpApi.compact).mockResolvedValue(view({ messages: [...before.messages, msg({ id: 's1', role: 'summary', run_status: 'queued' })] }))
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Compact' }))
    await waitFor(() => expect(followUpApi.compact).toHaveBeenCalledWith('wo-1'))
    expect(await screen.findByText('Summarising…')).toBeTruthy()
    expect(screen.getByText('Wait for the reply before sending another message.')).toBeTruthy()
  })

  it('revises with feedback and the conversation, then reports the new attempt', async () => {
    const run = makeRun({ id: 'r2' })
    vi.mocked(followUpApi.revise).mockResolvedValue({ run, feedback: note({ with_conversation: true }) })
    const onRevised = render()
    const button = await screen.findByRole('button', { name: 'Revise with feedback' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('What should change?'), { target: { value: '  Add Claude Haiku.  ' } })
    fireEvent.click(button)
    await waitFor(() => expect(followUpApi.revise).toHaveBeenCalledWith('wo-1', { feedback: 'Add Claude Haiku.', include_conversation: true }))
    expect(onRevised).toHaveBeenCalledWith(run)
    expect(await screen.findByText(/with the conversation/)).toBeTruthy()
  })

  it('sends pasted screenshots with the feedback: they join the work order files and the feedback names them', async () => {
    const upload = vi.spyOn(workOrdersApi, 'upload').mockResolvedValue([makeAttachment({ filename: 'pasted-20261006-170000.png' })])
    vi.mocked(followUpApi.revise).mockResolvedValue({ run: makeRun(), feedback: note() })
    render()
    const box = await screen.findByLabelText('What should change?')
    const shot = new File([new Uint8Array(8)], 'image.png', { type: 'image/png' })
    fireEvent.paste(box, { clipboardData: { items: [{ kind: 'file', type: 'image/png', getAsFile: () => shot }] } })
    expect(await screen.findByLabelText('Files to send')).toBeTruthy()
    fireEvent.change(box, { target: { value: 'The chart is wrong, see the screenshot.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Revise with feedback' }))
    await waitFor(() => expect(followUpApi.revise).toHaveBeenCalledWith('wo-1', {
      feedback: 'The chart is wrong, see the screenshot.\n\nAttached with this feedback: pasted-20261006-170000.png', include_conversation: true,
    }))
    expect(upload).toHaveBeenCalledWith('wo-1', [expect.any(File)])
    expect(screen.queryByLabelText('Files to send')).toBeNull()
    upload.mockRestore()
  })

  it('does not revise when the files cannot be uploaded', async () => {
    const upload = vi.spyOn(workOrdersApi, 'upload').mockRejectedValue(new Error('too big'))
    render()
    fireEvent.change(await screen.findByTestId('attach-input'),
      { target: { files: [new File([new Uint8Array(8)], 'a.pdf', { type: 'application/pdf' })] } })
    fireEvent.change(screen.getByLabelText('What should change?'), { target: { value: 'See a.pdf' } })
    fireEvent.click(screen.getByRole('button', { name: 'Revise with feedback' }))
    await waitFor(() => expect(upload).toHaveBeenCalled())
    expect(followUpApi.revise).not.toHaveBeenCalled()
    expect(screen.getByText('a.pdf')).toBeTruthy()
    upload.mockRestore()
  })

  it('names files uploaded before a failed revision when retrying, without uploading them again', async () => {
    const upload = vi.spyOn(workOrdersApi, 'upload').mockResolvedValue([makeAttachment({ filename: 'a.pdf' })])
    vi.mocked(followUpApi.revise).mockRejectedValueOnce(new Error('busy')).mockResolvedValue({ run: makeRun(), feedback: note() })
    render()
    fireEvent.change(await screen.findByTestId('attach-input'), { target: { files: [new File([new Uint8Array(8)], 'a.pdf', { type: 'application/pdf' })] } })
    fireEvent.change(screen.getByLabelText('What should change?'), { target: { value: 'See a.pdf' } })
    fireEvent.click(screen.getByRole('button', { name: 'Revise with feedback' }))
    expect(await screen.findByText("Already added to the work order's files: a.pdf")).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Revise with feedback' }))
    await waitFor(() => expect(followUpApi.revise).toHaveBeenCalledTimes(2))
    expect(vi.mocked(followUpApi.revise).mock.calls[1][1].feedback).toBe('See a.pdf\n\nAttached with this feedback: a.pdf')
    expect(upload).toHaveBeenCalledTimes(1)
    upload.mockRestore()
  })

  it('shows the files sent with a message', async () => {
    vi.mocked(followUpApi.conversation).mockResolvedValue(view({ messages: [
      msg({ id: 'm1', role: 'user', content: 'What is this?', attachments: [
        { id: 'f1', filename: 'shot.png', content_type: 'image/png', size_bytes: 10 },
        { id: 'f2', filename: 'spec.pdf', content_type: 'application/pdf', size_bytes: 10 }] }),
      msg({ id: 'm2', content: 'A chart.' })] }))
    render()
    const img = await screen.findByAltText('shot.png') as HTMLImageElement
    expect(img.getAttribute('src')).toBe('/api/attachments/f1/content')
    expect(screen.getByText('spec.pdf').closest('a')?.getAttribute('href')).toBe('/api/attachments/f2/content')
  })

  it('can leave the conversation out of a revision', async () => {
    vi.mocked(followUpApi.revise).mockResolvedValue({ run: makeRun(), feedback: note() })
    render()
    fireEvent.change(await screen.findByLabelText('What should change?'), { target: { value: 'Shorter.' } })
    fireEvent.click(screen.getByRole('checkbox'))
    fireEvent.click(screen.getByRole('button', { name: 'Revise with feedback' }))
    await waitFor(() => expect(followUpApi.revise).toHaveBeenCalledWith('wo-1', { feedback: 'Shorter.', include_conversation: false }))
  })

  it('waits for a running attempt before revising', async () => {
    render({ active: true })
    fireEvent.change(await screen.findByLabelText('What should change?'), { target: { value: 'x' } })
    expect((screen.getByRole('button', { name: 'Revise with feedback' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('Wait for the current attempt to finish.')).toBeTruthy()
  })

  it('lists earlier feedback, newest first', async () => {
    vi.mocked(followUpApi.feedback).mockResolvedValue([note({ id: 'a', feedback: 'First' }), note({ id: 'b', feedback: 'Second', revision_run_id: null })])
    render()
    const items = await screen.findAllByRole('listitem')
    expect(items.map((li) => li.querySelector('p')?.textContent)).toEqual(['Second', 'First'])
    expect(screen.getByText('The revision did not start.')).toBeTruthy()
  })

  it('shows only what the person may do', async () => {
    setPermissions('work_orders.view')
    render()
    expect(await screen.findByText(/needs the Chat with crews and agents permission/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revise with feedback' })).toBeNull()
  })
})
