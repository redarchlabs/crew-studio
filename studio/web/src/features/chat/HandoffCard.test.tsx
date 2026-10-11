import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chatApi } from '../../api/endpoints.chat'
import type { ChatMessage } from '../../api/types.chat'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { HandoffCard } from './HandoffCard'
import { msg } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.chat')

const NOW = Date.parse('2026-10-07T12:03:00Z')
const result = (over: Partial<ChatMessage> = {}): ChatMessage => msg({
  id: 'res-1', run_id: 'run-9', run_kind: 'handoff', run_status: 'running', created_at: '2026-10-07T12:00:00Z',
  handoff: { id: 'h1', title: 'Vendor comparison', worker_name: 'Ann', worker_role: 'Analyst', deadline_at: '2026-10-07T12:20:00Z', budget_usd: 0.5,
    timeout_minutes: 20, started_at: '2026-10-07T12:00:00Z' },
  ...over,
})

beforeEach(() => setupApi())
afterEach(cleanup)

const render = (m: ChatMessage, onStale = vi.fn(), opts: Parameters<typeof renderRoute>[1] = {}) => {
  renderRoute(<HandoffCard message={m} now={NOW} onStale={onStale} />, opts)
  return onStale
}

describe('HandoffCard', () => {
  it('shows a hand-off waiting for a slot', async () => {
    render(result({ run_status: 'queued' }))
    expect(await screen.findByText('Ann will start when a slot is free')).toBeTruthy()
    expect(screen.getByText('Vendor comparison')).toBeTruthy()
    // Waiting for a slot does not count against its time limit
    expect(screen.getByText('up to 20 min once it starts · up to $0.50')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeTruthy()
  })

  it('shows a working hand-off with its time, limits and run, and cancels it', async () => {
    vi.mocked(chatApi.cancelHandoff).mockResolvedValue({} as never)
    const onStale = render(result())
    expect(await screen.findByText('Ann is working in the background')).toBeTruthy()
    expect(screen.getByText('started 3m ago · up to 20 min · up to $0.50')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'run' }).getAttribute('href')).toBe('/runs/run-9')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(chatApi.cancelHandoff).toHaveBeenCalledWith('h1'))
    expect(onStale).toHaveBeenCalled()
  })

  it('says when cancelling failed and leaves out the run link without runs.view', async () => {
    vi.mocked(chatApi.cancelHandoff).mockRejectedValue(new Error('boom'))
    // An older server sends no time limit or start: both come from the message
    const older = result({ handoff: { id: 'h1', title: 'Vendor comparison', worker_name: 'Ann', worker_role: 'Analyst', deadline_at: '2026-10-07T12:20:00Z', budget_usd: 0.5 } })
    const onStale = render(older, vi.fn(), { perms: without('runs.view') })
    expect(await screen.findByText('Ann is working in the background')).toBeTruthy()
    expect(screen.getByText('started 3m ago · up to 20 min · up to $0.50')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'run' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(chatApi.cancelHandoff).toHaveBeenCalled())
    expect(onStale).toHaveBeenCalled()
  })

  it('says why a hand-off did not finish', async () => {
    render(result({ run_status: 'failed', run_error: 'The hand-off ran past its 20-minute limit and was stopped.', cost_usd: 0.12 }))
    expect(await screen.findByText('The hand-off did not finish')).toBeTruthy()
    expect(screen.getByText('The hand-off ran past its 20-minute limit and was stopped.')).toBeTruthy()
    expect(screen.getByText('$0.12 spent')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull()
  })

  it('says a cancelled hand-off was cancelled by you', async () => {
    render(result({ run_status: 'cancelled', run_error: 'Cancelled.' }))
    expect(await screen.findByText('Cancelled by you')).toBeTruthy()
  })

  it('says when a finished hand-off brought nothing back', async () => {
    render(result({ run_status: 'succeeded', content: '' }))
    expect(await screen.findByText('No result came back.')).toBeTruthy()
  })
})
