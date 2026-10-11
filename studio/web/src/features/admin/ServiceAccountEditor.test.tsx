import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { ServiceAccountEditorPage } from './ServiceAccountEditorPage'
import { api, apiError, keyPage, makeArea, makeKey, makeRole, makeUser, notifyMock, renderRoute, resetAll } from './testUtils'
import { invalidateAreas } from '../../lib/useAreas'
import type { UserInput } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const svc = makeUser({ id: 's-1', kind: 'service', name: 'ERP bot', email: null })
const type = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
const loc = () => screen.getByTestId('location').textContent

function renderSvc(id: string) {
  return renderRoute(<ServiceAccountEditorPage />, { path: '/admin/service-accounts/:id', url: `/admin/service-accounts/${id}`, extra: ['/admin/service-accounts'] })
}

let writeText: ReturnType<typeof vi.fn>

beforeEach(() => {
  invalidateAreas()
  resetAll(['admin.service_accounts', 'admin.roles'])
  api.adminApi.roles.list.mockResolvedValue([makeRole({ id: 'r-1', name: 'Caller' })])
  api.areasApi.list.mockResolvedValue([makeArea({ id: 'a-1', name: 'Finance' })])
  api.adminApi.users.get.mockResolvedValue(svc)
  api.adminApi.users.apiKeys.mockResolvedValue(keyPage([]))
  writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
})

afterEach(() => vi.useRealTimers())

describe('creating a service account', () => {
  it('has no email, explains keys come after creation, and creates with kind=service', async () => {
    api.adminApi.users.create.mockResolvedValue(makeUser({ id: 's-9', kind: 'service' }))
    renderSvc('new')
    await screen.findByText('New service account')
    expect(screen.queryByLabelText('Email')).toBeNull()
    expect(screen.getByText(/Create the account first, then add API keys/)).toBeTruthy()
    expect(screen.queryByText('Callback signing secret')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(screen.queryByText('Enter the email address they sign in with')).toBeNull()
    type('Name', ' Billing ')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(loc()).toBe('/admin/service-accounts/s-9'))
    expect((api.adminApi.users.create.mock.calls[0][0] as UserInput)).toMatchObject({ kind: 'service', name: 'Billing', email: '' })
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Created service account')
  })

  it('surfaces a 422 from the server', async () => {
    api.adminApi.users.create.mockRejectedValue(apiError(422, 'bad', 'validation', ['name: already taken']))
    renderSvc('new')
    await screen.findByText('New service account')
    type('Name', 'Billing')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('already taken')).toBeTruthy()
  })
})

describe('API keys', () => {
  async function open(keys = [makeKey()]) {
    api.adminApi.users.apiKeys.mockResolvedValue(keyPage(keys))
    renderSvc('s-1')
    await screen.findByRole('heading', { name: 'ERP bot' })
  }

  it('lists keys by masked prefix only (never a secret) with status chips', async () => {
    await open([
      makeKey({ id: 'k-old', name: 'Old', prefix: 'old111', revoked_at: '2026-02-01T00:00:00Z', created_at: '2026-01-02T00:00:00Z' }),
      makeKey({ id: 'k-exp', name: 'Expired', prefix: 'exp222', expires_at: '2020-01-01T00:00:00Z', created_at: '2026-01-03T00:00:00Z' }),
      makeKey({ id: 'k-act', name: 'Active', prefix: 'act333', created_at: '2026-01-01T00:00:00Z', last_used_at: new Date().toISOString() }),
    ])
    expect(await screen.findByText('csk_act333_…')).toBeTruthy()
    expect(screen.getByText('csk_old111_…')).toBeTruthy()
    const rows = screen.getAllByRole('row').slice(1)
    // Active first, then newest
    expect(rows.map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['Active', 'Expired', 'Old'])
    expect(within(rows[0]).getByText('active')).toBeTruthy()
    expect(within(rows[1]).getByText('expired')).toBeTruthy()
    expect(within(rows[2]).getByText('revoked')).toBeTruthy()
    // Revoked keys cannot be revoked again
    expect(within(rows[2]).queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(within(rows[0]).getByRole('button', { name: 'Revoke' })).toBeTruthy()
  })

  it('shows an empty message', async () => {
    await open([])
    expect(await screen.findByText(/No API keys yet/)).toBeTruthy()
  })

  it('shows a load error for keys', async () => {
    api.adminApi.users.apiKeys.mockRejectedValue(apiError(403, 'no keys for you'))
    renderSvc('s-1')
    expect(await screen.findByText('no keys for you')).toBeTruthy()
  })

  it('validates the new key form (name, past expiry) and sends nothing', async () => {
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    const dialog = screen.getByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    expect(within(dialog).getByText(/Give the key a name/)).toBeTruthy()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'CI' } })
    fireEvent.change(within(dialog).getByLabelText('Expires (optional)'), { target: { value: '2000-01-01' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    expect(within(dialog).getByText('Pick a date in the future')).toBeTruthy()
    expect(api.adminApi.users.createApiKey).not.toHaveBeenCalled()
  })

  it('creates a key, shows the secret once in a modal, lets you copy it, and never shows it again', async () => {
    const created = makeKey({ id: 'k-new', name: 'CI', prefix: 'zzz999' })
    api.adminApi.users.createApiKey.mockResolvedValue({ key: created, secret: 'csk_zzz999_SECRETVALUE' })
    await open([])
    await screen.findByText(/No API keys yet/)
    // After creating, the panel reloads its first page from the server
    api.adminApi.users.apiKeys.mockResolvedValue(keyPage([created]))
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    fireEvent.change(within(screen.getByRole('dialog')).getByLabelText('Name'), { target: { value: '  CI  ' } })
    fireEvent.change(screen.getByLabelText('Expires (optional)'), { target: { value: '2999-12-31' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))
    await screen.findByText('csk_zzz999_SECRETVALUE')
    const secretDialog = screen.getByRole('dialog')
    expect(within(secretDialog).getByText('csk_zzz999_SECRETVALUE')).toBeTruthy()
    expect(within(secretDialog).getByText(/cannot be shown again/)).toBeTruthy()
    const [, body] = api.adminApi.users.createApiKey.mock.calls[0]
    expect(api.adminApi.users.createApiKey.mock.calls[0][0]).toBe('s-1')
    expect(body.name).toBe('CI')
    expect(body.expires_at).toMatch(/^2999-12-31T|^3000-01-01T/)

    fireEvent.click(within(secretDialog).getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('csk_zzz999_SECRETVALUE'))
    expect(await within(secretDialog).findByText('Copied')).toBeTruthy()

    fireEvent.click(within(secretDialog).getByRole('button', { name: 'I have saved it' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.queryByText('csk_zzz999_SECRETVALUE')).toBeNull()
    // The list only carries the masked prefix
    expect(await screen.findByText('csk_zzz999_…')).toBeTruthy()
    expect(document.body.textContent).not.toContain('SECRETVALUE')
  })

  it('creates a non-expiring key when no date is given', async () => {
    api.adminApi.users.createApiKey.mockResolvedValue({ key: makeKey({ id: 'k2' }), secret: 's' })
    await open([])
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    fireEvent.change(within(screen.getByRole('dialog')).getByLabelText('Name'), { target: { value: 'CI' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))
    await screen.findByText(/created$/)
    expect(api.adminApi.users.createApiKey.mock.calls[0][1].expires_at).toBeNull()
  })

  it('reports a create failure and keeps the dialog open for another try', async () => {
    api.adminApi.users.createApiKey.mockRejectedValue(apiError(403, 'forbidden'))
    await open([])
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    fireEvent.change(within(screen.getByRole('dialog')).getByLabelText('Name'), { target: { value: 'CI' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not create the key', expect.anything()))
    expect(screen.getByRole('dialog')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Create key' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('closes the new key dialog with Cancel and with Escape', async () => {
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('reports a failed copy', async () => {
    writeText.mockRejectedValue(new Error('denied'))
    api.adminApi.users.createApiKey.mockResolvedValue({ key: makeKey({ id: 'k2' }), secret: 'sec' })
    await open([])
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    fireEvent.change(within(screen.getByRole('dialog')).getByLabelText('Name'), { target: { value: 'CI' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create key' }))
    await screen.findByText('sec')
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Copy failed', expect.any(Error)))
  })

  it('revokes a key after confirmation and marks it revoked locally', async () => {
    api.adminApi.users.revokeApiKey.mockResolvedValue(undefined)
    await open([makeKey({ id: 'k-1', name: 'ERP' })])
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke' }))
    expect(api.adminApi.users.revokeApiKey).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.adminApi.users.revokeApiKey).toHaveBeenCalledWith('s-1', 'k-1'))
    expect(await screen.findByText('revoked')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Revoked ERP')
  })

  it('leaves the key active when revoking fails', async () => {
    api.adminApi.users.revokeApiKey.mockRejectedValue(apiError(500, 'oops'))
    await open([makeKey()])
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not revoke ERP', expect.anything()))
    expect(screen.getByText('active')).toBeTruthy()
  })

  it('disarms the revoke confirmation after a timeout', async () => {
    await open([makeKey()])
    const revoke = await screen.findByRole('button', { name: 'Revoke' })
    vi.useFakeTimers()
    fireEvent.click(revoke)
    expect(screen.getByRole('button', { name: 'Confirm?' })).toBeTruthy()
    await act(async () => { await vi.advanceTimersByTimeAsync(3100) })
    expect(screen.queryByRole('button', { name: 'Confirm?' })).toBeNull()
  })
})

describe('signing secret', () => {
  async function open() {
    renderSvc('s-1')
    await screen.findByRole('heading', { name: 'ERP bot' })
  }

  it('is masked until revealed on request, and can be hidden again', async () => {
    api.adminApi.users.signingSecret.mockResolvedValue({ signing_secret: 'whsec_abc' })
    await open()
    expect(screen.queryByText('whsec_abc')).toBeNull()
    expect(api.adminApi.users.signingSecret).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Reveal' }))
    expect(await screen.findByText('whsec_abc')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('whsec_abc'))
    fireEvent.click(screen.getByRole('button', { name: 'Hide' }))
    expect(screen.queryByText('whsec_abc')).toBeNull()
  })

  it('reports a failed reveal', async () => {
    api.adminApi.users.signingSecret.mockRejectedValue(apiError(403, 'forbidden'))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Reveal' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not load the signing secret', expect.anything()))
    expect(screen.getByRole('button', { name: 'Reveal' })).toBeTruthy()
  })

  it('rotates only after confirmation and shows the new secret', async () => {
    api.adminApi.users.rotateSigningSecret.mockResolvedValue({ signing_secret: 'whsec_new' })
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }))
    expect(api.adminApi.users.rotateSigningSecret).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    expect(await screen.findByText('whsec_new')).toBeTruthy()
    expect(api.adminApi.users.rotateSigningSecret).toHaveBeenCalledWith('s-1')
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith(expect.stringContaining('Signing secret rotated'))
  })

  it('reports a failed rotation without revealing anything', async () => {
    api.adminApi.users.rotateSigningSecret.mockRejectedValue(apiError(500, 'nope'))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Rotate' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not rotate the signing secret', expect.anything()))
    expect(screen.queryByRole('button', { name: 'Hide' })).toBeNull()
  })
})
