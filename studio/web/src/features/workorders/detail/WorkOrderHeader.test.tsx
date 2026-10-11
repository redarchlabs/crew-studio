import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen } from '@testing-library/react'
import { makeRun, makeWorkOrder, renderRoute, setPermissions } from '../testUtils'
import { emptyTally } from '../../runs/timeline/spendTally'
import { WorkOrderHeader } from './WorkOrderHeader'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())
vi.mock('../../../lib/auth', async () => (await import('../testUtils')).authMock())

type Props = Parameters<typeof WorkOrderHeader>[0]

function renderHeader(over: Partial<Props> = {}) {
  const handlers = { onSubmit: vi.fn(), onCancel: vi.fn(), onEdit: vi.fn(), onDelete: vi.fn() }
  const props: Props = {
    workOrder: makeWorkOrder({ status: 'succeeded', run_count: 2 }),
    status: 'succeeded',
    run: null,
    runStatus: null,
    spent: null,
    budgetState: null,
    tally: emptyTally,
    stream: 'idle',
    busy: null,
    ...handlers,
    ...over,
  }
  return { ...renderRoute(<WorkOrderHeader {...props} />), ...handlers }
}

beforeEach(() => setPermissions('work_orders.create', 'work_orders.delete', 'work_orders.cancel'))
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

const btn = (name: RegExp | string) => screen.getByRole('button', { name }) as HTMLButtonElement

describe('identity', () => {
  it('shows the number, title, status, creator and a back link', () => {
    renderHeader()
    expect(screen.getByText('WO-7')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Quarterly review' })).toBeTruthy()
    expect(screen.getByText('succeeded')).toBeTruthy()
    expect(screen.getByText(/by Ada/)).toBeTruthy()
    expect(screen.getByRole('link', { name: /Work orders/ }).getAttribute('href')).toBe('/work-orders')
    expect(screen.getByRole('link', { name: /History/ }).getAttribute('href')).toContain('entity_id=wo-1')
  })

  it('falls back to "unknown" creator and shows the submit time when set', () => {
    renderHeader({ workOrder: makeWorkOrder({ created_by_name: '', submitted_at: '2026-10-03T10:05:00Z' }) })
    expect(screen.getByText(/by unknown/)).toBeTruthy()
    expect(screen.getByText(/^submitted /)).toBeTruthy()
  })

  it('links the crew and the source when known', () => {
    renderHeader({ workOrder: makeWorkOrder({ source: 'schedule', source_id: 's1', crew_id: 'crew-9' }) })
    const hrefs = screen.getAllByRole('link').map((a) => a.getAttribute('href'))
    expect(hrefs).toContain('/crews/crew-9')
    expect(hrefs).toContain('/schedules/s1')
  })

  it('shows a deleted crew by its old name without a link', () => {
    renderHeader({ workOrder: makeWorkOrder({ crew_id: null, target_name: 'Gone crew' }) })
    expect(screen.getByText('Gone crew')).toBeTruthy()
    expect(screen.getAllByRole('link').map((a) => a.getAttribute('href')).some((h) => h?.startsWith('/crews/'))).toBe(false)
  })

  it('shows the live badge only when streaming', () => {
    const { unmount } = renderHeader({ stream: 'live' })
    expect(screen.getByText('live')).toBeTruthy()
    unmount()
    renderHeader({ stream: 'reconnecting' })
    expect(screen.queryByText('live')).toBeNull()
  })

  it('shows a draft badge for drafts', () => {
    renderHeader({ workOrder: makeWorkOrder({ status: 'draft' }), status: 'draft' })
    expect(screen.getByText('draft')).toBeTruthy()
  })
})

describe('actions by state', () => {
  it('finished: Re-run, Edit and Delete are available, Cancel is not', () => {
    const h = renderHeader()
    expect(screen.queryByRole('button', { name: /Cancel/ })).toBeNull()
    fireEvent.click(btn(/Re-run/))
    expect(h.onSubmit).toHaveBeenCalled()
    fireEvent.click(btn(/Edit/))
    expect(h.onEdit).toHaveBeenCalled()
  })

  it('draft: the primary action reads Submit', () => {
    renderHeader({ workOrder: makeWorkOrder({ status: 'draft' }), status: 'draft' })
    expect(btn(/Submit/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Re-run/ })).toBeNull()
  })

  it('active: Cancel replaces Re-run, and Edit/Delete are disabled', () => {
    const h = renderHeader({ status: 'running', runStatus: 'running' })
    expect(screen.queryByRole('button', { name: /Re-run/ })).toBeNull()
    expect(btn(/Edit/).disabled).toBe(true)
    expect(btn(/Edit/).title).toMatch(/Wait for the current attempt/)
    expect(btn(/Delete/).disabled).toBe(true)
    fireEvent.click(btn(/Cancel/))
    expect(h.onCancel).toHaveBeenCalled()
  })

  it('queued counts as active too', () => {
    renderHeader({ status: 'queued' })
    expect(btn(/Cancel/)).toBeTruthy()
  })

  it('delete asks for confirmation first', () => {
    const h = renderHeader()
    fireEvent.click(btn(/Delete/))
    expect(h.onDelete).not.toHaveBeenCalled()
    fireEvent.click(btn('Confirm?'))
    expect(h.onDelete).toHaveBeenCalledTimes(1)
  })

  it.each(['submit', 'cancel', 'delete'] as const)('disables everything while %s is in flight', (busy) => {
    renderHeader({ busy, status: busy === 'cancel' ? 'running' : 'succeeded' })
    for (const b of screen.getAllByRole('button')) expect((b as HTMLButtonElement).disabled).toBe(true)
  })
})

describe('permissions', () => {
  it('hides actions the person cannot perform', () => {
    setPermissions()
    renderHeader()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('shows Cancel only with the cancel permission', () => {
    setPermissions('work_orders.create')
    renderHeader({ status: 'running' })
    expect(screen.queryByRole('button', { name: /Cancel/ })).toBeNull()
    expect(btn(/Edit/)).toBeTruthy()
  })

  it('can delete without being able to create', () => {
    setPermissions('work_orders.delete')
    renderHeader()
    expect(btn(/Delete/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Edit/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Re-run/ })).toBeNull()
  })
})

describe('attempt stats', () => {
  it('omits the stats row without a run', () => {
    renderHeader()
    expect(screen.queryByText('Spend')).toBeNull()
    expect(screen.queryByText('Duration')).toBeNull()
  })

  it('shows spend, duration, tokens and attempts for a finished run', () => {
    const run = makeRun({
      budget_usd: 2,
      started_at: '2026-10-03T10:00:00Z',
      finished_at: '2026-10-03T10:02:05Z',
      usage: { prompt_tokens: 1000, completion_tokens: 234, successful_requests: 5 } as never,
    })
    renderHeader({ run, runStatus: 'succeeded', spent: 0.75, workOrder: makeWorkOrder({ run_count: 3, finished_at: '2026-10-03T10:02:05Z' }) })
    expect(screen.getByText('$0.75')).toBeTruthy()
    expect(screen.getByText(/\/ \$2\.00/)).toBeTruthy()
    expect(screen.getByText('2m 5s')).toBeTruthy()
    expect(screen.getByText('1,234')).toBeTruthy()
    expect(screen.getByText('5 requests')).toBeTruthy()
    expect(screen.getByText('3')).toBeTruthy()
    expect(screen.getByText(/last finished/)).toBeTruthy()
  })

  it('says final usage is pending while active and shows the budget state', () => {
    const run = makeRun({ status: 'running', finished_at: null, usage: null })
    renderHeader({ run, runStatus: 'running', status: 'running', spent: 1, budgetState: 'hard_stopped' })
    expect(screen.getByText('final usage pending')).toBeTruthy()
    expect(screen.getByText('hard stop')).toBeTruthy()
  })

  it('shows live tokens from the attempt events while the final usage is pending', () => {
    const run = makeRun({ status: 'running', finished_at: null, usage: null })
    const tally = { ...emptyTally, llmCalls: 4, promptTokens: 90_000, completionTokens: 1_234 }
    renderHeader({ run, runStatus: 'running', status: 'running', spent: 0.13, tally })
    expect(screen.getByText('91,234')).toBeTruthy()
    expect(screen.getByText('4 LLM calls so far')).toBeTruthy()
  })

  it('handles a run that has not started', () => {
    const run = makeRun({ status: 'queued', started_at: null, finished_at: null })
    renderHeader({ run, runStatus: 'queued', status: 'queued', spent: null })
    expect(screen.getByText('not started')).toBeTruthy()
  })

  it('ticks the duration of an active run', () => {
    vi.useFakeTimers({ now: Date.parse('2026-10-03T10:00:10Z') })
    const run = makeRun({ status: 'running', started_at: '2026-10-03T10:00:00Z', finished_at: null })
    renderHeader({ run, runStatus: 'running', status: 'running', spent: 0 })
    expect(screen.getByText('10s')).toBeTruthy()
    act(() => vi.advanceTimersByTime(5000))
    expect(screen.getByText('15s')).toBeTruthy()
  })
})

import { act } from '@testing-library/react'
