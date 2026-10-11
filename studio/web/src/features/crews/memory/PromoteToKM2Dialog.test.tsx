import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import { renderRoute, setupApi } from '../testUtils'
import { MAX_PROMOTE, outcomeText, PROMOTE_CAP_MESSAGE, PromoteToKM2Dialog } from './PromoteToKM2Dialog'
import { makeMemory } from './testUtils'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.memory')

beforeEach(() => setupApi())
afterEach(cleanup)

const many = (n: number) => Array.from({ length: n }, (_, i) => makeMemory({ id: `m${i}`, content: `Lesson ${i}` }))

describe('outcomeText', () => {
  it.each([['created', 'created'], ['metadata_updated', 'metadata updated'], ['', 'saved']])('%s', (o, want) => expect(outcomeText(o)).toBe(want))
})

describe('PromoteToKM2Dialog', () => {
  it('saves the memories as one KM2 document and shows the outcome', async () => {
    vi.mocked(memoryApi.promote).mockResolvedValue({ outcome: 'created', document_id: 'doc-42', external_ref: 'atlas-vendor', folder_id: 'f1' })
    const onPromoted = vi.fn()
    const onClose = vi.fn()
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(7)} onClose={onClose} onPromoted={onPromoted} />)
    expect(await screen.findByText('Promote 7 memories to KM2')).toBeTruthy()
    expect(screen.getByText(/uses this crew's KM2 connection/)).toBeTruthy()
    expect(screen.getByText('Lesson 4')).toBeTruthy()
    expect(screen.queryByText('Lesson 5')).toBeNull()
    expect(screen.getByText('and 2 more')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: ' Vendor lessons ' } })
    fireEvent.change(screen.getByLabelText('Folder (optional)'), { target: { value: 'Projects.Atlas' } })
    fireEvent.change(screen.getByLabelText('External ref (optional)'), { target: { value: 'atlas-vendor' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    const result = await screen.findByTestId('memory-promote-result')
    expect(memoryApi.promote).toHaveBeenCalledWith('c1', { memory_ids: ['m0', 'm1', 'm2', 'm3', 'm4', 'm5', 'm6'], title: 'Vendor lessons', folder: 'Projects.Atlas', external_ref: 'atlas-vendor' })
    expect(result.textContent).toContain('created')
    expect(result.textContent).toContain('doc-42')
    expect(screen.getByRole('button', { name: 'Copy' })).toBeTruthy()
    expect(onPromoted).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('picks the folder from the folders the crew KM2 connection allows', async () => {
    vi.mocked(memoryApi.promoteFolders).mockResolvedValue({ ok: true, message: '', folder_count: 2, warnings: [],
      folders: [{ id: 'f-atlas', name: 'Atlas', path: 'Projects.Atlas', parent_id: null }, { id: 'f-proj', name: 'Projects', path: 'Projects', parent_id: null }],
      allowed: { default_folder_id: 'f-atlas', write_folder_ids: ['f-proj'] } })
    vi.mocked(memoryApi.promote).mockResolvedValue({ outcome: 'created', document_id: 'd', external_ref: 'r', folder_id: 'f-proj' })
    renderRoute(<PromoteToKM2Dialog crewId="c-folders" memories={many(1)} onClose={() => {}} onPromoted={() => {}} />)
    expect(await screen.findByRole('option', { name: "Connection's default folder (Projects.Atlas)" })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Lessons' } })
    fireEvent.change(screen.getByLabelText('Folder'), { target: { value: 'f-proj' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    await waitFor(() => expect(memoryApi.promote).toHaveBeenCalledWith('c-folders', { memory_ids: ['m0'], title: 'Lessons', folder: 'f-proj' }))
  })

  it('shows an unchanged outcome without a document id', async () => {
    vi.mocked(memoryApi.promote).mockResolvedValue({ outcome: 'unchanged', document_id: '', external_ref: '', folder_id: '' })
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(1)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    expect(await screen.findByText('Promote 1 memory to KM2')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'One' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    const result = await screen.findByTestId('memory-promote-result')
    expect(memoryApi.promote).toHaveBeenCalledWith('c1', { memory_ids: ['m0'], title: 'One' })
    expect(result.textContent).toContain('unchanged')
    expect(screen.queryByRole('button', { name: 'Copy' })).toBeNull()
  })

  it('shows a repeat as already saved, with the document id', async () => {
    vi.mocked(memoryApi.promote).mockResolvedValue({ outcome: 'already_saved', document_id: 'doc-7', external_ref: '', folder_id: 'f1' })
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(2)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Title'), { target: { value: 'Again' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    const result = await screen.findByTestId('memory-promote-result')
    expect(screen.getByText('Already in KM2')).toBeTruthy()
    expect(result.textContent).toContain('already saved')
    expect(result.textContent).toContain('doc-7')
    expect(screen.getByText(/You already saved these memories to KM2/).textContent).toContain('as document doc-7')
  })

  it('shows a repeat without a document id', async () => {
    vi.mocked(memoryApi.promote).mockResolvedValue({ outcome: 'already_saved', document_id: '', external_ref: '', folder_id: '' })
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(1)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Title'), { target: { value: 'Again' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    const text = (await screen.findByText(/You already saved these memories to KM2/)).textContent
    expect(text).not.toContain('as document')
  })

  it('explains the daily promotion limit', async () => {
    vi.mocked(memoryApi.promote).mockRejectedValueOnce(new ApiError(429, { code: 'km2_promote_cap', message: 'You have promoted 20 documents to KM2 in the last 24 hours, the most one person may.' }))
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(1)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Title'), { target: { value: 'T' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    expect(await screen.findByText(PROMOTE_CAP_MESSAGE)).toBeTruthy()
    expect(screen.queryByTestId('memory-promote-result')).toBeNull()
  })

  it('needs a title, and shows field and connection errors', async () => {
    vi.mocked(memoryApi.promote)
      .mockRejectedValueOnce(new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['folder: not listed on the connection', 'memory_ids: 1 to 50'] }))
      .mockRejectedValueOnce(new ApiError(409, { code: 'km2_connection_missing', message: 'This crew has no KM2 connection in your areas.' }))
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(2)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Save to KM2' }))
    expect(await screen.findByText('Give the document a title.')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'T' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    expect(await screen.findByText('not listed on the connection')).toBeTruthy()
    expect(screen.getByText('1 to 50')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    expect(await screen.findByText('This crew has no KM2 connection in your areas.')).toBeTruthy()
  })

  it('refuses memories waiting for review, and shows the server refusing them', async () => {
    vi.mocked(memoryApi.promote).mockRejectedValueOnce(
      new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['memory_ids: approve these memories first'] }),
    )
    const { unmount } = renderRoute(<PromoteToKM2Dialog crewId="c1" memories={[...many(1), { ...many(2)[1], status: 'pending' }]} onClose={vi.fn()} onPromoted={vi.fn()} />)
    expect(await screen.findByText('Approve pending memories before promoting them to KM2.')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save to KM2' }) as HTMLButtonElement).disabled).toBe(true)
    unmount()
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(1)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    fireEvent.change(await screen.findByLabelText('Title'), { target: { value: 'T' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    expect(await screen.findByText('approve these memories first')).toBeTruthy()
  })

  it('refuses more than the limit', async () => {
    renderRoute(<PromoteToKM2Dialog crewId="c1" memories={many(MAX_PROMOTE + 1)} onClose={vi.fn()} onPromoted={vi.fn()} />)
    expect(await screen.findByText(`Promote at most ${MAX_PROMOTE} memories at a time.`)).toBeTruthy()
    await waitFor(() => expect((screen.getByRole('button', { name: 'Save to KM2' }) as HTMLButtonElement).disabled).toBe(true))
  })
})
