import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { ApiError } from '../../api/client'
import { schedulesApi } from '../../api/endpoints'
import { makeSchedule, renderRoute, setupApi, without } from '../crews/testUtils'
import { SchedulesPage } from './SchedulesPage'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const inAnHour = () => new Date(Date.now() + 3600_000).toISOString()
const twoHoursAgo = () => new Date(Date.now() - 2 * 3600_000).toISOString()

const list = () => [
  makeSchedule({ id: 's2', name: 'Zulu', description: 'Last one', cron: '0 9 * * 1-5', timezone: 'UTC', next_run_at: inAnHour(), last_run_at: twoHoursAgo(), last_work_order_id: 'wo-9', run_as_name: 'Ada', target_name: 'Research' }),
  makeSchedule({ id: 's1', name: 'Alpha', enabled: false, cron: 'weird cron', next_run_at: inAnHour(), last_error: 'Run-as lost access', run_as_name: '' }),
]

beforeEach(() => {
  setupApi()
  vi.mocked(schedulesApi.list).mockResolvedValue(list())
})
afterEach(cleanup)

const row = (name: string) => screen.getByRole('link', { name }).closest('tr') as HTMLElement
const names = () => screen.getAllByRole('link').filter((l) => /^\/schedules\/s/.test(l.getAttribute('href') ?? '')).map((l) => l.textContent)

describe('SchedulesPage: list', () => {
  it('lists schedules sorted by name', async () => {
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    expect(names()).toEqual(['Alpha', 'Zulu'])
  })

  it('shows words, cron, zone, next run, last run and run-as for an active schedule', async () => {
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    const r = within(row('Zulu'))
    expect(r.getByText('Weekdays at 09:00')).toBeTruthy()
    expect(r.getByText('0 9 * * 1-5 · UTC')).toBeTruthy()
    expect(r.getByText(/^in (59m|60m|1h)$/)).toBeTruthy()
    expect(r.getByText('2h ago')).toBeTruthy()
    expect(r.getByRole('link', { name: 'Last work order' }).getAttribute('href')).toBe('/work-orders/wo-9')
    expect(r.getByText('Ada')).toBeTruthy()
    expect(r.getByText('Last one')).toBeTruthy()
    expect(r.getByText('Research').closest('a')?.getAttribute('href')).toBe('/crews/crew-1')
  })

  it('shows the raw cron when it cannot be described, paused state, never, dash for run-as and last error', async () => {
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    const r = within(row('Alpha'))
    expect(r.getAllByText('weird cron').length).toBeGreaterThan(0)
    expect(r.getByText('paused')).toBeTruthy()
    expect(r.getByText('never')).toBeTruthy()
    expect(r.getByText('Run-as lost access')).toBeTruthy()
    expect(r.getByText('—')).toBeTruthy()
    expect(r.queryByText('Last work order')).toBeNull()
  })

  it('shows a dash when an enabled schedule has no next run', async () => {
    vi.mocked(schedulesApi.list).mockResolvedValue([makeSchedule({ id: 's1', name: 'Solo', enabled: true, next_run_at: null })])
    renderRoute(<SchedulesPage />)
    await screen.findByText('Solo')
    expect(within(row('Solo')).getAllByText('—').length).toBeGreaterThan(0)
  })

  it('shows the empty state with a create action for managers', async () => {
    vi.mocked(schedulesApi.list).mockResolvedValue([])
    renderRoute(<SchedulesPage />)
    expect(await screen.findByText('No schedules yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Create a schedule' }).getAttribute('href')).toBe('/schedules/new')
  })

  it('empty state has no create action for others', async () => {
    vi.mocked(schedulesApi.list).mockResolvedValue([])
    renderRoute(<SchedulesPage />, { perms: without('schedules.manage') })
    expect(await screen.findByText('No schedules yet')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /Create a schedule|New schedule/ })).toBeNull()
  })

  it('shows an error and retries', async () => {
    vi.mocked(schedulesApi.list).mockRejectedValueOnce(new ApiError(500, { code: 'x', message: 'schedules failed' }))
    renderRoute(<SchedulesPage />)
    expect(await screen.findByText('schedules failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('Alpha')).toBeTruthy()
  })
})

describe('SchedulesPage: pause and resume', () => {
  it('shows the new schedule action to managers', async () => {
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    expect(screen.getByRole('link', { name: /New schedule/ }).getAttribute('href')).toBe('/schedules/new')
    expect(screen.getAllByRole('switch')).toHaveLength(2)
  })

  it('pauses an enabled schedule through update with the full body', async () => {
    const zulu = list()[0]
    vi.mocked(schedulesApi.update).mockResolvedValue({ ...zulu, enabled: false })
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    fireEvent.click(within(row('Zulu')).getByRole('switch'))
    await waitFor(() => expect(schedulesApi.update).toHaveBeenCalled())
    const [id, body] = vi.mocked(schedulesApi.update).mock.calls[0]
    expect(id).toBe('s2')
    expect(body).toMatchObject({ name: 'Zulu', cron: '0 9 * * 1-5', timezone: 'UTC', enabled: false, overlap_policy: 'skip' })
    expect(body).not.toHaveProperty('id')
    await waitFor(() => expect(within(row('Zulu')).getByRole('switch').getAttribute('aria-checked')).toBe('false'))
    expect(toast.success).toHaveBeenCalledWith('Paused Zulu')
    expect(within(row('Zulu')).getByText('paused')).toBeTruthy()
  })

  it('resumes a paused schedule', async () => {
    vi.mocked(schedulesApi.update).mockResolvedValue({ ...list()[1], enabled: true })
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    fireEvent.click(within(row('Alpha')).getByRole('switch'))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('Enabled Alpha'))
    expect(vi.mocked(schedulesApi.update).mock.calls[0][1].enabled).toBe(true)
  })

  it('disables the toggle while saving', async () => {
    let resolve!: (s: ReturnType<typeof list>[number]) => void
    vi.mocked(schedulesApi.update).mockReturnValue(new Promise((r) => { resolve = r }))
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    const sw = within(row('Zulu')).getByRole('switch') as HTMLButtonElement
    fireEvent.click(sw)
    await waitFor(() => expect(sw.disabled).toBe(true))
    resolve({ ...list()[0], enabled: false })
    await waitFor(() => expect(sw.disabled).toBe(false))
  })

  it('keeps state and toasts when the update fails', async () => {
    vi.mocked(schedulesApi.update).mockRejectedValue(new ApiError(403, { code: 'forbidden', message: 'No way' }))
    renderRoute(<SchedulesPage />)
    await screen.findByText('Alpha')
    fireEvent.click(within(row('Zulu')).getByRole('switch'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not update Zulu', { description: 'No way' }))
    expect(within(row('Zulu')).getByRole('switch').getAttribute('aria-checked')).toBe('true')
  })

  it('shows read-only chips instead of switches without schedules.manage', async () => {
    renderRoute(<SchedulesPage />, { perms: without('schedules.manage') })
    await screen.findByText('Alpha')
    expect(screen.queryAllByRole('switch')).toHaveLength(0)
    expect(within(row('Zulu')).getByText('on')).toBeTruthy()
    expect(screen.queryByRole('link', { name: /New schedule/ })).toBeNull()
  })
})
