import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { chatApi } from '../../api/endpoints.chat'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import ChatPage, { HANDOFF_POLL_MS, REPLY_POLL_MS } from './ChatPage'
import { MAX_MESSAGE } from './Composer'
import { chatAgent, chatCrew, conversation, msg, view } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.chat')

const routes = { path: '/chat/:conversationId?' }

beforeEach(() => {
  setupApi()
  vi.mocked(chatApi.crews).mockResolvedValue([chatCrew(), chatCrew({ id: 'crew-2', name: 'Finance desk', front_desk_name: 'Fin' })])
  vi.mocked(chatApi.agents).mockResolvedValue([])
  vi.mocked(chatApi.conversations).mockResolvedValue([conversation(), conversation({ id: 'conv-2', crew_id: 'crew-2', title: 'Budget?' })])
  vi.mocked(chatApi.conversation).mockResolvedValue(view())
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

const render = (url: string, perms?: NonNullable<Parameters<typeof renderRoute>[1]>['perms']) => renderRoute(<ChatPage />, { ...routes, url, perms })

describe('ChatPage', () => {
  it('explains when no crew or agent has chat on', async () => {
    vi.mocked(chatApi.crews).mockResolvedValue([])
    render('/chat')
    expect(await screen.findByText('No crew or agent you can chat with')).toBeTruthy()
    expect(screen.getByText(/Turn chat on in a crew/)).toBeTruthy()
  })

  it('tells people who cannot edit crews or agents whom to ask', async () => {
    vi.mocked(chatApi.crews).mockResolvedValue([])
    render('/chat', without('crews.edit', 'agents.edit'))
    expect(await screen.findByText(/Ask someone who can edit crews or agents/)).toBeTruthy()
  })

  it('starts a conversation with the chosen crew and opens it', async () => {
    vi.mocked(chatApi.start).mockResolvedValue(view({ conversation: conversation({ id: 'conv-9' }) }))
    const { router } = render('/chat?crew=crew-2')
    expect(await screen.findByText('Answered by Fin (PMO Coordinator).')).toBeTruthy()
    expect(await screen.findByText('Budget?')).toBeTruthy()
    expect(screen.queryByText('What is slipping?')).toBeNull()
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: '  Sweep now  ' } })
    fireEvent.keyDown(screen.getByLabelText('Message'), { key: 'Enter' })
    await waitFor(() => expect(chatApi.start).toHaveBeenCalledWith('crew-2', 'Sweep now', []))
    await waitFor(() => expect(router.state.location.pathname).toBe('/chat/conv-9'))
  })

  it('switches crews from the picker', async () => {
    const { router } = render('/chat')
    fireEvent.change(await screen.findByLabelText('Crew'), { target: { value: 'crew-2' } })
    await waitFor(() => expect(router.state.location.search).toBe('?crew=crew-2'))
  })

  it('shows a conversation, its budget, and sends the next message', async () => {
    vi.mocked(chatApi.send).mockResolvedValue(view({ messages: [...view().messages, msg({ id: 'u2', role: 'user', content: 'Why?' }), msg({ id: 'a2', run_status: 'queued' })] }))
    render('/chat/conv-1')
    expect(await screen.findByText('Atlas is.')).toBeTruthy()
    expect(screen.getByText(/\$0.25 of \$1.00 spent in the last 24 hours\./)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Why?' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(chatApi.send).toHaveBeenCalledWith('conv-1', 'Why?', []))
    expect(await screen.findByText('Waiting for a free slot…')).toBeTruthy()
    expect(screen.getByText('Wait for the reply before sending another message.')).toBeTruthy()
    expect((screen.getByLabelText('Message') as HTMLTextAreaElement).value).toBe('')
  })

  it('polls while a reply is being written', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.mocked(chatApi.conversation)
      .mockResolvedValueOnce(view({ messages: [msg({ id: 'a1', run_status: 'running' })] }))
      .mockResolvedValue(view({ messages: [msg({ id: 'a1', content: 'Done thinking.' })] }))
    render('/chat/conv-1')
    expect(await screen.findByText('Thinking…')).toBeTruthy()
    await act(async () => { await vi.advanceTimersByTimeAsync(REPLY_POLL_MS + 10) })
    expect(await screen.findByText('Done thinking.')).toBeTruthy()
    await waitFor(() => expect(chatApi.conversations).toHaveBeenCalledTimes(2))
  })

  it('keeps the text when sending fails, and shows failed and empty replies', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [
      msg({ id: 'a1', run_status: 'failed', run_error: 'Budget used up' }), msg({ id: 'a2', run_status: null, run_id: null }), msg({ id: 'a3', run_status: 'failed' }),
    ] }))
    vi.mocked(chatApi.send).mockRejectedValue(new ApiError(409, { code: 'chat_turn_active', message: 'Still answering' }))
    render('/chat/conv-1')
    expect(await screen.findByText('The reply failed: Budget used up')).toBeTruthy()
    expect(screen.getByText('The reply failed.')).toBeTruthy()
    expect(screen.getByText('No reply.')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'again' } })
    fireEvent.keyDown(screen.getByLabelText('Message'), { key: 'Enter', shiftKey: true })
    expect(chatApi.send).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(chatApi.send).toHaveBeenCalled())
    expect((screen.getByLabelText('Message') as HTMLTextAreaElement).value).toBe('again')
  })

  it('refuses messages that are too long, and stops when chat is off or the budget is spent', async () => {
    render('/chat/conv-1')
    fireEvent.change(await screen.findByLabelText('Message'), { target: { value: 'x'.repeat(MAX_MESSAGE + 1) } })
    expect(screen.getByText(/At most 4,000 characters/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement).disabled).toBe(true)
    cleanup()
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ chat_enabled: false }))
    render('/chat/conv-1')
    expect(await screen.findByText(/Chat is turned off for this crew/)).toBeTruthy()
    cleanup()
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ budget: { daily_budget_usd: 1, spent_last_24h_usd: 1, remaining_usd: 0 } }))
    render('/chat/conv-1')
    expect(await screen.findByText(/daily chat budget is used up/)).toBeTruthy()
  })

  it('names a crew whose chat was turned off', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: conversation({ crew_id: 'gone', crew_name: 'Old crew' }), chat_enabled: false }))
    render('/chat/conv-1')
    expect(await screen.findByRole('option', { name: 'Old crew' })).toBeTruthy()
    expect(screen.getAllByText('Front desk').length).toBeGreaterThan(0)
  })

  it('cannot start a conversation with a crew whose chat is off', async () => {
    render('/chat?crew=gone')
    expect(await screen.findByRole('option', { name: 'Chat is off for this crew' })).toBeTruthy()
    expect(screen.getByText('Chat is off for this crew.')).toBeTruthy()
  })

  it('deletes a conversation', async () => {
    vi.mocked(chatApi.remove).mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('nope'))
    const { router } = render('/chat/conv-1')
    await screen.findByText('Atlas is.')
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(chatApi.remove).toHaveBeenCalledWith('conv-1'))
    await waitFor(() => expect(router.state.location.search).toBe('?crew=crew-1'))
  })

  it('shows a failed delete', async () => {
    vi.mocked(chatApi.remove).mockRejectedValue(new Error('nope'))
    const { router } = render('/chat/conv-1')
    await screen.findByText('Atlas is.')
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(chatApi.remove).toHaveBeenCalled())
    expect(router.state.location.pathname).toBe('/chat/conv-1')
  })

  it('links replies to their runs only for people who can see runs', async () => {
    render('/chat/conv-1')
    const reply = (await screen.findByText('Atlas is.')).closest('li') as HTMLElement
    expect(within(reply).getByRole('link', { name: 'run' }).getAttribute('href')).toBe('/runs/r1')
    cleanup()
    render('/chat/conv-1', without('runs.view'))
    const again = (await screen.findByText('Atlas is.')).closest('li') as HTMLElement
    expect(within(again).queryByRole('link', { name: 'run' })).toBeNull()
  })
})

describe('ProposalCard', () => {
  const pending = msg({
    id: 'a1', content: 'I can start a sweep.', proposal_state: 'pending',
    proposal: { type: 'start_work_order', summary: 'Start a work order: Sweep', payload: { title: 'Sweep', brief: 'Sweep Atlas' } },
  })

  it('confirms a proposal and links its work order', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [pending] }))
    vi.mocked(chatApi.confirm).mockResolvedValue({ ...pending, proposal_state: 'confirmed', decided_by_name: 'Tester', decided_at: '2026-10-05T07:01:00Z',
      proposal_result: { work_order_id: 'w1', work_order_number: 12 } })
    render('/chat/conv-1')
    const card = within(await screen.findByRole('region', { name: 'Proposed change' }))
    expect(card.getByText('Waiting for you')).toBeTruthy()
    expect(card.getByText('Sweep Atlas')).toBeTruthy()
    fireEvent.click(card.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(chatApi.confirm).toHaveBeenCalledWith('a1'))
    expect(await card.findByRole('link', { name: 'Open WO-12' })).toBeTruthy()
    expect(card.getByText(/Confirmed by Tester/)).toBeTruthy()
    expect(card.queryByRole('button', { name: 'Confirm' })).toBeNull()
  })

  it('re-reads the conversation when a proposal turns out to be stale', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValueOnce(view({ messages: [pending] })).mockResolvedValue(view({ messages: [{ ...pending, proposal_state: 'expired' }] }))
    vi.mocked(chatApi.confirm).mockRejectedValue(new ApiError(409, { code: 'proposal_expired', message: 'Expired' }))
    render('/chat/conv-1')
    const card = within(await screen.findByRole('region', { name: 'Proposed change' }))
    fireEvent.click(card.getByRole('button', { name: 'Confirm' }))
    expect(await screen.findByText('Expired')).toBeTruthy()
    expect(chatApi.conversation).toHaveBeenCalledTimes(2)
  })

  it('shows the person budget when the server sends it', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ budget: { daily_budget_usd: 1, spent_last_24h_usd: 0.25, person_daily_budget_usd: 5, person_spent_last_24h_usd: 2, remaining_usd: 0.75 } }))
    render('/chat/conv-1')
    expect(await screen.findByText(/You: \$2.00 of \$5.00 with this crew\./)).toBeTruthy()
  })

  it('declines a proposal, and shows a failed decision', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [pending] }))
    vi.mocked(chatApi.confirm).mockRejectedValue(new ApiError(500, { code: 'internal', message: 'Boom' }))
    vi.mocked(chatApi.decline).mockResolvedValue({ ...pending, proposal_state: 'declined', decided_by_name: 'Tester' })
    render('/chat/conv-1')
    const card = within(await screen.findByRole('region', { name: 'Proposed change' }))
    fireEvent.click(card.getByRole('button', { name: 'Confirm' }))
    await waitFor(() => expect(chatApi.confirm).toHaveBeenCalled())
    expect(card.getByText('Waiting for you')).toBeTruthy()
    fireEvent.click(card.getByRole('button', { name: 'Decline' }))
    expect(await card.findByText('Declined')).toBeTruthy()
  })

  it('undoes a confirmed note change', async () => {
    const confirmed = msg({
      id: 'a1', content: 'Saved.', proposal_state: 'confirmed', decided_by_name: 'Tester',
      proposal: { type: 'notes_put', summary: 'Change the note risks (version 1)', payload: { name: 'risks', content: 'new', base_version: 1 } },
      proposal_result: { version: 2, undoable: true },
    })
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [confirmed] }))
    vi.mocked(chatApi.undo).mockRejectedValueOnce(new Error('changed')).mockResolvedValue({ ...confirmed, undone_at: '2026-10-05T08:00:00Z' })
    render('/chat/conv-1')
    const card = within(await screen.findByRole('region', { name: 'Proposed change' }))
    expect(card.getByRole('link', { name: 'Saved as version 2' }).getAttribute('href')).toBe('/crews/crew-1?tab=state')
    fireEvent.click(card.getByRole('button', { name: 'Undo' }))
    await waitFor(() => expect(chatApi.undo).toHaveBeenCalledTimes(1))
    fireEvent.click(card.getByRole('button', { name: 'Undo' }))
    expect(await card.findByText('Undone')).toBeTruthy()
    expect(card.queryByRole('button', { name: 'Undo' })).toBeNull()
  })

  it('shows expired proposals without buttons', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [{ ...pending, proposal_state: 'expired' }, msg({ id: 'a2', proposal: pending.proposal })]}))
    render('/chat/conv-1')
    const card = within(await screen.findByRole('region', { name: 'Proposed change' }))
    expect(card.getByText('Expired')).toBeTruthy()
    expect(card.queryByRole('button')).toBeNull()
  })
})

describe('Compacting a conversation', () => {
  const summary = (over: Parameters<typeof msg>[0] = {}) => msg({ id: 's1', role: 'summary', content: '', run_id: 'r9', run_status: 'running', ...over })

  it('compacts the conversation and waits for the summary', async () => {
    vi.mocked(chatApi.compact).mockResolvedValue(view({ messages: [...view().messages, summary()] }))
    render('/chat/conv-1')
    await screen.findByText('Atlas is.')
    expect(screen.getByText(/Each message resends about 7 tokens of this conversation\./)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Compact' }))
    await waitFor(() => expect(chatApi.compact).toHaveBeenCalledWith('conv-1'))
    expect(await screen.findByText('Summarising…')).toBeTruthy()
    expect(screen.getByText('Wait for the reply before sending another message.')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Compact' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows a finished summary, and nothing new to compact after it', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [...view().messages, summary({ content: 'Ada asked what slips; Atlas.', run_status: 'succeeded', cost_usd: 0.002 })] }))
    render('/chat/conv-1')
    const card = (await screen.findByLabelText('Conversation compacted')) as HTMLElement
    expect(within(card).getByText('From here on the crew is given this summary instead of the messages above.')).toBeTruthy()
    expect(within(card).getByText('Show the summary')).toBeTruthy()
    expect(within(card).getByText('Ada asked what slips; Atlas.')).toBeTruthy()
    expect(within(card).getByRole('link', { name: 'run' }).getAttribute('href')).toBe('/runs/r9')
    const button = screen.getByRole('button', { name: 'Compact' }) as HTMLButtonElement
    expect(button.disabled).toBe(true)
    expect(button.title).toBe('Nothing new to compact yet.')
  })

  it('says when a compaction failed, and when it could not start', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ messages: [...view().messages, summary({ run_status: 'failed', run_error: 'budget' })] }))
    vi.mocked(chatApi.compact).mockRejectedValue(new ApiError(409, { code: 'nothing_to_compact', message: 'Nothing new' }))
    render('/chat/conv-1')
    expect(await screen.findByText(/The compaction failed: budget\. The crew still gets the messages above\./)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Compact' }))
    await waitFor(() => expect(chatApi.compact).toHaveBeenCalled())
    await waitFor(() => expect((screen.getByRole('button', { name: 'Compact' }) as HTMLButtonElement).disabled).toBe(false))
  })
})


describe('ChatPage with agents', () => {
  const agentConv = conversation({ id: 'conv-a', crew_id: null, crew_name: '', agent_id: 'agent-1', agent_name: 'Deal Partner', title: 'Acme?' })

  beforeEach(() => {
    vi.mocked(chatApi.agents).mockResolvedValue([chatAgent(), chatAgent({ id: 'agent-3', name: 'Solo', role: 'Writer', specialists: [], crews: [] })])
    vi.mocked(chatApi.conversations).mockResolvedValue([conversation(), agentConv])
  })

  it('starts a conversation with an agent and says whom it can bring in', async () => {
    vi.mocked(chatApi.startWithAgent).mockResolvedValue(view({ conversation: agentConv }))
    const { router } = render('/chat?agent=agent-1')
    expect(await screen.findByText('Can ask: Ann.')).toBeTruthy()
    expect(screen.getByText('Can start: PMO.')).toBeTruthy()
    expect(screen.getByText(/Ask Deal Partner anything\. It can bring in Ann for parts of the work\. When a crew on its list fits/)).toBeTruthy()
    // The message box speaks of the agent, not a crew
    expect(screen.getByPlaceholderText('Message Deal Partner…')).toBeTruthy()
    expect(screen.getByText(/Runs the agent proposes wait for your Confirm\./)).toBeTruthy()
    expect(screen.queryByText(/Changes the crew proposes/)).toBeNull()
    // Only conversations with this agent are fetched and listed
    expect(await screen.findByText('Acme?')).toBeTruthy()
    expect(chatApi.conversations).toHaveBeenCalledWith(undefined, expect.anything(), 'agent-1')
    expect(screen.queryByText('What is slipping?')).toBeNull()
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Is Acme worth it?' } })
    fireEvent.keyDown(screen.getByLabelText('Message'), { key: 'Enter' })
    await waitFor(() => expect(chatApi.startWithAgent).toHaveBeenCalledWith('agent-1', 'Is Acme worth it?', []))
    expect(chatApi.start).not.toHaveBeenCalled()
    await waitFor(() => expect(router.state.location.pathname).toBe('/chat/conv-a'))
  })

  it('describes an agent that works alone', async () => {
    render('/chat?agent=agent-3')
    expect(await screen.findByText('Works alone.')).toBeTruthy()
    expect(screen.queryByText(/Can start/)).toBeNull()
    expect(screen.getByText(/^Ask Solo anything\./)).toBeTruthy()
  })

  it('switches between crews and agents', async () => {
    const { router } = render('/chat')
    fireEvent.click(await screen.findByRole('tab', { name: 'Agents' }))
    await waitFor(() => expect(router.state.location.search).toBe('?agent=agent-1'))
    fireEvent.change(await screen.findByLabelText('Agent'), { target: { value: 'agent-3' } })
    await waitFor(() => expect(router.state.location.search).toBe('?agent=agent-3'))
    fireEvent.click(screen.getByRole('tab', { name: 'Agents' }))
    fireEvent.click(screen.getByRole('tab', { name: 'Crews' }))
    await waitFor(() => expect(router.state.location.search).toBe('?crew=crew-1'))
  })

  it('opens with agents only when no crew has chat on', async () => {
    vi.mocked(chatApi.crews).mockResolvedValue([])
    render('/chat')
    expect(await screen.findByLabelText('Agent')).toBeTruthy()
    expect(screen.queryByRole('tab')).toBeNull()
  })

  it('speaks of the agent in an open conversation with it', async () => {
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv }))
    render('/chat/conv-a')
    expect(await screen.findByPlaceholderText('Message Deal Partner…')).toBeTruthy()
    expect(screen.getByText(/Runs the agent proposes wait for your Confirm\./)).toBeTruthy()
  })

  it('shows an agent conversation whose chat was turned off', async () => {
    vi.mocked(chatApi.agents).mockResolvedValue([])
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv, chat_enabled: false,
      budget: { daily_budget_usd: 1, spent_last_24h_usd: 0, person_daily_budget_usd: 5, person_spent_last_24h_usd: 1, remaining_usd: 1 } }))
    const { router } = render('/chat/conv-a')
    expect(await screen.findByRole('option', { name: 'Deal Partner' })).toBeTruthy()
    expect(screen.getByText(/Chat is turned off for this agent/)).toBeTruthy()
    expect(screen.getByText(/You: \$1\.00 of \$5\.00 with this agent\./)).toBeTruthy()
    vi.mocked(chatApi.remove).mockResolvedValue(undefined)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(router.state.location.search).toBe('?agent=agent-1'))
  })

  describe('hand-offs', () => {
    const handoff = { id: 'h1', title: 'Vendor comparison', worker_name: 'Ann', worker_role: 'Analyst', deadline_at: '2026-10-07T12:20:00Z', budget_usd: 0.5 }
    const working = msg({ id: 'res', run_id: 'run-h', run_kind: 'handoff', run_status: 'running', handoff })
    const card = msg({ id: 'a1', content: 'Handing it to Ann.', proposal: { type: 'start_handoff', summary: 'Hand off to Ann: Vendor comparison',
      payload: { worker_name: 'Ann', worker_role: 'Analyst', title: 'Vendor comparison', brief: 'Compare', budget_usd: 0.5, timeout_minutes: 20 } },
    proposal_state: 'pending', proposed_at: '2026-10-07T12:00:00Z' })

    it('polls slowly while a hand-off works, with the composer open', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      vi.mocked(chatApi.conversation)
        .mockResolvedValueOnce(view({ conversation: agentConv, messages: [card, working] }))
        .mockResolvedValue(view({ conversation: agentConv, messages: [card, { ...working, run_status: 'succeeded', content: 'Acme is cheapest.' }] }))
      render('/chat/conv-a')
      expect(await screen.findByText('Ann is working in the background')).toBeTruthy()
      // The composer stays open: hand-offs do not lock it, and deleting waits for them
      expect((screen.getByLabelText('Message') as HTMLTextAreaElement).disabled).toBe(false)
      expect((screen.getByRole('button', { name: 'Delete' }) as HTMLButtonElement).disabled).toBe(true)
      await act(async () => { await vi.advanceTimersByTimeAsync(REPLY_POLL_MS + 10) })
      expect(chatApi.conversation).toHaveBeenCalledTimes(1)
      const listed = vi.mocked(chatApi.conversations).mock.calls.length
      await act(async () => { await vi.advanceTimersByTimeAsync(HANDOFF_POLL_MS) })
      expect(await screen.findByText('Ann (hand-off result)')).toBeTruthy()
      expect(screen.getByText('Acme is cheapest.')).toBeTruthy()
      // The conversation list follows, as when a reply lands
      await waitFor(() => expect(chatApi.conversations).toHaveBeenCalledTimes(listed + 1))
    })

    it('polls quickly while a reply is written beside a hand-off', async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true })
      vi.mocked(chatApi.conversation)
        .mockResolvedValueOnce(view({ conversation: agentConv, messages: [working, msg({ id: 'a2', run_status: 'running' })] }))
        .mockResolvedValue(view({ conversation: agentConv, messages: [working, msg({ id: 'a2', content: 'Still on it.' })] }))
      render('/chat/conv-a')
      expect(await screen.findByText('Thinking…')).toBeTruthy()
      await act(async () => { await vi.advanceTimersByTimeAsync(REPLY_POLL_MS + 10) })
      expect(await screen.findByText('Still on it.')).toBeTruthy()
    })

    it('says a card on a hand-off result was proposed during the hand-off', async () => {
      const proposed = { ...working, run_status: 'succeeded' as const, content: 'Done.', proposal: { type: 'start_work_order', summary: 'Start a work order on PMO: Follow up',
        payload: { crew_name: 'PMO', title: 'Follow up', brief: 'b' } }, proposal_state: 'pending' as const, proposed_at: '2026-10-07T12:05:00Z' }
      vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv, messages: [proposed] }))
      render('/chat/conv-a')
      expect(await screen.findByText(/Proposed by Ann during a hand-off, while nobody was watching\./)).toBeTruthy()
    })

    it('shows the cards a hand-off proposed before its result, as plain text with a one-line lead-in', async () => {
      const comment = msg({ id: 'p1', content: 'Comment on **PRJ-10**', run_id: null, handoff, handoff_proposal: true,
        proposal: { type: 'tracker_comment', summary: 'Comment on PRJ-10', payload: { connection_name: 'Acme Jira', issue: 'PRJ-10',
          text: '<b>Summary</b> posted.', proposed_by: 'Ann' } }, proposal_state: 'confirmed',
        proposal_result: { issue: 'PRJ-10', url: 'https://acme.atlassian.net/browse/PRJ-10?focusedCommentId=101', outcome: 'unknown',
          message: 'The change was sent, but the answer never came back.' } })
      const question = { id: 'q1', handoff_id: handoff.id, conversation_id: 'conv-a', status: 'answered', question: 'Which prices?' }
      vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv, messages: [card, working, comment],
        questions: [question as never] }))
      render('/chat/conv-a')
      const lead = await screen.findByText('Ann proposed this during the hand-off “Vendor comparison”.')
      // Before the hand-off's card, though it was made later
      const items = screen.getAllByRole('listitem').map((li) => li.textContent ?? '')
      expect(items.findIndex((t) => t.includes('proposed this during the hand-off')))
        .toBeLessThan(items.findIndex((t) => t.includes('is working in the background')))
      expect(lead.closest('li')?.textContent).toContain('Ann (hand-off)')
      // The model's text is shown as text, never as HTML or Markdown
      expect(screen.getByText('<b>Summary</b> posted.')).toBeTruthy()
      expect(screen.queryByText('Comment on **PRJ-10**')).toBeNull()
      expect(screen.getByText(/Proposed by Ann during a hand-off, while nobody was watching\./)).toBeTruthy()
      const posted = screen.getByRole('link', { name: 'Open PRJ-10' })
      expect(posted.getAttribute('href')).toBe('https://acme.atlassian.net/browse/PRJ-10?focusedCommentId=101')
      expect(posted.getAttribute('target')).toBe('_blank')
      expect(screen.getByRole('note').textContent).toBe('The change was sent, but the answer never came back.')
      // The hand-off's questions show once, under its own card, not under each card it proposed
      expect(screen.getAllByRole('region', { name: 'Questions from the hand-off' })).toHaveLength(1)
    })

    it('says on a waiting tracker card what Studio will add when it posts it', async () => {
      const comment = msg({ id: 'p2', content: 'Comment on PRJ-10', proposal: { type: 'tracker_comment', summary: 'Comment on PRJ-10',
        payload: { connection_name: 'Acme Jira', issue: 'PRJ-10', text: 'Done.', proposed_by: 'Ann' } }, proposal_state: 'pending',
        proposed_at: '2026-10-07T12:05:00Z' })
      vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv, messages: [comment] }))
      render('/chat/conv-a')
      expect(await screen.findByText(/^Studio will add: \(Posted by .+ from a Crew Studio agent chat; drafted by Ann\.\)$/)).toBeTruthy()
    })

    it('waits for the reply to finish before a hand-off card can be confirmed', async () => {
      vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv, messages: [{ ...card, content: '', run_status: 'running' }] }))
      render('/chat/conv-a')
      expect(await screen.findByText('You can confirm once the reply has finished.')).toBeTruthy()
      expect((screen.getByRole('button', { name: 'Confirm' }) as HTMLButtonElement).disabled).toBe(true)
    })

    it('reloads the conversation once a hand-off card is confirmed', async () => {
      vi.mocked(chatApi.conversation)
        .mockResolvedValueOnce(view({ conversation: agentConv, messages: [card] }))
        .mockResolvedValue(view({ conversation: agentConv, messages: [{ ...card, proposal_state: 'confirmed', proposal_result: { run_id: 'run-h' } }, working] }))
      vi.mocked(chatApi.confirm).mockResolvedValue({ ...card, proposal_state: 'confirmed', proposal_result: { run_id: 'run-h' } })
      render('/chat/conv-a')
      expect(await screen.findByText('Hand off to Ann: Vendor comparison')).toBeTruthy()
      expect(screen.getByText('up to $0.50, up to 20 minutes')).toBeTruthy()
      fireEvent.click(screen.getByRole('button', { name: 'Confirm' }))
      expect(await screen.findByText('Ann is working in the background')).toBeTruthy()
      expect(screen.getByRole('link', { name: 'Hand-off run' }).getAttribute('href')).toBe('/runs/run-h')
    })
  })
})
