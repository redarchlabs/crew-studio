import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { cannotCreateReason, MyApiKeysPage } from './MyApiKeysPage'
import { api, keyPage, makeArea, makeKey, notifyMock, renderRoute, resetAll } from '../admin/testUtils'
import type { Me, Permission } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('../admin/testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('../admin/testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('../admin/testUtils')).notifyMock)

function open(perms: Permission[], me: Partial<Me> = {}, keys = [makeKey({ id: 'k-1', name: 'laptop', prefix: 'mine1111' })]) {
  resetAll(perms, me)
  api.accountApi.apiKeys.mockResolvedValue(keyPage(keys))
  renderRoute(<MyApiKeysPage />)
  return screen.findByRole('heading', { name: 'My API keys' })
}

describe('cannotCreateReason', () => {
  it('blocks API-key callers first, then people without the permission', () => {
    expect(cannotCreateReason(true, 'api_key')).toMatch(/cannot create more keys/)
    expect(cannotCreateReason(false, 'session')).toMatch(/Create personal API keys/)
    expect(cannotCreateReason(true, 'session')).toBeNull()
    expect(cannotCreateReason(true, undefined)).toBeNull()
  })
})

describe('My API keys', () => {
  it('lists my keys, creates one that is shown once, and shows how to use it', async () => {
    await open(['account.api_keys'])
    expect(await screen.findByText('csk_mine1111_…')).toBeTruthy()
    expect(screen.queryByRole('note')).toBeNull()
    expect(screen.getByText(/Authorization: Bearer csk_…/)).toBeTruthy()
    expect(screen.getByRole('link', { name: 'API explorer' }).getAttribute('href')).toBe('/api-docs')

    const newKey = makeKey({ id: 'k-2', name: 'script', prefix: 'new22222', created_at: '2026-05-01T00:00:00Z' })
    api.accountApi.createApiKey.mockResolvedValue({ key: newKey, secret: 'csk_new22222_SECRET' })
    api.accountApi.apiKeys.mockResolvedValue(keyPage([newKey, makeKey({ id: 'k-1', name: 'laptop', prefix: 'mine1111' })]))
    fireEvent.click(screen.getByRole('button', { name: 'New key' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).queryByLabelText('Service account')).toBeNull()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: ' script ' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    await waitFor(() => expect(api.accountApi.createApiKey).toHaveBeenCalledWith({ name: 'script', expires_at: null, allow_api: true, allow_mcp: true, permissions: null, area_ids: null }))
    const secret = await screen.findByRole('dialog')
    expect(within(secret).getByText('csk_new22222_SECRET')).toBeTruthy()
    expect(within(secret).getByText(/one-way hash/)).toBeTruthy()
    fireEvent.click(within(secret).getByRole('button', { name: 'I have saved it' }))
    expect(screen.queryByText('csk_new22222_SECRET')).toBeNull()
    expect(await screen.findByText('csk_new22222_…')).toBeTruthy()
    expect(api.accountApi.apiKeys).toHaveBeenLastCalledWith({ limit: 20, offset: 0 }, expect.anything())
  })

  it('limits a new key to MCP and some of my permissions and areas, offering only what I hold', async () => {
    await open(['account.api_keys', 'crews.view', 'crews.edit'], { area_ids: ['a-1'] })
    api.areasApi.list.mockResolvedValue([makeArea({ id: 'a-1', name: 'IT' }), makeArea({ id: 'a-2', name: 'Finance' })])
    api.accountApi.createApiKey.mockResolvedValue({ key: makeKey({ id: 'k-2' }), secret: 'csk_x' })
    fireEvent.click(await screen.findByRole('button', { name: 'New key' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Everything you can do')).toBeTruthy()
    expect(await within(dialog).findByText('Every area you are in')).toBeTruthy()
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'claude' } })
    fireEvent.click(within(dialog).getByLabelText('REST API'))
    fireEvent.click(within(within(dialog).getByRole('group', { name: 'Permissions' })).getByLabelText('Only these'))
    fireEvent.click(await within(dialog).findByRole('checkbox', { name: 'View' }))
    expect(within(dialog).queryByRole('checkbox', { name: 'Users' })).toBeNull()
    fireEvent.click(within(within(dialog).getByRole('group', { name: 'Functional areas' })).getByLabelText('Only these'))
    expect(within(dialog).queryByRole('checkbox', { name: 'Finance' })).toBeNull()
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'IT' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create key' }))
    await waitFor(() => expect(api.accountApi.createApiKey).toHaveBeenCalledWith({
      name: 'claude', expires_at: null, allow_api: false, allow_mcp: true, permissions: ['crews.view'], area_ids: ['a-1'],
    }))
  })

  it('shows a load error for the permissions instead of an empty list', async () => {
    await open(['account.api_keys', 'crews.view'])
    api.adminApi.permissions.mockRejectedValue(new Error('down'))
    fireEvent.click(await screen.findByRole('button', { name: 'New key' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(within(dialog).getByRole('group', { name: 'Permissions' })).getByLabelText('Only these'))
    expect(await within(dialog).findByText(/Could not load the permissions/)).toBeTruthy()
    api.adminApi.permissions.mockResolvedValue([{ key: 'crews.view', feature: 'Crews', label: 'View', description: 'See crews' }])
    fireEvent.click(within(dialog).getByRole('button', { name: 'Try again' }))
    expect(await within(dialog).findByRole('checkbox', { name: 'View' })).toBeTruthy()
  })

  it('revokes my own key', async () => {
    await open(['account.api_keys'])
    api.accountApi.revokeApiKey.mockResolvedValue(undefined)
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.accountApi.revokeApiKey).toHaveBeenCalledWith('k-1'))
    expect(await screen.findByText('revoked')).toBeTruthy()
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Revoked laptop')
  })

  it('without the permission: explains why, offers no New key, still lists keys to revoke', async () => {
    await open([])
    expect(await screen.findByText('csk_mine1111_…')).toBeTruthy()
    expect(screen.getByRole('note').textContent).toMatch(/do not have the "Create personal API keys" permission/)
    expect(screen.queryByRole('button', { name: 'New key' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Revoke' })).toBeTruthy()
  })

  it('signed in with an API key: no New key, and an empty list says so plainly', async () => {
    await open(['account.api_keys'], { via: 'api_key' }, [])
    expect(await screen.findByText('You have no API keys.')).toBeTruthy()
    expect(screen.getByRole('note').textContent).toMatch(/Sign in to Crew Studio/)
    expect(screen.queryByRole('button', { name: 'New key' })).toBeNull()
  })

  it('invites creating the first key', async () => {
    await open(['account.api_keys'], {}, [])
    expect(await screen.findByText(/No API keys yet/)).toBeTruthy()
  })
})
