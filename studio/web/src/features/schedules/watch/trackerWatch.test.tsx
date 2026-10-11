import { cleanup, fireEvent, render as rtlRender, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { watchesApi } from '../../../api/endpoints.watch'
import type { Rollup } from '../../../api/types.trackers'
import { makeSchedule, renderRoute, setupApi } from '../../crews/testUtils'
import { RollupsView } from './RollupsView'
import { makePreview, makeView, makeWatch } from './testUtils'
import { sourceSummary, WatchPanel } from './WatchPanel'
import { parseWatchInputs, WatchInputsSection } from './WatchInputsView'
import { effectiveTracked, emptyWatchForm, isTracker, toWatchForm, toWatchInput, validateWatchForm, type WatchForm } from './watchForm'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.watch')

const form = (over: Partial<WatchForm>): WatchForm => ({ ...emptyWatchForm(), ...over })

const rollup = (over: Partial<Rollup> = {}): Rollup => ({
  item: 'PRJ-10', title: 'Atlas rollout', project: 'Atlas', connection: 'Acme Jira', url: 'https://acme.atlassian.net/browse/PRJ-10',
  status: 'In Progress', due_date: '2026-11-15', lead: { account_id: 'acc-dana', name: 'Dana', studio_user: 'dana' },
  children: { total: 5, done: 2, in_progress: 1, to_do: 1, canceled: 1 }, overdue: ['PRJ-11'], due_after_parent: [{ id: 'PRJ-13', due_date: '2026-12-01' }],
  blocked: ['PRJ-11'], unassigned: ['PRJ-13'], stale: [{ id: 'PRJ-12', last_update: '2026-09-01' }], new_comments: 2, changed_since_last_check: true, ...over,
})

const trackerKinds = [
  ...makeView().source_kinds,
  { kind: 'tracker' as const, label: 'Tracker project (Jira or Linear)', connection: 'tracker', items: true, followups: true },
  { kind: 'tracking_list' as const, label: 'Tracking list', connection: '', items: true, followups: true },
]
const connections = [{ id: 'k1', name: 'Tracker API', kind: 'http' }, { id: 'j1', name: 'Acme Jira', kind: 'jira' }, { id: 'l1', name: 'Acme Linear', kind: 'linear' }, { id: 'm1', name: 'PMO Sandbox', kind: 'km2_tracker' }]

beforeEach(() => setupApi())
afterEach(cleanup)

describe('tracker watch form', () => {
  it('builds tracker and tracking-list requests with fixed ids and only their fields', () => {
    const tracker = toWatchInput(form({ kind: 'tracker', connection_id: 'j1', project: ' prj ', jql: ' labels = x ', lead: ' acc-1 ', comments: true, stale_days: 7, mention_cap: 2, comment_cap: 9 }))
    expect(tracker).toMatchObject({
      connection_id: 'j1', id_field: 'key', tracked_fields: [], mention_cap_per_day: 2, comment_cap_per_run: 9,
      source: { kind: 'tracker', project: 'prj', jql: 'labels = x', include_done: false, lead: 'acc-1', comments: true, stale_days: 7 },
    })
    const list = toWatchInput(form({ kind: 'tracking_list', connection_id: 'j1', tracked_fields: 'status, due_date' }))
    expect(list).toMatchObject({ connection_id: null, id_field: 'key', tracked_fields: ['status', 'due_date'], source: { kind: 'tracking_list', comments: true, stale_days: 0 } })
    expect(list).not.toHaveProperty('mention_cap_per_day')
  })

  it('checks tracker sources locally', () => {
    expect(validateWatchForm(form({ kind: 'tracker' }))).toMatchObject({ connection_id: expect.any(String), project: expect.any(String) })
    const bad = validateWatchForm(form({ kind: 'tracking_list', tracked_fields: 'status, bogus', stale_days: 999, mention_cap: 0.5, comment_cap: 501 }))
    expect(bad.tracked_fields).toBe('Not tracker fields: bogus.')
    expect(bad).toHaveProperty('stale_days')
    expect(bad).toHaveProperty('mention_cap')
    expect(bad).toHaveProperty('comment_cap')
    expect(validateWatchForm(form({ kind: 'tracking_list' }))).toEqual({})
    // A ladder on the default fields can use due_date
    const ladder = form({ kind: 'tracking_list', ladder_on: true })
    ladder.ladder.date_field = 'due_date'
    expect(validateWatchForm(ladder)).toEqual({})
    expect(effectiveTracked(form({ kind: 'tracker' }))).toEqual(['status', 'assignee', 'due_date', 'priority'])
    expect(effectiveTracked(form({ kind: 'http_json', tracked_fields: '' }))).toEqual([])
    expect(isTracker('tracker') && isTracker('tracking_list') && !isTracker('web_page')).toBe(true)
  })

  it('round-trips a saved tracker watch with its caps', () => {
    const w = makeWatch({ source: { kind: 'tracker', project: 'PRJ', jql: 'a', include_done: true, lead: 'x', comments: true, stale_days: 9 },
      id_field: 'key', tracked_fields: ['status'], mention_cap_per_day: 4, comment_cap_per_run: 12, followups: null })
    const f = toWatchForm(w)
    expect(f).toMatchObject({ kind: 'tracker', project: 'PRJ', jql: 'a', include_done: true, lead: 'x', comments: true, stale_days: 9, mention_cap: 4, comment_cap: 12 })
    expect(sourceSummary(w)).toBe('Tracker API PRJ (a)')
    expect(sourceSummary(makeWatch({ source: { kind: 'tracking_list' } }))).toBe("the crew's tracking list")
    expect(toWatchForm(makeWatch({ source: { kind: 'web_page', url: 'u' } }))).toMatchObject({ project: '', comments: false, mention_cap: null })
  })
})

describe('WatchPanel with trackers', () => {
  it('sets up a Jira project watch: only tracker connections, JQL for Jira, limits, and a preview with roll-ups', async () => {
    vi.mocked(watchesApi.get).mockResolvedValue(makeView({ watch: null, stats: null, checks: [], source_kinds: trackerKinds, connections }))
    vi.mocked(watchesApi.save).mockResolvedValue(makeView())
    vi.mocked(watchesApi.preview).mockResolvedValue(makePreview({ rollups: { items: [rollup()], stale_days: 14 } }))
    renderRoute(<WatchPanel schedule={makeSchedule()} canManage canClear />)
    fireEvent.click(await screen.findByRole('button', { name: 'Add a watch' }))
    // The HTTP source lists only HTTP connections
    expect(screen.queryByRole('option', { name: 'Acme Jira' })).toBeNull()
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'tracker' } })
    expect(screen.queryByRole('option', { name: 'Tracker API' })).toBeNull()
    expect(screen.queryByLabelText(/Extra JQL/)).toBeNull()
    // KM2 issues connections are trackers too, without JQL
    expect(screen.getByRole('option', { name: 'PMO Sandbox' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Connection'), { target: { value: 'm1' } })
    expect(screen.queryByLabelText(/Extra JQL/)).toBeNull()
    fireEvent.change(screen.getByLabelText('Connection'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add watch' }))
    expect(await screen.findByText('Pick the tracker connection to read.')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Connection'), { target: { value: 'j1' } })
    fireEvent.change(screen.getByLabelText('Project'), { target: { value: 'PRJ' } })
    fireEvent.change(screen.getByLabelText(/Extra JQL/), { target: { value: 'labels = atlas' } })
    fireEvent.click(screen.getByRole('switch', { name: /Include done issues/ }))
    fireEvent.click(screen.getByText('Tracker tool limits'))
    fireEvent.change(screen.getByLabelText(/Mentions per person per day/), { target: { value: '2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    const preview = within(await screen.findByTestId('watch-preview'))
    expect(preview.getByText('PRJ-10')).toBeTruthy()
    expect(preview.getByText('overdue 1')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add watch' }))
    await waitFor(() => expect(watchesApi.save).toHaveBeenCalledWith('s1', expect.objectContaining({
      connection_id: 'j1', id_field: 'key', mention_cap_per_day: 2,
      source: expect.objectContaining({ kind: 'tracker', project: 'PRJ', jql: 'labels = atlas', include_done: true, comments: true }),
    })))
  })

  it('a tracking-list watch points to the State tab and a ladder can use the default fields', async () => {
    vi.mocked(watchesApi.get).mockResolvedValue(makeView({ watch: null, stats: null, checks: [], source_kinds: trackerKinds, connections }))
    vi.mocked(watchesApi.save).mockResolvedValue(makeView())
    renderRoute(<WatchPanel schedule={makeSchedule()} canManage canClear />)
    fireEvent.click(await screen.findByRole('button', { name: 'Add a watch' }))
    fireEvent.change(screen.getByLabelText('Source'), { target: { value: 'tracking_list' } })
    expect(screen.getByRole('link', { name: /Manage the tracking list/ })).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: /Follow-up ladder/ }))
    fireEvent.change(screen.getByLabelText('Date field'), { target: { value: 'due_date' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add watch' }))
    await waitFor(() => expect(watchesApi.save).toHaveBeenCalledWith('s1', expect.objectContaining({
      source: { kind: 'tracking_list', comments: true, stale_days: 0 }, followups: expect.objectContaining({ date_field: 'due_date' }),
    })))
  })
})

describe('roll-ups on the Brief tab', () => {
  it('parses the rollups input and shows each tracked item with what needs attention', () => {
    const inputs = { rollups: JSON.stringify({ items: [rollup()], unchanged_items: 2, stale_days: 14 }), dry_run: 'true' }
    expect(parseWatchInputs(inputs).rollups?.items).toHaveLength(1)
    expect(parseWatchInputs({ rollups: '{"items": 3}' }).rollups).toBeUndefined()
    rtlRender(<WatchInputsSection inputs={inputs} />)
    const card = within(screen.getByTestId('rollup'))
    expect(card.getByText('PRJ-10').getAttribute('href')).toBe('https://acme.atlassian.net/browse/PRJ-10')
    expect(card.getByText(/5 children: 2 done, 1 in progress, 1 to do, 1 canceled/)).toBeTruthy()
    expect(card.getByText('PRJ-13 (2026-12-01)')).toBeTruthy()
    expect(card.getByText('2 new comments')).toBeTruthy()
    expect(card.getByText(/lead dana/)).toBeTruthy()
    expect(screen.getByText(/2 more tracked items had no changes/)).toBeTruthy()
  })

  it('shows an empty or unchanged roll-up', () => {
    rtlRender(<RollupsView rollups={[rollup({ changed_since_last_check: false, new_comments: 0, lead: undefined, due_date: '', overdue: [], children: { total: 0, done: 0, in_progress: 0, to_do: 0, canceled: 0 } })]} />)
    expect(screen.getByText('no change')).toBeTruthy()
    expect(screen.getByText(/0 children/)).toBeTruthy()
    cleanup()
    rtlRender(<RollupsView rollups={[]} />)
    expect(screen.getByText('No tracked item changed.')).toBeTruthy()
  })

  it('names whom a tracker follow-up notifies and counts left-out comments', () => {
    const preview = makePreview()
    const due = { ...preview.followups_due!.due[0], notify_person: { account_id: 'acc-sam', name: 'Sam Lee', studio_user: 'sam' } }
    const inputs = {
      changes: JSON.stringify({ ...preview.changes, new_comments: [{ id: 'jira:PRJ-1', author: 'Sam', at: '2026-10-06T00:00:00Z', excerpt: 'late' }], new_comments_omitted: 3 }),
      followups_due: JSON.stringify({ ...preview.followups_due, due: [due] }),
    }
    rtlRender(<WatchInputsSection inputs={inputs} />)
    expect(screen.getByText('(sam)')).toBeTruthy()
    expect(screen.getByText(/3 more new comments were left out/)).toBeTruthy()
  })
})
