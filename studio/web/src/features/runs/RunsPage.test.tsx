import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { crewsApi, runsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { notifyError, notifySuccess } from '../../lib/notify'
import { makeCrew } from '../../lib/testFixtures'
import { RunsPage } from './RunsPage'
import { RunsTable, TriggerChip } from './RunsTable'
import { makeRun, renderAt, setPerms } from './testUtils'

vi.mock('../../api/endpoints', () => ({
  crewsApi: { list: vi.fn() },
  runsApi: { list: vi.fn(), cancel: vi.fn(), remove: vi.fn() },
}))
vi.mock('../../lib/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMockModule())

const list = vi.mocked(runsApi.list)
const page = (items: ReturnType<typeof makeRun>[], over: { total?: number; limit?: number; offset?: number } = {}) => ({
  items, meta: { total: over.total ?? items.length, limit: over.limit ?? 25, offset: over.offset ?? 0 },
})

beforeEach(() => {
  setPerms('crews.view', 'runs.view')
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c-b', name: 'Beta' }), makeCrew({ id: 'c-a', name: 'Alpha' })])
  list.mockResolvedValue(page([makeRun()]))
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('RunsPage', () => {
  it('shows a loading state then the runs table', async () => {
    renderAt(<RunsPage />, { route: '/runs' })
    expect(screen.getByRole('status')).toBeTruthy()
    expect(await screen.findByText('Research crew')).toBeTruthy()
    expect(list).toHaveBeenCalledWith({ crew_id: undefined, status: undefined, trigger_type: undefined, limit: 25, offset: 0 }, expect.anything())
  })

  it('shows an error state with retry', async () => {
    list.mockRejectedValueOnce(new ApiError(500, { code: 'internal', message: 'boom' }))
    renderAt(<RunsPage />, { route: '/runs' })
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('boom')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('Research crew')).toBeTruthy()
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('shows the plain empty state when there are no runs', async () => {
    list.mockResolvedValue(page([]))
    renderAt(<RunsPage />, { route: '/runs' })
    expect(await screen.findByText('No runs yet')).toBeTruthy()
  })

  it('shows a filter-specific empty state when filters are active', async () => {
    list.mockResolvedValue(page([]))
    renderAt(<RunsPage />, { route: '/runs?status=failed' })
    expect(await screen.findByText('No runs match these filters')).toBeTruthy()
    expect((screen.getByLabelText('Filter by status') as HTMLSelectElement).value).toBe('failed')
  })

  it('reads status, trigger, crew and offset from the URL and ignores invalid values', async () => {
    renderAt(<RunsPage />, { route: '/runs?status=running&trigger_type=webhook&crew_id=c-a&offset=50' })
    await screen.findByText('Research crew')
    expect(list).toHaveBeenCalledWith({ crew_id: 'c-a', status: 'running', trigger_type: 'webhook', limit: 25, offset: 50 }, expect.anything())
    cleanup()
    list.mockClear()
    renderAt(<RunsPage />, { route: '/runs?status=bogus&trigger_type=nope&offset=-5' })
    await screen.findByText('Research crew')
    expect(list).toHaveBeenCalledWith({ crew_id: undefined, status: undefined, trigger_type: undefined, limit: 25, offset: 0 }, expect.anything())
  })

  it('lists crews sorted by name in the crew filter', async () => {
    renderAt(<RunsPage />, { route: '/runs' })
    await screen.findByText('Research crew')
    const select = screen.getByLabelText('Filter by crew')
    await waitFor(() => expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['All crews', 'Alpha', 'Beta']))
  })

  it('hides the crew filter and skips loading crews without crews.view', async () => {
    setPerms('runs.view')
    renderAt(<RunsPage />, { route: '/runs' })
    await screen.findByText('Research crew')
    expect(screen.queryByLabelText('Filter by crew')).toBeNull()
    expect(crewsApi.list).not.toHaveBeenCalled()
  })

  it('changing the status filter updates the URL, resets the offset and refetches', async () => {
    const h = renderAt(<RunsPage />, { route: '/runs?offset=25&trigger_type=api' })
    await screen.findByText('Research crew')
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'failed' } })
    await waitFor(() => expect(list).toHaveBeenLastCalledWith({ crew_id: undefined, status: 'failed', trigger_type: 'api', limit: 25, offset: 0 }, expect.anything()))
    expect(h.search()).toBe('?status=failed&trigger_type=api')
  })

  it('changing the trigger and crew filters updates the URL', async () => {
    const h = renderAt(<RunsPage />, { route: '/runs' })
    await screen.findByText('Research crew')
    fireEvent.change(screen.getByLabelText('Filter by trigger'), { target: { value: 'schedule' } })
    await waitFor(() => expect(h.search()).toBe('?trigger_type=schedule'))
    await waitFor(() => expect(within(screen.getByLabelText('Filter by crew')).getAllByRole('option')).toHaveLength(3))
    fireEvent.change(screen.getByLabelText('Filter by crew'), { target: { value: 'c-b' } })
    await waitFor(() => expect(h.search()).toBe('?crew_id=c-b&trigger_type=schedule'))
    fireEvent.change(screen.getByLabelText('Filter by crew'), { target: { value: '' } })
    await waitFor(() => expect(h.search()).toBe('?trigger_type=schedule'))
  })

  it('filters by kind and marks chat turns', async () => {
    list.mockResolvedValue(page([makeRun({ id: 'chat-1', kind: 'chat', crew_name: 'PMO' })]))
    const h = renderAt(<RunsPage />, { route: '/runs' })
    const row = (await screen.findByText('PMO')).closest('tr') as HTMLElement
    expect(within(row).getByText('chat')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Filter by kind'), { target: { value: 'chat' } })
    await waitFor(() => expect(h.search()).toBe('?kind=chat'))
    await waitFor(() => expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ kind: 'chat' }), expect.anything()))
  })

  it('marks hand-offs and filters by them', async () => {
    list.mockResolvedValue(page([makeRun({ id: 'h-1', kind: 'handoff', crew_name: 'Deal Partner' })]))
    const h = renderAt(<RunsPage />, { route: '/runs' })
    const row = (await screen.findByText('Deal Partner')).closest('tr') as HTMLElement
    expect(within(row).getByText('hand-off')).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Hand-offs' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Filter by kind'), { target: { value: 'handoff' } })
    await waitFor(() => expect(h.search()).toBe('?kind=handoff'))
  })

  it('paginates with next and previous', async () => {
    list.mockImplementation((q) => Promise.resolve(page([makeRun()], { total: 100, limit: 25, offset: q.offset })))
    const h = renderAt(<RunsPage />, { route: '/runs?offset=25' })
    expect(await screen.findByText('26–50 of 100')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Next page'))
    await waitFor(() => expect(h.search()).toBe('?offset=50'))
    await screen.findByText('51–75 of 100')
    fireEvent.click(screen.getByLabelText('Previous page'))
    await waitFor(() => expect(h.search()).toBe('?offset=25'))
  })

  it('goes back to the first page without an offset param', async () => {
    list.mockResolvedValue(page([makeRun()], { total: 60, limit: 25, offset: 25 }))
    const h = renderAt(<RunsPage />, { route: '/runs?offset=25' })
    await screen.findByText('26–50 of 60')
    fireEvent.click(screen.getByLabelText('Previous page'))
    await waitFor(() => expect(h.search()).toBe(''))
  })

  describe('cancel and delete', () => {
    it('offers no row actions without runs.cancel / runs.delete', async () => {
      list.mockResolvedValue(page([makeRun({ id: 'r-active-1', status: 'running' }), makeRun({ id: 'r-done-111', status: 'failed' })]))
      renderAt(<RunsPage />, { route: '/runs' })
      await screen.findAllByText('Research crew')
      expect(screen.queryByText('Cancel')).toBeNull()
      expect(screen.queryByText('Delete')).toBeNull()
    })

    it('cancels an active run and merges the response into the row', async () => {
      setPerms('runs.view', 'runs.cancel')
      list.mockResolvedValue(page([makeRun({ id: 'r-active-1', status: 'running', finished_at: null })]))
      vi.mocked(runsApi.cancel).mockResolvedValue(makeRun({ id: 'r-active-1', status: 'cancelled' }))
      renderAt(<RunsPage />, { route: '/runs' })
      fireEvent.click(await screen.findByText('Cancel'))
      await waitFor(() => expect(notifySuccess).toHaveBeenCalledWith('Run cancelled'))
      expect(runsApi.cancel).toHaveBeenCalledWith('r-active-1')
      await waitFor(() => expect(within(screen.getByRole('table')).getByText('cancelled')).toBeTruthy())
      expect(screen.queryByText('Cancel')).toBeNull()
    })

    it('reports a cancel failure', async () => {
      setPerms('runs.view', 'runs.cancel')
      list.mockResolvedValue(page([makeRun({ status: 'queued' })]))
      const err = new ApiError(409, { code: 'conflict', message: 'already done' })
      vi.mocked(runsApi.cancel).mockRejectedValue(err)
      renderAt(<RunsPage />, { route: '/runs' })
      fireEvent.click(await screen.findByText('Cancel'))
      await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not cancel the run', err))
      expect(screen.getByText('Cancel')).toBeTruthy()
    })

    it('deletes a finished run only after confirming, then reloads', async () => {
      setPerms('runs.view', 'runs.delete')
      list.mockResolvedValueOnce(page([makeRun({ id: 'r-done-111' })])).mockResolvedValue(page([]))
      vi.mocked(runsApi.remove).mockResolvedValue(undefined)
      renderAt(<RunsPage />, { route: '/runs' })
      fireEvent.click(await screen.findByText('Delete'))
      expect(runsApi.remove).not.toHaveBeenCalled()
      fireEvent.click(screen.getByText('Confirm?'))
      await waitFor(() => expect(runsApi.remove).toHaveBeenCalledWith('r-done-111'))
      await waitFor(() => expect(notifySuccess).toHaveBeenCalledWith('Run deleted'))
      expect(await screen.findByText('No runs yet')).toBeTruthy()
    })

    it('reports a delete failure', async () => {
      setPerms('runs.view', 'runs.delete')
      const err = new Error('nope')
      vi.mocked(runsApi.remove).mockRejectedValue(err)
      renderAt(<RunsPage />, { route: '/runs' })
      fireEvent.click(await screen.findByText('Delete'))
      fireEvent.click(screen.getByText('Confirm?'))
      await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not delete the run', err))
    })

    it('does not show Cancel on finished runs nor Delete on active runs', async () => {
      setPerms('runs.view', 'runs.cancel', 'runs.delete')
      list.mockResolvedValue(page([makeRun({ id: 'r-active-1', status: 'running' }), makeRun({ id: 'r-done-111', status: 'succeeded' })]))
      renderAt(<RunsPage />, { route: '/runs' })
      await screen.findAllByText('Research crew')
      expect(screen.getAllByText('Cancel')).toHaveLength(1)
      expect(screen.getAllByText('Delete')).toHaveLength(1)
    })
  })

  it('polls while a run is still active and stops once all are finished', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    list.mockResolvedValueOnce(page([makeRun({ status: 'running' })])).mockResolvedValue(page([makeRun({ status: 'succeeded' })]))
    renderAt(<RunsPage />, { route: '/runs' })
    await within(await screen.findByRole('table')).findByText('running')
    expect(list).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(4100) })
    await within(screen.getByRole('table')).findByText('succeeded')
    expect(list).toHaveBeenCalledTimes(2)
    await act(async () => { await vi.advanceTimersByTimeAsync(12000) })
    expect(list).toHaveBeenCalledTimes(2)
  })

  it('does not poll when everything is finished', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    renderAt(<RunsPage />, { route: '/runs' })
    await screen.findByText('Research crew')
    await act(async () => { await vi.advanceTimersByTimeAsync(9000) })
    expect(list).toHaveBeenCalledTimes(1)
  })
})

describe('RunsTable', () => {
  const now = Date.parse('2026-01-01T00:00:31Z')

  it('renders crew link, short id, cost, budget, duration and who started it', () => {
    const run = makeRun({ id: 'abcdef12-0000', cost_usd: 0.1234, budget_usd: 2, created_by_name: 'Alice', started_at: '2026-01-01T00:00:00Z', finished_at: '2026-01-01T00:00:12Z' })
    renderAt(<RunsTable runs={[run]} now={now} />)
    const link = screen.getByText('Research crew') as HTMLAnchorElement
    expect(link.getAttribute('href')).toBe('/runs/abcdef12-0000')
    expect(screen.getByText('abcdef12')).toBeTruthy()
    expect(screen.getByText('Alice')).toBeTruthy()
    expect(screen.getByText('$0.12')).toBeTruthy()
    expect(screen.getByText('$2.00')).toBeTruthy()
    expect(screen.getByText('12s')).toBeTruthy()
  })

  it('uses a dash when nobody started it and when cost is unknown', () => {
    renderAt(<RunsTable runs={[makeRun({ cost_usd: null, created_by_name: '' })]} now={now} />)
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(2)
  })

  it('shows a live duration for an active run and falls back to created_at when not started', () => {
    renderAt(<RunsTable runs={[makeRun({ status: 'running', started_at: '2026-01-01T00:00:01Z', finished_at: null })]} now={now} />)
    expect(screen.getByText('30s')).toBeTruthy()
    cleanup()
    renderAt(<RunsTable runs={[makeRun({ status: 'queued', started_at: null, finished_at: null })]} now={now} />)
    expect(screen.getByText('queued')).toBeTruthy()
  })

  it('omits the actions column when compact or when no handlers are given', () => {
    const { unmount } = renderAt(<RunsTable runs={[makeRun({ status: 'running' })]} now={now} compact onCancel={() => {}} onDelete={() => {}} />)
    expect(screen.queryByText('Cancel')).toBeNull()
    expect(screen.getAllByRole('columnheader')).toHaveLength(8)
    unmount()
    renderAt(<RunsTable runs={[makeRun({ status: 'running' })]} now={now} onCancel={() => {}} />)
    expect(screen.getAllByRole('columnheader')).toHaveLength(9)
  })

  it('invokes the handlers with the run and disables the busy row', () => {
    const onCancel = vi.fn()
    const run = makeRun({ status: 'running' })
    const { unmount } = renderAt(<RunsTable runs={[run]} now={now} onCancel={onCancel} />)
    fireEvent.click(screen.getByText('Cancel'))
    expect(onCancel).toHaveBeenCalledWith(run)
    unmount()
    renderAt(<RunsTable runs={[run]} now={now} onCancel={onCancel} busyId={run.id} />)
    expect((screen.getByText('Cancel').closest('button') as HTMLButtonElement).disabled).toBe(true)
  })
})

describe('TriggerChip', () => {
  it.each([
    ['manual', 'Manual'], ['api', 'API'], ['schedule', 'Schedule'], ['webhook', 'Webhook'], ['work_order', 'Work order'],
  ] as const)('labels %s runs', (type, label) => {
    renderAt(<TriggerChip run={{ trigger_type: type, work_order_id: null }} />)
    expect(screen.getByText(label)).toBeTruthy()
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('links to the work order when there is one', () => {
    renderAt(<TriggerChip run={{ trigger_type: 'work_order', work_order_id: 'wo-9' }} />)
    expect(screen.getByRole('link').getAttribute('href')).toBe('/work-orders/wo-9')
  })

  it('treats a missing trigger type as manual and shows unknown types verbatim', () => {
    renderAt(<TriggerChip run={{ trigger_type: undefined as never, work_order_id: null }} />)
    expect(screen.getByText('Manual')).toBeTruthy()
    cleanup()
    renderAt(<TriggerChip run={{ trigger_type: 'mystery' as never, work_order_id: null }} />)
    expect(screen.getByText('mystery')).toBeTruthy()
  })
})
