import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { ApiError } from '../../../api/client'
import { workOrdersApi } from '../../../api/endpoints'
import { makeCrew, makeTask } from '../../../lib/testFixtures'
import { makeAttachment, makeRun, makeWorkOrder, renderRoute } from '../testUtils'
import type { TargetCatalogData } from '../target/useTargetCatalog'
import { emptyWorkOrder, toWorkOrderInput } from './composerForm'
import { WorkOrderComposer } from './WorkOrderComposer'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

const api = vi.mocked(workOrdersApi)

const catalog: TargetCatalogData = {
  crews: [
    makeCrew({ id: 'c1', name: 'Crew One', budget_usd: 4, task_ids: ['t1', 't2'], inputs: [{ key: 'topic', label: '', default: '', required: true }] }),
    makeCrew({ id: 'c2', name: 'Crew Two', task_ids: ['t2'] }),
  ],
  tasks: [makeTask({ id: 't1', name: 'Research' }), makeTask({ id: 't2', name: 'Write up' })],
  forbidden: { crews: false, tasks: false },
}

type Props = Partial<Parameters<typeof WorkOrderComposer>[0]>
function renderComposer(props: Props = {}) {
  const onSubmitted = vi.fn()
  const onSaved = vi.fn()
  const view = renderRoute(
    <WorkOrderComposer workOrder={null} initial={emptyWorkOrder('c1')} catalog={catalog} backTo="/work-orders" backLabel="Work orders" onSubmitted={onSubmitted} onSaved={onSaved} {...props} />,
  )
  return { ...view, onSubmitted, onSaved }
}

beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset()
  URL.createObjectURL = vi.fn(() => 'blob:x')
  URL.revokeObjectURL = vi.fn()
})
afterEach(cleanup)

const title = () => screen.getByLabelText('Title') as HTMLInputElement
const fillBasics = () => {
  fireEvent.change(title(), { target: { value: 'My order' } })
  fireEvent.change(screen.getByLabelText(/Topic/), { target: { value: 'AI' } })
}

describe('new work order', () => {
  it('renders the form with a draft action set and no unsaved marker', () => {
    renderComposer()
    expect(screen.getByRole('heading', { name: 'New work order' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Save draft/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Submit/ })).toBeTruthy()
    expect(screen.queryByText('unsaved')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull()
    expect(screen.getByRole('link', { name: /Work orders/ }).getAttribute('href')).toBe('/work-orders')
    expect(screen.getByText(/Submitting starts a run straight away/)).toBeTruthy()
  })

  it('picks a crew (no task/crew toggle), showing each crew\'s tasks in order and the single-task hint', () => {
    renderComposer({ initial: emptyWorkOrder() })
    expect(screen.queryByRole('radiogroup')).toBeNull()
    expect(screen.getByText(/To run a single task, make a crew with just that task/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Choose a crew/ }))
    expect(screen.getByText('Research → Write up')).toBeTruthy()
    fireEvent.click(screen.getByRole('option', { name: /Crew One/ }))
    expect(screen.getByLabelText(/Topic/)).toBeTruthy()
  })

  it('shows inputs derived from the preselected crew', () => {
    renderComposer()
    expect(screen.getByLabelText(/Topic/)).toBeTruthy()
  })

  it('prompts to choose a crew when none is selected', () => {
    renderComposer({ initial: emptyWorkOrder() })
    expect(screen.getByText('Choose a crew to see the inputs it needs.')).toBeTruthy()
  })

  it('flags unsaved edits', () => {
    renderComposer()
    fireEvent.change(title(), { target: { value: 'x' } })
    expect(screen.getByText('unsaved')).toBeTruthy()
  })

  it('shows local validation errors without calling the API', async () => {
    renderComposer()
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    expect(await screen.findByText('Give the work order a title.')).toBeTruthy()
    expect(api.create).not.toHaveBeenCalled()
  })

  it('saves a draft, calls onSaved and then shows the saved badge and open link', async () => {
    const wo = makeWorkOrder({ id: 'new', number: 21 })
    api.create.mockResolvedValue(wo)
    const { onSaved } = renderComposer()
    fillBasics()
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(wo))
    expect(screen.getByText('WO-21')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Open draft' }).getAttribute('href')).toBe('/work-orders/new')
    expect(screen.queryByText('unsaved')).toBeNull()
  })

  it('submits: creates, submits and reports both to onSubmitted', async () => {
    const wo = makeWorkOrder({ id: 'new' })
    const run = makeRun({ status: 'queued' })
    api.create.mockResolvedValue(wo)
    api.submit.mockResolvedValue(run)
    const { onSubmitted } = renderComposer()
    fillBasics()
    fireEvent.click(screen.getByRole('button', { name: /Submit/ }))
    await waitFor(() => expect(onSubmitted).toHaveBeenCalledWith(wo, run))
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ title: 'My order', inputs: { topic: 'AI' }, crew_id: 'c1' }))
  })

  it('requires inputs on submit and shows the message on the field', async () => {
    renderComposer()
    fireEvent.change(title(), { target: { value: 'My order' } })
    fireEvent.click(screen.getByRole('button', { name: /Submit/ }))
    expect(await screen.findByText('Topic is required')).toBeTruthy()
    expect(api.create).not.toHaveBeenCalled()
  })

  it('shows server 422 problems on the right fields and in the general list', async () => {
    api.create.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'invalid', details: ['title: already used', 'budget_usd: too high', 'something else'] }))
    renderComposer()
    fillBasics()
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    expect(await screen.findByText('already used')).toBeTruthy()
    expect(screen.getByText('too high')).toBeTruthy()
    expect(screen.getByText('something else')).toBeTruthy()
  })

  it('shows a 422 on the crew', async () => {
    api.create.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'invalid', details: ['crew_id: crew not found'] }))
    renderComposer()
    fillBasics()
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    expect(await screen.findByText('crew not found')).toBeTruthy()
  })

  it('shows a 422 on the inputs panel', async () => {
    api.create.mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'invalid', details: ['inputs: too many'] }))
    renderComposer()
    fillBasics()
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    expect(await screen.findByText('too many')).toBeTruthy()
  })

  it('disables both actions while saving', async () => {
    let finish!: (w: ReturnType<typeof makeWorkOrder>) => void
    api.create.mockReturnValue(new Promise((r) => { finish = r }))
    renderComposer()
    fillBasics()
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    await waitFor(() => expect((screen.getByRole('button', { name: /Submit/ }) as HTMLButtonElement).disabled).toBe(true))
    finish(makeWorkOrder())
    await waitFor(() => expect((screen.getByRole('button', { name: /Submit/ }) as HTMLButtonElement).disabled).toBe(false))
  })

  it('shows the crew budget fallback', () => {
    renderComposer({ initial: emptyWorkOrder('c1') })
    expect(screen.getByText(/the crew's budget of \$4\.00/)).toBeTruthy()
  })

  it('sets the budget', async () => {
    api.create.mockResolvedValue(makeWorkOrder())
    renderComposer()
    fillBasics()
    fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '2.5' } })
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    await waitFor(() => expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ budget_usd: 2.5 })))
  })

  it('rejects a non-positive budget locally', async () => {
    renderComposer()
    fillBasics()
    fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: /Save draft/ }))
    expect(await screen.findByText(/Budget must be greater than 0/)).toBeTruthy()
  })
})

describe('attachments in the composer', () => {
  it('creates a draft and uploads a pasted image into the tray', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'd1', number: 5 }))
    api.upload.mockResolvedValue([makeAttachment({ id: 'u1', filename: 'shot.png', content_type: 'image/png' })])
    renderComposer()
    fireEvent.change(title(), { target: { value: 'With image' } })
    const img = new File([new Uint8Array(4)], 'image.png', { type: 'image/png' })
    fireEvent.paste(screen.getByLabelText('Brief'), { clipboardData: { items: [{ kind: 'file', type: 'image/png', getAsFile: () => img }] } })
    expect(await screen.findByText('shot.png')).toBeTruthy()
    expect(api.create).toHaveBeenCalledTimes(1)
    expect(api.upload).toHaveBeenCalledWith('d1', [expect.objectContaining({ type: 'image/png' })])
    expect(screen.getByText('WO-5')).toBeTruthy()
  })

  it('tells the user files wait for a crew', async () => {
    renderComposer({ initial: emptyWorkOrder() })
    const f = new File(['x'], 'notes.txt', { type: 'text/plain' })
    const input = document.querySelector('input[type=file]') as HTMLInputElement
    fireEvent.change(input, { target: { files: [f] } })
    expect(await screen.findByText(/uploads once a crew is chosen/)).toBeTruthy()
    expect(api.upload).not.toHaveBeenCalled()
  })

  it('blocks submit with a message when a file failed to upload', async () => {
    api.create.mockResolvedValue(makeWorkOrder({ id: 'd1' }))
    api.upload.mockRejectedValue(new Error('Storage quota exceeded'))
    renderComposer()
    fillBasics()
    const f = new File(['x'], 'big.txt', { type: 'text/plain' })
    fireEvent.change(document.querySelector('input[type=file]') as HTMLInputElement, { target: { files: [f] } })
    expect(await screen.findByText('Storage quota exceeded')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Submit/ }))
    expect(await screen.findByText(/Some files have not uploaded/)).toBeTruthy()
    expect(api.submit).not.toHaveBeenCalled()
    // Remove the failed file; submitting then proceeds
    fireEvent.click(screen.getByRole('button', { name: 'Remove big.txt' }))
    api.submit.mockResolvedValue(makeRun())
    fireEvent.click(screen.getByRole('button', { name: /Submit/ }))
    await waitFor(() => expect(api.submit).toHaveBeenCalled())
  })
})

describe('editing an existing work order', () => {
  const draft = makeWorkOrder({ id: 'w1', number: 8, status: 'draft', inputs: { topic: 'AI' } })
  const finished = makeWorkOrder({ id: 'w2', number: 9, status: 'succeeded', inputs: { topic: 'AI' }, attachments: [makeAttachment({ filename: 'kept.txt' })] })

  it('edits a draft: Save draft / Submit and an Edit heading', () => {
    renderComposer({ workOrder: draft, initial: toWorkOrderInput(draft) })
    expect(screen.getByRole('heading', { name: 'Edit work order' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Save draft/ })).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'Open draft' })).toBeNull()
    expect((title()).value).toBe('Quarterly review')
  })

  it('edits a finished order: Save changes / Save & re-run with its files', () => {
    renderComposer({ workOrder: finished, initial: toWorkOrderInput(finished) })
    expect(screen.getByRole('button', { name: /Save changes/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: /Save & re-run/ })).toBeTruthy()
    expect(screen.getByText(/Saving changes applies to the next attempt/)).toBeTruthy()
    expect(screen.getByText('kept.txt')).toBeTruthy()
  })

  it('uses update rather than create, and a cancel button when provided', async () => {
    api.update.mockResolvedValue(finished)
    const onCancel = vi.fn()
    const { onSaved } = renderComposer({ workOrder: finished, initial: toWorkOrderInput(finished), onCancel })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalled()
    fireEvent.change(title(), { target: { value: 'Renamed' } })
    fireEvent.click(screen.getByRole('button', { name: /Save changes/ }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(api.update).toHaveBeenCalledWith('w2', expect.objectContaining({ title: 'Renamed' }))
    expect(api.create).not.toHaveBeenCalled()
  })

  it('re-runs: submits the saved order', async () => {
    api.update.mockResolvedValue(finished)
    const run = makeRun({ id: 'again' })
    api.submit.mockResolvedValue(run)
    const { onSubmitted } = renderComposer({ workOrder: finished, initial: toWorkOrderInput(finished) })
    fireEvent.click(screen.getByRole('button', { name: /Save & re-run/ }))
    await waitFor(() => expect(onSubmitted).toHaveBeenCalledWith(finished, run))
    expect(api.update).not.toHaveBeenCalled()
  })

  it('shows the status badge of the saved order', () => {
    renderComposer({ workOrder: finished, initial: toWorkOrderInput(finished) })
    expect(screen.getByText('succeeded')).toBeTruthy()
    expect(screen.getByText('WO-9')).toBeTruthy()
  })
})
