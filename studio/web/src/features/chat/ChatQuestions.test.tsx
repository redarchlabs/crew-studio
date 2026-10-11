import { act, cleanup, fireEvent, renderHook, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chatApi } from '../../api/endpoints.chat'
import { humanApi } from '../../api/endpoints.human'
import type { HumanQuestion } from '../../api/types.human'
import { renderRoute, setupApi } from '../crews/testUtils'
import { AI_NOTICE } from '../human/OpenQuestion'
import { useWaitingQuestions } from '../human/waitingQuestions'
import { ChatQuestions, useConversationQuestions, WaitingConversations } from './ChatQuestions'
import { MessageList } from './MessageList'
import { msg, view } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.chat')
vi.mock('../../api/endpoints.human')
vi.mock('../human/waitingQuestions', () => ({ useWaitingQuestions: vi.fn(() => []) }))

const TS = '2026-10-07T12:00:00Z'
function question(over: Partial<HumanQuestion> = {}): HumanQuestion {
  return {
    id: 'q1', run_id: 'run-h', work_order_id: null, work_order_number: null, work_order_title: '', crew_name: 'Lead', assignee_id: 'me-1',
    agent_role: 'Research lead', asked_for: 'Market analyst', task_name: 'Hand-off', question: 'List or negotiated prices?',
    context: 'The table needs one.', status: 'open', answer: '', answered_by_name: '', asked_at: TS, deadline_at: '2026-10-07T12:30:00Z',
    answered_at: null, conversation_id: 'conv-1', conversation_title: 'Vendors', handoff_id: 'h1', handoff_title: 'Vendor comparison', ...over,
  }
}

const handoff = { id: 'h1', title: 'Vendor comparison', worker_name: 'Ann', worker_role: 'Analyst', deadline_at: '2026-10-07T12:20:00Z', budget_usd: 0.5 }
const working = msg({ id: 'res', run_id: 'run-h', run_kind: 'handoff', run_status: 'running', handoff })

beforeEach(() => {
  setupApi()
  vi.mocked(useWaitingQuestions).mockReturnValue([])
  vi.mocked(chatApi.questions).mockResolvedValue([])
})
afterEach(cleanup)

describe('ChatQuestions', () => {
  it('shows an open question as a Waiting for you card, asked by the specialist through the lead, and answers it', async () => {
    const answered = question({ status: 'answered', answer: 'List prices.', answered_by_name: 'Ada' })
    vi.mocked(humanApi.answer).mockResolvedValue(answered)
    const onAnswered = vi.fn()
    renderRoute(<ChatQuestions questions={[question(), question({ id: 'q0', status: 'timed_out', question: 'Which region?' })]} onAnswered={onAnswered} />)
    expect(await screen.findByRole('region', { name: 'Waiting for you' })).toBeTruthy()
    expect(screen.getAllByText(/Asked by Market analyst through Research lead · Hand-off/)).toHaveLength(2)
    expect(screen.getByText('The table needs one.')).toBeTruthy()
    expect(screen.getByText(/it follows the agent's rules/)).toBeTruthy()
    expect(screen.getByText(AI_NOTICE)).toBeTruthy()
    expect(screen.getByText('Earlier questions (1)')).toBeTruthy()
    expect(screen.getByText('no answer in time')).toBeTruthy()
    fireEvent.change(await screen.findByLabelText('Your answer'), { target: { value: ' List prices. ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send answer' }))
    await waitFor(() => expect(humanApi.answer).toHaveBeenCalledWith('q1', 'List prices.'))
    expect(onAnswered).toHaveBeenCalledWith(answered)
  })

  it('keeps the card when the answer cannot be sent', async () => {
    vi.mocked(humanApi.answer).mockRejectedValue(new Error('This question timed out; the agent has moved on.'))
    const onAnswered = vi.fn()
    renderRoute(<ChatQuestions questions={[question({ asked_for: '', agent_role: '' })]} onAnswered={onAnswered} />)
    expect(await screen.findByText(/An agent · Hand-off/)).toBeTruthy()
    fireEvent.change(await screen.findByLabelText('Your answer'), { target: { value: 'List.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send answer' }))
    await waitFor(() => expect(humanApi.answer).toHaveBeenCalled())
    expect(onAnswered).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Your answer')).toBeTruthy()
  })

  it('warns only on questions from a hand-off', async () => {
    renderRoute(<ChatQuestions questions={[question({ handoff_id: null, conversation_id: null })]} onAnswered={vi.fn()} />)
    expect(await screen.findByText(/it follows the crew's rules/)).toBeTruthy()
    expect(screen.queryByText(AI_NOTICE)).toBeNull()
  })

  it('renders what the agents wrote as text, never markup', async () => {
    renderRoute(<ChatQuestions questions={[question({ question: '<img src=x onerror=alert(1)>', context: '<b>bold</b>' })]} onAnswered={vi.fn()} />)
    expect(await screen.findByText('<img src=x onerror=alert(1)>')).toBeTruthy()
    expect(screen.getByText('<b>bold</b>')).toBeTruthy()
    expect(document.querySelector('img')).toBeNull()
  })

  it('cannot be answered with an API key, or by someone else', async () => {
    renderRoute(<ChatQuestions questions={[question()]} onAnswered={vi.fn()} />, { me: { via: 'api_key' } })
    expect(await screen.findByText('Waiting on the person this conversation belongs to, signed in.')).toBeTruthy()
    expect(screen.queryByLabelText('Your answer')).toBeNull()
    cleanup()
    renderRoute(<ChatQuestions questions={[question({ assignee_id: 'someone-else' })]} onAnswered={vi.fn()} />)
    expect(await screen.findByText('Waiting on the person this conversation belongs to, signed in.')).toBeTruthy()
    cleanup()
    // Never a work order operator, even with nobody addressed
    renderRoute(<ChatQuestions questions={[question({ assignee_id: null })]} onAnswered={vi.fn()} />)
    expect(await screen.findByText('Waiting on the person this conversation belongs to, signed in.')).toBeTruthy()
  })

  it('shows earlier questions only under a fold, and nothing without questions', async () => {
    renderRoute(<ChatQuestions questions={[question({ status: 'answered', answer: 'Yes', answered_by_name: '' })]} onAnswered={vi.fn()} />)
    expect(await screen.findByRole('region', { name: 'Questions from the hand-off' })).toBeTruthy()
    expect(screen.getByText('Someone:')).toBeTruthy()
    cleanup()
    const { container } = renderRoute(<ChatQuestions questions={[]} onAnswered={vi.fn()} />)
    await waitFor(() => expect(container.querySelector('section')).toBeNull())
  })

  it('places each question under its hand-off in the thread', async () => {
    const other = msg({ id: 'res2', run_id: 'run-2', run_kind: 'handoff', run_status: 'running', handoff: { ...handoff, id: 'h2', title: 'Other' } })
    renderRoute(<MessageList messages={[working, other]} crewId="" frontDesk="Lead" now={Date.parse(TS)} onChange={vi.fn()}
      questions={[question()]} onAnswered={vi.fn()} />)
    expect(await screen.findAllByRole('region', { name: 'Waiting for you' })).toHaveLength(1)
  })
})

describe('useConversationQuestions', () => {
  const agentView = (over = {}) => view({ messages: [working], handoffs: [], ...over })

  it('merges the conversation list, the view and the pushed list, and reloads when one is asked or answered', async () => {
    vi.mocked(chatApi.questions).mockResolvedValue([question({ id: 'q0', status: 'answered' })])
    const { result, rerender } = renderHook(({ v }) => useConversationQuestions(v), { initialProps: { v: agentView({ questions: [question()] }) } })
    await waitFor(() => expect(result.current.questions.map((q) => q.id).sort()).toEqual(['q0', 'q1']))
    expect(chatApi.questions).toHaveBeenCalledTimes(1)
    // A question pushed by the stream for this conversation shows at once and the list is read again; others do not count
    vi.mocked(useWaitingQuestions).mockReturnValue([question({ id: 'q2' }), question({ id: 'q9', conversation_id: 'conv-other' })])
    rerender({ v: agentView({ questions: [question()] }) })
    await waitFor(() => expect(chatApi.questions).toHaveBeenCalledTimes(2))
    expect(result.current.questions.map((q) => q.id).sort()).toEqual(['q0', 'q1', 'q2'])
    // An answer given here replaces the card at once
    act(() => result.current.answered(question({ id: 'q2', status: 'answered' })))
    expect(result.current.questions.find((q) => q.id === 'q2')?.status).toBe('answered')
  })

  it('does not ask for questions without hand-offs', async () => {
    const { result } = renderHook(() => useConversationQuestions(view()))
    await waitFor(() => expect(result.current.questions).toEqual([]))
    expect(chatApi.questions).not.toHaveBeenCalled()
  })
})

describe('WaitingConversations', () => {
  it('links to the other conversations waiting on the person', async () => {
    vi.mocked(useWaitingQuestions).mockReturnValue([
      question(), question({ id: 'q2', conversation_id: 'conv-2', conversation_title: 'Budget' }), question({ id: 'q3', conversation_id: 'conv-2' }),
      question({ id: 'q4', conversation_id: 'conv-3', conversation_title: '' }), question({ id: 'q5', conversation_id: null }),
    ])
    renderRoute(<WaitingConversations current="conv-1" />)
    expect((await screen.findByRole('link', { name: 'Budget (2)' })).getAttribute('href')).toBe('/chat/conv-2')
    expect(screen.getByRole('link', { name: 'A conversation' }).getAttribute('href')).toBe('/chat/conv-3')
    expect(screen.queryByRole('link', { name: /Vendors/ })).toBeNull()
  })

  it('shows nothing when no other conversation waits', async () => {
    vi.mocked(useWaitingQuestions).mockReturnValue([question()])
    const { container } = renderRoute(<WaitingConversations current="conv-1" />)
    await waitFor(() => expect(container.querySelector('section')).toBeNull())
  })
})

describe('ChatPage with a question from a hand-off', () => {
  it('shows the Waiting for you card under its hand-off; answering it reads the conversation again', async () => {
    const { default: ChatPage } = await import('./ChatPage')
    vi.mocked(chatApi.crews).mockResolvedValue([])
    vi.mocked(chatApi.agents).mockResolvedValue([{ id: 'agent-1', name: 'Lead', role: 'Research lead', goal: '', specialists: [], crews: [] }])
    vi.mocked(chatApi.conversations).mockResolvedValue([])
    const agentConv = { id: 'conv-1', crew_id: null, crew_name: '', agent_id: 'agent-1', agent_name: 'Lead', user_id: 'me-1', title: 'Vendors', created_at: TS, updated_at: TS }
    vi.mocked(chatApi.conversation).mockResolvedValue(view({ conversation: agentConv, messages: [working], questions: [question()], handoffs: [] }))
    vi.mocked(humanApi.answer).mockResolvedValue(question({ status: 'answered', answer: 'List.' }))
    renderRoute(<ChatPage />, { path: '/chat/:conversationId?', url: '/chat/conv-1' })
    expect(await screen.findByText('Ann is working in the background')).toBeTruthy()
    expect(screen.getByRole('region', { name: 'Waiting for you' })).toBeTruthy()
    fireEvent.change(await screen.findByLabelText('Your answer'), { target: { value: 'List.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Send answer' }))
    await waitFor(() => expect(humanApi.answer).toHaveBeenCalledWith('q1', 'List.'))
    await waitFor(() => expect(vi.mocked(chatApi.conversation).mock.calls.length).toBeGreaterThan(1))
  })
})
