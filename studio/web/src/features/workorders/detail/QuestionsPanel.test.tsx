import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { humanApi } from '../../../api/endpoints.human'
import type { HumanQuestion } from '../../../api/types.human'
import { renderRoute, setPermissions } from '../testUtils'
import { QuestionsPanel } from './QuestionsPanel'

vi.mock('../../../api/endpoints.human')
vi.mock('../../../lib/auth', async () => (await import('../testUtils')).authMock())

const question = (over: Partial<HumanQuestion> = {}): HumanQuestion => ({
  id: 'q1', run_id: 'r1', work_order_id: 'wo-1', work_order_number: 6, work_order_title: 'Pricing', crew_name: 'AI Research Crew',
  assignee_id: 'u1', agent_role: 'Senior Research Analyst', task_name: 'AI Research Task', question: 'What budget should I assume?',
  context: 'The brief gives none.', status: 'open', answer: '', answered_by_name: '', asked_at: '2026-10-06T13:30:00Z',
  deadline_at: '2026-10-06T14:00:00Z', answered_at: null, ...over,
})

const list = vi.mocked(humanApi.workOrderQuestions)
beforeEach(() => {
  setPermissions('work_orders.view')
  list.mockReset().mockResolvedValue([question()])
})
afterEach(cleanup)

describe('QuestionsPanel', () => {
  it('shows nothing when the crew asked nothing', async () => {
    list.mockResolvedValue([])
    const { container } = renderRoute(<QuestionsPanel workOrderId="wo-1" refreshKey={0} />)
    await waitFor(() => expect(list).toHaveBeenCalled())
    expect(container.querySelector('section')).toBeNull()
  })

  it('lets the person asked send an answer', async () => {
    vi.mocked(humanApi.answer).mockResolvedValue(question({ status: 'answered', answer: 'Assume $5k.', answered_by_name: 'Ada' }))
    renderRoute(<QuestionsPanel workOrderId="wo-1" refreshKey={0} />)
    expect(await screen.findByText('Waiting for you (1)')).toBeTruthy()
    expect(screen.getByText('The brief gives none.')).toBeTruthy()
    const send = screen.getByRole('button', { name: 'Send answer' }) as HTMLButtonElement
    expect(send.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Your answer'), { target: { value: '  Assume $5k.  ' } })
    fireEvent.click(send)
    await waitFor(() => expect(humanApi.answer).toHaveBeenCalledWith('q1', 'Assume $5k.'))
    expect(await screen.findByText('Questions from the crew')).toBeTruthy()
    expect(screen.getByText('Assume $5k.')).toBeTruthy()
  })

  it('shows someone else’s question without an answer box', async () => {
    list.mockResolvedValue([question({ assignee_id: 'someone-else' })])
    renderRoute(<QuestionsPanel workOrderId="wo-1" refreshKey={0} />)
    expect(await screen.findByText('Waiting on the person who started this run.')).toBeTruthy()
    expect(screen.queryByLabelText('Your answer')).toBeNull()
  })

  it('lets work order operators answer unaddressed questions', async () => {
    setPermissions('work_orders.view', 'work_orders.cancel')
    list.mockResolvedValue([question({ assignee_id: null })])
    renderRoute(<QuestionsPanel workOrderId="wo-1" refreshKey={0} />)
    expect(await screen.findByLabelText('Your answer')).toBeTruthy()
  })

  it('lists earlier questions with their outcome and refetches when the journal changes', async () => {
    list.mockResolvedValue([
      question({ id: 'q2', status: 'timed_out', question: 'Which region?' }),
      question({ id: 'q3', status: 'answered', answer: 'EU', answered_by_name: 'Sam' }),
    ])
    const { rerender } = renderRoute(<QuestionsPanel workOrderId="wo-1" refreshKey={0} />)
    expect(await screen.findByText('Earlier questions (2)')).toBeTruthy()
    expect(screen.getByText('no answer in time')).toBeTruthy()
    expect(screen.getByText('EU')).toBeTruthy()
    expect(list).toHaveBeenCalledTimes(1)
    rerender(<QuestionsPanel workOrderId="wo-1" refreshKey={1} />)
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    // The list stays on screen while it reloads
    expect(screen.getByText('Earlier questions (2)')).toBeTruthy()
  })
})
