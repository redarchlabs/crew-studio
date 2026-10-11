import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { crewsApi, overviewApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import type { Overview, Permission } from '../../api/types'
import { makeCrew } from '../../lib/testFixtures'
import { makeRun, makeWorkOrder, renderAt, setPerms } from '../runs/testUtils'
import { OverviewPage } from './OverviewPage'

vi.mock('../../api/endpoints', () => ({ overviewApi: { get: vi.fn() }, crewsApi: { list: vi.fn() } }))
vi.mock('../../lib/auth', async () => (await import('../runs/testUtils')).authMockModule())
vi.mock('../crews/RunCrewModal', () => ({
  RunCrewForm: ({ crew, onCancel }: { crew: { name: string }; onCancel: () => void }) => (
    <div data-testid="run-form">Form for {crew.name}<button onClick={onCancel}>cancel form</button></div>
  ),
}))

const get = vi.mocked(overviewApi.get)
const ALL: Permission[] = ['work_orders.view', 'runs.view', 'schedules.view', 'crews.view', 'agents.view', 'tasks.view', 'llms.view', 'pricing.view']

const overview = (over: Partial<Overview> = {}): Overview => ({
  counts: { work_orders: 11, schedules: 2, llms: 3, pricing: 4, agents: 5, tasks: 6, crews: 7, runs: 1234 },
  recent_runs: [makeRun({ id: 'run-aaaaaaaa', crew_name: 'Alpha crew' })],
  recent_work_orders: [makeWorkOrder({ id: 'wo-1', number: 7, title: 'Write report', target_name: 'Alpha crew', cost_usd: 0.5 })],
  spend_last_30d_usd: 12.5,
  ...over,
})
const show = () => renderAt(<OverviewPage />, { route: '/', path: '/' })

beforeEach(() => {
  setPerms(...ALL)
  get.mockResolvedValue(overview())
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c-b', name: 'Beta' }), makeCrew({ id: 'c-a', name: 'Alpha' })])
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
})

const tile = (label: string) => screen.getByText(label, { selector: '.eyebrow' }).closest('a')!

describe('OverviewPage stats', () => {
  it('shows a loading state, then spend and count tiles linked to their lists', async () => {
    show()
    expect(screen.getByRole('status')).toBeTruthy()
    expect(await screen.findByText('$12.50')).toBeTruthy()
    const expected: Record<string, [string, string]> = {
      'Work orders': ['/work-orders', '11'], Runs: ['/runs', '1,234'], Schedules: ['/schedules', '2'], Crews: ['/crews', '7'],
      Agents: ['/agents', '5'], Tasks: ['/tasks', '6'], LLMs: ['/llms', '3'], Prices: ['/pricing', '4'],
    }
    for (const [label, [to, count]] of Object.entries(expected)) {
      const a = tile(label)
      expect(a.getAttribute('href')).toBe(to)
      expect(a.textContent).toContain(count)
    }
  })

  it('shows only the tiles the person may open', async () => {
    setPerms('runs.view', 'agents.view')
    show()
    await screen.findByText('$12.50')
    expect(tile('Runs')).toBeTruthy()
    expect(tile('Agents')).toBeTruthy()
    expect(screen.queryByText('Crews', { selector: '.eyebrow' })).toBeNull()
    expect(screen.queryByText('Work orders', { selector: '.eyebrow' })).toBeNull()
  })

  it('shows dashes for counts the server omitted', async () => {
    get.mockResolvedValue(overview({ counts: undefined as never }))
    show()
    await screen.findByText('$12.50')
    expect(tile('Agents').textContent).toContain('—')
  })

  it('says how much of the spend was chat', async () => {
    get.mockResolvedValue(overview({ chat_spend_last_30d_usd: 1.5 }))
    show()
    expect(await screen.findByText('incl. $1.50 in chat')).toBeTruthy()
  })

  it('shows small spend with extra precision', async () => {
    get.mockResolvedValue(overview({ spend_last_30d_usd: 0.0042 }))
    show()
    expect(await screen.findByText('$0.0042')).toBeTruthy()
  })

  it('shows an error with retry', async () => {
    get.mockRejectedValueOnce(new ApiError(500, { code: 'internal', message: 'db down' }))
    show()
    expect(await screen.findByText('db down')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('$12.50')).toBeTruthy()
  })
})

describe('OverviewPage recent work', () => {
  it('lists recent work orders with number, title, target, cost and a link', async () => {
    show()
    const link = (await screen.findByText('Write report')).closest('a')!
    expect(link.getAttribute('href')).toBe('/work-orders/wo-1')
    expect(link.textContent).toContain('WO-7')
    const panel = screen.getByText('Recent work orders').closest('section')!
    expect(within(panel).getByText('Alpha crew')).toBeTruthy()
    expect(within(panel).getByText('$0.50')).toBeTruthy()
    expect(within(panel).getByText('All work orders →').getAttribute('href')).toBe('/work-orders')
  })

  it('shows draft work orders as a plain chip and others as status badges', async () => {
    get.mockResolvedValue(overview({ recent_work_orders: [makeWorkOrder({ id: 'a', status: 'draft' }), makeWorkOrder({ id: 'b', status: 'running', title: 'B' })] }))
    show()
    await screen.findByText('draft')
    const panel = screen.getByText('Recent work orders').closest('section')!
    expect(within(panel).getByText('running')).toBeTruthy()
  })

  it('shows a dash when a work order has no target', async () => {
    get.mockResolvedValue(overview({ recent_work_orders: [makeWorkOrder({ target_name: '' })] }))
    show()
    const panel = (await screen.findByText('Recent work orders')).closest('section')!
    expect(within(panel).getByText('—')).toBeTruthy()
  })

  it('shows empty states for no work orders and no runs, and tolerates null lists', async () => {
    get.mockResolvedValue(overview({ recent_work_orders: [], recent_runs: [] }))
    show()
    expect(await screen.findByText('No work orders yet')).toBeTruthy()
    expect(screen.getByText('No runs yet')).toBeTruthy()
    cleanup()
    get.mockResolvedValue(overview({ recent_work_orders: null as never, recent_runs: null as never }))
    show()
    expect(await screen.findByText('No work orders yet')).toBeTruthy()
    expect(screen.getByText('No runs yet')).toBeTruthy()
  })

  it('lists recent runs in a compact table linking to each run, with no actions', async () => {
    setPerms(...ALL, 'runs.cancel', 'runs.delete')
    show()
    const panel = (await screen.findByText('Recent runs')).closest('section')!
    expect(within(panel).getByText('Alpha crew').getAttribute('href')).toBe('/runs/run-aaaaaaaa')
    expect(within(panel).getByText('All runs →').getAttribute('href')).toBe('/runs')
    expect(within(panel).queryByText('Delete')).toBeNull()
    expect(within(panel).queryByText('Cancel')).toBeNull()
  })

  it('hides the work order and run panels without the matching permissions', async () => {
    setPerms('agents.view')
    show()
    await screen.findByText('$12.50')
    expect(screen.queryByText('Recent work orders')).toBeNull()
    expect(screen.queryByText('Recent runs')).toBeNull()
  })
})

describe('OverviewPage polling', () => {
  it('polls while a recent run is in progress', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    get.mockResolvedValue(overview({ recent_runs: [makeRun({ status: 'running' })], recent_work_orders: [] }))
    show()
    await screen.findByText('$12.50')
    expect(get).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(3100) })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('polls while a work order is queued or running', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    get.mockResolvedValue(overview({ recent_work_orders: [makeWorkOrder({ status: 'queued' })] }))
    show()
    await screen.findByText('$12.50')
    await act(async () => { await vi.advanceTimersByTimeAsync(3100) })
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('stops polling once everything has finished', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    show()
    await screen.findByText('$12.50')
    await act(async () => { await vi.advanceTimersByTimeAsync(10000) })
    expect(get).toHaveBeenCalledTimes(1)
  })
})

describe('OverviewPage quick run', () => {
  it('hides the button without crews.run and crews.view', async () => {
    setPerms(...ALL)
    show()
    await screen.findByText('$12.50')
    expect(screen.queryByText('Run a crew')).toBeNull()
    cleanup()
    setPerms('crews.run')
    show()
    await screen.findByText('$12.50')
    expect(screen.queryByText('Run a crew')).toBeNull()
  })

  it('opens a modal listing crews alphabetically, then shows the run form for the chosen crew', async () => {
    setPerms(...ALL, 'crews.run')
    show()
    fireEvent.click(await screen.findByText('Run a crew'))
    const select = await screen.findByLabelText('Crew')
    await waitFor(() => expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['Choose a crew…', 'Alpha', 'Beta']))
    expect(screen.queryByTestId('run-form')).toBeNull()
    fireEvent.change(select, { target: { value: 'c-b' } })
    expect(screen.getByTestId('run-form').textContent).toContain('Form for Beta')
    fireEvent.change(select, { target: { value: '' } })
    expect(screen.queryByTestId('run-form')).toBeNull()
  })

  it('closes the modal from the form cancel', async () => {
    setPerms(...ALL, 'crews.run')
    show()
    fireEvent.click(await screen.findByText('Run a crew'))
    fireEvent.change(await screen.findByLabelText('Crew'), { target: { value: 'c-a' } })
    fireEvent.click(screen.getByText('cancel form'))
    await waitFor(() => expect(screen.queryByLabelText('Crew')).toBeNull())
  })

  it('shows guidance when there are no crews', async () => {
    setPerms(...ALL, 'crews.run')
    vi.mocked(crewsApi.list).mockResolvedValue([])
    show()
    fireEvent.click(await screen.findByText('Run a crew'))
    expect(await screen.findByText('No crews to run')).toBeTruthy()
    expect(screen.getByText('Build your first crew').getAttribute('href')).toBe('/crews/new')
  })
})
