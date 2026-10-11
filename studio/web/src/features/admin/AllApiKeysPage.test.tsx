import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { AllApiKeysPage, PAGE_SIZE } from './AllApiKeysPage'
import { api, apiError, keyPage, makeKey, makeUser, notifyMock, renderRoute, resetAll } from './testUtils'
import type { AccountApiKey, Permission } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const KEYS: Partial<AccountApiKey>[] = [
  { id: 'k-erp', name: 'prod', prefix: 'erp11111', user_id: 's-1', created_by_name: 'Ada', created_at: '2026-03-01T00:00:00Z' },
  { id: 'k-pat', name: 'Laptop', prefix: 'pat22222', user_id: 'u-2', user_name: 'Pat Lee', user_kind: 'human', created_at: '2026-02-01T00:00:00Z' },
  { id: 'k-old', name: 'old', prefix: 'old33333', user_id: 's-1', revoked_at: '2026-01-05T00:00:00Z', created_at: '2026-01-01T00:00:00Z' },
]
const ALL: Permission[] = ['admin.users', 'admin.service_accounts', 'admin.audit']

function open(perms: Permission[] = ALL, keys: Partial<AccountApiKey>[] = KEYS) {
  resetAll(perms)
  api.adminApi.apiKeys.mockResolvedValue(keyPage(keys, { limit: PAGE_SIZE }))
  renderRoute(<AllApiKeysPage />)
  return screen.findByRole('heading', { name: 'API keys' })
}

const rows = () => screen.getAllByRole('row').slice(1)
const names = () => rows().map((r) => within(r).getAllByRole('cell')[0].textContent)
const lastQuery = () => api.adminApi.apiKeys.mock.calls.at(-1)?.[0]

beforeEach(() => {
  notifyMock.notifyError.mockReset()
})

describe('listing every key', () => {
  it('shows each key with its account, kind and creator, and links to the account', async () => {
    await open()
    expect(await screen.findByText('csk_erp11111_…')).toBeTruthy()
    expect(lastQuery()).toEqual({ limit: PAGE_SIZE, offset: 0, q: undefined, kind: undefined, status: undefined })
    expect(names()).toEqual(['prod', 'Laptop', 'old'])
    const [erp, pat] = rows()
    expect(within(erp).getByRole('link', { name: 'ERP' }).getAttribute('href')).toBe('/admin/service-accounts/s-1')
    expect(within(erp).getByText('Service account')).toBeTruthy()
    expect(within(erp).getByText('by Ada')).toBeTruthy()
    expect(within(pat).getByRole('link', { name: 'Pat Lee' }).getAttribute('href')).toBe('/admin/users/u-2')
    expect(within(pat).getByText('Person')).toBeTruthy()
    expect(within(rows()[2]).queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(screen.getByRole('link', { name: /history/i }).getAttribute('href')).toContain('entity_type=api_key')
  })

  it('sends the filters to the server (search debounced) and starts again from the first page', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    fireEvent.change(screen.getByLabelText('Filter by account'), { target: { value: 'service' } })
    await waitFor(() => expect(lastQuery()).toMatchObject({ kind: 'service', offset: 0 }))
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'revoked' } })
    await waitFor(() => expect(lastQuery()).toMatchObject({ kind: 'service', status: 'revoked' }))
    const calls = api.adminApi.apiKeys.mock.calls.length
    fireEvent.change(screen.getByLabelText('Search keys'), { target: { value: ' pa' } })
    fireEvent.change(screen.getByLabelText('Search keys'), { target: { value: ' pat ' } })
    await waitFor(() => expect(lastQuery()).toMatchObject({ q: 'pat', kind: 'service', status: 'revoked', offset: 0 }))
    // Only the settled value reaches the server
    expect(api.adminApi.apiKeys.mock.calls.length).toBe(calls + 1)
  })

  it('says when no key matches the filters, keeping the filters on screen', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    api.adminApi.apiKeys.mockResolvedValue(keyPage([]))
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'expired' } })
    expect(await screen.findByText('No keys match these filters.')).toBeTruthy()
    expect(screen.queryByText('No API keys yet')).toBeNull()
    expect(screen.getByLabelText('Filter by status')).toBeTruthy()
  })

  it('pages through keys', async () => {
    await open(ALL, KEYS)
    api.adminApi.apiKeys.mockResolvedValue(keyPage(KEYS, { total: 120, limit: PAGE_SIZE }))
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'active' } })
    await waitFor(() => expect(screen.getByRole('button', { name: /next/i })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ offset: PAGE_SIZE, status: 'active' }))
  })

  it('offers a way back from a page that has emptied since it was chosen', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    api.adminApi.apiKeys.mockResolvedValue(keyPage([], { total: 3, limit: PAGE_SIZE, offset: PAGE_SIZE }))
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'active' } })
    expect(await screen.findByText('No keys on this page.')).toBeTruthy()
    expect(screen.queryByText('No API keys yet')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /previous/i }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ offset: 0 }))
  })

  it('hides the account filter and issuing from someone who manages only people', async () => {
    await open(['admin.users'], [KEYS[1]])
    await screen.findByText('csk_pat22222_…')
    expect(screen.queryByLabelText('Filter by account')).toBeNull()
    expect(screen.queryByRole('button', { name: 'New service account key' })).toBeNull()
    expect(screen.queryByRole('link', { name: /history/i })).toBeNull()
  })

  it('explains where keys come from when there are none', async () => {
    await open(ALL, [])
    expect(await screen.findByText('No API keys yet')).toBeTruthy()
  })

  it('shows a load error', async () => {
    resetAll(ALL)
    api.adminApi.apiKeys.mockRejectedValue(apiError(403, 'not for you'))
    renderRoute(<AllApiKeysPage />)
    expect(await screen.findByText('not for you')).toBeTruthy()
  })
})

describe('revoking', () => {
  it('revokes through the owning account and marks the row revoked', async () => {
    await open()
    api.adminApi.users.revokeApiKey.mockResolvedValue(undefined)
    await screen.findByText('csk_pat22222_…')
    fireEvent.click(within(rows()[1]).getByRole('button', { name: 'Revoke' }))
    fireEvent.click(within(rows()[1]).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.adminApi.users.revokeApiKey).toHaveBeenCalledWith('u-2', 'k-pat'))
    // Revoked keys sort after active ones, newest first
    await waitFor(() => expect(within(rows()[1]).getByText('revoked')).toBeTruthy())
    expect(names()).toEqual(['prod', 'Laptop', 'old'])
    expect(within(rows()[1]).queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Revoked Laptop')
  })

  it('reloads after revoking while showing only active keys', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'active' } })
    await waitFor(() => expect(lastQuery()).toMatchObject({ status: 'active' }))
    await screen.findByText('csk_erp11111_…')
    api.adminApi.users.revokeApiKey.mockResolvedValue(undefined)
    api.adminApi.apiKeys.mockResolvedValue(keyPage([KEYS[1]]))
    const loads = api.adminApi.apiKeys.mock.calls.length
    fireEvent.click(within(rows()[0]).getByRole('button', { name: 'Revoke' }))
    fireEvent.click(within(rows()[0]).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(names()).toEqual(['Laptop']))
    expect(api.adminApi.apiKeys.mock.calls.length).toBe(loads + 1)
  })

  it('offers no Revoke where the server says you cannot, and explains why', async () => {
    await open(ALL, [{ ...KEYS[1], can_revoke: false }])
    await screen.findByText('csk_pat22222_…')
    expect(within(rows()[0]).queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(within(rows()[0]).getByText('Cannot revoke').getAttribute('title')).toMatch(/every permission/)
  })

  it('reports a refused revoke', async () => {
    await open()
    api.adminApi.users.revokeApiKey.mockRejectedValue(apiError(403, 'they outrank you'))
    await screen.findByText('csk_erp11111_…')
    fireEvent.click(within(rows()[0]).getByRole('button', { name: 'Revoke' }))
    fireEvent.click(within(rows()[0]).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not revoke prod', expect.anything()))
  })
})

describe('issuing a service account key', () => {
  it('picks an active service account, shows the secret once and reloads the first page', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    api.adminApi.users.list.mockResolvedValue([
      makeUser({ id: 's-1', kind: 'service', name: 'ERP' }),
      makeUser({ id: 's-2', kind: 'service', name: 'Retired', status: 'disabled' }),
    ])
    api.adminApi.users.createApiKey.mockResolvedValue({ key: makeKey({ id: 'k-new', name: 'nightly', prefix: 'new44444', user_id: 's-1' }), secret: 'csk_new44444_SECRET' })
    fireEvent.click(screen.getByRole('button', { name: 'New service account key' }))
    const dialog = await screen.findByRole('dialog')
    expect(api.adminApi.users.list).toHaveBeenCalledWith('service')
    expect(within(dialog).queryByRole('option', { name: 'Retired' })).toBeNull()

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'nightly' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    expect(within(dialog).getByText(/Choose the service account/)).toBeTruthy()
    expect(api.adminApi.users.createApiKey).not.toHaveBeenCalled()

    api.adminApi.apiKeys.mockResolvedValue(keyPage([{ id: 'k-new', name: 'nightly', prefix: 'new44444', created_by_name: 'Me Myself', created_at: '2026-04-01T00:00:00Z' }, ...KEYS]))
    const loads = api.adminApi.apiKeys.mock.calls.length
    fireEvent.change(within(dialog).getByLabelText('Service account'), { target: { value: 's-1' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    await waitFor(() => expect(api.adminApi.users.createApiKey).toHaveBeenCalledWith('s-1', { name: 'nightly', expires_at: null }))
    const secret = await screen.findByRole('dialog')
    expect(within(secret).getByText('csk_new44444_SECRET')).toBeTruthy()
    fireEvent.click(within(secret).getByRole('button', { name: 'I have saved it' }))
    expect(screen.queryByText('csk_new44444_SECRET')).toBeNull()
    await waitFor(() => expect(names()[0]).toBe('nightly'))
    expect(api.adminApi.apiKeys.mock.calls.length).toBe(loads + 1)
    expect(lastQuery()).toMatchObject({ offset: 0 })
    expect(within(rows()[0]).getByText('by Me Myself')).toBeTruthy()
  })

  it('goes back to the first page after creating a key from a later page', async () => {
    await open()
    api.adminApi.apiKeys.mockResolvedValue(keyPage(KEYS, { total: 120, limit: PAGE_SIZE }))
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'active' } })
    await waitFor(() => expect(screen.getByRole('button', { name: /next/i })).toBeTruthy())
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ offset: PAGE_SIZE }))
    await screen.findByText('csk_erp11111_…')
    api.adminApi.users.list.mockResolvedValue([makeUser({ id: 's-1', kind: 'service', name: 'ERP' })])
    api.adminApi.users.createApiKey.mockResolvedValue({ key: makeKey({ id: 'k-new', user_id: 's-1' }), secret: 's' })
    fireEvent.click(screen.getByRole('button', { name: 'New service account key' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Service account'), { target: { value: 's-1' } })
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'n' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ offset: 0 }))
  })

  it('says so when there are no active service accounts, and closes with Cancel', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    api.adminApi.users.list.mockResolvedValue([])
    fireEvent.click(screen.getByRole('button', { name: 'New service account key' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('option', { name: 'No active service accounts' })).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('reports when service accounts cannot be loaded', async () => {
    await open()
    await screen.findByText('csk_erp11111_…')
    api.adminApi.users.list.mockRejectedValue(apiError(500, 'down'))
    fireEvent.click(screen.getByRole('button', { name: 'New service account key' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not load service accounts', expect.anything()))
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
