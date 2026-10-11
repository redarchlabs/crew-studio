import { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { schedulesApi } from '../../api/endpoints'
import { CronField } from './CronField'

vi.mock('../../api/endpoints')

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(schedulesApi.preview).mockResolvedValue({ next: [] })
})
afterEach(cleanup)

type HarnessProps = { cron?: string; tz?: string; cronError?: string; tzError?: string; onCronSpy?: (c: string) => void }
function Harness({ cron = '0 9 * * 1-5', tz = 'UTC', cronError, tzError, onCronSpy }: HarnessProps) {
  const [c, setC] = useState(cron)
  const [z, setZ] = useState(tz)
  return <CronField cron={c} timezone={z} onCron={(x) => { onCronSpy?.(x); setC(x) }} onTimezone={setZ} cronError={cronError} timezoneError={tzError} />
}
const cronInput = () => screen.getByLabelText('Cron expression') as HTMLInputElement
const preset = () => screen.getByLabelText('Repeats') as HTMLSelectElement
const wait = { timeout: 3000 }

describe('CronField: presets and words', () => {
  it('selects the matching preset and describes it', () => {
    render(<Harness />)
    expect(preset().value).toBe('weekdays')
    expect(screen.getByText('Weekdays at 09:00', { selector: 'span.font-medium' })).toBeTruthy()
    expect(screen.getByText('(UTC)')).toBeTruthy()
  })

  it('shows Custom for an expression that matches no preset', () => {
    render(<Harness cron="*/10 * * * *" />)
    expect(preset().value).toBe('custom')
    expect(screen.getByText('Every 10 minutes')).toBeTruthy()
  })

  it('choosing a preset writes its cron', () => {
    const spy = vi.fn()
    render(<Harness onCronSpy={spy} />)
    fireEvent.change(preset(), { target: { value: 'monday' } })
    expect(spy).toHaveBeenCalledWith('0 9 * * 1')
    expect(cronInput().value).toBe('0 9 * * 1')
    expect(preset().value).toBe('monday')
  })

  it('choosing Custom keeps the current expression and reveals local validation', () => {
    const spy = vi.fn()
    render(<Harness onCronSpy={spy} />)
    fireEvent.change(preset(), { target: { value: 'custom' } })
    expect(spy).not.toHaveBeenCalled()
    expect(preset().value).toBe('custom')
    expect(cronInput().value).toBe('0 9 * * 1-5')
  })

  it('typing flips to custom even when the text still equals a preset', () => {
    render(<Harness />)
    fireEvent.change(cronInput(), { target: { value: '0 9 * * 1' } })
    expect(preset().value).toBe('custom')
  })

  it('switching back from custom to a preset', () => {
    render(<Harness cron="*/10 * * * *" />)
    fireEvent.change(preset(), { target: { value: 'hourly' } })
    expect(preset().value).toBe('hourly')
    expect(cronInput().value).toBe('0 * * * *')
  })
})

describe('CronField: validation', () => {
  it('shows an invalid-expression message once custom', () => {
    render(<Harness />)
    fireEvent.change(cronInput(), { target: { value: 'nonsense' } })
    expect(screen.getByText(/Use 5 fields/, { selector: '[role=alert]' })).toBeTruthy()
    expect(cronInput().getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByText('Cannot describe this expression.')).toBeTruthy()
  })

  it('shows the minimum interval error', () => {
    render(<Harness />)
    fireEvent.change(cronInput(), { target: { value: '*/2 * * * *' } })
    expect(screen.getByText('Schedules may not fire more often than every 5 minutes.', { selector: '[role=alert]' })).toBeTruthy()
  })

  it('accepts exactly every 5 minutes', () => {
    render(<Harness />)
    fireEvent.change(cronInput(), { target: { value: '*/5 * * * *' } })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows empty-expression message', () => {
    render(<Harness />)
    fireEvent.change(cronInput(), { target: { value: '' } })
    expect(screen.getByText('Enter a cron expression.')).toBeTruthy()
  })

  it('does not nag about a preset expression', () => {
    render(<Harness />)
    expect(screen.queryByRole('alert')).toBeNull()
    expect(cronInput().getAttribute('aria-invalid')).toBe('false')
  })

  it('a server-provided cron error wins over the local one', () => {
    render(<Harness cronError="server says no" />)
    expect(screen.getByText('server says no')).toBeTruthy()
    fireEvent.change(cronInput(), { target: { value: 'bad' } })
    expect(screen.getByText('server says no')).toBeTruthy()
    expect(screen.queryByText(/Use 5 fields/)).toBeNull()
  })

  it('shows a time zone error', () => {
    render(<Harness tzError="Choose a time zone." />)
    expect(screen.getByText('Choose a time zone.')).toBeTruthy()
  })
})

describe('CronField: time zone', () => {
  it('lists UTC first and changes zone, re-describing in that zone', () => {
    render(<Harness />)
    const tz = screen.getByLabelText('Time zone') as HTMLSelectElement
    expect(tz.options[0].value).toBe('UTC')
    const other = Array.from(tz.options).find((o) => o.value === 'Asia/Tokyo' || o.value.startsWith('Europe/'))!
    fireEvent.change(tz, { target: { value: other.value } })
    expect(tz.value).toBe(other.value)
    expect(screen.getByText(`(${other.value})`)).toBeTruthy()
  })

  it('includes a saved zone the browser does not list, at the top', () => {
    render(<Harness tz="Mars/Olympus" />)
    const tz = screen.getByLabelText('Time zone') as HTMLSelectElement
    expect(tz.options[0].value).toBe('Mars/Olympus')
    expect(tz.options[0].textContent).toBe('Mars/Olympus')
    expect(tz.value).toBe('Mars/Olympus')
  })

  it('replaces underscores in zone labels', () => {
    render(<Harness />)
    const opt = Array.from((screen.getByLabelText('Time zone') as HTMLSelectElement).options).find((o) => o.value === 'America/New_York')
    expect(opt?.textContent).toBe('America/New York')
  })

  it('falls back to UTC wording with no timezone', () => {
    render(<Harness tz="" />)
    expect(screen.getByText('(UTC)')).toBeTruthy()
    expect(schedulesApi.preview).not.toHaveBeenCalled()
  })
})

describe('CronField: next runs preview', () => {
  it('previews through the server, with trimmed cron and the zone', async () => {
    vi.mocked(schedulesApi.preview).mockResolvedValue({ next: ['2026-10-05T09:00:00Z', '2026-10-06T09:00:00Z'] })
    render(<Harness cron="  0 9 * * 1-5 " />)
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(2), wait)
    expect(schedulesApi.preview).toHaveBeenCalledWith('0 9 * * 1-5', 'UTC')
    expect(screen.getAllByRole('listitem')[0].textContent).toMatch(/0?9:00/)
  })

  it('debounces typing into a single request', async () => {
    render(<Harness />)
    await waitFor(() => expect(schedulesApi.preview).toHaveBeenCalledTimes(1), wait)
    fireEvent.change(cronInput(), { target: { value: '0 1 * * *' } })
    fireEvent.change(cronInput(), { target: { value: '0 2 * * *' } })
    fireEvent.change(cronInput(), { target: { value: '0 3 * * *' } })
    await waitFor(() => expect(schedulesApi.preview).toHaveBeenCalledTimes(2), wait)
    expect(schedulesApi.preview).toHaveBeenLastCalledWith('0 3 * * *', 'UTC')
  })

  it('shows loading state before the response', async () => {
    let resolve!: (v: { next: string[] }) => void
    vi.mocked(schedulesApi.preview).mockReturnValue(new Promise((r) => { resolve = r }))
    render(<Harness />)
    expect(screen.getByText('Working out the next runs…')).toBeTruthy()
    await waitFor(() => expect(schedulesApi.preview).toHaveBeenCalled(), wait)
    resolve({ next: [] })
    expect(await screen.findByText('No upcoming runs.')).toBeTruthy()
  })

  it('shows the server 422 reason', async () => {
    vi.mocked(schedulesApi.preview).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['cron: too frequent', 'minimum is 5m'] }))
    render(<Harness />)
    expect(await screen.findByText('cron: too frequent · minimum is 5m', undefined, wait)).toBeTruthy()
    expect(screen.queryByText('No upcoming runs.')).toBeNull()
  })

  it('shows a generic error message for other failures', async () => {
    vi.mocked(schedulesApi.preview).mockRejectedValue(new Error('network down'))
    render(<Harness />)
    expect(await screen.findByText('network down', undefined, wait)).toBeTruthy()
  })

  it('skips the server and explains when the local check fails', async () => {
    render(<Harness cron="nonsense" />)
    expect(screen.getByText('Fix the expression to see upcoming runs.')).toBeTruthy()
    await new Promise((r) => setTimeout(r, 600))
    expect(schedulesApi.preview).not.toHaveBeenCalled()
  })

  it('clears stale runs when the expression becomes invalid', async () => {
    vi.mocked(schedulesApi.preview).mockResolvedValue({ next: ['2026-10-05T09:00:00Z'] })
    render(<Harness />)
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(1), wait)
    fireEvent.change(cronInput(), { target: { value: 'bad' } })
    await waitFor(() => expect(screen.queryAllByRole('listitem')).toHaveLength(0))
    expect(screen.getByText('Fix the expression to see upcoming runs.')).toBeTruthy()
  })

  it('ignores a response that arrives after the expression changed', async () => {
    let first!: (v: { next: string[] }) => void
    vi.mocked(schedulesApi.preview)
      .mockReturnValueOnce(new Promise((r) => { first = r }))
      .mockResolvedValueOnce({ next: ['2026-11-01T09:00:00Z'] })
    render(<Harness />)
    await waitFor(() => expect(schedulesApi.preview).toHaveBeenCalledTimes(1), wait)
    fireEvent.change(cronInput(), { target: { value: '0 5 * * *' } })
    await waitFor(() => expect(schedulesApi.preview).toHaveBeenCalledTimes(2), wait)
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(1), wait)
    first({ next: ['2026-10-05T09:00:00Z', '2026-10-06T09:00:00Z', '2026-10-07T09:00:00Z'] })
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.getAllByRole('listitem')).toHaveLength(1)
  })

  it('shows the viewer-local time when it differs from the schedule zone', async () => {
    vi.spyOn(Intl, 'DateTimeFormat').mockImplementation(((..._a: unknown[]) => ({ resolvedOptions: () => ({ timeZone: 'Asia/Tokyo' }) })) as unknown as typeof Intl.DateTimeFormat)
    vi.mocked(schedulesApi.preview).mockResolvedValue({ next: ['2026-10-05T09:00:00Z'] })
    render(<Harness />)
    await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(1), wait)
    expect(screen.getAllByRole('listitem')[0].textContent).toContain('·')
    vi.restoreAllMocks()
  })

  it('tolerates a response without next', async () => {
    vi.mocked(schedulesApi.preview).mockResolvedValue({} as { next: string[] })
    render(<Harness />)
    expect(await screen.findByText('No upcoming runs.', undefined, wait)).toBeTruthy()
  })
})
