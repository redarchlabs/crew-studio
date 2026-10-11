import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { pmoApi } from '../../api/endpoints.delivery'
import type { PMOOptions } from '../../api/types.delivery'
import { renderRoute, setupApi } from '../crews/testUtils'
import { PmoSetupPage } from './PmoSetupPage'
import { connectionsApi } from '../../api/endpoints.crewstate'
import { clearFolderCache } from '../connections/km2Folders'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.delivery')
vi.mock('../../api/endpoints.crewstate')

const OPTIONS: PMOOptions = {
  trackers: [{ id: 't1', name: 'Acme Jira', kind: 'jira', area_ids: ['area-1'] }, { id: 't2', name: 'Ops Linear', kind: 'linear', area_ids: ['area-2'] },
    { id: 't3', name: 'PMO Sandbox', kind: 'km2_tracker', area_ids: ['area-1'] }],
  destinations: [{ id: 'c-slack', name: 'PMO Slack', kind: 'slack', area_ids: ['area-1'] }],
  llms: [{ id: 'l1', name: 'Claude Haiku 4.5', kind: 'anthropic/claude-haiku-4-5' }, { id: 'l2', name: 'Claude Sonnet 5.5', kind: 'anthropic/claude-sonnet-5-5' }],
  coordinator_llm_id: 'l1', reporter_llm_id: 'l2', sweep_cron: '0 7 * * 1-5', report_cron: '0 8 * * 1',
}

const render = () => renderRoute(<PmoSetupPage />, { path: '/pmo/setup', me: { all_areas: false, area_ids: ['area-1'] } })
const next = () => fireEvent.click(screen.getByRole('button', { name: 'Next' }))

beforeEach(() => {
  setupApi()
  clearFolderCache()
  vi.mocked(pmoApi.options).mockResolvedValue(OPTIONS)
})
afterEach(cleanup)

describe('PmoSetupPage', () => {
  it('walks through the wizard and sets up the PMO', async () => {
    vi.mocked(pmoApi.install).mockResolvedValue({ crew: { id: 'crew-9', name: 'Atlas PMO' }, agents: [], tasks: [], schedules: [] })
    render()
    const name = await screen.findByLabelText('Name')
    fireEvent.change(name, { target: { value: '' } })
    next()
    expect(await screen.findByText('Give the PMO a name')).toBeTruthy()
    fireEvent.change(name, { target: { value: 'Atlas PMO' } })
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'UTC' } })
    next()
    // Only the tracker in the chosen area is offered
    const tracker = await screen.findByLabelText('Tracker connection')
    expect(screen.queryByRole('option', { name: 'Ops Linear (Linear)' })).toBeNull()
    expect(screen.getByRole('option', { name: 'Acme Jira (Jira)' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'PMO Sandbox (KM2)' })).toBeTruthy()
    next()
    expect(await screen.findByText('Choose the tracker connection to follow')).toBeTruthy()
    fireEvent.change(tracker, { target: { value: 't1' } })
    next()
    fireEvent.click(await screen.findByRole('button', { name: 'Add destination' }))
    next()
    await screen.findByLabelText('PMO Coordinator model')
    expect(screen.queryByText('Choose a connection')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(screen.getAllByTestId('destination-row')).toHaveLength(1)
    next()
    expect((await screen.findByLabelText('PMO Coordinator model') as HTMLSelectElement).value).toBe('l1')
    expect(screen.getByTestId('pmo-summary').textContent).toContain('Weekly report on Mondays at 08:00 to the work order, Slack')
    fireEvent.click(screen.getByRole('switch', { name: /Turn the schedules on/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Set up the PMO' }))
    await waitFor(() => expect(pmoApi.install).toHaveBeenCalledWith({
      name: 'Atlas PMO', area_ids: ['area-1'], timezone: 'UTC', tracker_connection_id: 't1', coordinator_llm_id: 'l1', reporter_llm_id: 'l2',
      destinations: [{ kind: 'slack', connection_id: 'c-slack', recipients: [], folder: '' }], enabled: false,
    }))
    expect((await screen.findByTestId('where')).textContent).toBe('/crews/crew-9')
  })

  it('shows what the server refused and checks the models', async () => {
    vi.mocked(pmoApi.install).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'A crew named "PMO" already exists.' }))
    render()
    await screen.findByLabelText('Name')
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'UTC' } })
    next()
    fireEvent.change(await screen.findByLabelText('Tracker connection'), { target: { value: 't1' } })
    next()
    next()
    fireEvent.change(await screen.findByLabelText('Status Reporter model'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Set up the PMO' }))
    expect(await screen.findByText('Choose an LLM')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Status Reporter model'), { target: { value: 'l2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Set up the PMO' }))
    expect(await screen.findByText('A crew named "PMO" already exists.')).toBeTruthy()
    vi.mocked(pmoApi.install).mockRejectedValue(new Error('offline'))
    fireEvent.click(screen.getByRole('button', { name: 'Set up the PMO' }))
    expect(await screen.findByText('offline')).toBeTruthy()
  })

  it('says when there is no tracker to follow or destination to send to', async () => {
    vi.mocked(pmoApi.options).mockResolvedValue({ ...OPTIONS, trackers: [], destinations: [] })
    render()
    await screen.findByLabelText('Name')
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'UTC' } })
    next()
    expect(await screen.findByText('No tracker connection')).toBeTruthy()
    vi.mocked(pmoApi.options).mockResolvedValue({ ...OPTIONS, destinations: [] })
    cleanup()
    render()
    await screen.findByLabelText('Name')
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'UTC' } })
    next()
    fireEvent.change(await screen.findByLabelText('Tracker connection'), { target: { value: 't1' } })
    next()
    expect(await screen.findByText(/No Slack, Teams, email or KM2 connection in these areas yet/)).toBeTruthy()
  })

  it('sends a taken name back to the Name field, warns about missing provider keys, and stops a KM2 destination that cannot file', async () => {
    vi.mocked(pmoApi.options).mockResolvedValue({
      ...OPTIONS, destinations: [...OPTIONS.destinations, { id: 'c-km2', name: 'KM2 Wiki', kind: 'km2', area_ids: ['area-1'] }],
      llms: [{ id: 'l1', name: 'Claude Haiku 4.5', kind: 'anthropic/claude-haiku-4-5', warning: 'ANTHROPIC_API_KEY is not set in the provider env file.' },
        { id: 'l2', name: 'GPT', kind: 'openai/gpt-6-luna' }],
    })
    vi.mocked(connectionsApi.km2Folders).mockResolvedValue({ ok: true, message: '', folders: [], folder_count: 0, warnings: [],
      allowed: { default_folder_id: '', write_folder_ids: [] } })
    vi.mocked(pmoApi.install).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Please fix the highlighted fields.',
      details: ['name: an agent named "PMO Coordinator" already exists; choose another name'] }))
    render()
    const name = await screen.findByLabelText('Name') as HTMLInputElement
    expect(name.value).toBe('Team PMO')
    fireEvent.change(screen.getByLabelText('Time zone'), { target: { value: 'UTC' } })
    next()
    fireEvent.change(await screen.findByLabelText('Tracker connection'), { target: { value: 't1' } })
    next()
    fireEvent.click(await screen.findByRole('button', { name: 'Add destination' }))
    const row = within(screen.getByTestId('destination-row'))
    fireEvent.change(row.getByLabelText('Destination'), { target: { value: 'km2' } })
    expect(await row.findByRole('option', { name: 'No default folder: saving is off' })).toBeTruthy()
    next()
    expect(await screen.findByText(/KM2 Wiki has no default folder, so reports cannot be filed/)).toBeTruthy()
    fireEvent.click(row.getByRole('button', { name: /Remove/ }))
    next()
    expect((await screen.findByTestId('llm-warnings')).textContent).toContain('Claude Haiku 4.5: ANTHROPIC_API_KEY is not set')
    fireEvent.click(screen.getByRole('button', { name: 'Set up the PMO' }))
    expect(await screen.findByText('Name: an agent named "PMO Coordinator" already exists; choose another name')).toBeTruthy()
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Acme Jira PMO')
  })
})
