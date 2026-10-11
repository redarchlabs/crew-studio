import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { agentsApi, crewsApi, tasksApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeAgent, makeCrew, makeTask } from '../../lib/testFixtures'
import { renderRoute, setupApi, without, type RenderOpts } from '../crews/testUtils'
import { TaskEditorPage } from './TaskEditorPage'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const task = makeTask({ id: 't1', name: 'Gather', description: 'Research {company}', expected_output: 'A report', agent_id: 'a1', context_task_ids: ['t2'], area_ids: ['area-1'], markdown: true })
const other = makeTask({ id: 't2', name: 'Earlier', expected_output: 'E'.repeat(200), area_ids: ['area-1'] })

beforeEach(() => {
  setupApi()
  vi.mocked(tasksApi.list).mockResolvedValue([task, other])
  vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1', name: 'Ann', role: 'Analyst' }), makeAgent({ id: 'a2', name: 'Bob', role: 'Writer' })])
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Crew One', task_ids: ['t1'], inputs: [{ key: 'company', label: '', default: '', required: true }] })])
})
afterEach(cleanup)

function open(id: string, opts: RenderOpts = {}) {
  return renderRoute(<TaskEditorPage />, { path: '/tasks/:id', url: `/tasks/${id}`, ...opts })
}
const field = async (label: string) => (await screen.findByLabelText(label)) as HTMLInputElement
const btn = (name: string) => screen.getByRole('button', { name }) as HTMLButtonElement
const fill = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })

describe('TaskEditorPage: loading', () => {
  it('shows not found', async () => {
    open('missing')
    expect(await screen.findByText('Task not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to tasks' }).getAttribute('href')).toBe('/tasks')
  })

  it('shows a load error', async () => {
    vi.mocked(agentsApi.list).mockRejectedValue(new ApiError(500, { code: 'x', message: 'agents broke' }))
    open('t1')
    expect(await screen.findByText('agents broke')).toBeTruthy()
  })

  it('loads values, agent, context selection, and crews', async () => {
    open('t1')
    expect((await field('Name')).value).toBe('Gather')
    expect((screen.getByLabelText('Agent') as HTMLSelectElement).value).toBe('a1')
    expect(screen.getByText('Open agent').getAttribute('href')).toBe('/agents/a1')
    expect(screen.getByRole('option', { name: /Earlier/ }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByRole('link', { name: 'Crew One' }).getAttribute('href')).toBe('/crews/c1')
    expect(btn('Save').disabled).toBe(true)
  })

  it('excludes the task itself from context options and truncates hints', async () => {
    open('t1')
    await field('Name')
    expect(screen.queryByRole('option', { name: /Gather/ })).toBeNull()
    expect(screen.getByRole('option', { name: /Earlier/ }).textContent).toContain('E'.repeat(90))
    expect(screen.getByRole('option', { name: /Earlier/ }).textContent).not.toContain('E'.repeat(91))
  })

  it('says a task not in any crew is unused', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([])
    open('t1')
    await field('Name')
    expect(screen.getByText('Not in any crew yet.')).toBeTruthy()
  })

  it('says there are no other tasks to use as context', async () => {
    vi.mocked(tasksApi.list).mockResolvedValue([task])
    open('t1')
    await field('Name')
    expect(screen.getByText('No other tasks exist yet.')).toBeTruthy()
  })
})

describe('TaskEditorPage: create', () => {
  it('requires name, description, expected output and an area', async () => {
    open('new', { me: { all_areas: false, area_ids: ['area-1', 'area-2'] } })
    await field('Name')
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(screen.getByText('Description is required')).toBeTruthy()
    expect(screen.getByText('Expected output is required')).toBeTruthy()
    expect(screen.getByText('Choose at least one functional area')).toBeTruthy()
    expect(tasksApi.create).not.toHaveBeenCalled()
  })

  it('creates with agent, context tasks, markdown off and area then navigates', async () => {
    vi.mocked(tasksApi.create).mockResolvedValue(makeTask({ id: 'new-3' }))
    const { router } = open('new')
    fireEvent.change(await field('Name'), { target: { value: ' Draft ' } })
    fill('Description', 'Write {topic}')
    fill('Expected output', 'A post')
    fill('Agent', 'a2')
    fireEvent.click(await screen.findByRole('option', { name: /Ops/ }))
    fireEvent.click(screen.getByRole('option', { name: /Gather/ }))
    fireEvent.click(screen.getByRole('switch', { name: /Markdown output/ }))
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/tasks/new-3'))
    expect(tasksApi.create).toHaveBeenCalledWith({
      name: 'Draft', description: 'Write {topic}', expected_output: 'A post', agent_id: 'a2', context_task_ids: ['t1'], markdown: false, area_ids: ['area-2'],
    })
  })

  it('defaults to the only area of a single-area person and to unassigned', async () => {
    vi.mocked(tasksApi.create).mockResolvedValue(makeTask({ id: 'n' }))
    open('new', { me: { all_areas: false, area_ids: ['area-2'] } })
    fireEvent.change(await field('Name'), { target: { value: 'T' } })
    fill('Description', 'D')
    fill('Expected output', 'E')
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(tasksApi.create).toHaveBeenCalled())
    expect(vi.mocked(tasksApi.create).mock.calls[0][0]).toMatchObject({ agent_id: null, area_ids: ['area-2'], markdown: true, context_task_ids: [] })
  })

  it('maps 422 details to fields and general errors', async () => {
    vi.mocked(tasksApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['name: duplicated', 'context_task_ids: cycle', 'other problem'] }))
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await field('Name'), { target: { value: 'T' } })
    fill('Description', 'D')
    fill('Expected output', 'E')
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('duplicated')).toBeTruthy()
    expect(screen.getByText('cycle')).toBeTruthy()
    expect(screen.getByText('other problem')).toBeTruthy()
  })

  it('has no delete button or Open agent link when new', async () => {
    open('new')
    await field('Name')
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.queryByText('Open agent')).toBeNull()
    expect(screen.getByRole('heading', { name: 'New task' })).toBeTruthy()
  })

  it('submits via form submit', async () => {
    vi.mocked(tasksApi.create).mockResolvedValue(makeTask({ id: 'n' }))
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await field('Name'), { target: { value: 'T' } })
    fill('Description', 'D')
    fill('Expected output', 'E')
    fireEvent.submit(screen.getByLabelText('Name').closest('form') as HTMLFormElement)
    await waitFor(() => expect(tasksApi.create).toHaveBeenCalled())
  })
})

describe('TaskEditorPage: edit', () => {
  it('updates and clears the dirty marker', async () => {
    vi.mocked(tasksApi.update).mockResolvedValue(task)
    open('t1')
    fireEvent.change(await field('Name'), { target: { value: 'Gather v2' } })
    expect(screen.getByText('unsaved')).toBeTruthy()
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(tasksApi.update).toHaveBeenCalled())
    expect(vi.mocked(tasksApi.update).mock.calls[0][0]).toBe('t1')
    expect(vi.mocked(tasksApi.update).mock.calls[0][1]).toMatchObject({ name: 'Gather v2', context_task_ids: ['t2'], agent_id: 'a1' })
    await waitFor(() => expect(screen.queryByText('unsaved')).toBeNull())
  })

  it('unassigns the agent and removes a context task', async () => {
    vi.mocked(tasksApi.update).mockResolvedValue(task)
    open('t1')
    await field('Name')
    fill('Agent', '')
    fireEvent.click(screen.getByRole('option', { name: /Earlier/ }))
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(tasksApi.update).toHaveBeenCalled())
    expect(vi.mocked(tasksApi.update).mock.calls[0][1]).toMatchObject({ agent_id: null, context_task_ids: [] })
    expect(screen.queryByText('Open agent')).toBeNull()
  })

  it('keeps an assigned agent the person cannot see selectable', async () => {
    vi.mocked(tasksApi.list).mockResolvedValue([{ ...task, agent_id: 'secret' }, other])
    open('t1')
    const select = (await screen.findByLabelText('Agent')) as HTMLSelectElement
    expect(select.value).toBe('secret')
    expect(within(select).getByText('Assigned agent (not visible to you)')).toBeTruthy()
  })

  it('deletes after a confirming click and goes back to the list', async () => {
    vi.mocked(tasksApi.remove).mockResolvedValue(undefined)
    const { router } = open('t1')
    await field('Name')
    fireEvent.click(btn('Delete'))
    expect(tasksApi.remove).not.toHaveBeenCalled()
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/tasks'))
    expect(tasksApi.remove).toHaveBeenCalledWith('t1')
    expect(toast.success).toHaveBeenCalledWith('Deleted Gather')
  })

  it('reports a 409 when the task is in use by a crew', async () => {
    vi.mocked(tasksApi.remove).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'Task is used by crew Crew One' }))
    const { router } = open('t1')
    await field('Name')
    fireEvent.click(btn('Delete'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete Gather', { description: 'Task is used by crew Crew One' }))
    expect(router.state.location.pathname).toBe('/tasks/t1')
  })

  it('is read-only without tasks.edit', async () => {
    open('t1', { perms: without('tasks.edit') })
    const name = await field('Name')
    expect(screen.getByRole('note').textContent).toMatch(/tasks\.edit/)
    expect(name.closest('fieldset')?.disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
  })

  it('ignores form submit when read-only', async () => {
    open('t1', { perms: without('tasks.edit') })
    await field('Name')
    fireEvent.submit(screen.getByLabelText('Name').closest('form') as HTMLFormElement)
    expect(tasksApi.update).not.toHaveBeenCalled()
  })

  it('hides delete without tasks.delete', async () => {
    open('t1', { perms: without('tasks.delete') })
    fireEvent.change(await field('Name'), { target: { value: 'x' } })
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(btn('Save')).toBeTruthy()
  })

  it('guards navigation while dirty', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { router } = open('t1')
    fireEvent.change(await field('Name'), { target: { value: 'Dirty' } })
    await act(async () => { await router.navigate('/away') })
    await waitFor(() => expect(router.state.location.pathname).toBe('/away'))
    expect(confirm).toHaveBeenCalled()
    confirm.mockRestore()
  })
})

describe('TaskEditorPage: Make a crew', () => {
  it('opens the crew builder with a draft made from the saved task', async () => {
    open('t1')
    await field('Name')
    fireEvent.click(btn('Make a crew'))
    expect((await screen.findByTestId('where')).textContent).toBe('/crews/new?fromTask=t1')
  })

  it('is disabled while the task has unsaved changes', async () => {
    open('t1')
    await field('Name')
    fill('Name', 'Changed')
    expect(btn('Make a crew').disabled).toBe(true)
    expect(btn('Make a crew').title).toBe('Save the task first')
  })

  it('is not offered for a new task', async () => {
    open('new')
    await field('Name')
    expect(screen.queryByRole('button', { name: 'Make a crew' })).toBeNull()
  })

  it('needs crews.create', async () => {
    open('t1', { perms: without('crews.create') })
    await field('Name')
    expect(screen.queryByRole('button', { name: 'Make a crew' })).toBeNull()
    expect(screen.queryByText(/To run just this task, use Make a crew/)).toBeNull()
  })

  it('is offered to read-only viewers who can create crews', async () => {
    open('t1', { perms: without('tasks.edit') })
    await field('Name')
    expect(screen.getByText('read-only')).toBeTruthy()
    expect(btn('Make a crew').disabled).toBe(false)
    expect(screen.getByText(/To run just this task, use Make a crew/)).toBeTruthy()
  })
})
