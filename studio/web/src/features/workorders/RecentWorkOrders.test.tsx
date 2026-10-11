import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, screen, waitFor } from '@testing-library/react'
import { workOrdersApi } from '../../api/endpoints'
import { makeWorkOrder, renderRoute } from './testUtils'
import { RecentWorkOrders } from './RecentWorkOrders'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock())

const list = vi.mocked(workOrdersApi.list)

beforeEach(() => { list.mockReset() })
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe('RecentWorkOrders', () => {
  it('queries by source and shows the table', async () => {
    list.mockResolvedValue({ items: [makeWorkOrder({ title: 'Nightly' })], meta: { total: 1, limit: 10, offset: 0 } })
    renderRoute(<RecentWorkOrders source="schedule" sourceId="s1" />)
    expect(await screen.findByText('Nightly')).toBeTruthy()
    expect(list).toHaveBeenCalledWith({ source: 'schedule', source_id: 's1', limit: 10, offset: 0 }, expect.anything())
    expect(screen.queryByText(/All schedule work orders/)).toBeNull()
  })

  it('says none yet when empty', async () => {
    list.mockResolvedValue({ items: [], meta: { total: 0, limit: 10, offset: 0 } })
    renderRoute(<RecentWorkOrders source="webhook" sourceId="w1" />)
    expect(await screen.findByText('None yet.')).toBeTruthy()
  })

  it('links to the full list when there are more than shown', async () => {
    list.mockResolvedValue({ items: [makeWorkOrder()], meta: { total: 11, limit: 10, offset: 0 } })
    renderRoute(<RecentWorkOrders source="webhook" sourceId="w1" />)
    const link = await screen.findByText('All webhook work orders')
    expect(link.getAttribute('href')).toBe('/work-orders?source=webhook')
  })

  it('shows an error state when loading fails', async () => {
    list.mockImplementation(() => Promise.reject(new Error('boom')))
    renderRoute(<RecentWorkOrders source="schedule" sourceId="s1" />)
    expect(await screen.findByText('boom')).toBeTruthy()
  })

  it('refetches when the refresh key changes', async () => {
    list.mockResolvedValue({ items: [], meta: { total: 0, limit: 10, offset: 0 } })
    const { rerender } = renderRoute(<RecentWorkOrders source="schedule" sourceId="s1" refreshKey={0} />)
    await screen.findByText('None yet.')
    expect(list).toHaveBeenCalledTimes(1)
    void rerender
  })

  it('polls while a work order is active', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    list.mockResolvedValue({ items: [makeWorkOrder({ status: 'running' })], meta: { total: 1, limit: 10, offset: 0 } })
    renderRoute(<RecentWorkOrders source="schedule" sourceId="s1" />)
    await screen.findByText('Quarterly review')
    const before = list.mock.calls.length
    await vi.advanceTimersByTimeAsync(5100)
    await waitFor(() => expect(list.mock.calls.length).toBeGreaterThan(before))
  })
})
