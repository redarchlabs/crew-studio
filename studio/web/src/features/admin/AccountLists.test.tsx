import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { UsersPage } from './UsersPage'
import { ServiceAccountsPage } from './ServiceAccountsPage'
import { api, apiError, makeArea, makeIdentity, makeMe, makeRole, makeUser, renderRoute, resetAll, setAuth } from './testUtils'
import { invalidateAreas } from '../../lib/useAreas'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const ROLES = [makeRole({ id: 'r-admin', name: 'Administrator', builtin: true }), makeRole({ id: 'r-an', name: 'Analyst' })]

beforeEach(() => {
  invalidateAreas()
  resetAll(['admin.users', 'admin.service_accounts', 'admin.roles'], { id: 'me-1' })
  api.adminApi.roles.list.mockResolvedValue(ROLES)
  api.areasApi.list.mockResolvedValue([makeArea({ id: 'a-1', name: 'Finance' }), makeArea({ id: 'a-2', name: 'IT' }), makeArea({ id: 'a-3', name: 'HR' })])
})

const rows = () => screen.getAllByRole('row').slice(1)

describe('UsersPage', () => {
  const people = [
    makeUser({ id: 'me-1', name: 'Zed Self', email: 'zed@example.com', status: 'active', role_ids: ['r-admin'], all_areas: true, has_password: true, last_login_at: new Date().toISOString() }),
    makeUser({ id: 'u-2', name: 'Bea Linked', email: 'bea@example.com', status: 'active', role_ids: ['r-an', 'r-unknown'], area_ids: ['a-1', 'a-2', 'a-3'], identities: [makeIdentity({ provider: 'microsoft' }), makeIdentity({ provider: 'clerk' }), makeIdentity({ provider: 'microsoft' })] }),
    makeUser({ id: 'u-3', name: 'Al Disabled', email: null, status: 'disabled', role_ids: [] }),
  ]

  it('requests only people and lists them sorted by name', async () => {
    api.adminApi.users.list.mockResolvedValue(people)
    renderRoute(<UsersPage />)
    await screen.findByText('Bea Linked')
    expect(api.adminApi.users.list).toHaveBeenCalledWith('human', expect.anything())
    expect(rows().map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['Al Disabled', 'Bea Linked', 'Zed Self(you)'])
  })

  it('renders roles, areas, status and sign-in methods', async () => {
    api.adminApi.users.list.mockResolvedValue(people)
    renderRoute(<UsersPage />)
    await screen.findByText('Bea Linked')
    const [al, bea, zed] = rows()
    expect(within(al).getByText('No roles')).toBeTruthy()
    expect(within(al).getByText('disabled')).toBeTruthy()
    expect(within(al).getByText('not linked yet')).toBeTruthy()
    expect(within(al).getByText('never')).toBeTruthy()
    expect(within(bea).getByText('Analyst')).toBeTruthy()
    expect(within(bea).getAllByText('+1')).toHaveLength(2) // one unknown role, one area beyond the two shown
    expect(within(bea).getAllByText('microsoft')).toHaveLength(1)
    expect(within(bea).getByText('clerk')).toBeTruthy()
    expect(within(zed).getByText('All areas')).toBeTruthy()
    expect(within(zed).getByText('Administrator')).toBeTruthy()
    expect(within(zed).getByText('password')).toBeTruthy()
    expect(within(zed).queryByText('not linked yet')).toBeNull()
  })

  it('links each person to the editor', async () => {
    api.adminApi.users.list.mockResolvedValue(people)
    renderRoute(<UsersPage />)
    expect((await screen.findByText('Bea Linked')).closest('a')?.getAttribute('href')).toBe('/admin/users/u-2')
  })

  it('filters by text (name or email, case-insensitive) and status', async () => {
    api.adminApi.users.list.mockResolvedValue(people)
    renderRoute(<UsersPage />)
    await screen.findByText('Bea Linked')
    fireEvent.change(screen.getByPlaceholderText('Filter by name or email…'), { target: { value: 'BEA@' } })
    expect(rows()).toHaveLength(1)
    fireEvent.change(screen.getByPlaceholderText('Filter by name or email…'), { target: { value: '' } })
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'disabled' } })
    expect(rows().map((r) => r.textContent)).toEqual([expect.stringContaining('Al Disabled')])
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'active' } })
    expect(rows()).toHaveLength(2)
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'bogus' } })
    expect(rows()).toHaveLength(3)
    fireEvent.change(screen.getByPlaceholderText('Filter by name or email…'), { target: { value: 'nobody' } })
    expect(screen.getByText('No people match these filters.')).toBeTruthy()
  })

  it('shows an empty state with an invite action', async () => {
    api.adminApi.users.list.mockResolvedValue([])
    renderRoute(<UsersPage />)
    expect(await screen.findByText('No people yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Invite someone' }).getAttribute('href')).toBe('/admin/users/new')
  })

  it('surfaces a 403 load failure with a retry', async () => {
    api.adminApi.users.list.mockRejectedValueOnce(apiError(403, 'You lack admin.users', 'forbidden'))
    renderRoute(<UsersPage />)
    expect(await screen.findByText('You lack admin.users')).toBeTruthy()
    api.adminApi.users.list.mockResolvedValue(people)
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Bea Linked')).toBeTruthy()
  })

  it('shows the history link only to people holding admin.audit', async () => {
    api.adminApi.users.list.mockResolvedValue(people)
    const first = renderRoute(<UsersPage />)
    await screen.findByText('Bea Linked')
    expect(screen.queryByText('History')).toBeNull()
    first.unmount()
    setAuth({ me: makeMe({ permissions: ['admin.users', 'admin.audit'] }) })
    renderRoute(<UsersPage />)
    await screen.findByText('Bea Linked')
    expect(screen.getByText('History').closest('a')?.getAttribute('href')).toContain('entity_type=user')
  })

  it('offers the invite action', async () => {
    api.adminApi.users.list.mockResolvedValue(people)
    renderRoute(<UsersPage />)
    await screen.findByText('Bea Linked')
    expect(screen.getByRole('link', { name: /Invite person/ }).getAttribute('href')).toBe('/admin/users/new')
  })
})

describe('ServiceAccountsPage', () => {
  const svc = [
    makeUser({ id: 's-1', kind: 'service', name: 'Zeta bot', email: null, role_ids: ['r-an'], all_areas: true }),
    makeUser({ id: 's-2', kind: 'service', name: 'Alpha bot', email: null, status: 'disabled', role_ids: ['r-an'], area_ids: ['a-1'] }),
  ]

  it('lists service accounts only, sorted, with role names when roles are readable', async () => {
    api.adminApi.users.list.mockResolvedValue(svc)
    renderRoute(<ServiceAccountsPage />)
    await screen.findByText('Alpha bot')
    expect(api.adminApi.users.list).toHaveBeenCalledWith('service', expect.anything())
    expect(rows().map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['Alpha bot', 'Zeta bot'])
    expect(screen.getAllByText('Analyst')).toHaveLength(2)
    expect(screen.getByRole('link', { name: 'Alpha bot' }).getAttribute('href')).toBe('/admin/service-accounts/s-2')
  })

  it('does not request roles for a person who cannot list them; counts are shown instead', async () => {
    resetAll(['admin.service_accounts'])
    api.adminApi.users.list.mockResolvedValue(svc)
    renderRoute(<ServiceAccountsPage />)
    await screen.findByText('Alpha bot')
    expect(api.adminApi.roles.list).not.toHaveBeenCalled()
    expect(screen.getAllByText('+1')).toHaveLength(2)
    expect(screen.queryByText('Analyst')).toBeNull()
  })

  it('filters by status and name', async () => {
    api.adminApi.users.list.mockResolvedValue(svc)
    renderRoute(<ServiceAccountsPage />)
    await screen.findByText('Alpha bot')
    fireEvent.change(screen.getByLabelText('Filter by status'), { target: { value: 'disabled' } })
    expect(rows()).toHaveLength(1)
    fireEvent.change(screen.getByPlaceholderText('Filter by name…'), { target: { value: 'zeta' } })
    expect(screen.getByText('No service accounts match these filters.')).toBeTruthy()
  })

  it('shows an empty state', async () => {
    api.adminApi.users.list.mockResolvedValue([])
    renderRoute(<ServiceAccountsPage />)
    expect(await screen.findByText('No service accounts yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Create a service account' }).getAttribute('href')).toBe('/admin/service-accounts/new')
  })

  it('shows the history link only with admin.audit', async () => {
    api.adminApi.users.list.mockResolvedValue(svc)
    const first = renderRoute(<ServiceAccountsPage />)
    await screen.findByText('Alpha bot')
    expect(screen.queryByText('History')).toBeNull()
    first.unmount()
    setAuth({ me: makeMe({ permissions: ['admin.service_accounts', 'admin.audit'] }) })
    renderRoute(<ServiceAccountsPage />)
    await waitFor(() => expect(screen.getByText('History')).toBeTruthy())
  })

  it('surfaces load errors', async () => {
    api.adminApi.users.list.mockRejectedValue(apiError(500, 'boom'))
    renderRoute(<ServiceAccountsPage />)
    expect(await screen.findByText('boom')).toBeTruthy()
  })
})
