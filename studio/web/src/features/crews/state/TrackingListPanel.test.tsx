import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { trackingApi } from '../../../api/endpoints.trackers'
import type { TrackingCheck, TrackingItem, TrackingView } from '../../../api/types.trackers'
import { renderRoute, setupApi } from '../testUtils'
import { TrackingListPanel } from './TrackingListPanel'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.trackers')

const TS = '2026-10-05T07:00:00Z'
const item = (over: Partial<TrackingItem> = {}): TrackingItem => ({
  id: 't1', crew_id: 'c1', schedule_id: null, schedule_name: '', connection_id: 'j1', connection_name: 'Acme Jira', connection_kind: 'jira',
  project: 'Atlas', item_ref: 'PRJ-10', include_children: 'all', note: 'Phase 2', lead_account_id: 'acc-dana', lead_name: 'Dana Ruiz', added_by: 'u1', added_by_name: 'Ada',
  added_at: TS, removed_at: null, removed_by_name: '', ...over,
})
const view = (over: Partial<TrackingView> = {}): TrackingView => ({ items: [item()], connections: [{ id: 'j1', name: 'Acme Jira', kind: 'jira' }], ...over })
const check = (over: Partial<TrackingCheck> = {}): TrackingCheck => ({
  connection: { id: 'j1', name: 'Acme Jira', kind: 'jira' },
  issue: { key: 'PRJ-20', title: 'Birch launch', url: 'https://acme.atlassian.net/browse/PRJ-20', status: 'To Do', status_category: 'to_do',
    assignee: { account_id: 'acc-sam', name: 'Sam' }, due_date: '2026-12-01', priority: 'High', updated_at: TS, parent: '', labels: [], project: 'PRJ' },
  direct_children: 4, children_capped: false, already_tracked: false, ...over,
})

beforeEach(() => {
  setupApi()
  vi.mocked(trackingApi.list).mockResolvedValue(view())
})
afterEach(cleanup)

// "Track an item" sits in the panel header and shows before the list loads, but the form only renders with the list
async function openAddForm() {
  fireEvent.click(await screen.findByRole('button', { name: /Track an item/ }))
  await screen.findByLabelText('Item')
}

describe('TrackingListPanel', () => {
  it('lists tracked items with who added them', async () => {
    renderRoute(<TrackingListPanel crewId="c1" canManage={false} />)
    expect(await screen.findByText('PRJ-10')).toBeTruthy()
    expect(screen.getByText(/Acme Jira · all levels of children/)).toBeTruthy()
    expect(screen.getByText(/Phase 2/)).toBeTruthy()
    expect(screen.getByText(/added by Ada/)).toBeTruthy()
    expect(screen.getByText('Dana Ruiz').getAttribute('title')).toBe('acc-dana')
    expect(screen.queryByText('acc-dana')).toBeNull()
    expect(screen.queryByRole('button', { name: /Track an item/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Stop tracking/ })).toBeNull()
    vi.mocked(trackingApi.list).mockResolvedValue(view({ items: [item({ removed_at: TS, removed_by_name: 'Bo', schedule_name: 'daily sweep' })] }))
    fireEvent.click(screen.getByRole('switch', { name: /Show removed items/ }))
    await waitFor(() => expect(trackingApi.list).toHaveBeenLastCalledWith('c1', true, expect.anything()))
    expect(await screen.findByText(/removed by Bo/)).toBeTruthy()
    expect(screen.getByText('daily sweep')).toBeTruthy()
  })

  it('shows the lead by account id when the people map does not know them, and no lead when none is set', async () => {
    vi.mocked(trackingApi.list).mockResolvedValue(view({ items: [
      item({ lead_account_id: 'acc-zed', lead_name: '' }),
      item({ id: 't2', item_ref: 'PRJ-11', lead_account_id: '', lead_name: '' }),
    ] }))
    renderRoute(<TrackingListPanel crewId="c1" canManage={false} />)
    expect((await screen.findByText('acc-zed')).className).toContain('font-mono')
    expect(screen.getByText('PRJ-11').closest('li')?.textContent).not.toMatch(/\blead\b/)
  })

  it('looks an item up, then adds it only after the person confirms', async () => {
    vi.mocked(trackingApi.check).mockResolvedValue(check())
    vi.mocked(trackingApi.add).mockResolvedValue(item({ id: 't2', item_ref: 'PRJ-20', project: 'Birch' }))
    renderRoute(<TrackingListPanel crewId="c1" canManage />)
    await openAddForm()
    fireEvent.click(screen.getByRole('button', { name: 'Look up' }))
    expect(await screen.findByText('Enter the item key, such as PRJ-10.')).toBeTruthy()
    expect(trackingApi.check).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('Item'), { target: { value: 'prj-20' } })
    fireEvent.change(screen.getByLabelText('Project (optional)'), { target: { value: 'Birch' } })
    fireEvent.change(screen.getByLabelText('Track'), { target: { value: 'direct' } })
    fireEvent.click(screen.getByRole('button', { name: 'Look up' }))
    const found = within(await screen.findByTestId('tracking-check'))
    expect(found.getByText('Birch launch')).toBeTruthy()
    expect(found.getByText(/4 direct children/)).toBeTruthy()
    expect(trackingApi.add).not.toHaveBeenCalled()
    fireEvent.click(found.getByRole('button', { name: /Confirm and track direct children only/ }))
    await waitFor(() => expect(trackingApi.add).toHaveBeenCalledWith('c1', expect.objectContaining({ connection_id: 'j1', item_ref: 'prj-20', project: 'Birch', include_children: 'direct' })))
    await waitFor(() => expect(trackingApi.list).toHaveBeenCalledTimes(2))
  })

  it('an edit after the lookup needs a new lookup; tracked items cannot be added twice; refusals are shown', async () => {
    vi.mocked(trackingApi.check).mockResolvedValueOnce(check({ already_tracked: true, children_capped: true }))
    vi.mocked(trackingApi.check).mockRejectedValueOnce(new ApiError(422, { code: 'tracker_check_failed', message: 'PRJ-99 was not found in Acme Jira, or the connection cannot see it' }))
    vi.mocked(trackingApi.check).mockResolvedValueOnce(check())
    vi.mocked(trackingApi.add).mockRejectedValue(new ApiError(409, { code: 'already_tracked', message: 'This item is already on the tracking list.' }))
    renderRoute(<TrackingListPanel crewId="c1" canManage />)
    await openAddForm()
    fireEvent.change(screen.getByLabelText('Item'), { target: { value: 'PRJ-20' } })
    fireEvent.click(screen.getByRole('button', { name: 'Look up' }))
    const found = within(await screen.findByTestId('tracking-check'))
    expect(found.getByText(/4\+ direct children/)).toBeTruthy()
    expect(found.getByText('This item is already on the tracking list.')).toBeTruthy()
    expect((found.getByRole('button', { name: /Confirm and track/ }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Item'), { target: { value: 'PRJ-99' } })
    expect(screen.queryByTestId('tracking-check')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Look up' }))
    expect(await screen.findByText(/PRJ-99 was not found in Acme Jira/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Look up' }))
    fireEvent.click(within(await screen.findByTestId('tracking-check')).getByRole('button', { name: /Confirm and track/ }))
    expect(await screen.findByText('This item is already on the tracking list.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByTestId('tracking-check')).toBeNull()
  })

  it('stops tracking an item after a confirm, and explains when the crew has no tracker', async () => {
    vi.mocked(trackingApi.remove).mockResolvedValueOnce(item({ removed_at: TS }))
    vi.mocked(trackingApi.remove).mockRejectedValueOnce(new Error('offline'))
    renderRoute(<TrackingListPanel crewId="c1" canManage />)
    fireEvent.click(await screen.findByRole('button', { name: /Stop tracking/ }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(trackingApi.remove).toHaveBeenCalledWith('c1', 't1'))
    await waitFor(() => expect(trackingApi.list).toHaveBeenCalledTimes(2)) // reloaded
    fireEvent.click(await screen.findByRole('button', { name: /Stop tracking/ }))
    fireEvent.click(await screen.findByRole('button', { name: /Confirm/ }))
    await waitFor(() => expect(trackingApi.remove).toHaveBeenCalledTimes(2))
    expect(await screen.findByRole('button', { name: /Stop tracking/ })).toBeTruthy() // still listed after a failure
    cleanup()
    vi.mocked(trackingApi.list).mockResolvedValue(view({ items: [], connections: [] }))
    renderRoute(<TrackingListPanel crewId="c1" canManage />)
    expect(await screen.findByText('Nothing is tracked yet.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Track an item/ }))
    expect(screen.getByText(/let this crew use a tracker connection/)).toBeTruthy()
  })
})
