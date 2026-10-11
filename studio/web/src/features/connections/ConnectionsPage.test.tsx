import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { connectionsApi } from '../../api/endpoints.crewstate'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { ConnectionsPage } from './ConnectionsPage'
import { KINDS, makeConnection } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')

const api = ({
  id: 'c-api', name: 'Ticket API', kind: 'http' as const, secret_keys: ['auth_value'], secret_updated_at: new Date(Date.now() - 3600_000).toISOString(),
  config: { base_url: 'https://api.example', auth_header: 'X-API-Key' }, crew_count: 2,
})
const jira = { id: 'c-jira', name: 'Acme Jira', kind: 'jira' as const, enabled: false, missing_secrets: ['api_token'], config: { site_url: 'https://acme.atlassian.net', email: 'bot@acme.example' } }

beforeEach(() => {
  setupApi()
  vi.mocked(connectionsApi.kinds).mockResolvedValue(KINDS)
  vi.mocked(connectionsApi.list).mockResolvedValue([makeConnection(api), makeConnection(jira)])
})
afterEach(cleanup)

const row = (name: string) => within(screen.getByText(name).closest('tr') as HTMLElement)
const field = (label: RegExp) => screen.getByLabelText(label) as HTMLInputElement

describe('ConnectionsPage', () => {
  it('lists connections by name with kind, status, secrets and crews, never secret values', async () => {
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Ticket API')
    const names = screen.getAllByRole('row').slice(1).map((r) => r.querySelector('td')?.firstChild?.textContent)
    expect(names).toEqual(['Acme Jira', 'Ticket API'])
    expect(row('Ticket API').getByText('HTTP (JSON API)')).toBeTruthy()
    expect(row('Ticket API').getByText(/Auth header value · set 1h ago/)).toBeTruthy()
    expect(row('Ticket API').getByText('2')).toBeTruthy()
    expect(row('Acme Jira').getByText('disabled')).toBeTruthy()
    expect(row('Acme Jira').getByText('missing API token')).toBeTruthy()
    expect(row('Acme Jira').getByText('none')).toBeTruthy()
  })

  it('creates a connection with its secret', async () => {
    vi.mocked(connectionsApi.create).mockResolvedValue(makeConnection({ id: 'new', name: 'Linear', kind: 'http' }))
    renderRoute(<ConnectionsPage />)
    fireEvent.click(await screen.findByRole('button', { name: /New connection/ }))
    const dialog = within(screen.getByRole('dialog'))
    fireEvent.change(dialog.getByLabelText('Kind'), { target: { value: 'http' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Create' }))
    expect(await dialog.findByText('Name is required')).toBeTruthy()
    expect(dialog.getByText('Required')).toBeTruthy()
    expect(connectionsApi.create).not.toHaveBeenCalled()

    fireEvent.change(field(/^Name/), { target: { value: 'Linear' } })
    fireEvent.click(await dialog.findByRole('option', { name: /Finance/ }))
    fireEvent.change(field(/^Base URL/), { target: { value: 'https://x.example/' } })
    fireEvent.change(field(/^Auth header name/), { target: { value: 'X-Key' } })
    const secret = field(/^Auth header value/)
    expect(secret.type).toBe('password')
    fireEvent.change(secret, { target: { value: 's3cret' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(connectionsApi.create).toHaveBeenCalledWith({
      name: 'Linear', kind: 'http', config: { base_url: 'https://x.example', auth_header: 'X-Key' }, area_ids: ['area-1'], enabled: true, secrets: { auth_value: 's3cret' },
    }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.getAllByText('Linear').length).toBeGreaterThan(0)
  })

  it('shows server problems on their fields', async () => {
    vi.mocked(connectionsApi.update).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['config.base_url: must be an http or https URL', 'name: taken somewhere'] }))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Ticket API')
    fireEvent.click(row('Ticket API').getByRole('button', { name: /Edit/ }))
    const dialog = within(screen.getByRole('dialog'))
    expect(dialog.getByText(/cannot be changed/)).toBeTruthy()
    expect(dialog.getByText(/removes the saved secrets/)).toBeTruthy()
    expect(dialog.queryByText('Secrets')).toBeNull()
    expect(field(/^Base URL/).value).toBe('https://api.example')
    fireEvent.click(dialog.getByRole('switch'))
    fireEvent.click(dialog.getByRole('button', { name: 'Save' }))
    expect(await dialog.findByText('must be an http or https URL')).toBeTruthy()
    expect(dialog.getByText('taken somewhere')).toBeTruthy()
    expect(vi.mocked(connectionsApi.update).mock.calls[0][1]).toMatchObject({ enabled: false })
    fireEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('replaces a secret without ever showing the old one', async () => {
    vi.mocked(connectionsApi.replaceSecret).mockResolvedValue(makeConnection({ ...jira, missing_secrets: [], secret_keys: ['api_token'] }))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Acme Jira')
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /Replace secret/ }))
    const dialog = within(screen.getByRole('dialog'))
    fireEvent.click(dialog.getByRole('button', { name: 'Replace' }))
    expect(await dialog.findByText('Enter at least one secret')).toBeTruthy()
    fireEvent.change(field(/^API token/), { target: { value: 'tok' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Replace' }))
    await waitFor(() => expect(connectionsApi.replaceSecret).toHaveBeenCalledWith('c-jira', { api_token: 'tok' }))
    await waitFor(() => expect(screen.queryByText('missing API token')).toBeNull())
  })

  it('reports a failed secret replacement in the dialog', async () => {
    vi.mocked(connectionsApi.replaceSecret).mockRejectedValue(new ApiError(409, { code: 'secrets_key_missing', message: 'Connections are turned off' }))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Acme Jira')
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /Replace secret/ }))
    fireEvent.change(field(/^API token/), { target: { value: 'tok' } })
    fireEvent.click(screen.getByRole('button', { name: 'Replace' }))
    expect(await screen.findByText('Connections are turned off')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  })

  it('tests a connection and shows the outcome', async () => {
    vi.mocked(connectionsApi.test).mockResolvedValueOnce({ supported: true, ok: true, message: 'Connected: the server answered HTTP 200.', status_code: 200, duration_ms: 12 })
    vi.mocked(connectionsApi.test).mockRejectedValueOnce(new Error('offline'))
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Ticket API')
    fireEvent.click(row('Ticket API').getByRole('button', { name: /Test/ }))
    expect(await screen.findByText('Connected: the server answered HTTP 200.')).toBeTruthy()
    fireEvent.click(row('Acme Jira').getByRole('button', { name: /Test/ }))
    await waitFor(() => expect(connectionsApi.test).toHaveBeenCalledTimes(2))
  })

  it('treats a KM2 issues connection as a tracker: people map, and the organisation check from Test', async () => {
    const km2 = makeConnection({ id: 'c-km2t', name: 'PMO Sandbox', kind: 'km2_tracker', config: { base_url: 'https://km2.example.com', self_email: 'bot@example.com' } })
    vi.mocked(connectionsApi.list).mockResolvedValue([km2])
    const message = 'Connected as Crew Studio, but the KM2 organisation is not ready: trk_issue lacks the fields labels.'
    vi.mocked(connectionsApi.test).mockResolvedValueOnce({ supported: true, ok: false, message, duration_ms: 30 })
    renderRoute(<ConnectionsPage />)
    await screen.findByText('PMO Sandbox')
    expect(row('PMO Sandbox').getByRole('button', { name: /People map/ })).toBeTruthy()
    fireEvent.click(row('PMO Sandbox').getByRole('button', { name: /Test/ }))
    expect(await screen.findByText(message)).toBeTruthy()
  })

  it('deletes after confirming, and reports failures', async () => {
    vi.mocked(connectionsApi.remove).mockRejectedValueOnce(new Error('in use')).mockResolvedValueOnce(undefined)
    renderRoute(<ConnectionsPage />)
    await screen.findByText('Ticket API')
    const del = () => row('Ticket API').getByRole('button', { name: /Delete|Confirm/ })
    fireEvent.click(del())
    fireEvent.click(del())
    await waitFor(() => expect(connectionsApi.remove).toHaveBeenCalledTimes(1))
    fireEvent.click(del())
    fireEvent.click(del())
    await waitFor(() => expect(screen.queryByText('Ticket API')).toBeNull())
  })

  it('is read-only without connections.manage', async () => {
    renderRoute(<ConnectionsPage />, { perms: without('connections.manage') })
    await screen.findByText('Ticket API')
    expect(screen.queryByRole('button', { name: /New connection|Edit|Test|Delete/ })).toBeNull()
  })

  it('explains when the server has no secrets key, and shows the empty state', async () => {
    vi.mocked(connectionsApi.kinds).mockResolvedValue({ ...KINDS, secrets_ready: false })
    vi.mocked(connectionsApi.list).mockResolvedValue([])
    renderRoute(<ConnectionsPage />)
    expect(await screen.findByText('No connections yet')).toBeTruthy()
    expect(screen.getByText('STUDIO_SECRETS_KEY')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /New connection/ })).toBeNull()
  })
})
