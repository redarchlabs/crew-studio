import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { crewsApi, workOrdersApi } from '../../api/endpoints'
import { makeCrew } from '../../lib/testFixtures'
import { makeWorkOrder, renderRoute, setPermissions } from './testUtils'
import { WorkOrdersPage } from './WorkOrdersPage'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock())
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock())

const list = vi.mocked(workOrdersApi.list)
const crewList = vi.mocked(crewsApi.list)
const page = (items = [makeWorkOrder()], total = items.length, offset = 0) => ({ items, meta: { total, limit: 25, offset } })

beforeEach(() => {
  list.mockReset()
  list.mockResolvedValue(page())
  crewList.mockReset()
  crewList.mockResolvedValue([makeCrew({ id: 'c2', name: 'Zed crew' }), makeCrew({ id: 'c1', name: 'Alpha crew' })])
  setPermissions('work_orders.create', 'crews.view')
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

const lastQuery = () => list.mock.calls[list.mock.calls.length - 1][0]

describe('WorkOrdersPage', () => {
  it('lists work orders and offers New work order to people who can create', async () => {
    renderRoute(<WorkOrdersPage />)
    expect((await screen.findAllByText('Quarterly review')).length).toBeGreaterThan(0)
    expect(lastQuery()).toEqual({ status: undefined, source: undefined, crew_id: undefined, q: undefined, limit: 25, offset: 0 })
    expect(screen.getByRole('link', { name: /New work order/ }).getAttribute('href')).toBe('/work-orders/new')
  })

  it('hides New work order without the permission', async () => {
    setPermissions()
    renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('Quarterly review')
    expect(screen.queryByRole('link', { name: /New work order/ })).toBeNull()
  })

  it('reads filters from the URL', async () => {
    renderRoute(<WorkOrdersPage />, { url: '/?status=failed&source=api&crew_id=c1&q=WO-5&offset=25' })
    await screen.findAllByText('Quarterly review')
    expect(lastQuery()).toEqual({ status: 'failed', source: 'api', crew_id: 'c1', q: 'WO-5', limit: 25, offset: 25 })
    await waitFor(() => expect((screen.getByLabelText('Filter by crew') as HTMLSelectElement).value).toBe('c1'))
    expect((screen.getByLabelText('Filter by status') as HTMLSelectElement).value).toBe('failed')
    expect((screen.getByLabelText('Search work orders') as HTMLInputElement).value).toBe('WO-5')
  })

  it('treats a junk offset as zero', async () => {
    renderRoute(<WorkOrdersPage />, { url: '/?offset=-4' })
    await screen.findAllByText('Quarterly review')
    expect(lastQuery().offset).toBe(0)
    list.mockClear()
  })

  it.each([
    ['Filter by status', 'running', 'status=running'],
    ['Filter by source', 'webhook', 'source=webhook'],
    ['Filter by crew', 'c1', 'crew_id=c1'],
  ])('%s updates the URL and refetches', async (label, value, qs) => {
    const view = renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('Quarterly review')
    await screen.findByRole('option', { name: 'Alpha crew' })
    list.mockClear()
    fireEvent.change(screen.getByLabelText(label), { target: { value } })
    await waitFor(() => expect(list).toHaveBeenCalled())
    expect(view.location().search).toBe(`?${qs}`)
  })

  it('the crew filter lists visible crews by name after "All crews" and sends crew_id', async () => {
    renderRoute(<WorkOrdersPage />)
    await screen.findByRole('option', { name: 'Alpha crew' })
    const select = screen.getByLabelText('Filter by crew') as HTMLSelectElement
    expect([...select.options].map((o) => o.textContent)).toEqual(['All crews', 'Alpha crew', 'Zed crew'])
    fireEvent.change(select, { target: { value: 'c2' } })
    await waitFor(() => expect(lastQuery().crew_id).toBe('c2'))
  })

  it('keeps a crew id from the URL selected when that crew is not listed', async () => {
    renderRoute(<WorkOrdersPage />, { url: '/?crew_id=gone' })
    expect(await screen.findByRole('option', { name: 'Crew not visible' })).toBeTruthy()
    expect((screen.getByLabelText('Filter by crew') as HTMLSelectElement).value).toBe('gone')
    expect(lastQuery().crew_id).toBe('gone')
  })

  it('hides the crew filter without crews.view, and when there are no crews', async () => {
    setPermissions('work_orders.create')
    renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('Quarterly review')
    expect(screen.queryByLabelText('Filter by crew')).toBeNull()
    expect(crewList).not.toHaveBeenCalled()
    cleanup()
    setPermissions('crews.view')
    crewList.mockResolvedValue([])
    renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('Quarterly review')
    await waitFor(() => expect(crewList).toHaveBeenCalled())
    expect(screen.queryByLabelText('Filter by crew')).toBeNull()
  })

  it('disables the crew filter when crews cannot be loaded', async () => {
    crewList.mockRejectedValue(new Error('nope'))
    renderRoute(<WorkOrdersPage />)
    await waitFor(() => expect((screen.getByLabelText('Filter by crew') as HTMLSelectElement).disabled).toBe(true))
    expect(screen.getByLabelText('Filter by crew').getAttribute('title')).toBe('Could not load crews')
  })

  it('keeps the URL crew selected while crews are loading or failed', async () => {
    crewList.mockRejectedValue(new Error('nope'))
    renderRoute(<WorkOrdersPage />, { url: '/?crew_id=c9' })
    expect(await screen.findByRole('option', { name: 'Selected crew' })).toBeTruthy()
    expect((screen.getByLabelText('Filter by crew') as HTMLSelectElement).disabled).toBe(false)
  })

  it('changing a filter resets the page offset', async () => {
    const view = renderRoute(<WorkOrdersPage />, { url: '/?offset=25&source=api' })
    await screen.findAllByText('Quarterly review')
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'draft' } })
    await waitFor(() => expect(view.location().search).not.toContain('offset'))
    expect(view.location().search).toContain('status=draft')
    expect(view.location().search).toContain('source=api')
  })

  it('clearing a filter removes it from the URL', async () => {
    const view = renderRoute(<WorkOrdersPage />, { url: '/?status=draft' })
    await screen.findAllByText('Quarterly review')
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: '' } })
    await waitFor(() => expect(view.location().search).toBe(''))
  })

  it('debounces the search box into ?q= (trimmed)', async () => {
    const view = renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('Quarterly review')
    list.mockClear()
    fireEvent.change(screen.getByLabelText('Search work orders'), { target: { value: '  WO-12 ' } })
    expect(list).not.toHaveBeenCalled()
    await waitFor(() => expect(view.location().search).toBe('?q=WO-12'), { timeout: 2000 })
    await waitFor(() => expect(lastQuery().q).toBe('WO-12'))
  })

  it('shows the no-data empty state with a create action', async () => {
    list.mockResolvedValue(page([], 0))
    renderRoute(<WorkOrdersPage />)
    expect(await screen.findByText('No work orders yet')).toBeTruthy()
    expect(screen.getAllByRole('link', { name: 'New work order' }).length).toBeGreaterThan(0)
  })

  it('shows a filtered empty state without the create action', async () => {
    list.mockResolvedValue(page([], 0))
    renderRoute(<WorkOrdersPage />, { url: '/?status=failed' })
    expect(await screen.findByText('No work orders match these filters')).toBeTruthy()
    expect(screen.getByText('Try clearing a filter.')).toBeTruthy()
  })

  it('shows an error with retry', async () => {
    list.mockImplementationOnce(() => Promise.reject(new Error('db down')))
    renderRoute(<WorkOrdersPage />)
    expect(await screen.findByText('db down')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect((await screen.findAllByText('Quarterly review')).length).toBeGreaterThan(0)
  })

  it('paginates with next and previous', async () => {
    list.mockResolvedValue(page([makeWorkOrder()], 60, 25))
    const view = renderRoute(<WorkOrdersPage />, { url: '/?offset=25' })
    await screen.findByText('26–50 of 60')
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    await waitFor(() => expect(view.location().search).toBe('?offset=50'))
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }))
    await waitFor(() => expect(view.location().search).toBe(''))
  })

  it('polls while any listed work order is active and stops when none are', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    list.mockResolvedValueOnce(page([makeWorkOrder({ status: 'running' })]))
    list.mockResolvedValue(page([makeWorkOrder({ status: 'succeeded' })]))
    renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('running')
    await vi.advanceTimersByTimeAsync(4100)
    await screen.findAllByText('succeeded')
    const calls = list.mock.calls.length
    await vi.advanceTimersByTimeAsync(9000)
    expect(list.mock.calls.length).toBe(calls)
  })

  it('table links go to the detail page', async () => {
    renderRoute(<WorkOrdersPage />)
    await screen.findAllByText('Quarterly review')
    const table = screen.getByRole('table')
    expect(within(table).getAllByRole('link').map((a) => a.getAttribute('href'))).toEqual(['/work-orders/wo-1', '/crews/crew-1'])
  })
})
