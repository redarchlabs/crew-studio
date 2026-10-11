import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { AccessPage } from './AccessPage'
import { api, apiError, isDisabled, makeAccess, makeMe, makeRole, makeStatus, notifyMock, renderRoute, resetAll, setAuth } from './testUtils'
import type { AccessSettings } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const ROLES = [makeRole({ id: 'r-b', name: 'Viewer' }), makeRole({ id: 'r-a', name: 'Analyst' })]
const saveBtn = () => screen.getByRole('button', { name: 'Save sign-in settings' }) as HTMLButtonElement
const roleSelect = () => screen.getByLabelText('Role for automatically created accounts') as HTMLSelectElement
const autoToggle = () => screen.getByRole('switch', { name: /Create accounts automatically/ })

beforeEach(() => {
  resetAll(['admin.access', 'admin.roles'])
  setAuth({ status: makeStatus({ local_login: true, providers: [{ key: 'microsoft', label: 'Microsoft Entra' }] }) })
  api.adminApi.access.get.mockResolvedValue(makeAccess())
  api.adminApi.roles.list.mockResolvedValue(ROLES)
})

async function open() {
  renderRoute(<AccessPage />)
  await screen.findByRole('switch', { name: /Create accounts automatically/ })
}

describe('AccessPage SSO auto-provisioning', () => {
  it('loads with auto-provisioning off and the role picker disabled', async () => {
    await open()
    expect(autoToggle().getAttribute('aria-checked')).toBe('false')
    expect(isDisabled(roleSelect())).toBe(true)
    expect(saveBtn().disabled).toBe(true)
  })

  it('lists roles alphabetically after a "no role" default', async () => {
    await open()
    expect(Array.from(roleSelect().options).map((o) => o.textContent)).toEqual(['No role (they see nothing until given one)', 'Analyst', 'Viewer'])
  })

  it('enables auto-provision with a default role and saves exactly that', async () => {
    api.adminApi.access.update.mockImplementation(async (b: AccessSettings) => b)
    await open()
    fireEvent.click(autoToggle())
    expect(isDisabled(roleSelect())).toBe(false)
    fireEvent.change(roleSelect(), { target: { value: 'r-b' } })
    expect(saveBtn().disabled).toBe(false)
    fireEvent.click(saveBtn())
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Sign-in settings saved'))
    expect(api.adminApi.access.update).toHaveBeenCalledWith({ sso_auto_provision: true, auto_provision_role_id: 'r-b', callback_allowed_hosts: [] })
    await waitFor(() => expect(saveBtn().disabled).toBe(true))
  })

  it('selecting "no role" sends null', async () => {
    api.adminApi.access.get.mockResolvedValue(makeAccess({ sso_auto_provision: true, auto_provision_role_id: 'r-a' }))
    api.adminApi.access.update.mockImplementation(async (b: AccessSettings) => b)
    await open()
    expect(roleSelect().value).toBe('r-a')
    fireEvent.change(roleSelect(), { target: { value: '' } })
    fireEvent.click(saveBtn())
    await waitFor(() => expect(api.adminApi.access.update).toHaveBeenCalledWith(expect.objectContaining({ auto_provision_role_id: null })))
  })

  it('keeps a saved role that was deleted selectable and labelled', async () => {
    api.adminApi.access.get.mockResolvedValue(makeAccess({ sso_auto_provision: true, auto_provision_role_id: 'r-gone' }))
    await open()
    expect(roleSelect().value).toBe('r-gone')
    expect(within(roleSelect()).getByText('Deleted role')).toBeTruthy()
  })

  it('does not list roles for someone who cannot, but keeps the current choice', async () => {
    resetAll(['admin.access'])
    api.adminApi.access.get.mockResolvedValue(makeAccess({ sso_auto_provision: true, auto_provision_role_id: 'r-a' }))
    renderRoute(<AccessPage />)
    await screen.findByRole('switch', { name: /Create accounts automatically/ })
    expect(api.adminApi.roles.list).not.toHaveBeenCalled()
    expect(within(roleSelect()).getByText('Current role (you cannot list roles)')).toBeTruthy()
    expect(roleSelect().value).toBe('r-a')
  })

  it('discards unsaved edits', async () => {
    await open()
    fireEvent.click(autoToggle())
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(autoToggle().getAttribute('aria-checked')).toBe('false')
    expect(screen.queryByRole('button', { name: 'Discard changes' })).toBeNull()
  })

  it('maps 422 errors onto the fields and toasts', async () => {
    api.adminApi.access.update.mockRejectedValue(apiError(422, 'bad', 'validation', ['auto_provision_role_id: role does not exist', 'sso_auto_provision: not allowed', 'callback_allowed_hosts: bad host x', 'misc: general']))
    await open()
    fireEvent.click(autoToggle())
    fireEvent.click(saveBtn())
    expect(await screen.findByText('role does not exist')).toBeTruthy()
    expect(screen.getByText('not allowed')).toBeTruthy()
    expect(screen.getByText('bad host x')).toBeTruthy()
    expect(screen.getByText('misc: general')).toBeTruthy()
    expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not save sign-in settings', expect.anything())
  })

  it('reports a 403 without clearing the edit', async () => {
    api.adminApi.access.update.mockRejectedValue(apiError(403, 'forbidden'))
    await open()
    fireEvent.click(autoToggle())
    fireEvent.click(saveBtn())
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalled())
    expect(autoToggle().getAttribute('aria-checked')).toBe('true')
    expect(saveBtn().disabled).toBe(false)
  })

  it('surfaces load errors with retry', async () => {
    api.adminApi.access.get.mockRejectedValueOnce(apiError(500, 'cannot load'))
    renderRoute(<AccessPage />)
    expect(await screen.findByText('cannot load')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await screen.findByRole('switch', { name: /Create accounts automatically/ })
  })

  it('tolerates a missing hosts list from the server', async () => {
    api.adminApi.access.get.mockResolvedValue({ sso_auto_provision: false, auto_provision_role_id: null } as unknown as AccessSettings)
    await open()
    expect(screen.getByText(/No hosts: callbacks may only reach public https addresses/)).toBeTruthy()
  })
})

describe('AccessPage callback hosts', () => {
  it('adds and removes hosts and saves them', async () => {
    api.adminApi.access.update.mockImplementation(async (b: AccessSettings) => b)
    api.adminApi.access.get.mockResolvedValue(makeAccess({ callback_allowed_hosts: ['erp.internal'] }))
    await open()
    fireEvent.change(screen.getByLabelText('Host to allow'), { target: { value: ' 10.0.0.5 ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove erp.internal' }))
    fireEvent.click(saveBtn())
    await waitFor(() => expect(api.adminApi.access.update).toHaveBeenCalledWith({ sso_auto_provision: false, auto_provision_role_id: null, callback_allowed_hosts: ['10.0.0.5'] }))
  })
})

describe('AccessPage sign-in methods panel', () => {
  it('shows enabled providers and enabled local login', async () => {
    await open()
    expect(screen.getByText('Microsoft Entra')).toBeTruthy()
    const row = screen.getByText('Administrator password sign-in').closest('li')!
    expect(within(row).getByText('enabled')).toBeTruthy()
  })

  it('shows local login as disabled and no providers when none are configured', async () => {
    setAuth({ status: makeStatus({ local_login: false, providers: [] }) })
    await open()
    expect(screen.getByText('No single sign-on provider configured')).toBeTruthy()
    const row = screen.getByText('Administrator password sign-in').closest('li')!
    expect(within(row).getByText('disabled')).toBeTruthy()
  })

  it('copes with an unknown status', async () => {
    setAuth({ status: null })
    await open()
    expect(screen.getByText('No single sign-on provider configured')).toBeTruthy()
  })

  it('is informational only: provider settings cannot be edited here', async () => {
    await open()
    expect(screen.getByText(/cannot be changed here/)).toBeTruthy()
  })
})

describe('AccessPage header', () => {
  it('shows history only with admin.audit', async () => {
    await open()
    expect(screen.queryByText('History')).toBeNull()
    setAuth({ me: makeMe({ permissions: ['admin.access', 'admin.audit'] }) })
    renderRoute(<AccessPage />)
    await waitFor(() => expect(screen.getByText('History').closest('a')?.getAttribute('href')).toContain('access_settings'))
  })
})
