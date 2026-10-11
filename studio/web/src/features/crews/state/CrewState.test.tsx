import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { watchesApi } from '../../../api/endpoints.watch'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { connectionsApi, crewStateApi } from '../../../api/endpoints.crewstate'
import { trackingApi } from '../../../api/endpoints.trackers'
import type { ActionEntry, NoteInfo, NoteVersion } from '../../../api/types.crewstate'
import { makeConnection } from '../../connections/testUtils'
import { renderRoute, setupApi, without } from '../testUtils'
import { ActionLogPanel, detailsText } from './ActionLogPanel'
import { canSeeCrewState, CrewStatePanel } from './CrewStatePanel'
import { CrewConnectionsPanel } from './CrewConnectionsPanel'
import { NotesPanel } from './NotesPanel'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.crewstate')
vi.mock('../../../api/endpoints.watch')
vi.mock('../../../api/endpoints.trackers')

const TS = '2026-10-05T07:00:00Z'
const note = (over: Partial<NoteInfo>): NoteInfo => ({
  schedule_id: null, schedule_name: '', subject: '', name: 'n', version: 1, bytes: 10, written_by: 'run', run_id: null, updated_at: TS, ...over,
})
const version = (v: number, content: string): NoteVersion => ({
  id: `v${v}`, crew_id: 'c1', schedule_id: 's1', subject: 'project:atlas', name: 'weekly-report', version: v, content, run_id: null, written_by: v === 2 ? 'Ada' : 'run', created_at: TS,
})
const entry = (over: Partial<ActionEntry>): ActionEntry => ({
  id: 'e1', crew_id: 'c1', schedule_id: 's1', schedule_name: 'atlas sweep', subject: 'jira:PRJ-15', action: 'followup:due_soon', run_id: 'r1',
  dry_run: false, details: { to: 'sam' }, idempotency_key: null, at: TS, ...over,
})

beforeEach(() => {
  setupApi()
  vi.mocked(watchesApi.ofCrew).mockResolvedValue([])
  vi.mocked(trackingApi.list).mockResolvedValue({ items: [], connections: [] })
  vi.mocked(crewStateApi.notes).mockResolvedValue([
    note({ name: 'weekly-report', subject: 'project:atlas', schedule_id: 's1', schedule_name: 'atlas sweep', version: 2, bytes: 2048, written_by: 'Ada' }),
    note({ name: 'people-preferences' }),
  ])
  vi.mocked(crewStateApi.versions).mockResolvedValue([version(2, 'second'), version(1, 'first')])
  vi.mocked(crewStateApi.log).mockResolvedValue({ items: [entry({}), entry({ id: 'e2', schedule_id: null, dry_run: true, details: {}, run_id: null })], meta: { total: 30, limit: 25, offset: 0 } })
  vi.mocked(crewStateApi.connections).mockResolvedValue([makeConnection({ id: 'k1', name: 'Ticket API' }), makeConnection({ id: 'k2', name: 'Old', enabled: false })])
  vi.mocked(connectionsApi.list).mockResolvedValue([makeConnection({ id: 'k1', name: 'Ticket API' }), makeConnection({ id: 'k3', name: 'Acme Jira', kind: 'jira' })])
})
afterEach(cleanup)

describe('NotesPanel and NoteHistory', () => {
  it('lists the latest version of each note with its schedule', async () => {
    renderRoute(<NotesPanel crewId="c1" canManage />)
    const report = within((await screen.findByText('weekly-report')).closest('tr') as HTMLElement)
    expect(report.getByText('atlas sweep')).toBeTruthy()
    expect(report.getByText('v2')).toBeTruthy()
    expect(report.getByText('2.0 KB')).toBeTruthy()
    expect(within(screen.getByText('people-preferences').closest('tr') as HTMLElement).getByText('Crew-wide')).toBeTruthy()
    expect(crewStateApi.notes).toHaveBeenCalledWith('c1', '', expect.anything())
  })

  it('shows the empty state', async () => {
    vi.mocked(crewStateApi.notes).mockResolvedValue([])
    renderRoute(<NotesPanel crewId="c1" canManage />)
    expect(await screen.findByText('No notes yet')).toBeTruthy()
  })

  const openHistory = async () => {
    fireEvent.click(within((await screen.findByText('weekly-report')).closest('tr') as HTMLElement).getByRole('button', { name: /History/ }))
    return within(await screen.findByRole('dialog'))
  }
  const key = { schedule_id: 's1', subject: 'project:atlas', name: 'weekly-report' }

  it('restores an older version as a new one', async () => {
    vi.mocked(crewStateApi.rollback).mockResolvedValue(version(3, 'first'))
    renderRoute(<NotesPanel crewId="c1" canManage />)
    const dialog = await openHistory()
    expect(await dialog.findByText('second')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: /v1/ }))
    expect(dialog.getByText('first')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: 'Restore v1' }))
    await waitFor(() => expect(crewStateApi.rollback).toHaveBeenCalledWith('c1', key, 1, 2))
    await waitFor(() => expect(crewStateApi.versions).toHaveBeenCalledTimes(2))
  })

  it('edits the latest version and shows a refused save', async () => {
    vi.mocked(crewStateApi.putNote).mockRejectedValueOnce(new ApiError(409, { code: 'version_conflict', message: 'The note is at version 3, not 2.' }))
    vi.mocked(crewStateApi.putNote).mockResolvedValueOnce(version(3, 'edited'))
    renderRoute(<NotesPanel crewId="c1" canManage />)
    const dialog = await openHistory()
    fireEvent.click(await dialog.findByRole('button', { name: /Edit/ }))
    fireEvent.change(dialog.getByLabelText(/saves as v3/), { target: { value: 'edited' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await dialog.findByText('The note is at version 3, not 2.')).toBeTruthy()
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(crewStateApi.putNote).toHaveBeenLastCalledWith('c1', key, 'edited', 2))
    fireEvent.click(await dialog.findByRole('button', { name: /Edit/ }))
    fireEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    expect(dialog.queryByRole('button', { name: 'Save' })).toBeNull()
  })

  it('deletes the whole note and closes', async () => {
    vi.mocked(crewStateApi.deleteNote).mockResolvedValue(undefined)
    renderRoute(<NotesPanel crewId="c1" canManage />)
    const dialog = await openHistory()
    const del = await dialog.findByRole('button', { name: /Delete note/ })
    fireEvent.click(del)
    fireEvent.click(dialog.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(crewStateApi.deleteNote).toHaveBeenCalledWith('c1', key))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('is read-only without state.manage, and copes with a note that has gone', async () => {
    vi.mocked(crewStateApi.versions).mockResolvedValue([])
    renderRoute(<NotesPanel crewId="c1" canManage={false} />)
    const dialog = await openHistory()
    expect(await dialog.findByText('This note has no versions left.')).toBeTruthy()
    expect(dialog.queryByRole('button', { name: /Edit|Delete/ })).toBeNull()
  })
})

describe('ActionLogPanel', () => {
  it('lists entries with schedule, details, dry runs and run links', async () => {
    renderRoute(<ActionLogPanel crewId="c1" canManage />)
    const first = within((await screen.findAllByText('jira:PRJ-15'))[0].closest('tr') as HTMLElement)
    expect(first.getByText('atlas sweep')).toBeTruthy()
    expect(first.getByText('{"to":"sam"}')).toBeTruthy()
    expect(first.getByRole('link', { name: 'open' }).getAttribute('href')).toBe('/runs/r1')
    const second = within(screen.getAllByText('jira:PRJ-15')[1].closest('tr') as HTMLElement)
    expect(second.getByText('dry run')).toBeTruthy()
    expect(second.getByText('Crew-wide')).toBeTruthy()
    expect(screen.getByText('1–25 of 30')).toBeTruthy()
  })

  it('filters, pages and deletes', async () => {
    vi.mocked(crewStateApi.deleteLogEntry).mockRejectedValueOnce(new Error('nope')).mockResolvedValueOnce(undefined)
    renderRoute(<ActionLogPanel crewId="c1" canManage />)
    await screen.findAllByText('jira:PRJ-15')
    fireEvent.change(screen.getByLabelText('Subject filter'), { target: { value: ' PRJ-15 ' } })
    fireEvent.change(screen.getByLabelText('Action filter'), { target: { value: 'due' } })
    fireEvent.click(screen.getByRole('button', { name: /Filter/ }))
    await waitFor(() => expect(crewStateApi.log).toHaveBeenLastCalledWith('c1', { subject: 'PRJ-15', action: 'due', limit: 25, offset: 0 }, expect.anything()))
    fireEvent.click(await screen.findByRole('button', { name: 'Next page' }))
    await waitFor(() => expect(crewStateApi.log).toHaveBeenLastCalledWith('c1', { subject: 'PRJ-15', action: 'due', limit: 25, offset: 25 }, expect.anything()))
    const row = () => within((screen.getAllByText('jira:PRJ-15'))[0].closest('tr') as HTMLElement)
    await screen.findAllByText('jira:PRJ-15')
    for (let i = 0; i < 2; i++) {
      fireEvent.click(row().getByRole('button', { name: /Delete/ }))
      fireEvent.click(row().getByRole('button', { name: 'Confirm?' }))
      await waitFor(() => expect(crewStateApi.deleteLogEntry).toHaveBeenCalledTimes(i + 1))
    }
    expect(crewStateApi.deleteLogEntry).toHaveBeenCalledWith('c1', 'e1')
  })

  it('has empty states for no entries and no matches, and no delete without state.manage', async () => {
    vi.mocked(crewStateApi.log).mockResolvedValue({ items: [], meta: { total: 0, limit: 25, offset: 0 } })
    renderRoute(<ActionLogPanel crewId="c1" canManage={false} />)
    expect(await screen.findByText('Nothing logged yet')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Action filter'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: /Filter/ }))
    expect(await screen.findByText('No matching entries')).toBeTruthy()
  })

  it('detailsText is compact JSON or empty', () => {
    expect(detailsText({})).toBe('')
    expect(detailsText({ a: 1 })).toBe('{"a":1}')
  })
})

describe('CrewConnectionsPanel', () => {
  it('shows linked connections and changes them', async () => {
    vi.mocked(crewStateApi.setConnections).mockResolvedValue([makeConnection({ id: 'k3', name: 'Acme Jira' })])
    renderRoute(<CrewConnectionsPanel crewId="c1" canEdit />)
    expect(await screen.findByText(/Ticket API/)).toBeTruthy()
    expect(screen.getByText(/Old \(disabled\)/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Change/ }))
    fireEvent.click(await screen.findByRole('option', { name: /Acme Jira/ }))
    fireEvent.click(screen.getByRole('option', { name: /Ticket API/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Save connections' }))
    await waitFor(() => expect(crewStateApi.setConnections).toHaveBeenCalledWith('c1', ['k2', 'k3']))
    expect(await screen.findByText(/Acme Jira/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Save connections' })).toBeNull()
  })

  it('keeps editing when saving fails, and can cancel', async () => {
    vi.mocked(crewStateApi.setConnections).mockRejectedValue(new Error('denied'))
    renderRoute(<CrewConnectionsPanel crewId="c1" canEdit />)
    fireEvent.click(await screen.findByRole('button', { name: /Change/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Save connections' }))
    await waitFor(() => expect(crewStateApi.setConnections).toHaveBeenCalled())
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('button', { name: /Change/ })).toBeTruthy()
  })

  it('explains when there are none, and cannot change without crews.edit', async () => {
    vi.mocked(crewStateApi.connections).mockResolvedValue([])
    renderRoute(<CrewConnectionsPanel crewId="c1" canEdit={false} />)
    expect((await screen.findByRole('link', { name: 'connections' })).getAttribute('href')).toBe('/connections')
    expect(screen.queryByRole('button', { name: /Change/ })).toBeNull()
  })
})

describe('CrewStatePanel', () => {
  it('shows every section to someone with all permissions', async () => {
    renderRoute(<CrewStatePanel crewId="c1" />)
    expect(await screen.findByText('Notes')).toBeTruthy()
    expect(screen.getByText('Action log')).toBeTruthy()
    expect(screen.getByText('Connections')).toBeTruthy()
    expect(screen.getByText('Tracking list')).toBeTruthy()
  })

  it('leaves out what the person cannot see', async () => {
    renderRoute(<CrewStatePanel crewId="c1" />, { perms: without('state.view', 'connections.view') })
    await screen.findByText(/Crew state lasts between runs/)
    expect(screen.queryByText('Notes')).toBeNull()
    expect(screen.queryByText('Connections')).toBeNull()
    expect(crewStateApi.notes).not.toHaveBeenCalled()
  })

  it('canSeeCrewState needs state.view or connections.view', () => {
    expect(canSeeCrewState((p) => p === 'connections.view')).toBe(true)
    expect(canSeeCrewState(() => false)).toBe(false)
  })
})
