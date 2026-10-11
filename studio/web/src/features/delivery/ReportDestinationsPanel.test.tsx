import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { destinationsApi } from '../../api/endpoints.delivery'
import { makeSchedule, renderRoute, setupApi } from '../crews/testUtils'
import { ReportDestinationsPanel } from './ReportDestinationsPanel'
import { clearFolderCache } from '../connections/km2Folders'
import { makeDestination, makeDestinationsView } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.delivery')
vi.mock('../../api/endpoints.crewstate')

const render = (canManage = true) => renderRoute(<ReportDestinationsPanel schedule={makeSchedule()} canManage={canManage} />)

beforeEach(() => {
  setupApi()
  clearFolderCache()
  vi.mocked(destinationsApi.get).mockResolvedValue(makeDestinationsView())
})
afterEach(cleanup)

describe('ReportDestinationsPanel', () => {
  it('adds an email destination and saves the list', async () => {
    vi.mocked(destinationsApi.put).mockResolvedValue(makeDestinationsView({
      destinations: [makeDestination(), makeDestination({ id: 'd2', kind: 'email', connection_id: 'c-mail', connection_name: 'PMO Mail', recipients: ['sam@example.com'] })],
    }))
    render()
    expect(await screen.findAllByTestId('destination-row')).toHaveLength(1)
    fireEvent.click(screen.getByRole('button', { name: 'Add destination' }))
    const rows = screen.getAllByTestId('destination-row')
    const second = within(rows[1])
    fireEvent.change(second.getByLabelText('Destination'), { target: { value: 'email' } })
    expect((second.getByLabelText('Connection') as HTMLSelectElement).value).toBe('c-mail')
    fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }))
    expect(await screen.findByText('List at least one email address')).toBeTruthy()
    expect(destinationsApi.put).not.toHaveBeenCalled()
    fireEvent.change(second.getByLabelText('Recipients'), { target: { value: 'sam@example.com' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }))
    await waitFor(() => expect(destinationsApi.put).toHaveBeenCalledWith('s1', [
      { kind: 'slack', connection_id: 'c-slack', recipients: [], folder: '' },
      { kind: 'email', connection_id: 'c-mail', recipients: ['sam@example.com'], folder: '' },
    ]))
    await waitFor(() => expect((within(screen.getAllByTestId('destination-row')[1]).getByLabelText('Recipients') as HTMLTextAreaElement).value).toBe('sam@example.com'))
  })

  it('removes a row, sets a KM2 folder, and shows server problems', async () => {
    vi.mocked(destinationsApi.put).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Please fix the highlighted fields.', details: ['destinations[0].connection_id: the crew may not use KM2'] }))
    render()
    await screen.findAllByTestId('destination-row')
    fireEvent.click(screen.getByRole('button', { name: 'Remove destination 1' }))
    expect(screen.getByText(/Only the work order gets the report/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add destination' }))
    const row = within(screen.getByTestId('destination-row'))
    fireEvent.change(row.getByLabelText('Destination'), { target: { value: 'km2' } })
    // The folders cannot be listed here, so the folder is typed
    fireEvent.change(await row.findByLabelText('Folder (optional)'), { target: { value: 'AAAAAAAA-0000-4000-8000-000000000001' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }))
    expect(await screen.findByText('destinations[0].connection_id: the crew may not use KM2')).toBeTruthy()
    fireEvent.change(row.getByLabelText('Destination'), { target: { value: 'teams' } })
    expect(row.getByRole('option', { name: 'No Microsoft Teams connection the crew may use' })).toBeTruthy()
    vi.mocked(destinationsApi.put).mockRejectedValue(new Error('offline'))
    fireEvent.change(row.getByLabelText('Destination'), { target: { value: 'slack' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }))
    expect(await screen.findByText('offline')).toBeTruthy()
  })

  it('says when no connection can be used', async () => {
    vi.mocked(destinationsApi.get).mockResolvedValue(makeDestinationsView({ destinations: [], connections: [] }))
    render()
    expect(await screen.findByText(/No Slack, Teams, email or KM2 connection is available/)).toBeTruthy()
  })

  it('lists destinations read-only for people who cannot manage the schedule', async () => {
    render(false)
    expect(await screen.findByText('Slack: PMO Slack')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Save destinations' })).toBeNull()
    cleanup()
    vi.mocked(destinationsApi.get).mockResolvedValue(makeDestinationsView({ destinations: [] }))
    render(false)
    expect(await screen.findByText('Reports go only to the work order.')).toBeTruthy()
  })
})
