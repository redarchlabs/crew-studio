import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { chatApi } from '../../api/endpoints.chat'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { Agent, Crew, LLM } from '../../api/types'
import type { AgentChatSettings } from '../../api/types.chat'
import { makeConnection } from '../connections/testUtils'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { AgentChatPanel, MAX_CONNECTIONS, MAX_CREWS, MAX_SPECIALISTS } from './AgentChatPanel'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.chat')
vi.mock('../../api/endpoints.crewstate')

const TS = '2026-10-06T07:00:00Z'
const agent = (id: string, name: string, llm: string | null = 'l1') => ({ id, name, role: `${name} role`, llm_id: llm }) as Agent
const crew = (id: string, name: string, description = '') => ({ id, name, description }) as Crew
const llm = (id: string, name: string) => ({ id, name, model: `openai/${id}` }) as LLM
const settings = (over: Partial<AgentChatSettings> = {}): AgentChatSettings => ({
  agent_id: 'lead', enabled: false, llm_id: null, specialist_ids: [], crew_ids: [], connection_ids: [], daily_budget_usd: 1, person_daily_budget_usd: 5,
  updated_by_name: '', updated_at: null, hidden_specialists: 0, hidden_crews: 0, hidden_connections: 0, handoffs_enabled: false, handoff_budget_usd: 0.5, handoff_timeout_minutes: 20, max_active_handoffs: 2,
  questions_enabled: false, question_rules: '', question_timeout_minutes: 30, question_on_timeout: 'proceed', max_questions: 3, specialists_may_propose: false,
  ...over,
})
const jira = makeConnection({ id: 'k1', name: 'Acme Jira', kind: 'jira' })
const km2 = makeConnection({ id: 'k2', name: 'Finance KM2', kind: 'km2', enabled: false })
const hook = makeConnection({ id: 'k3', name: 'Hook', kind: 'http' })

const lead = agent('lead', 'Lead')
const agents = [lead, agent('a1', 'Analyst'), agent('a2', 'No model', null)]

beforeEach(() => {
  setupApi()
  vi.mocked(chatApi.agentSettings).mockResolvedValue(settings())
  vi.mocked(connectionsApi.list).mockResolvedValue([km2, hook, jira])
})
afterEach(cleanup)

const render = (canEdit = true, list: Agent[] = agents, opts: Parameters<typeof renderRoute>[1] = {}) =>
  renderRoute(<AgentChatPanel agent={lead} agents={list} crews={[crew('c1', 'PMO', 'Sweeps projects')]} llms={[llm('l1', 'Cheap')]} canEdit={canEdit} />, opts)

describe('AgentChatPanel', () => {
  it('turns chat on with a specialist, a crew, a cheaper model and budgets', async () => {
    vi.mocked(chatApi.saveAgentSettings).mockResolvedValue(settings({ enabled: true, specialist_ids: ['a1'], crew_ids: ['c1'], updated_by_name: 'Tester', updated_at: TS }))
    render()
    const save = await screen.findByRole('button', { name: 'Save chat settings' })
    expect((save as HTMLButtonElement).disabled).toBe(true)
    // The agent itself is not offered, and an agent without a model cannot be picked
    expect(screen.queryByRole('option', { name: /^Lead/ })).toBeNull()
    expect((screen.getByRole('option', { name: /No model/ }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('Give it an LLM first.')).toBeTruthy()
    expect(screen.getByText('Sweeps projects')).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: /People can chat with this agent/ }))
    fireEvent.click(screen.getByRole('option', { name: /Analyst/ }))
    fireEvent.click(screen.getByRole('option', { name: /PMO/ }))
    fireEvent.change(screen.getByLabelText('Chat model'), { target: { value: 'l1' } })
    fireEvent.change(screen.getByLabelText('Daily budget per conversation (USD)'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Daily budget per conversation (USD)'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Daily budget per person (USD)'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Daily budget per person (USD)'), { target: { value: '9' } })
    fireEvent.click(save)
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', {
      enabled: true, llm_id: 'l1', specialist_ids: ['a1'], crew_ids: ['c1'], connection_ids: [], daily_budget_usd: 2, person_daily_budget_usd: 9,
      handoffs_enabled: false, handoff_budget_usd: 0.5, handoff_timeout_minutes: 20, max_active_handoffs: 2,
      questions_enabled: false, question_rules: '', question_timeout_minutes: 30, question_on_timeout: 'proceed', max_questions: 3, specialists_may_propose: false,
    }))
    expect(await screen.findByText(/Last changed by Tester/)).toBeTruthy()
  })

  it('says how many the server kept outside the editor’s areas and shows server errors', async () => {
    // The server leaves out specialists and crews outside the editor's areas and only counts them
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ llm_id: 'hidden', specialist_ids: ['a1'], crew_ids: [],
      hidden_specialists: 2, hidden_crews: 1, updated_at: TS }))
    vi.mocked(chatApi.saveAgentSettings).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix',
      details: ['specialist_ids: Analyst has the same role as Lead; give each a role of its own'] }))
    render()
    expect(await screen.findByRole('option', { name: 'Chosen model (not visible to you)' })).toBeTruthy()
    expect(screen.getByText(/2 more outside your areas are kept\./)).toBeTruthy()
    expect(screen.getByText(/1 more outside your areas is kept\./)).toBeTruthy()
    expect(screen.getByText(/Last changed by someone/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Chat model'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    // Only what the editor sees is sent; the server puts the others back
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', {
      enabled: false, llm_id: null, specialist_ids: ['a1'], crew_ids: [], connection_ids: [], daily_budget_usd: 1, person_daily_budget_usd: 5,
      handoffs_enabled: false, handoff_budget_usd: 0.5, handoff_timeout_minutes: 20, max_active_handoffs: 2,
      questions_enabled: false, question_rules: '', question_timeout_minutes: 30, question_on_timeout: 'proceed', max_questions: 3, specialists_may_propose: false,
    }))
    expect(await screen.findByText('Analyst has the same role as Lead; give each a role of its own')).toBeTruthy()
  })

  it('says nothing about hidden ones when there are none, even for ids it has no name for', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ specialist_ids: ['unlisted'], crew_ids: ['unlisted-crew'] }))
    render()
    await screen.findByLabelText('Chat model')
    expect(screen.queryByText(/outside your areas/)).toBeNull()
  })

  it('stops at the most specialists', async () => {
    const many = Array.from({ length: MAX_SPECIALISTS + 1 }, (_, i) => agent(`s${i}`, `Spec ${i}`))
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ specialist_ids: many.slice(0, MAX_SPECIALISTS).map((a) => a.id) }))
    render(true, [lead, ...many])
    const last = await screen.findByRole('option', { name: new RegExp(`Spec ${MAX_SPECIALISTS} `) })
    expect((last as HTMLButtonElement).disabled).toBe(true)
  })

  it('counts the hidden specialists and crews towards the most it may have', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ specialist_ids: [], hidden_specialists: MAX_SPECIALISTS,
      crew_ids: [], hidden_crews: MAX_CREWS }))
    render()
    expect(((await screen.findByRole('option', { name: /Analyst/ })) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('option', { name: /PMO/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('lists the tracker and KM2 connections it may read, with those outside the editor’s areas counted and kept', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ connection_ids: ['k2'], hidden_connections: 1 }))
    vi.mocked(chatApi.saveAgentSettings).mockResolvedValue(settings({ connection_ids: ['k2', 'k1'], hidden_connections: 1, updated_at: TS }))
    render()
    const option = await screen.findByRole('option', { name: /Acme Jira/ })
    // Only the kinds its read tools use are offered; a disabled one says it is not used until enabled
    expect(screen.queryByRole('option', { name: /Hook/ })).toBeNull()
    expect(screen.getByRole('option', { name: /Finance KM2/ }).textContent).toMatch(/KM2 · disabled: not used until enabled/)
    expect(option.textContent).toMatch(/Jira/)
    expect(screen.getByText(/1 more outside your areas is kept\./)).toBeTruthy()
    fireEvent.click(option)
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', expect.objectContaining({ connection_ids: ['k2', 'k1'] })))
  })

  it('offers MCP connections too, whose tools marked Reads the agent gets', async () => {
    vi.mocked(connectionsApi.list).mockResolvedValue([hook, makeConnection({ id: 'k4', name: 'Notes MCP', kind: 'mcp' })])
    render()
    expect((await screen.findByRole('option', { name: /Notes MCP/ })).textContent).toMatch(/MCP/)
    expect(screen.queryByRole('option', { name: /Hook/ })).toBeNull()
    expect(screen.getByText(/An MCP connection gives the agent itself the server’s tools marked Reads/)).toBeTruthy()
  })

  it('stops at the most connections, counting hidden ones', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ connection_ids: [], hidden_connections: MAX_CONNECTIONS }))
    render()
    expect(((await screen.findByRole('option', { name: /Acme Jira/ })) as HTMLButtonElement).disabled).toBe(true)
  })

  it('without View connections, says how many are listed instead of offering a list', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ connection_ids: ['k1'], hidden_connections: 2 }))
    render(true, agents, { perms: without('connections.view') })
    expect(await screen.findByText(/3 connections are listed\. You need View connections to see or change them\./)).toBeTruthy()
    expect(connectionsApi.list).not.toHaveBeenCalled()
    expect(screen.queryByRole('option', { name: /Acme Jira/ })).toBeNull()
  })

  it('turns hand-offs on with their limits and shows their errors', async () => {
    vi.mocked(chatApi.saveAgentSettings).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix',
      details: ['handoff_timeout_minutes: must be between 1 and 180'] }))
    render()
    const toggle = await screen.findByRole('switch', { name: /Hand off longer work/ })
    expect(screen.queryByLabelText('Time limit (minutes)')).toBeNull()
    fireEvent.click(toggle)
    fireEvent.change(screen.getByLabelText('Budget per hand-off (USD)'), { target: { value: '0.75' } })
    fireEvent.change(screen.getByLabelText('Time limit (minutes)'), { target: { value: '45' } })
    fireEvent.change(screen.getByLabelText('At once per conversation'), { target: { value: '3' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', expect.objectContaining({
      handoffs_enabled: true, handoff_budget_usd: 0.75, handoff_timeout_minutes: 45, max_active_handoffs: 3 })))
    expect(await screen.findByText('must be between 1 and 180')).toBeTruthy()
  })

  it('lets the lead ask the person during a hand-off, with rules, a wait, what then and a cap, and shows their errors', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ handoffs_enabled: true }))
    vi.mocked(chatApi.saveAgentSettings).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix',
      details: ['question_rules: say when the lead should ask the person'] }))
    render()
    const toggle = await screen.findByRole('switch', { name: /The lead may ask the person during a hand-off/ })
    expect(screen.queryByLabelText('When to ask the person')).toBeNull()
    fireEvent.click(toggle)
    fireEvent.change(screen.getByLabelText('When to ask the person'), { target: { value: 'Ask before choosing prices' } })
    fireEvent.change(screen.getByLabelText('Wait for an answer (minutes)'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Wait for an answer (minutes)'), { target: { value: '10' } })
    fireEvent.change(screen.getByLabelText('If nobody answers'), { target: { value: 'fail' } })
    fireEvent.change(screen.getByLabelText('Questions per hand-off'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Questions per hand-off'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', expect.objectContaining({
      questions_enabled: true, question_rules: 'Ask before choosing prices', question_timeout_minutes: 10, question_on_timeout: 'fail', max_questions: 5, specialists_may_propose: false })))
    expect(await screen.findByText('say when the lead should ask the person')).toBeTruthy()
  })

  it('hides the question settings while hand-offs are off', async () => {
    render()
    await screen.findByRole('switch', { name: /Hand off longer work/ })
    expect(screen.queryByRole('switch', { name: /The lead may ask the person/ })).toBeNull()
  })

  it('fills in hand-off defaults for an older server', async () => {
    const old = settings({ enabled: true }) as Partial<AgentChatSettings>
    delete old.handoffs_enabled
    delete old.handoff_budget_usd
    delete old.handoff_timeout_minutes
    delete old.max_active_handoffs
    delete old.questions_enabled
    delete old.question_rules
    delete old.question_timeout_minutes
    delete old.question_on_timeout
    delete old.max_questions
    vi.mocked(chatApi.agentSettings).mockResolvedValue(old as AgentChatSettings)
    vi.mocked(chatApi.saveAgentSettings).mockResolvedValue(settings({ updated_at: TS }))
    render()
    fireEvent.click(await screen.findByRole('switch', { name: /People can chat with this agent/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', expect.objectContaining({
      handoffs_enabled: false, handoff_budget_usd: 0.5, handoff_timeout_minutes: 20, max_active_handoffs: 2,
      questions_enabled: false, question_rules: '', question_timeout_minutes: 30, question_on_timeout: 'proceed', max_questions: 3, specialists_may_propose: false })))
  })

  it('is read-only without agents.edit', async () => {
    render(false)
    await screen.findByLabelText('Chat model')
    expect(screen.queryByRole('button', { name: 'Save chat settings' })).toBeNull()
  })

  it('lets specialists propose changes, listing which of their tools become cards', async () => {
    const writer = { ...lead, tools: ['tracker_comment', 'km_save_document', 'google_search'] }
    const analyst = { ...agent('a1', 'Analyst'), tools: ['tracker_suggest_change', 'tracker_create_issue'] }
    const reader = { ...agent('a3', 'Reader'), tools: ['google_search'] }
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ specialist_ids: ['a1', 'a3'] }))
    vi.mocked(chatApi.saveAgentSettings).mockResolvedValue(settings({ specialist_ids: ['a1', 'a3'], specialists_may_propose: true, updated_at: TS }))
    renderRoute(<AgentChatPanel agent={writer} agents={[writer, analyst, reader]} crews={[]} llms={[]} canEdit />)
    const list = await screen.findByRole('list', { name: 'Tools that become cards' })
    expect(list.textContent).toContain('Lead: Comment on Tracker Issue, Save to Knowledge (KM2).')
    expect(list.textContent).toContain('Analyst: Suggest Tracker Change, Create Tracker Issue (only with this on).')
    expect(list.textContent).not.toContain('Reader')
    fireEvent.click(screen.getByRole('switch', { name: /Specialists may propose changes/ }))
    expect(list.textContent).toContain('Analyst: Suggest Tracker Change, Create Tracker Issue.')
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    await waitFor(() => expect(chatApi.saveAgentSettings).toHaveBeenCalledWith('lead', expect.objectContaining({ specialists_may_propose: true })))
  })

  it('says when neither the agent nor its specialists have tools that become cards', async () => {
    vi.mocked(chatApi.agentSettings).mockResolvedValue(settings({ specialist_ids: ['a1'] }))
    render()
    const list = await screen.findByRole('list', { name: 'Tools that become cards' })
    expect(list.textContent).toContain('Lead has no tools that post to a tracker or save to KM2.')
    expect(list.textContent).toContain('None of its specialists you can see has such a tool.')
  })
})
