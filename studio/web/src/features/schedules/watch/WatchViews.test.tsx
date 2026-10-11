import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { crewsApi, schedulesApi, tasksApi } from '../../../api/endpoints'
import { watchesApi } from '../../../api/endpoints.watch'
import { makeCrew } from '../../../lib/testFixtures'
import { CrewWatchesPanel } from '../../crews/state/CrewWatchesPanel'
import { makeSchedule, renderRoute, setupApi } from '../../crews/testUtils'
import { ScheduleEditorPage } from '../ScheduleEditorPage'
import { makeCrewWatch, makePreview, makeView } from './testUtils'
import { ChangesView, FollowupsView, parseWatchInputs, valueText, WatchInputsSection } from './WatchInputsView'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.watch')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

beforeEach(() => {
  setupApi()
})
afterEach(cleanup)

const preview = makePreview()
const inputs = { changes: JSON.stringify(preview.changes), followups_due: JSON.stringify(preview.followups_due), dry_run: 'true', topic: 'AI' }

describe('watch inputs', () => {
  it('parses only well-formed watch inputs', () => {
    expect(parseWatchInputs(inputs).changes?.source).toBe('Tracker API: /issues')
    expect(parseWatchInputs(inputs).dryRun).toBe(true)
    expect(parseWatchInputs({ changes: 'not json', followups_due: '[]' })).toEqual({ changes: undefined, followups: undefined, dryRun: undefined })
    expect(parseWatchInputs({ changes: '{"added": 1}', dry_run: 'false' })).toEqual({ changes: undefined, followups: undefined, dryRun: false })
    expect(valueText(null)).toBe('—')
    expect(valueText('')).toBe('""')
    expect(valueText({ a: 1 })).toBe('{"a":1}')
  })

  it('renders changes, follow-ups and the dry-run note', async () => {
    renderRoute(<WatchInputsSection inputs={inputs} />)
    expect(await screen.findByText('dry run')).toBeTruthy()
    expect(screen.getByText('PRJ-3')).toBeTruthy()
    expect(screen.getByText('2026-10-12')).toBeTruthy()
    expect(screen.getByText('no longer matches the query')).toBeTruthy()
    expect(screen.getByText('(sam)', { exact: false })).toBeTruthy()
    expect(screen.getByText('never')).toBeTruthy()
  })

  it('renders a first check, new comments, omitted fields and empty lists', async () => {
    const changes = { ...preview.changes, previous_check_at: null, added: [{ id: 'A' }], changed: [], removed: [], added_fields_omitted: true,
      new_comments: [{ id: 'A', author: 'sam', at: '2026-10-06T00:00:00Z', excerpt: 'Blocked on vendor' }] }
    renderRoute(<ChangesView changes={changes} />)
    expect(await screen.findByText('first check')).toBeTruthy()
    expect(screen.getByText(/Fields left out/)).toBeTruthy()
    expect(screen.getByText('Blocked on vendor')).toBeTruthy()
    cleanup()
    renderRoute(<ChangesView changes={{ ...changes, added: [], new_comments: undefined }} />)
    expect(await screen.findByText('No changes.')).toBeTruthy()
    cleanup()
    const late = { ...preview.followups_due!.due[0], days_late: 3, repeat: '7d', notify_value: undefined, last_sent_at: '2026-09-29T00:00:00Z' }
    renderRoute(<FollowupsView followups={{ due: [late], more_waiting: 0, undated_items: 2, dry_run: false, how_to_record: '' }} />)
    expect(await screen.findByText(/3d late/)).toBeTruthy()
    expect(screen.getByText(/2 items have no usable date/)).toBeTruthy()
    expect(screen.getByText(/every 7d/)).toBeTruthy()
    cleanup()
    renderRoute(<FollowupsView followups={{ due: [], more_waiting: 0, dry_run: false, how_to_record: '' }} />)
    expect(await screen.findByText('No follow-ups due.')).toBeTruthy()
    cleanup()
    renderRoute(<><WatchInputsSection inputs={{ topic: 'x' }} /><span>shown</span></>)
    expect(await screen.findByText('shown')).toBeTruthy()
    expect(screen.queryByTestId('watch-inputs')).toBeNull()
  })
})

describe('crew State tab watches', () => {
  it('lists each watch with its status and savings', async () => {
    vi.mocked(watchesApi.ofCrew).mockResolvedValue([makeCrewWatch(), makeCrewWatch({ id: 'w2', schedule_id: 's2', schedule_name: 'Quiet', stats: { ...makeView().stats!, avg_run_tokens: 0 } })])
    renderRoute(<CrewWatchesPanel crewId="crew-1" />)
    expect((await screen.findByRole('link', { name: 'Weekly digest' })).getAttribute('href')).toBe('/schedules/s1')
    expect(screen.getByText(/about 96k tokens saved/)).toBeTruthy()
    expect(screen.getAllByText(/8 runs skipped/)).toHaveLength(2)
    expect(watchesApi.ofCrew).toHaveBeenCalledWith('crew-1', expect.anything())
  })

  it('explains when no schedule has a watch', async () => {
    vi.mocked(watchesApi.ofCrew).mockResolvedValue([])
    renderRoute(<CrewWatchesPanel crewId="crew-1" />)
    expect(await screen.findByText(/No schedule of this crew has a watch/)).toBeTruthy()
  })
})

describe('schedule editor', () => {
  beforeEach(() => {
    vi.mocked(tasksApi.list).mockResolvedValue([])
    vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'crew-1', name: 'Research crew' })])
    vi.mocked(schedulesApi.preview).mockResolvedValue({ next: [] })
    vi.mocked(schedulesApi.get).mockResolvedValue(makeSchedule())
    vi.mocked(watchesApi.get).mockResolvedValue(makeView())
  })

  it('shows the watch panel for a saved schedule', async () => {
    renderRoute(<ScheduleEditorPage />, { path: '/schedules/:id', url: '/schedules/s1' })
    expect(await screen.findByRole('button', { name: 'Save watch' })).toBeTruthy()
    expect(watchesApi.get).toHaveBeenCalledWith('s1', expect.anything())
  })

  it('asks to save a new schedule first', async () => {
    renderRoute(<ScheduleEditorPage />, { path: '/schedules/:id', url: '/schedules/new' })
    expect(await screen.findByText(/Save the schedule to add a watch/)).toBeTruthy()
    fireEvent.click(screen.getByText(/Save the schedule to add a watch/))
    expect(watchesApi.get).not.toHaveBeenCalled()
  })
})
