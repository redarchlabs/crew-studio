import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { agentsApi, crewsApi, llmsApi, tasksApi, toolsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeAgent, makeCrew, makeLlm, makeTask, makeTool } from '../../lib/testFixtures'
import CrewBuilderPage from './CrewBuilderPage'
import { renderRoute, setupApi, without, type RenderOpts } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))
vi.mock('../../components/graph/FlowCanvas', async () => ({ FlowCanvas: (await import('./testUtils')).FlowCanvasStub }))

const agents = [makeAgent({ id: 'a1', name: 'Ann', role: 'Analyst', tools: ['web'], llm_id: 'l1' }), makeAgent({ id: 'a2', name: 'Bob', role: 'Writer' })]
const tasks = [
  makeTask({ id: 't1', name: 'Gather', description: 'Research {company}', agent_id: 'a1' }),
  makeTask({ id: 't2', name: 'Write', description: 'Write about {company} for {audience}', agent_id: 'a2' }),
]
const llms = [makeLlm({ id: 'l1', name: 'Fast', model: 'openai/fast' })]
const tools = [makeTool({ key: 'web', name: 'Web search', description: 'Searches', available: false, requires_env: ['KEY'] })]
const crew = makeCrew({ id: 'c1', name: 'Research', task_ids: ['t1'], agent_ids: ['a1'], area_ids: ['area-1'] })

beforeEach(() => {
  setupApi()
  Element.prototype.scrollIntoView = vi.fn()
  vi.mocked(agentsApi.list).mockResolvedValue(agents)
  vi.mocked(tasksApi.list).mockResolvedValue(tasks)
  vi.mocked(llmsApi.list).mockResolvedValue(llms)
  vi.mocked(toolsApi.list).mockResolvedValue(tools)
  vi.mocked(crewsApi.list).mockResolvedValue([crew])
  vi.mocked(crewsApi.validate).mockResolvedValue({ valid: true, errors: [] })
})
afterEach(cleanup)

function open(id: string, opts: RenderOpts = {}) {
  return renderRoute(<CrewBuilderPage />, { path: '/crews/:id', url: `/crews/${id}`, ...opts })
}
const nameInput = () => screen.findByLabelText('Name') as Promise<HTMLInputElement>
const saveBtn = (label: string) => screen.getByRole('button', { name: label }) as HTMLButtonElement

describe('CrewBuilderPage: loading', () => {
  it('shows not found for an unknown crew', async () => {
    open('missing')
    expect(await screen.findByText('Crew not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to crews' }).getAttribute('href')).toBe('/crews')
  })

  it('shows an error when the catalog fails to load', async () => {
    vi.mocked(crewsApi.list).mockRejectedValue(new ApiError(500, { code: 'x', message: 'catalog down' }))
    open('c1')
    expect(await screen.findByText('catalog down')).toBeTruthy()
  })

  it('loads an existing crew into the form', async () => {
    open('c1')
    expect((await nameInput()).value).toBe('Research')
    expect(screen.getByRole('heading', { name: 'Research' })).toBeTruthy()
    expect(screen.getByText('Gather')).toBeTruthy()
    expect(saveBtn('Save').disabled).toBe(true)
  })
})

describe('CrewBuilderPage: creating', () => {
  it('shows area validation instead of calling the API when no area is chosen', async () => {
    open('new', { me: { all_areas: false, area_ids: ['area-1', 'area-2'] } })
    fireEvent.change(await nameInput(), { target: { value: 'My crew' } })
    fireEvent.click(saveBtn('Create'))
    expect(await screen.findByText('Choose at least one functional area')).toBeTruthy()
    expect(crewsApi.create).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalled()
  })

  it('pre-selects the only area of a single-area person and creates with area_ids', async () => {
    vi.mocked(crewsApi.create).mockResolvedValue(makeCrew({ id: 'new-1', name: 'My crew' }))
    const { router } = open('new', { me: { all_areas: false, area_ids: ['area-2'] } })
    fireEvent.change(await nameInput(), { target: { value: '  My crew  ' } })
    fireEvent.click(saveBtn('Create'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/crews/new-1'))
    expect(crewsApi.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'My crew', area_ids: ['area-2'], process: 'sequential', manager_llm_id: null }))
    expect(toast.success).toHaveBeenCalledWith('Created crew')
  })

  it('lets an all-areas person pick areas then saves them', async () => {
    vi.mocked(crewsApi.create).mockResolvedValue(makeCrew({ id: 'n2' }))
    open('new')
    fireEvent.change(await nameInput(), { target: { value: 'C' } })
    fireEvent.click(await screen.findByRole('option', { name: /Finance/ }))
    fireEvent.click(screen.getByRole('option', { name: /Ops/ }))
    fireEvent.click(saveBtn('Create'))
    await waitFor(() => expect(crewsApi.create).toHaveBeenCalled())
    expect(vi.mocked(crewsApi.create).mock.calls[0][0].area_ids).toEqual(['area-1', 'area-2'])
  })

  it('requires a name and validates the budget', async () => {
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    await nameInput()
    fireEvent.change(screen.getByLabelText(/Budget per run/), { target: { value: '0' } })
    fireEvent.click(saveBtn('Create'))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(screen.getByText('Budget must be greater than 0')).toBeTruthy()
    fireEvent.change(screen.getByLabelText(/Budget per run/), { target: { value: '' } })
    fireEvent.click(saveBtn('Create'))
    expect(screen.getByText('Budget must be greater than 0')).toBeTruthy()
    expect(crewsApi.create).not.toHaveBeenCalled()
  })

  it('hides the history link and delete for a new crew, and disables Run', async () => {
    open('new')
    await nameInput()
    expect(screen.queryByText('History')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(saveBtn('Run').disabled).toBe(true)
    expect(screen.getByText(/Save the crew to run server validation/)).toBeTruthy()
    expect(crewsApi.validate).not.toHaveBeenCalled()
  })

  it('maps 422 field details onto fields and general messages', async () => {
    vi.mocked(crewsApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['name: already taken', 'something else'] }))
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await nameInput(), { target: { value: 'Dup' } })
    fireEvent.click(saveBtn('Create'))
    expect(await screen.findByText('already taken')).toBeTruthy()
    expect(screen.getByText('something else')).toBeTruthy()
    expect(toast.error).toHaveBeenCalledWith('Could not save crew', expect.any(Object))
  })
})

describe('CrewBuilderPage: tasks, agents, process', () => {
  it('adding a task also adds its agent and the crew can then be saved', async () => {
    vi.mocked(crewsApi.update).mockResolvedValue({ ...crew, task_ids: ['t1', 't2'], agent_ids: ['a1', 'a2'] })
    open('c1')
    await nameInput()
    fireEvent.change(screen.getByLabelText('Task to add'), { target: { value: 't2' } })
    fireEvent.click(screen.getAllByRole('button', { name: 'Add' })[0])
    expect(screen.getByText('unsaved')).toBeTruthy()
    expect(screen.getByText('Write')).toBeTruthy()
    fireEvent.click(saveBtn('Save'))
    await waitFor(() => expect(crewsApi.update).toHaveBeenCalled())
    const [id, body] = vi.mocked(crewsApi.update).mock.calls[0]
    expect(id).toBe('c1')
    expect(body.task_ids).toEqual(['t1', 't2'])
    expect(body.agent_ids).toEqual(['a1', 'a2'])
    expect(body.area_ids).toEqual(['area-1'])
    await waitFor(() => expect(screen.queryByText('unsaved')).toBeNull())
    expect(toast.success).toHaveBeenCalledWith('Saved crew')
  })

  it('removes and reorders tasks', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, task_ids: ['t1', 't2'], agent_ids: ['a1', 'a2'] }])
    open('c1')
    await nameInput()
    const items = () => screen.getAllByRole('listitem').filter((li) => li.querySelector('a[href^="/tasks/"]')).map((li) => li.querySelector('a')?.textContent)
    expect(items()).toEqual(['Gather', 'Write'])
    fireEvent.click(screen.getAllByRole('button', { name: 'Move down' })[0])
    expect(items()).toEqual(['Write', 'Gather'])
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove task' })[0])
    expect(items()).toEqual(['Gather'])
  })

  it('adds a missing agent from the task row', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, task_ids: ['t2'], agent_ids: [] }])
    open('c1')
    await nameInput()
    fireEvent.click(screen.getByRole('button', { name: /not in crew/ }))
    expect(screen.queryByRole('button', { name: /not in crew/ })).toBeNull()
    expect(screen.getByText('unsaved')).toBeTruthy()
  })

  it('toggles agents with the multi select', async () => {
    open('c1')
    await nameInput()
    fireEvent.click(screen.getByRole('option', { name: /Bob/ }))
    expect(screen.getByRole('option', { name: /Bob/ }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByText('unsaved')).toBeTruthy()
  })

  it('hierarchical process requires and then saves a manager LLM', async () => {
    vi.mocked(crewsApi.update).mockResolvedValue(crew)
    open('c1')
    await nameInput()
    expect(screen.queryByLabelText('Manager LLM')).toBeNull()
    fireEvent.change(screen.getByLabelText('Process'), { target: { value: 'hierarchical' } })
    fireEvent.click(saveBtn('Save'))
    expect(await screen.findByText('Hierarchical crews need a manager LLM')).toBeTruthy()
    expect(crewsApi.update).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('Manager LLM'), { target: { value: 'l1' } })
    fireEvent.click(saveBtn('Save'))
    await waitFor(() => expect(crewsApi.update).toHaveBeenCalled())
    expect(vi.mocked(crewsApi.update).mock.calls[0][1]).toMatchObject({ process: 'hierarchical', manager_llm_id: 'l1' })
  })

  it('keeps a manager LLM the person cannot list selectable', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, process: 'hierarchical', manager_llm_id: 'secret' }])
    open('c1', { perms: without('llms.view') })
    await nameInput()
    const select = screen.getByLabelText('Manager LLM') as HTMLSelectElement
    expect(select.value).toBe('secret')
    expect(within(select).getByText('Assigned LLM (not visible to you)')).toBeTruthy()
    expect(llmsApi.list).not.toHaveBeenCalled()
  })

  it('switching back to sequential drops the manager on save', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, process: 'hierarchical', manager_llm_id: 'l1' }])
    vi.mocked(crewsApi.update).mockResolvedValue(crew)
    open('c1')
    await nameInput()
    fireEvent.change(screen.getByLabelText('Process'), { target: { value: 'sequential' } })
    fireEvent.click(saveBtn('Save'))
    await waitFor(() => expect(crewsApi.update).toHaveBeenCalled())
    expect(vi.mocked(crewsApi.update).mock.calls[0][1].manager_llm_id).toBeNull()
  })
})

describe('CrewBuilderPage: inputs', () => {
  it('adds an input row and blocks save with an invalid key', async () => {
    open('c1')
    await nameInput()
    fireEvent.click(screen.getAllByRole('button', { name: 'Add' })[1])
    fireEvent.change(screen.getByLabelText('Input 1 key'), { target: { value: 'Bad Key' } })
    fireEvent.click(saveBtn('Save'))
    expect(await screen.findByText(/must match \[a-z\]/)).toBeTruthy()
    expect(crewsApi.update).not.toHaveBeenCalled()
  })

  it('detects placeholders from tasks and agents, then reports none left', async () => {
    open('c1')
    await nameInput()
    fireEvent.click(screen.getByRole('button', { name: /Detect from placeholders/ }))
    expect(toast.success).toHaveBeenCalledWith('Added 1 input', { description: 'company' })
    expect((screen.getByLabelText('Input 1 key') as HTMLInputElement).value).toBe('company')
    fireEvent.click(screen.getByRole('button', { name: /Detect from placeholders/ }))
    expect(toast.info).toHaveBeenCalledWith('No new placeholders found', expect.any(Object))
  })

  it('treats placeholders from the crew agents and task agents as used keys', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1', name: 'Ann', goal: 'Study {topic}' })])
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, task_ids: ['t1'], agent_ids: ['a1'], inputs: [{ key: 'topic', label: 'Topic', default: '', required: true }, { key: 'idle', label: '', default: '', required: false }] }])
    open('c1')
    await nameInput()
    expect(screen.getByText('Unused keys: idle')).toBeTruthy()
  })

  it('removes an input', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, inputs: [{ key: 'k', label: '', default: '', required: true }] }])
    open('c1')
    await nameInput()
    fireEvent.click(screen.getByRole('button', { name: 'Remove input' }))
    expect(screen.queryByLabelText('Input 1 key')).toBeNull()
  })
})

describe('CrewBuilderPage: delete, run, validation, permissions', () => {
  it('requires a second click to delete, then navigates to the list', async () => {
    vi.mocked(crewsApi.remove).mockResolvedValue(undefined)
    const { router } = open('c1')
    await nameInput()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(crewsApi.remove).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/crews'))
    expect(crewsApi.remove).toHaveBeenCalledWith('c1')
    expect(toast.success).toHaveBeenCalledWith('Deleted Research')
  })

  it('reports a delete failure and stays', async () => {
    vi.mocked(crewsApi.remove).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'in use' }))
    const { router } = open('c1')
    await nameInput()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete Research', { description: 'in use' }))
    expect(router.state.location.pathname).toBe('/crews/c1')
  })

  it('hides delete without crews.delete', async () => {
    open('c1', { perms: without('crews.delete') })
    await nameInput()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
  })

  it('Run is enabled for a clean saved crew and opens the modal; disabled when dirty', async () => {
    open('c1')
    const input = await nameInput()
    expect(saveBtn('Run').disabled).toBe(false)
    fireEvent.change(input, { target: { value: 'Changed' } })
    expect(saveBtn('Run').disabled).toBe(true)
    fireEvent.change(input, { target: { value: 'Research' } })
    fireEvent.click(saveBtn('Run'))
    expect(await screen.findByRole('dialog')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('has no Run button without crews.run', async () => {
    open('c1', { perms: without('crews.run') })
    await nameInput()
    expect(screen.queryByRole('button', { name: 'Run' })).toBeNull()
  })

  it('is read-only without crews.edit', async () => {
    open('c1', { perms: without('crews.edit') })
    const input = await nameInput()
    expect(screen.getByRole('note').textContent).toMatch(/Read-only/)
    expect(screen.getByText('crews.edit')).toBeTruthy()
    expect(input.closest('fieldset')?.disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
  })

  it('a new crew needs crews.create or the form is read-only', async () => {
    open('new', { perms: without('crews.create') })
    await nameInput()
    expect(screen.getByText('crews.create')).toBeTruthy()
  })

  it('runs server validation for a saved crew and refreshes on demand', async () => {
    vi.mocked(crewsApi.validate).mockResolvedValueOnce({ valid: false, errors: ['crew has no agents'] })
    open('c1')
    expect(await screen.findByText('crew has no agents')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Re-validate' }))
    expect(await screen.findByText(/Ready to run/)).toBeTruthy()
    expect(crewsApi.validate).toHaveBeenCalledTimes(2)
  })

  it('shows draft warnings from the unsaved graph', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, task_ids: ['t2'], agent_ids: [] }])
    open('c1')
    await nameInput()
    expect(screen.getByText('Write: Agent is not in this crew')).toBeTruthy()
  })
})

describe('CrewBuilderPage: unsaved guard', () => {
  it('asks before leaving a dirty form and stays when declined', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { router } = open('c1')
    fireEvent.change(await nameInput(), { target: { value: 'Dirty' } })
    await act(async () => { await router.navigate('/elsewhere') })
    expect(confirm).toHaveBeenCalledWith(expect.stringMatching(/unsaved changes/))
    expect(router.state.location.pathname).toBe('/crews/c1')
    confirm.mockRestore()
  })

  it('leaves when confirmed', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { router } = open('c1')
    fireEvent.change(await nameInput(), { target: { value: 'Dirty' } })
    await act(async () => { await router.navigate('/elsewhere') })
    await waitFor(() => expect(router.state.location.pathname).toBe('/elsewhere'))
    confirm.mockRestore()
  })

  it('does not ask when clean', async () => {
    const confirm = vi.spyOn(window, 'confirm')
    const { router } = open('c1')
    await nameInput()
    await act(async () => { await router.navigate('/elsewhere') })
    expect(confirm).not.toHaveBeenCalled()
    expect(router.state.location.pathname).toBe('/elsewhere')
    confirm.mockRestore()
  })

  it('sets a beforeunload handler only while dirty', async () => {
    const add = vi.spyOn(window, 'addEventListener')
    open('c1')
    fireEvent.change(await nameInput(), { target: { value: 'Dirty' } })
    expect(add.mock.calls.some(([t]) => t === 'beforeunload')).toBe(true)
    const ev = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
    add.mockRestore()
  })
})

describe('CrewBuilderPage: canvas navigation', () => {
  beforeEach(() => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, process: 'hierarchical', manager_llm_id: 'l1' }])
  })

  it.each([
    ['task:t1', '/tasks/t1'],
    ['agent:a1', '/agents/a1'],
    ['llm:l1', '/llms?edit=l1'],
  ])('clicking node %s navigates to %s', async (nodeId, target) => {
    const { router } = open('c1')
    await nameInput()
    fireEvent.click(await screen.findByTestId(`node-${nodeId}`))
    await waitFor(() => expect(router.state.location.pathname + router.state.location.search).toBe(target))
  })

  it('default llm node goes to the llm list', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([{ ...crew, agent_ids: ['a2'] }])
    const { router } = open('c1')
    await nameInput()
    fireEvent.click(await screen.findByTestId('node-llm:default'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/llms'))
  })

  it('clicking the crew node scrolls to the form', async () => {
    open('c1')
    await nameInput()
    fireEvent.click(await screen.findByTestId('node-crew'))
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
  })

  it('clicking a tool node shows an info toast including unavailable env', async () => {
    open('c1')
    await nameInput()
    fireEvent.click(await screen.findByTestId('node-tool:web'))
    expect(toast.info).toHaveBeenCalledWith('Web search', { description: 'Searches — unavailable, needs KEY' })
  })

  it('clicking an unknown tool node explains it is not in the catalog', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1', name: 'Ann', tools: ['mystery'] })])
    open('c1')
    await nameInput()
    fireEvent.click(await screen.findByTestId('node-tool:mystery'))
    expect(toast.info).toHaveBeenCalledWith('mystery', { description: expect.stringMatching(/not in the server catalog/) })
  })
})
