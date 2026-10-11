import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { reportsApi } from '../../api/endpoints.delivery'
import { notifyError, notifySuccess } from '../../lib/notify'
import { renderRoute, setupApi } from '../crews/testUtils'
import { cadenceLabel, deliveryTarget, ReportsTab } from './ReportsTab'
import { makeDelivery, makeReport, makeSnapshot } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.delivery')
vi.mock('../../lib/notify')

const render = (canRetry = true, live = false) => renderRoute(<ReportsTab workOrderId="wo-1" canRetry={canRetry} live={live} />)

beforeEach(() => {
  setupApi()
  vi.mocked(reportsApi.list).mockResolvedValue({ reports: [makeReport()], retried: 0, sent: 0 })
})
afterEach(cleanup)

describe('ReportsTab', () => {
  it('shows each delivery and retries the failed ones', async () => {
    const fixed = makeReport({ retryable: 0, deliveries: makeReport().deliveries.map((d) => ({ ...d, status: 'sent' as const })) })
    vi.mocked(reportsApi.retry).mockResolvedValue({ reports: [fixed], retried: 1, sent: 1 })
    render()
    expect(await screen.findByText('Atlas status, 2026-W41')).toBeTruthy()
    expect(screen.getAllByTestId('delivery-row')).toHaveLength(3)
    expect(screen.getByText('Microsoft Teams: PMO Teams')).toBeTruthy()
    expect(screen.getByText('Teams had a problem (HTTP 502). (2 attempts)')).toBeTruthy()
    expect(screen.getByText('Email: PMO Mail (1 recipient)')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Show report' }))
    expect(screen.getByRole('heading', { name: 'Atlas' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Hide report' }))
    fireEvent.click(screen.getByRole('button', { name: 'Retry failed deliveries' }))
    await waitFor(() => expect(notifySuccess).toHaveBeenCalledWith('Retried 1 delivery; 1 sent'))
    expect(screen.queryByRole('button', { name: 'Retry failed deliveries' })).toBeNull()
  })

  it('reports nothing to retry and failures to retry', async () => {
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [makeReport({ retryable: 2 })], retried: 0, sent: 0 })
    vi.mocked(reportsApi.retry).mockResolvedValueOnce({ reports: [makeReport({ retryable: 2 })], retried: 0, sent: 0 })
    render()
    expect(await screen.findByText(/2 deliveries failed/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry failed deliveries' }))
    await waitFor(() => expect(notifySuccess).toHaveBeenCalledWith('Nothing to retry'))
    vi.mocked(reportsApi.retry).mockResolvedValueOnce({ reports: [makeReport({ retryable: 0 })], retried: 2, sent: 0 })
    fireEvent.click(screen.getByRole('button', { name: 'Retry failed deliveries' }))
    await waitFor(() => expect(notifySuccess).toHaveBeenCalledWith('Retried 2 deliveries; 0 sent'))
    cleanup()
    vi.mocked(reportsApi.retry).mockRejectedValue(new Error('down'))
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Retry failed deliveries' }))
    await waitFor(() => expect(notifyError).toHaveBeenCalledWith('Could not retry the deliveries', expect.any(Error)))
  })

  it('hides retry from people who cannot create work orders, and shows dry runs and empty states', async () => {
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [makeReport({ dry_run: true, retryable: 0, deliveries: [makeDelivery({ kind: 'slack', connection_name: '', status: 'would_send' }), makeDelivery({ id: 'x', kind: 'tracker', status: 'pending', connection_name: 'Jira' })] })], retried: 0, sent: 0 })
    render(false)
    expect(await screen.findByText('dry run')).toBeTruthy()
    expect(screen.getByText('would send')).toBeTruthy()
    expect(screen.getByText('Slack: deleted connection')).toBeTruthy()
    expect(screen.getByText('Tracker: Jira')).toBeTruthy()
    cleanup()
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [makeReport()], retried: 0, sent: 0 })
    render(false)
    await screen.findByText('Atlas status, 2026-W41')
    expect(screen.queryByRole('button', { name: 'Retry failed deliveries' })).toBeNull()
    cleanup()
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [], retried: 0, sent: 0 })
    render(true, true)
    expect(await screen.findByText('No reports')).toBeTruthy()
  })

  it('labels each report with its cadence and period', async () => {
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [
      makeReport({ cadence: 'weekly', period: '2026-W41' }),
      makeReport({ id: 'r2', cadence: 'daily', period: '2026-10-06', title: 'Atlas daily report, 2026-10-06', dry_run: true, retryable: 0 }),
    ], retried: 0, sent: 0 })
    render()
    expect(await screen.findByText('Daily · 2026-10-06')).toBeTruthy()
    expect(screen.getByText('Weekly · 2026-W41')).toBeTruthy()
    expect(screen.getByText('Atlas daily report, 2026-10-06')).toBeTruthy()
    expect(cadenceLabel({ week: '2026-W40' })).toBe('Weekly · 2026-W40')
  })

  it('shows the designed report when a snapshot was taken, with the text and a download behind it', async () => {
    vi.mocked(reportsApi.htmlUrl).mockReturnValue('/api/work-orders/wo-1/reports/r2/html')
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [
      makeReport({ id: 'r2', cadence: 'daily', period: '2026-10-06', markdown: 'Vendor access is the critical path.', snapshot: makeSnapshot() }),
    ], retried: 0, sent: 0 })
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Show report' }))
    expect(screen.getByTestId('due-report')).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Report' }).getAttribute('aria-selected')).toBe('true')
    const download = screen.getByRole('link', { name: 'Download HTML' })
    expect(download.getAttribute('href')).toBe('/api/work-orders/wo-1/reports/r2/html')
    expect(download.hasAttribute('download')).toBe(true)
    expect(reportsApi.htmlUrl).toHaveBeenCalledWith('wo-1', 'r2')
    fireEvent.click(screen.getByRole('tab', { name: 'Text' }))
    expect(screen.queryByTestId('due-report')).toBeNull()
    expect(screen.getByText('Vendor access is the critical path.')).toBeTruthy()
    fireEvent.click(screen.getByRole('tab', { name: 'Report' }))
    expect(screen.getByTestId('kpi-overdue')).toBeTruthy()
  })

  it('shows only the text of a report without a snapshot', async () => {
    vi.mocked(reportsApi.list).mockResolvedValue({ reports: [makeReport({ snapshot: null })], retried: 0, sent: 0 })
    render()
    fireEvent.click(await screen.findByRole('button', { name: 'Show report' }))
    expect(screen.getByRole('heading', { name: 'Atlas' })).toBeTruthy()
    expect(screen.queryByRole('tab')).toBeNull()
    expect(screen.queryByRole('link', { name: 'Download HTML' })).toBeNull()
  })

  it('describes targets', () => {
    expect(deliveryTarget(makeDelivery())).toBe('Work order')
    expect(deliveryTarget(makeDelivery({ kind: 'email', connection_name: 'Mail', recipients: ['a', 'b'] }))).toBe('Email: Mail (2 recipients)')
    expect(deliveryTarget(makeDelivery({ kind: 'km2', connection_name: 'KM2' }))).toBe('KM2: KM2')
  })
})
