import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { ApiError } from '../../api/client'
import { crewsApi, schedulesApi, tasksApi, workOrdersApi } from '../../api/endpoints'
import { makeCrew, makeTask } from '../../lib/testFixtures'
import { makeSchedule, makeWorkOrder, renderRoute, setupApi, without, type RenderOpts } from '../crews/testUtils'
import { ScheduleEditorPage } from './ScheduleEditorPage'
import { watchesApi } from '../../api/endpoints.watch'
import { destinationsApi } from '../../api/endpoints.delivery'
import { makeDestinationsView } from '../delivery/testUtils'
import { makeView } from './watch/testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.watch')
// The report destinations panel loads its own data; unmocked, it would fetch for real and show an error alert
vi.mock('../../api/endpoints.delivery')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const tasks = [makeTask({ id: 'task-1', name: 'Study' }), makeTask({ id: 'task-2', name: 'Nothing' })]
const crews = [
  makeCrew({ id: 'crew-1', name: 'Research crew', task_ids: ['task-1'], inputs: [{ key: 'company', label: '', default: '', required: true }] }),
  makeCrew({ id: 'crew-2', name: 'Plain crew', task_ids: ['task-2'] }),
  makeCrew({ id: 'crew-3', name: 'Big crew', budget_usd: 4, inputs: [{ key: 'topic', label: 'Topic', default: 'AI', required: true }] }),
]

beforeEach(() => {
  setupApi()
  vi.mocked(tasksApi.list).mockResolvedValue(tasks)
  vi.mocked(crewsApi.list).mockResolvedValue(crews)
  vi.mocked(schedulesApi.preview).mockResolvedValue({ next: [] })
  vi.mocked(schedulesApi.get).mockResolvedValue(makeSchedule({ inputs: { company: 'ACME' } }))
  vi.mocked(watchesApi.get).mockResolvedValue(makeView({ watch: null, stats: null, checks: [] }))
  vi.mocked(destinationsApi.get).mockResolvedValue(makeDestinationsView())
})
afterEach(cleanup)

function open(id: string, opts: RenderOpts = {}) {
  return renderRoute(<ScheduleEditorPage />, { path: '/schedules/:id', url: `/schedules/${id}`, ...opts })
}
const name = async () => (await screen.findByLabelText('Name')) as HTMLInputElement
const btn = (n: string) => screen.getByRole('button', { name: n }) as HTMLButtonElement
async function pick(label: string) {
  fireEvent.click(document.querySelector('[aria-haspopup=listbox]') as HTMLElement)
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(label) }))
}

describe('ScheduleEditorPage: loading', () => {
  it('shows not found when the schedule is missing', async () => {
    vi.mocked(schedulesApi.get).mockResolvedValue(null as never)
    open('gone')
    expect(await screen.findByText('Schedule not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to schedules' }).getAttribute('href')).toBe('/schedules')
  })

  it('shows an error when loading fails', async () => {
    vi.mocked(schedulesApi.get).mockRejectedValue(new ApiError(500, { code: 'x', message: 'get failed' }))
    open('s1')
    expect(await screen.findByText('get failed')).toBeTruthy()
  })

  it('does not fetch a schedule for /new', async () => {
    open('new')
    await name()
    expect(schedulesApi.get).not.toHaveBeenCalled()
    expect(screen.getByRole('heading', { name: 'New schedule' })).toBeTruthy()
  })

  it('loads an existing schedule', async () => {
    vi.mocked(schedulesApi.get).mockResolvedValue(makeSchedule({ name: 'Digest', brief: 'Hello', overlap_policy: 'allow', budget_usd: 2 }))
    open('s1')
    expect((await name()).value).toBe('Digest')
    expect(screen.getByRole('heading', { name: 'Digest' })).toBeTruthy()
    expect((screen.getByLabelText('Cron expression') as HTMLInputElement).value).toBe('0 9 * * 1-5')
    expect(screen.getByRole('radio', { name: /Allow/ }).getAttribute('aria-checked')).toBe('true')
    expect((screen.getByLabelText('Budget (USD)') as HTMLInputElement).value).toBe('2')
    expect(btn('Save').disabled).toBe(true)
  })

  it('shows the last error banner', async () => {
    vi.mocked(schedulesApi.get).mockResolvedValue(makeSchedule({ last_error: 'Run-as lost access' }))
    open('s1')
    await name()
    expect(screen.getByRole('alert', { name: '' }).textContent).toMatch(/Last problem:\s*Run-as lost access/)
  })

  it('lists recent work orders of this schedule', async () => {
    vi.mocked(workOrdersApi.list).mockResolvedValue({ items: [makeWorkOrder({ id: 'wo-5', number: 5, title: 'Fired once' })], meta: { total: 1, limit: 10, offset: 0 } })
    open('s1')
    await name()
    expect(await screen.findByText('Fired once')).toBeTruthy()
    expect(workOrdersApi.list).toHaveBeenCalledWith({ source: 'schedule', source_id: 's1', limit: 10, offset: 0 }, expect.anything())
  })

  it('has no recent work orders panel for a new schedule', async () => {
    open('new')
    await name()
    expect(screen.queryByText('Recent work orders')).toBeNull()
  })
})

describe('ScheduleEditorPage: create', () => {
  it('shows required errors for name and target and does not call the API', async () => {
    open('new')
    await name()
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Give the schedule a name.')).toBeTruthy()
    expect(screen.getByText('Choose a crew to run.')).toBeTruthy()
    expect(schedulesApi.create).not.toHaveBeenCalled()
  })

  it('shows a cron error from local validation on save', async () => {
    open('new')
    fireEvent.change(await name(), { target: { value: 'N' } })
    await pick('Plain')
    fireEvent.change(screen.getByLabelText('Cron expression'), { target: { value: '* * * * *' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findAllByText('Schedules may not fire more often than every 5 minutes.')).not.toHaveLength(0)
    expect(schedulesApi.create).not.toHaveBeenCalled()
  })

  it('creates a crew schedule with trimmed name, normalized cron, and required inputs', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue(makeSchedule({ id: 'new-1' }))
    const { router } = open('new')
    fireEvent.change(await name(), { target: { value: '  Weekly  ' } })
    await pick('Research')
    expect(await screen.findByLabelText(/Company/)).toBeTruthy()
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Company is required')).toBeTruthy()
    expect(schedulesApi.create).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText(/Company/), { target: { value: 'ACME' } })
    fireEvent.change(screen.getByLabelText('Cron expression'), { target: { value: ' 0  8 * * 1 ' } })
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'UTC' } })
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/schedules/new-1'))
    expect(schedulesApi.create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Weekly', crew_id: 'crew-1', cron: '0 8 * * 1', timezone: 'UTC', inputs: { company: 'ACME' }, overlap_policy: 'skip', enabled: true, budget_usd: null,
    }))
  })

  it('has no task/crew toggle; choosing another crew swaps in its inputs and budget hint', async () => {
    open('new')
    await name()
    expect(screen.getByText('Choose a crew to see the inputs it needs.')).toBeTruthy()
    expect(screen.queryByRole('radio', { name: 'Task' })).toBeNull()
    await pick('Research crew')
    expect(await screen.findByLabelText(/Company/)).toBeTruthy()
    await pick('Big crew')
    expect(await screen.findByLabelText(/Topic/)).toBeTruthy()
    expect((screen.getByLabelText(/Topic/) as HTMLTextAreaElement).value).toBe('AI')
    expect(screen.queryByLabelText(/Company/)).toBeNull()
    expect(screen.getByText(/crew's budget of \$4\.00/)).toBeTruthy()
  })

  it('creates a crew schedule sending only the crew id', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue(makeSchedule({ id: 'n2' }))
    open('new')
    fireEvent.change(await name(), { target: { value: 'Crew run' } })
    await pick('Big crew')
    fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '3.5' } })
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(schedulesApi.create).toHaveBeenCalled())
    expect(vi.mocked(schedulesApi.create).mock.calls[0][0]).toMatchObject({ crew_id: 'crew-3', inputs: { topic: 'AI' }, budget_usd: 3.5 })
    expect(vi.mocked(schedulesApi.create).mock.calls[0][0]).not.toHaveProperty('task_id')
  })

  it('validates the budget', async () => {
    open('new')
    fireEvent.change(await name(), { target: { value: 'N' } })
    await pick('Plain')
    fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '0' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findByText(/Budget must be greater than 0/)).toBeTruthy()
    expect(schedulesApi.create).not.toHaveBeenCalled()
  })

  it('chooses the overlap policy', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue(makeSchedule({ id: 'n3' }))
    open('new')
    fireEvent.change(await name(), { target: { value: 'N' } })
    await pick('Plain')
    expect(screen.getByRole('radio', { name: /Skip/ }).getAttribute('aria-checked')).toBe('true')
    fireEvent.click(screen.getByRole('radio', { name: /Allow/ }))
    expect(screen.getByRole('radio', { name: /Allow/ }).getAttribute('aria-checked')).toBe('true')
    expect(screen.getByRole('radio', { name: /Skip/ }).getAttribute('aria-checked')).toBe('false')
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(schedulesApi.create).toHaveBeenCalled())
    expect(vi.mocked(schedulesApi.create).mock.calls[0][0].overlap_policy).toBe('allow')
  })

  it('creates a paused schedule when Enabled is switched off', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue(makeSchedule({ id: 'n4' }))
    open('new')
    fireEvent.change(await name(), { target: { value: 'N' } })
    await pick('Plain')
    fireEvent.click(screen.getByRole('switch', { name: /Enabled/ }))
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(schedulesApi.create).toHaveBeenCalled())
    expect(vi.mocked(schedulesApi.create).mock.calls[0][0].enabled).toBe(false)
  })

  it('maps a server 422 onto the cron field', async () => {
    vi.mocked(schedulesApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['cron: runs too often', 'run_as: lost'] }))
    open('new')
    fireEvent.change(await name(), { target: { value: 'N' } })
    await pick('Plain')
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('runs too often')).toBeTruthy()
    expect(screen.getByText('run_as: lost')).toBeTruthy()
    expect(toast.error).toHaveBeenCalledWith('Could not save schedule', expect.any(Object))
  })

  it('runs as the signed-in person and offers no delete or run now', async () => {
    open('new', { me: { name: 'Pat' } })
    await name()
    expect(screen.getByText('Pat')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Run now' })).toBeNull()
  })

  it('keeps the dirty marker until saved', async () => {
    open('new')
    expect(screen.queryByText('unsaved')).toBeNull()
    fireEvent.change(await name(), { target: { value: 'x' } })
    expect(screen.getByText('unsaved')).toBeTruthy()
  })
})

describe('ScheduleEditorPage: edit', () => {
  it('updates, keeps the entity id and clears dirty', async () => {
    vi.mocked(schedulesApi.update).mockResolvedValue(makeSchedule({ name: 'Renamed' }))
    open('s1')
    fireEvent.change(await name(), { target: { value: 'Renamed' } })
    expect(screen.getByText('unsaved')).toBeTruthy()
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(schedulesApi.update).toHaveBeenCalled())
    expect(vi.mocked(schedulesApi.update).mock.calls[0][0]).toBe('s1')
    expect(vi.mocked(schedulesApi.update).mock.calls[0][1]).toMatchObject({ name: 'Renamed', crew_id: 'crew-1' })
    await waitFor(() => expect(screen.queryByText('unsaved')).toBeNull())
    expect(screen.getByRole('heading', { name: 'Renamed' })).toBeTruthy()
  })

  it('refreshes the recent work orders after saving', async () => {
    vi.mocked(schedulesApi.update).mockResolvedValue(makeSchedule())
    open('s1')
    fireEvent.change(await name(), { target: { value: 'Renamed' } })
    await waitFor(() => expect(workOrdersApi.list).toHaveBeenCalledTimes(1))
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(workOrdersApi.list).toHaveBeenCalledTimes(2))
  })

  it('toggling enabled off then saving pauses the schedule', async () => {
    vi.mocked(schedulesApi.update).mockResolvedValue(makeSchedule({ enabled: false }))
    open('s1')
    await name()
    fireEvent.click(screen.getByRole('switch', { name: /Enabled/ }))
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(schedulesApi.update).toHaveBeenCalled())
    expect(vi.mocked(schedulesApi.update).mock.calls[0][1].enabled).toBe(false)
    expect(await screen.findByText('paused')).toBeTruthy()
  })

  it('shows who it runs as and warns that saving changes that for someone else', async () => {
    vi.mocked(schedulesApi.get).mockResolvedValue(makeSchedule({ run_as: 'other', run_as_name: 'Olive' }))
    open('s1')
    await name()
    expect(screen.getByText('Olive')).toBeTruthy()
    expect(screen.getByText(/Saving makes/)).toBeTruthy()
  })

  it('does not show the run-as warning when it already runs as you', async () => {
    open('s1')
    await name()
    expect(screen.queryByText(/Saving makes/)).toBeNull()
  })

  it('removes saved inputs the crew no longer uses', async () => {
    vi.mocked(schedulesApi.get).mockResolvedValue(makeSchedule({ crew_id: 'crew-2', inputs: { stale: 'x' } }))
    vi.mocked(schedulesApi.update).mockResolvedValue(makeSchedule())
    open('s1')
    await name()
    fireEvent.click(screen.getByRole('button', { name: 'Remove stale' }))
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(schedulesApi.update).toHaveBeenCalled())
    expect(vi.mocked(schedulesApi.update).mock.calls[0][1].inputs).toEqual({})
  })
})

describe('ScheduleEditorPage: run now', () => {
  it('creates a work order and opens it', async () => {
    vi.mocked(schedulesApi.runNow).mockResolvedValue(makeWorkOrder({ id: 'wo-42', number: 42 }))
    const { router } = open('s1')
    await name()
    fireEvent.click(btn('Run now'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/work-orders/wo-42'))
    expect(schedulesApi.runNow).toHaveBeenCalledWith('s1')
    expect(toast.success).toHaveBeenCalledWith('Started WO-42')
  })

  it('is disabled while there are unsaved changes', async () => {
    open('s1')
    fireEvent.change(await name(), { target: { value: 'x' } })
    expect(btn('Run now').disabled).toBe(true)
    expect(btn('Run now').title).toMatch(/Save your changes first/)
  })

  it('stays and toasts when it fails, then can retry', async () => {
    vi.mocked(schedulesApi.runNow).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'Previous run still active' }))
    const { router } = open('s1')
    await name()
    fireEvent.click(btn('Run now'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not run the schedule now', { description: 'Previous run still active' }))
    expect(router.state.location.pathname).toBe('/schedules/s1')
    await waitFor(() => expect(btn('Run now').disabled).toBe(false))
  })

  it('is hidden without work_orders.create', async () => {
    open('s1', { perms: without('work_orders.create') })
    await name()
    expect(screen.queryByRole('button', { name: 'Run now' })).toBeNull()
  })
})

describe('ScheduleEditorPage: delete', () => {
  it('deletes after confirmation and returns to the list', async () => {
    vi.mocked(schedulesApi.remove).mockResolvedValue(undefined)
    const { router } = open('s1')
    await name()
    fireEvent.click(btn('Delete'))
    expect(schedulesApi.remove).not.toHaveBeenCalled()
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/schedules'))
    expect(schedulesApi.remove).toHaveBeenCalledWith('s1')
    expect(toast.success).toHaveBeenCalledWith('Deleted Weekly digest')
  })

  it('stays and toasts when delete fails', async () => {
    vi.mocked(schedulesApi.remove).mockRejectedValue(new ApiError(500, { code: 'x', message: 'nope' }))
    const { router } = open('s1')
    await name()
    fireEvent.click(btn('Delete'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete Weekly digest', { description: 'nope' }))
    expect(router.state.location.pathname).toBe('/schedules/s1')
  })
})

describe('ScheduleEditorPage: permissions and guard', () => {
  it('is read-only without schedules.manage', async () => {
    open('s1', { perms: without('schedules.manage') })
    const input = await name()
    expect(screen.getByText(/Read only: changing schedules needs/)).toBeTruthy()
    expect(input.closest('fieldset')?.disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Run now' })).toBeNull()
    expect(screen.getByRole('link', { name: 'Back to schedules' })).toBeTruthy()
    expect(screen.queryByText(/Saving makes/)).toBeNull()
  })

  it('read-only header falls back to a generic title for a new schedule', async () => {
    open('new', { perms: without('schedules.manage') })
    await name()
    expect(screen.getByRole('heading', { name: 'Schedule' })).toBeTruthy()
  })

  it('does not guard navigation when read-only', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const { router } = open('s1', { perms: without('schedules.manage') })
    await name()
    await act(async () => { await router.navigate('/away') })
    expect(confirm).not.toHaveBeenCalled()
    confirm.mockRestore()
  })

  it('asks before leaving with unsaved changes', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { router } = open('s1')
    fireEvent.change(await name(), { target: { value: 'dirty' } })
    await act(async () => { await router.navigate('/away') })
    expect(confirm).toHaveBeenCalled()
    expect(router.state.location.pathname).toBe('/schedules/s1')
    confirm.mockRestore()
  })

  it('shows a forbidden crew list gracefully', async () => {
    vi.mocked(crewsApi.list).mockRejectedValue(new ApiError(403, { code: 'forbidden', message: 'no' }))
    open('new')
    await name()
    expect(within(document.body).getByText('You cannot view crews')).toBeTruthy()
  })
})
