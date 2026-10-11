import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { ApiError } from '../../api/client'
import { crewsApi, tasksApi, triggersApi, workOrdersApi } from '../../api/endpoints'
import { makeCrew, makeTask } from '../../lib/testFixtures'
import { makeTrigger, makeWorkOrder, renderRoute, setupApi, without, type RenderOpts } from '../crews/testUtils'
import { TriggerEditorPage } from './TriggerEditorPage'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const tasks = [makeTask({ id: 'task-1', name: 'Study' }), makeTask({ id: 'task-2', name: 'Nothing' })]
const input = (key: string) => ({ key, label: '', default: '', required: true })
const crews = [
  makeCrew({ id: 'crew-1', name: 'Research crew', task_ids: ['task-1'], inputs: [input('company'), input('region')] }),
  makeCrew({ id: 'crew-2', name: 'Plain crew', task_ids: ['task-2'] }),
  makeCrew({ id: 'crew-3', name: 'Big crew', budget_usd: 4, inputs: [{ key: 'topic', label: 'Topic', default: '', required: true }] }),
]

beforeEach(() => {
  setupApi()
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
  vi.mocked(tasksApi.list).mockResolvedValue(tasks)
  vi.mocked(crewsApi.list).mockResolvedValue(crews)
  vi.mocked(triggersApi.get).mockResolvedValue(makeTrigger({ signing_secret: 'whsec_xyz', input_map: { company: 'customer.name', region: 'region' } }))
})
afterEach(cleanup)

function open(id: string, opts: RenderOpts = {}) {
  return renderRoute(<TriggerEditorPage />, { path: '/triggers/:id', url: `/triggers/${id}`, ...opts })
}
const name = async () => (await screen.findByLabelText('Name')) as HTMLInputElement
const btn = (n: string) => screen.getByRole('button', { name: n }) as HTMLButtonElement
async function pick(label: string) {
  fireEvent.click(document.querySelector('[aria-haspopup=listbox]') as HTMLElement)
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(label) }))
}
const createdTrigger = (over = {}) => makeTrigger({ id: 'new-1', name: 'Hook', token: 'cst_ab_ONLYONCE', signing_secret: 'whsec_new', ...over })

describe('TriggerEditorPage: loading', () => {
  it('shows not found', async () => {
    vi.mocked(triggersApi.get).mockResolvedValue(null as never)
    open('gone')
    expect(await screen.findByText('Trigger not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to triggers' }).getAttribute('href')).toBe('/triggers')
  })

  it('shows an error', async () => {
    vi.mocked(triggersApi.get).mockRejectedValue(new ApiError(500, { code: 'x', message: 'get failed' }))
    open('tr1')
    expect(await screen.findByText('get failed')).toBeTruthy()
  })

  it('does not fetch for /new and explains the token', async () => {
    open('new')
    await name()
    expect(triggersApi.get).not.toHaveBeenCalled()
    expect(screen.getByText(/The token is shown once/)).toBeTruthy()
    expect(screen.queryByText('Callback signing secret')).toBeNull()
    expect(screen.queryByText('Recent work orders')).toBeNull()
  })

  it('loads an existing trigger with its endpoint panel and callback URL', async () => {
    vi.mocked(triggersApi.get).mockResolvedValue(makeTrigger({ id: 'tr1', name: 'Inbound', callback_url: 'https://cb.example/x', fire_count: 3, last_fired_at: new Date(Date.now() - 7200_000).toISOString() }))
    open('tr1')
    expect((await name()).value).toBe('Inbound')
    expect((screen.getByLabelText('Callback URL') as HTMLInputElement).value).toBe('https://cb.example/x')
    expect(screen.getByText('http://localhost/api/hooks/tr1')).toBeTruthy()
    expect(screen.getByText('cst_cs_abcd_…')).toBeTruthy()
    expect(screen.getByText(/Fired 3 times/)).toBeTruthy()
    expect(screen.getByText('2h ago')).toBeTruthy()
    expect(btn('Save').disabled).toBe(true)
  })

  it('shows existing input mappings', async () => {
    open('tr1')
    await name()
    const keys = screen.getAllByLabelText('Input key') as HTMLInputElement[]
    expect(keys.map((k) => k.value)).toEqual(['company', 'region'])
    expect((screen.getAllByLabelText('Payload path') as HTMLInputElement[]).map((k) => k.value)).toEqual(['customer.name', 'region'])
  })

  it('shows recent work orders of the webhook source', async () => {
    vi.mocked(workOrdersApi.list).mockResolvedValue({ items: [makeWorkOrder({ id: 'wo-1', title: 'From webhook' })], meta: { total: 1, limit: 10, offset: 0 } })
    open('tr1')
    await name()
    expect(await screen.findByText('From webhook')).toBeTruthy()
    expect(workOrdersApi.list).toHaveBeenCalledWith({ source: 'webhook', source_id: 'tr1', limit: 10, offset: 0 }, expect.anything())
  })
})

describe('TriggerEditorPage: create and token reveal', () => {
  it('validates name and target', async () => {
    open('new')
    await name()
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Give the trigger a name.')).toBeTruthy()
    expect(screen.getByText('Choose a crew to run.')).toBeTruthy()
    expect(triggersApi.create).not.toHaveBeenCalled()
  })

  it('validates the input map and callback URL before saving', async () => {
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(btn('Add mapping'))
    fireEvent.change(screen.getByLabelText('Input key'), { target: { value: 'Bad Key' } })
    fireEvent.change(screen.getByLabelText('Payload path'), { target: { value: 'a.b' } })
    fireEvent.change(screen.getByLabelText('Callback URL'), { target: { value: 'not a url' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findByText(/"Bad Key" is not a valid input key/)).toBeTruthy()
    expect(screen.getByText(/Enter a full URL/)).toBeTruthy()
    expect(triggersApi.create).not.toHaveBeenCalled()
  })

  it('flags a mapping row with no payload path', async () => {
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(btn('Add mapping'))
    fireEvent.change(screen.getByLabelText('Input key'), { target: { value: 'k' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Give "k" a dotted payload path, e.g. customer.name.')).toBeTruthy()
  })

  it('creates, shows the token exactly once in a modal, then navigates to the trigger on close', async () => {
    vi.mocked(triggersApi.create).mockResolvedValue(createdTrigger())
    const { router } = open('new')
    fireEvent.change(await name(), { target: { value: '  Hook  ' } })
    await pick('Research')
    fireEvent.click(btn('Map all 2 crew inputs'))
    fireEvent.change(screen.getByLabelText('Callback URL'), { target: { value: ' https://cb.example/done ' } })
    fireEvent.click(btn('Create'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('cst_ab_ONLYONCE', { selector: 'pre' })).toBeTruthy()
    expect(vi.mocked(triggersApi.create).mock.calls[0][0]).not.toHaveProperty('task_id')
    expect(vi.mocked(triggersApi.create).mock.calls[0][0]).not.toHaveProperty('target_type')
    expect(triggersApi.create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Hook', crew_id: 'crew-1', callback_url: 'https://cb.example/done', input_map: { company: 'company', region: 'region' }, include_payload: true, enabled: true,
    }))
    expect(router.state.location.pathname).toBe('/triggers/new')
    fireEvent.click(within(dialog).getByRole('button', { name: 'I have saved the token' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/triggers/new-1'))
    expect(screen.queryByText('cst_ab_ONLYONCE')).toBeNull()
  })

  it('sends a null callback when left blank', async () => {
    vi.mocked(triggersApi.create).mockResolvedValue(createdTrigger())
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(btn('Create'))
    await screen.findByRole('dialog')
    expect(vi.mocked(triggersApi.create).mock.calls[0][0].callback_url).toBeNull()
  })

  it('does not warn about unsaved changes while the token modal is open or after it closes', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    vi.mocked(triggersApi.create).mockResolvedValue(createdTrigger())
    const { router } = open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(btn('Create'))
    await screen.findByRole('dialog')
    fireEvent.click(screen.getByRole('button', { name: 'I have saved the token' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/triggers/new-1'))
    expect(confirm).not.toHaveBeenCalled()
    confirm.mockRestore()
  })

  it('creates a crew-targeted trigger with a budget', async () => {
    vi.mocked(triggersApi.create).mockResolvedValue(createdTrigger())
    open('new')
    fireEvent.change(await name(), { target: { value: 'Crew hook' } })
    await pick('Big crew')
    expect(screen.getByText(/crew's budget of \$4\.00/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '2.5' } })
    fireEvent.click(btn('Create'))
    await screen.findByRole('dialog')
    expect(vi.mocked(triggersApi.create).mock.calls[0][0]).toMatchObject({ crew_id: 'crew-3', budget_usd: 2.5 })
  })

  it('rejects a bad budget', async () => {
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '-1' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findByText(/Budget must be greater than 0/)).toBeTruthy()
  })

  it('can create a disabled trigger without including the payload', async () => {
    vi.mocked(triggersApi.create).mockResolvedValue(createdTrigger())
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(screen.getByRole('switch', { name: /Enabled/ }))
    fireEvent.click(screen.getByRole('switch', { name: /Include the payload/ }))
    fireEvent.click(btn('Create'))
    await screen.findByRole('dialog')
    expect(vi.mocked(triggersApi.create).mock.calls[0][0]).toMatchObject({ enabled: false, include_payload: false })
  })

  it('maps server 422 details to fields', async () => {
    vi.mocked(triggersApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['callback_url: host not allowed', 'weird extra'] }))
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('host not allowed')).toBeTruthy()
    expect(screen.getByText('weird extra')).toBeTruthy()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('without a token in the response there is no modal and the editor stays usable', async () => {
    vi.mocked(triggersApi.create).mockResolvedValue(createdTrigger({ token: undefined }))
    open('new')
    fireEvent.change(await name(), { target: { value: 'Hook' } })
    await pick('Plain')
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(screen.getByText('Callback signing secret')).toBeTruthy())
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

describe('TriggerEditorPage: edit', () => {
  it('updates without showing a token', async () => {
    vi.mocked(triggersApi.update).mockResolvedValue(makeTrigger({ name: 'Renamed', signing_secret: 'whsec_xyz' }))
    open('tr1')
    fireEvent.change(await name(), { target: { value: 'Renamed' } })
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(triggersApi.update).toHaveBeenCalled())
    expect(vi.mocked(triggersApi.update).mock.calls[0][0]).toBe('tr1')
    expect(vi.mocked(triggersApi.update).mock.calls[0][1]).toMatchObject({ name: 'Renamed', input_map: { company: 'customer.name', region: 'region' } })
    await waitFor(() => expect(screen.queryByText('unsaved')).toBeNull())
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('keeps the signing secret when the update response omits it', async () => {
    vi.mocked(triggersApi.update).mockResolvedValue(makeTrigger({ signing_secret: undefined }))
    open('tr1')
    fireEvent.change(await name(), { target: { value: 'Renamed' } })
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(triggersApi.update).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: 'Reveal' }))
    expect(screen.getByText('whsec_xyz')).toBeTruthy()
  })

  it('adds, removes and edits mappings which are saved', async () => {
    vi.mocked(triggersApi.update).mockResolvedValue(makeTrigger())
    open('tr1')
    await name()
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove mapping' })[1])
    fireEvent.change(screen.getByLabelText('Payload path'), { target: { value: 'who.name' } })
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(triggersApi.update).toHaveBeenCalled())
    expect(vi.mocked(triggersApi.update).mock.calls[0][1].input_map).toEqual({ company: 'who.name' })
  })

  it('reveals the signing secret and shows the verifier', async () => {
    open('tr1')
    await name()
    expect(screen.queryByText('whsec_xyz')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Reveal' }))
    expect(screen.getByText('whsec_xyz')).toBeTruthy()
    expect(screen.getByText(/verifyCrewStudio/)).toBeTruthy()
  })

  it('asks before leaving with unsaved changes', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { router } = open('tr1')
    fireEvent.change(await name(), { target: { value: 'dirty' } })
    await act(async () => { await router.navigate('/away') })
    expect(confirm).toHaveBeenCalled()
    expect(router.state.location.pathname).toBe('/triggers/tr1')
    confirm.mockRestore()
  })

  it('deletes after confirmation', async () => {
    vi.mocked(triggersApi.remove).mockResolvedValue(undefined)
    const { router } = open('tr1')
    await name()
    fireEvent.click(btn('Delete'))
    expect(triggersApi.remove).not.toHaveBeenCalled()
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/triggers'))
    expect(triggersApi.remove).toHaveBeenCalledWith('tr1')
    expect(toast.success).toHaveBeenCalledWith('Deleted Inbound hook')
  })

  it('stays and toasts when delete fails', async () => {
    vi.mocked(triggersApi.remove).mockRejectedValue(new ApiError(500, { code: 'x', message: 'cannot delete' }))
    const { router } = open('tr1')
    await name()
    fireEvent.click(btn('Delete'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete Inbound hook', { description: 'cannot delete' }))
    expect(router.state.location.pathname).toBe('/triggers/tr1')
  })

  it('shows who the trigger runs as and the run-as warning for someone else', async () => {
    vi.mocked(triggersApi.get).mockResolvedValue(makeTrigger({ run_as: 'other', run_as_name: 'Olive' }))
    open('tr1')
    await name()
    expect(screen.getByText('Olive')).toBeTruthy()
    expect(screen.getByText(/Saving makes/)).toBeTruthy()
  })

  it('omits the run-as warning when it already runs as you', async () => {
    open('tr1')
    await name()
    expect(screen.queryByText(/Saving makes/)).toBeNull()
  })
})

describe('TriggerEditorPage: rotate token', () => {
  it('requires confirmation, then shows the new token once and the old one is gone', async () => {
    vi.mocked(triggersApi.rotate).mockResolvedValue(makeTrigger({ id: 'tr1', token: 'cst_cs_ROTATED', token_prefix: 'zzzz', signing_secret: undefined }))
    open('tr1')
    await name()
    fireEvent.click(btn('Rotate token'))
    expect(triggersApi.rotate).not.toHaveBeenCalled()
    fireEvent.click(btn('Confirm?'))
    const dialog = await screen.findByRole('dialog')
    expect(triggersApi.rotate).toHaveBeenCalledWith('tr1')
    expect(within(dialog).getByText('cst_cs_ROTATED', { selector: 'pre' })).toBeTruthy()
    expect(toast.success).toHaveBeenCalledWith('Token rotated; the old token no longer works')
    expect(screen.getByText('cst_zzzz_…')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'I have saved the token' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.queryByText('cst_cs_ROTATED')).toBeNull()
    // the signing secret survives a rotate response that omits it
    fireEvent.click(screen.getByRole('button', { name: 'Reveal' }))
    expect(screen.getByText('whsec_xyz')).toBeTruthy()
  })

  it('toasts and shows no modal when rotating fails', async () => {
    vi.mocked(triggersApi.rotate).mockRejectedValue(new ApiError(403, { code: 'forbidden', message: 'not yours' }))
    open('tr1')
    await name()
    fireEvent.click(btn('Rotate token'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not rotate the token', { description: 'not yours' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('does not open a modal when the response has no token', async () => {
    vi.mocked(triggersApi.rotate).mockResolvedValue(makeTrigger({ token: undefined }))
    open('tr1')
    await name()
    fireEvent.click(btn('Rotate token'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(toast.success).toHaveBeenCalled())
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('is not offered without work_orders.create', async () => {
    open('tr1', { perms: without('work_orders.create') })
    await name()
    expect(screen.queryByRole('button', { name: 'Rotate token' })).toBeNull()
  })

  it('is not offered without triggers.manage', async () => {
    open('tr1', { perms: without('triggers.manage') })
    await name()
    expect(screen.queryByRole('button', { name: 'Rotate token' })).toBeNull()
  })
})

describe('TriggerEditorPage: read-only', () => {
  it('locks the form, hides save/delete and the secret', async () => {
    vi.mocked(triggersApi.get).mockResolvedValue(makeTrigger({ signing_secret: undefined }))
    open('tr1', { perms: without('triggers.manage') })
    const input = await name()
    expect(screen.getByText(/Read only: changing triggers needs/)).toBeTruthy()
    expect(input.closest('fieldset')?.disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getByText(/Only people who can manage triggers/)).toBeTruthy()
    expect(screen.queryByText(/Saving makes/)).toBeNull()
  })

  it('uses a generic title for a new trigger', async () => {
    open('new', { perms: without('triggers.manage') })
    await name()
    expect(screen.getByRole('heading', { name: 'Trigger' })).toBeTruthy()
  })

  it('does not guard navigation', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const { router } = open('tr1', { perms: without('triggers.manage') })
    await name()
    await act(async () => { await router.navigate('/away') })
    expect(confirm).not.toHaveBeenCalled()
    confirm.mockRestore()
  })
})
