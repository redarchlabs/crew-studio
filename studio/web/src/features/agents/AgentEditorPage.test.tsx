import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { agentsApi, crewsApi, llmsApi, pricingApi, tasksApi, toolsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { chatApi } from '../../api/endpoints.chat'
import { connectionsApi } from '../../api/endpoints.crewstate'
import { makeAgent, makeCrew, makeLlm, makePrice, makeTask, makeTool } from '../../lib/testFixtures'
import { renderRoute, setupApi, without, type RenderOpts } from '../crews/testUtils'
import { AgentEditorPage } from './AgentEditorPage'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.chat')
vi.mock('../../api/endpoints.crewstate')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const agent = makeAgent({ id: 'a1', name: 'Ann', role: 'Analyst of {industry}', goal: 'Win', backstory: 'Old hand', llm_id: 'l1', tools: ['web'], area_ids: ['area-1'] })

beforeEach(() => {
  setupApi()
  vi.mocked(agentsApi.list).mockResolvedValue([agent])
  vi.mocked(llmsApi.list).mockResolvedValue([makeLlm({ id: 'l1', name: 'Fast', model: 'openai/fast' }), makeLlm({ id: 'l0', name: 'Alt', model: 'openai/alt' })])
  vi.mocked(toolsApi.list).mockResolvedValue([makeTool({ key: 'web', name: 'Web search' }), makeTool({ key: 'cal', name: 'Calendar' })])
  vi.mocked(pricingApi.list).mockResolvedValue([makePrice({ id: 'p', model: 'fast' })])
  vi.mocked(tasksApi.list).mockResolvedValue([makeTask({ id: 't1', name: 'Gather', agent_id: 'a1' })])
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Crew One', agent_ids: ['a1'], inputs: [{ key: 'industry', label: '', default: '', required: true }] })])
  vi.mocked(chatApi.agentSettings).mockResolvedValue({ agent_id: 'a1', enabled: false, llm_id: null, specialist_ids: [], crew_ids: [],
    connection_ids: [], daily_budget_usd: 1, person_daily_budget_usd: 5, updated_by_name: '', updated_at: null, hidden_specialists: 0,
    hidden_crews: 0, hidden_connections: 0, handoffs_enabled: false, handoff_budget_usd: 0.5, handoff_timeout_minutes: 20, max_active_handoffs: 2,
    questions_enabled: false, question_rules: '', question_timeout_minutes: 30, question_on_timeout: 'proceed', max_questions: 3, specialists_may_propose: false })
  vi.mocked(connectionsApi.list).mockResolvedValue([])
})
afterEach(cleanup)

function open(id: string, opts: RenderOpts = {}) {
  return renderRoute(<AgentEditorPage />, { path: '/agents/:id', url: `/agents/${id}`, ...opts })
}
const field = async (label: string) => (await screen.findByLabelText(label)) as HTMLInputElement
const btn = (name: string) => screen.getByRole('button', { name }) as HTMLButtonElement

describe('AgentEditorPage: chat', () => {
  it('shows the chat panel for a saved agent only', async () => {
    open('a1')
    expect(await screen.findByText('People can chat with this agent')).toBeTruthy()
    expect(chatApi.agentSettings).toHaveBeenCalledWith('a1', expect.anything())
    cleanup()
    open('new')
    await field('Name')
    expect(screen.queryByText('People can chat with this agent')).toBeNull()
  })
})

describe('AgentEditorPage: loading', () => {
  it('shows not found', async () => {
    open('nope')
    expect(await screen.findByText('Agent not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to agents' }).getAttribute('href')).toBe('/agents')
  })

  it('shows a load error', async () => {
    vi.mocked(toolsApi.list).mockRejectedValue(new ApiError(500, { code: 'x', message: 'tools broke' }))
    open('a1')
    expect(await screen.findByText('tools broke')).toBeTruthy()
  })

  it('loads values, price badge, and usage', async () => {
    open('a1')
    expect((await field('Name')).value).toBe('Ann')
    expect((screen.getByLabelText('Role') as HTMLTextAreaElement).value).toBe('Analyst of {industry}')
    expect((screen.getByLabelText('LLM') as HTMLSelectElement).value).toBe('l1')
    expect(screen.getByText(/matched without provider prefix/)).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Crew One' }).getAttribute('href')).toBe('/crews/c1')
    expect(screen.getByRole('link', { name: 'Gather' }).getAttribute('href')).toBe('/tasks/t1')
    expect(screen.getByText('Inputs available')).toBeTruthy()
    expect(screen.getAllByText('{industry}').length).toBeGreaterThan(0)
    expect(btn('Save').disabled).toBe(true)
  })

  it('shows empty usage and a missing-price warning for a lone agent', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([])
    vi.mocked(tasksApi.list).mockResolvedValue([])
    vi.mocked(pricingApi.list).mockResolvedValue([])
    open('a1')
    await field('Name')
    expect(screen.getByText('Not in any crew.')).toBeTruthy()
    expect(screen.getByText('Not assigned to any task.')).toBeTruthy()
    expect(screen.getByText(/No price entry/)).toBeTruthy()
    expect(screen.queryByText('Inputs available')).toBeNull()
  })

  it('says when the crews using the agent define no inputs', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', agent_ids: ['a1'], inputs: [] })])
    open('a1')
    await field('Name')
    expect(screen.getByText('Those crews define no inputs.')).toBeTruthy()
  })
})

describe('AgentEditorPage: create', () => {
  it('blocks save and shows all required-field errors, including the area', async () => {
    open('new', { me: { all_areas: false, area_ids: ['area-1', 'area-2'] } })
    await field('Name')
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(screen.getByText('Role is required')).toBeTruthy()
    expect(screen.getByText('Goal is required')).toBeTruthy()
    expect(screen.getByText('Choose at least one functional area')).toBeTruthy()
    expect(agentsApi.create).not.toHaveBeenCalled()
  })

  it('creates with trimmed text, chosen LLM, tools, toggles, and areas then navigates', async () => {
    vi.mocked(agentsApi.create).mockResolvedValue(makeAgent({ id: 'new-7' }))
    const { router } = open('new')
    fireEvent.change(await field('Name'), { target: { value: '  Researcher ' } })
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: ' Analyst ' } })
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: ' Find ' } })
    fireEvent.change(screen.getByLabelText('Backstory'), { target: { value: 'Story' } })
    fireEvent.change(screen.getByLabelText('LLM'), { target: { value: 'l0' } })
    fireEvent.click(await screen.findByRole('option', { name: /Finance/ }))
    fireEvent.click(screen.getByRole('option', { name: /Web search/ }))
    fireEvent.change(screen.getByLabelText('Max iterations'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('switch', { name: /Allow delegation/ }))
    fireEvent.click(screen.getByRole('switch', { name: /Verbose/ }))
    fireEvent.click(btn('Create'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/agents/new-7'))
    expect(agentsApi.create).toHaveBeenCalledWith({
      name: 'Researcher', role: 'Analyst', goal: 'Find', backstory: 'Story', llm_id: 'l0', tools: ['web'],
      max_iter: 5, allow_delegation: true, verbose: true, area_ids: ['area-1'],
    })
  })

  it('validates max iterations', async () => {
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await field('Name'), { target: { value: 'A' } })
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'R' } })
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'G' } })
    fireEvent.change(screen.getByLabelText('Max iterations'), { target: { value: '500' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('Whole number from 1 to 100', { selector: '[role=alert]' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Max iterations'), { target: { value: '' } })
    fireEvent.click(btn('Create'))
    expect(screen.getAllByText('Whole number from 1 to 100').length).toBeGreaterThan(0)
    expect(agentsApi.create).not.toHaveBeenCalled()
  })

  it('maps a 409 duplicate name onto the name field', async () => {
    vi.mocked(agentsApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['name: already exists', 'tools.0: unknown'] }))
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await field('Name'), { target: { value: 'Dup' } })
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'R' } })
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'G' } })
    fireEvent.click(btn('Create'))
    expect(await screen.findByText('already exists')).toBeTruthy()
    expect(screen.getByText('unknown')).toBeTruthy()
  })

  it('submits the form with Enter semantics (form submit)', async () => {
    vi.mocked(agentsApi.create).mockResolvedValue(makeAgent({ id: 'x' }))
    open('new', { me: { all_areas: false, area_ids: ['area-1'] } })
    fireEvent.change(await field('Name'), { target: { value: 'A' } })
    fireEvent.change(screen.getByLabelText('Role'), { target: { value: 'R' } })
    fireEvent.change(screen.getByLabelText('Goal'), { target: { value: 'G' } })
    fireEvent.submit(screen.getByLabelText('Name').closest('form') as HTMLFormElement)
    await waitFor(() => expect(agentsApi.create).toHaveBeenCalled())
  })

  it('offers no delete or history for a new agent', async () => {
    open('new')
    await field('Name')
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.queryByText('History')).toBeNull()
    expect(screen.getByRole('heading', { name: 'New agent' })).toBeTruthy()
  })
})

describe('AgentEditorPage: edit', () => {
  it('saves changes through update and clears the dirty marker', async () => {
    vi.mocked(agentsApi.update).mockResolvedValue(agent)
    open('a1')
    fireEvent.change(await field('Name'), { target: { value: 'Anna' } })
    expect(screen.getByText('unsaved')).toBeTruthy()
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(agentsApi.update).toHaveBeenCalled())
    expect(vi.mocked(agentsApi.update).mock.calls[0][0]).toBe('a1')
    expect(vi.mocked(agentsApi.update).mock.calls[0][1]).toMatchObject({ name: 'Anna', tools: ['web'], llm_id: 'l1' })
    await waitFor(() => expect(screen.queryByText('unsaved')).toBeNull())
  })

  it('can clear the LLM back to the default and deselect a tool', async () => {
    vi.mocked(agentsApi.update).mockResolvedValue(agent)
    open('a1')
    await field('Name')
    fireEvent.change(screen.getByLabelText('LLM'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('option', { name: /Web search/ }))
    fireEvent.click(btn('Save'))
    await waitFor(() => expect(agentsApi.update).toHaveBeenCalled())
    expect(vi.mocked(agentsApi.update).mock.calls[0][1]).toMatchObject({ llm_id: null, tools: [] })
    expect(screen.queryByText(/matched without provider prefix/)).toBeNull()
  })

  it('keeps an LLM the person cannot list and labels it', async () => {
    open('a1', { perms: without('llms.view') })
    const select = (await screen.findByLabelText('LLM')) as HTMLSelectElement
    expect(select.value).toBe('l1')
    expect(within(select).getByText('Assigned LLM (you cannot view LLMs)')).toBeTruthy()
    expect(screen.queryByText('Manage LLMs')).toBeNull()
  })

  it('labels a deleted LLM as missing when llms are viewable', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue([{ ...agent, llm_id: 'gone' }])
    open('a1')
    const select = (await screen.findByLabelText('LLM')) as HTMLSelectElement
    expect(within(select).getByText('Missing LLM')).toBeTruthy()
    expect(screen.getByText('Manage LLMs').getAttribute('href')).toBe('/llms')
  })

  it('deletes after confirming and navigates to the list', async () => {
    vi.mocked(agentsApi.remove).mockResolvedValue(undefined)
    const { router } = open('a1')
    await field('Name')
    fireEvent.click(btn('Delete'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(router.state.location.pathname).toBe('/agents'))
    expect(agentsApi.remove).toHaveBeenCalledWith('a1')
    expect(toast.success).toHaveBeenCalledWith('Deleted Ann')
  })

  it('reports an in-use conflict (409) when deleting and stays', async () => {
    vi.mocked(agentsApi.remove).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'Agent is used by task Gather' }))
    const { router } = open('a1')
    await field('Name')
    fireEvent.click(btn('Delete'))
    fireEvent.click(btn('Confirm?'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete Ann', { description: 'Agent is used by task Gather' }))
    expect(router.state.location.pathname).toBe('/agents/a1')
    expect(btn('Delete')).toBeTruthy()
  })

  it('is read-only without agents.edit', async () => {
    open('a1', { perms: without('agents.edit') })
    const name = await field('Name')
    expect(screen.getByRole('note').textContent).toMatch(/agents\.edit/)
    expect(name.closest('fieldset')?.disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getByText('read-only')).toBeTruthy()
  })

  it('does not submit via the form when read-only', async () => {
    open('a1', { perms: without('agents.edit') })
    await field('Name')
    fireEvent.submit(screen.getByLabelText('Name').closest('form') as HTMLFormElement)
    expect(agentsApi.update).not.toHaveBeenCalled()
  })

  it('hides delete without agents.delete but keeps save', async () => {
    open('a1', { perms: without('agents.delete') })
    fireEvent.change(await field('Name'), { target: { value: 'Z' } })
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(btn('Save').disabled).toBe(false)
  })

  it('warns before leaving with unsaved changes', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { router } = open('a1')
    fireEvent.change(await field('Name'), { target: { value: 'Dirty' } })
    await act(async () => { await router.navigate('/away') })
    expect(confirm).toHaveBeenCalled()
    expect(router.state.location.pathname).toBe('/agents/a1')
    confirm.mockRestore()
  })
})
