import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { watchesApi } from '../../../api/endpoints.watch'
import { makeSchedule, renderRoute, setupApi, without } from '../../crews/testUtils'
import { makeCheck, makePreview, makeView, makeWatch } from './testUtils'
import { sourceSummary, WatchPanel } from './WatchPanel'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.watch')

const schedule = makeSchedule()
const render = (canManage = true, canClear = true) =>
  renderRoute(<WatchPanel schedule={schedule} canManage={canManage} canClear={canClear} />)

beforeEach(() => {
  setupApi()
  vi.mocked(watchesApi.get).mockResolvedValue(makeView())
})
afterEach(cleanup)

const field = (label: string | RegExp) => screen.getByLabelText(label) as HTMLInputElement

describe('WatchPanel', () => {
  it('shows status, savings and recent checks, and saves an edited watch', async () => {
    vi.mocked(watchesApi.save).mockResolvedValue(makeView({ watch: makeWatch({ run_when: 'always' }) }))
    render()
    expect(await screen.findByText(/items in the snapshot/)).toBeTruthy()
    expect(screen.getByText('Runs skipped')).toBeTruthy()
    expect(screen.getByText('96k')).toBeTruthy()
    expect(screen.getByText('started a run')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'work order' }).getAttribute('href')).toBe('/work-orders/wo-1')
    expect(field('Path').value).toBe('/issues')
    fireEvent.click(screen.getByRole('radio', { name: /Every time/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save watch' }))
    await waitFor(() => expect(watchesApi.save).toHaveBeenCalledWith('s1', expect.objectContaining({ run_when: 'always', connection_id: 'k1', tracked_fields: ['status', 'due'] })))
  })

  it('adds a watch from scratch and shows local problems first', async () => {
    vi.mocked(watchesApi.get).mockResolvedValue(makeView({ watch: null, stats: null, checks: [], connections: [] }))
    vi.mocked(watchesApi.save).mockResolvedValue(makeView({ watch: makeWatch({ source: { kind: 'web_page', url: 'https://status.example.com' } }) }))
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Add a watch' }))
    expect(screen.getByRole('option', { name: 'No HTTP connections for this crew' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add watch' }))
    expect(await screen.findByText('Pick the HTTP connection to read through.')).toBeTruthy()
    expect(watchesApi.save).not.toHaveBeenCalled()
    fireEvent.change(field('Source'), { target: { value: 'web_page' } })
    fireEvent.change(field('Page URL'), { target: { value: 'https://status.example.com' } })
    fireEvent.change(field('CSS selector'), { target: { value: 'main' } })
    fireEvent.click(screen.getByRole('switch', { name: /Dry run/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add watch' }))
    await waitFor(() => expect(watchesApi.save).toHaveBeenCalledWith('s1', expect.objectContaining({
      source: { kind: 'web_page', url: 'https://status.example.com', selector: 'main' }, dry_run: false, connection_id: null,
    })))
    expect(await screen.findByText(/items in the snapshot/)).toBeTruthy()
  })

  it('previews without saving, and shows a failed preview', async () => {
    vi.mocked(watchesApi.preview).mockResolvedValueOnce(makePreview())
    vi.mocked(watchesApi.preview).mockRejectedValueOnce(new ApiError(422, { code: 'source_failed', message: 'The watch could not read its source.', details: ['Tracker answered HTTP 403'] }))
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Preview' }))
    const result = within(await screen.findByTestId('watch-preview'))
    expect(result.getByText('would start a run')).toBeTruthy()
    expect(result.getByText('PRJ-3')).toBeTruthy()
    expect(result.getByText('2 more wait for the next check (per-check cap).')).toBeTruthy()
    expect(watchesApi.save).not.toHaveBeenCalled()
    fireEvent.change(field('Path'), { target: { value: '/issues?x=1' } })
    expect(screen.queryByTestId('watch-preview')).toBeNull() // an edit makes the old preview go away
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    expect(await screen.findByText('Tracker answered HTTP 403')).toBeTruthy()
  })

  it('removes the watch and clears its snapshot', async () => {
    vi.mocked(watchesApi.remove).mockResolvedValue(undefined)
    vi.mocked(watchesApi.clearSnapshot).mockResolvedValue(undefined)
    render()
    fireEvent.change(await screen.findByLabelText('Path'), { target: { value: '/edited' } })
    fireEvent.click(screen.getByRole('button', { name: 'Clear snapshot' }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(watchesApi.clearSnapshot).toHaveBeenCalledWith('s1'))
    expect(await screen.findByText(/the next check is the first/)).toBeTruthy()
    expect(field('Path').value).toBe('/edited') // clearing keeps unsaved edits
    vi.mocked(watchesApi.get).mockResolvedValue(makeView({ watch: null, stats: null, checks: [] }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove watch' }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(watchesApi.remove).toHaveBeenCalledWith('s1'))
    expect(await screen.findByRole('button', { name: 'Add a watch' })).toBeTruthy()
  })

  it('reports a refused save', async () => {
    vi.mocked(watchesApi.save).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Please fix the highlighted fields.', details: ['connection_id: the connection Tracker API is disabled'] }))
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Save watch' }))
    expect(await screen.findByText('connection_id: the connection Tracker API is disabled')).toBeTruthy()
    vi.mocked(watchesApi.save).mockRejectedValue(new Error('offline'))
    fireEvent.click(screen.getByRole('button', { name: 'Save watch' }))
    expect(await screen.findByText('offline')).toBeTruthy()
  })

  it('edits the follow-up ladder', async () => {
    vi.mocked(watchesApi.save).mockResolvedValue(makeView())
    render()
    await screen.findByText('Steps')
    fireEvent.click(screen.getByRole('button', { name: 'Add step' }))
    fireEvent.change(field('Step 2 name'), { target: { value: 'overdue' } })
    fireEvent.change(field('Step 2 offset'), { target: { value: '+2d' } })
    fireEvent.change(field('Step 2 notify'), { target: { value: 'lead' } })
    fireEvent.change(field('Step 2 repeat'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save watch' }))
    expect(await screen.findByText(/Step 2: Repeat/)).toBeTruthy()
    fireEvent.change(field('Step 2 repeat'), { target: { value: '7d' } })
    fireEvent.click(screen.getByRole('button', { name: 'Remove step 1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add rule' }))
    fireEvent.change(field('Skip rule 2 field'), { target: { value: 'status' } })
    fireEvent.change(field('Skip rule 2 values'), { target: { value: 'Canceled' } })
    fireEvent.click(screen.getByRole('button', { name: 'Remove skip rule 1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Mon' }))
    fireEvent.click(screen.getByRole('button', { name: 'Sat' }))
    fireEvent.change(field('Date field'), { target: { value: 'due' } })
    fireEvent.change(field('Time zone'), { target: { value: 'America/Denver' } })
    fireEvent.change(field('Most per check'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save watch' }))
    await waitFor(() => expect(watchesApi.save).toHaveBeenCalled())
    expect(vi.mocked(watchesApi.save).mock.calls[0][1].followups).toEqual({
      date_field: 'due', skip_when: { status: ['Canceled'] }, quiet_days: ['Sun', 'Mon'], timezone: 'America/Denver', max_per_tick: 5,
      steps: [{ name: 'overdue', offset: '+2d', notify: 'lead', repeat: '7d' }],
    })
  })

  it('turns the ladder off and validates it when on', async () => {
    render()
    await screen.findByText('Steps')
    fireEvent.change(field('Date field'), { target: { value: 'created' } })
    for (const d of ['Mon', 'Tue', 'Wed', 'Thu', 'Fri']) fireEvent.click(screen.getByRole('button', { name: d }))
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    expect(await screen.findByText('The date field must be one of the tracked fields.')).toBeTruthy()
    expect(screen.getByText('Leave at least one working day.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Remove step 1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    expect(await screen.findByText('Add at least one step.')).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: /Follow-up ladder/ }))
    expect(screen.queryByText('Steps')).toBeNull()
  })

  it('switches between source kinds', async () => {
    render()
    await screen.findByText('Steps')
    fireEvent.change(field('Source'), { target: { value: 'workspace_file' } })
    expect(field('File')).toBeTruthy()
    expect(field('Items path (JSON)')).toBeTruthy()
    fireEvent.change(field('File'), { target: { value: 'inbox/a.csv' } })
    expect(screen.queryByLabelText('Items path (JSON)')).toBeNull()
    fireEvent.change(field('Format'), { target: { value: 'json' } })
    expect(field('Items path (JSON)')).toBeTruthy()
    fireEvent.change(field('ID field'), { target: { value: 'id' } })
    fireEvent.change(field('Tracked fields'), { target: { value: 'due' } })
    fireEvent.change(field('Max items'), { target: { value: '10' } })
    fireEvent.change(field('Source'), { target: { value: 'web_page' } })
    expect(screen.getByText(/The page's text is compared as one item/)).toBeTruthy()
    expect(screen.queryByText('Follow-up ladder')).toBeNull()
    fireEvent.change(field('Source'), { target: { value: 'http_json' } })
    fireEvent.change(field('Items path'), { target: { value: 'data' } })
    fireEvent.change(field('Connection'), { target: { value: '' } })
    expect(field('Connection').value).toBe('')
    expect(screen.getByRole('link', { name: /State tab/ }).getAttribute('href')).toBe('/crews/crew-1?tab=state')
  })

  it('is read only without schedules.manage', async () => {
    render(false, false)
    expect(await screen.findByText(/Reads/)).toBeTruthy()
    expect(screen.getByText(/follow-ups: due_today/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Save watch|Preview/ })).toBeNull()
    cleanup()
    vi.mocked(watchesApi.get).mockResolvedValue(makeView({ watch: null }))
    renderRoute(<WatchPanel schedule={schedule} canManage={false} canClear={false} />, { perms: without('schedules.manage') })
    expect(await screen.findByText(/starts a work order every time/)).toBeTruthy()
  })

  it('shows a watch error and the busy outcome', async () => {
    vi.mocked(watchesApi.get).mockResolvedValue(makeView({
      watch: makeWatch({ last_error: 'Watch: there is no file a.csv', last_checked_at: null, last_changed_at: null, snapshot_at: null, dry_run: false }),
      stats: { days: 30, checks: 1, runs_started: 0, runs_skipped: 0, avg_run_tokens: 0, est_tokens_saved: 0, est_cost_saved_usd: 0 },
      checks: [makeCheck({ outcome: 'busy' }), makeCheck({ id: 'c3', outcome: 'error' })],
    }))
    render(false, false)
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText(/Not checked yet/)).toBeTruthy()
    expect(screen.getByText('no finished runs to average yet')).toBeTruthy()
    expect(screen.getByText('skipped: busy')).toBeTruthy()
    expect(screen.getByText('failed')).toBeTruthy()
  })
})

describe('sourceSummary', () => {
  it('describes each kind', () => {
    expect(sourceSummary(makeWatch())).toBe('Tracker API /issues')
    expect(sourceSummary(makeWatch({ connection_name: '', source: { kind: 'http_json' } }))).toBe('HTTP ')
    expect(sourceSummary(makeWatch({ source: { kind: 'web_page', url: 'https://x', selector: 'main' } }))).toBe('https://x (main)')
    expect(sourceSummary(makeWatch({ source: { kind: 'web_page' } }))).toBe('')
    expect(sourceSummary(makeWatch({ source: { kind: 'workspace_file', file: 'a.csv' } }))).toBe('workspace: a.csv')
    expect(sourceSummary(makeWatch({ source: { kind: 'workspace_file' } }))).toBe('workspace: ')
  })
})
