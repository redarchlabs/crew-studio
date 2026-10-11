import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { chatApi } from '../../api/endpoints.chat'
import type { Agent, LLM } from '../../api/types'
import type { ChatSettings } from '../../api/types.chat'
import { ChatSettingsPanel } from './ChatSettingsPanel'
import { renderRoute, setupApi } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.chat')

const TS = '2026-10-05T07:00:00Z'
const agent = (id: string, name: string) => ({ id, name, role: `${name} role` }) as Agent
const llm = (id: string, name: string) => ({ id, name, model: `openai/${id}` }) as LLM
const settings = (over: Partial<ChatSettings> = {}): ChatSettings => ({
  crew_id: 'crew-1', enabled: false, front_desk_agent_id: null, llm_id: null, daily_budget_usd: 1, person_daily_budget_usd: 5, updated_by_name: '', updated_at: null, ...over,
})

beforeEach(() => {
  setupApi()
  vi.mocked(chatApi.settings).mockResolvedValue(settings())
})
afterEach(cleanup)

const render = (canEdit = true) =>
  renderRoute(<ChatSettingsPanel crewId="crew-1" agentIds={['a1', 'a2']} agents={[agent('a1', 'Desk'), agent('a2', 'Writer'), agent('a3', 'Outsider')]}
    llms={[llm('l1', 'Cheap')]} canEdit={canEdit} />)

describe('ChatSettingsPanel', () => {
  it('turns chat on with a front desk, a cheaper model and a budget', async () => {
    vi.mocked(chatApi.saveSettings).mockResolvedValue(settings({ enabled: true, front_desk_agent_id: 'a1', llm_id: 'l1', daily_budget_usd: 2.5, updated_by_name: 'Tester', updated_at: TS }))
    render()
    const save = await screen.findByRole('button', { name: 'Save chat settings' })
    expect((save as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByRole('option', { name: /Outsider/ })).toBeNull()
    fireEvent.click(screen.getByRole('switch'))
    fireEvent.change(screen.getByLabelText('Front desk agent'), { target: { value: 'a1' } })
    fireEvent.change(screen.getByLabelText('Chat model'), { target: { value: 'l1' } })
    fireEvent.change(screen.getByLabelText('Daily budget per conversation (USD)'), { target: { value: '2.5' } })
    fireEvent.change(screen.getByLabelText('Daily budget per person (USD)'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Daily budget per person (USD)'), { target: { value: '10' } })
    fireEvent.click(save)
    await waitFor(() => expect(chatApi.saveSettings).toHaveBeenCalledWith('crew-1', { enabled: true, front_desk_agent_id: 'a1', llm_id: 'l1', daily_budget_usd: 2.5, person_daily_budget_usd: 10 }))
    expect(await screen.findByText(/Last changed by Tester/)).toBeTruthy()
  })

  it('shows field errors from the server and keeps an unlisted model', async () => {
    vi.mocked(chatApi.settings).mockResolvedValue(settings({ llm_id: 'hidden', updated_at: TS }))
    vi.mocked(chatApi.saveSettings).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['front_desk_agent_id: pick the agent that answers chat'] }))
    render()
    expect(await screen.findByRole('option', { name: 'Chosen model (not visible to you)' })).toBeTruthy()
    expect(screen.getByText(/Last changed by someone/)).toBeTruthy()
    fireEvent.click(screen.getByRole('switch'))
    fireEvent.change(screen.getByLabelText('Daily budget per conversation (USD)'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Daily budget per conversation (USD)'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Front desk agent'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Chat model'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save chat settings' }))
    expect(await screen.findByText('pick the agent that answers chat')).toBeTruthy()
  })

  it('is read-only without crews.edit', async () => {
    render(false)
    await screen.findByLabelText('Front desk agent')
    expect(screen.queryByRole('button', { name: 'Save chat settings' })).toBeNull()
  })
})
