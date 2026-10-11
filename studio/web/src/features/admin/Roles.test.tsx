import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { RolesPage } from './RolesPage'
import { RoleEditorPage } from './RoleEditorPage'
import { api, apiError, auth, DEFS, isDisabled, makeMe, makeRole, notifyMock, renderRoute, resetAll, setAuth } from './testUtils'
import type { RoleInput } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const BUILTIN = makeRole({ id: 'r-admin', name: 'Administrator', builtin: true, description: 'Everything', permissions: [], user_count: 2 })
const ANALYST = makeRole({ id: 'r-an', name: 'Analyst', description: 'Reads', permissions: ['crews.view', 'runs.view'], user_count: 1 })
const EMPTYROLE = makeRole({ id: 'r-ee', name: 'Empty', permissions: [], user_count: 0 })
const loc = () => screen.getByTestId('location').textContent
const type = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
const perm = (description: string) => within(screen.getByTitle(new RegExp(description))).getByRole('checkbox') as HTMLInputElement

beforeEach(() => {
  resetAll(['admin.roles', 'admin.users'])
  api.adminApi.roles.list.mockResolvedValue([ANALYST, EMPTYROLE, BUILTIN])
})

describe('RolesPage', () => {
  it('lists the built-in role first, then others by name, with permission and holder counts', async () => {
    renderRoute(<RolesPage />)
    await screen.findByText('Analyst')
    const rows = screen.getAllByRole('row').slice(1)
    expect(rows.map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['Administrator built-in', 'Analyst', 'Empty'])
    expect(within(rows[0]).getByText('all')).toBeTruthy()
    expect(within(rows[1]).getByText('2')).toBeTruthy()
    expect(within(rows[2]).getByText('—')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Analyst' }).getAttribute('href')).toBe('/admin/roles/r-an')
  })

  it('offers creating roles to people with admin.roles and no read-only note', async () => {
    renderRoute(<RolesPage />)
    await screen.findByText('Analyst')
    expect(screen.getByRole('link', { name: /New role/ }).getAttribute('href')).toBe('/admin/roles/new')
    expect(screen.queryByRole('note')).toBeNull()
  })

  it('hides role creation and says why for someone with only admin.users', async () => {
    resetAll(['admin.users'])
    api.adminApi.roles.list.mockResolvedValue([ANALYST])
    renderRoute(<RolesPage />)
    await screen.findByText('Analyst')
    expect(screen.queryByRole('link', { name: /New role/ })).toBeNull()
    expect(screen.getByRole('note').textContent).toContain('admin.roles')
  })

  it('shows the history link only with admin.audit', async () => {
    renderRoute(<RolesPage />)
    await screen.findByText('Analyst')
    expect(screen.queryByText('History')).toBeNull()
    setAuth({ me: makeMe({ permissions: ['admin.roles', 'admin.audit'] }) })
    renderRoute(<RolesPage />)
    await waitFor(() => expect(screen.getByText('History')).toBeTruthy())
  })

  it('shows an empty state with a create action when permitted', async () => {
    api.adminApi.roles.list.mockResolvedValue([])
    renderRoute(<RolesPage />)
    expect(await screen.findByText('No roles yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Create a role' })).toBeTruthy()
  })

  it('shows an empty state without the create action when not permitted', async () => {
    resetAll(['admin.users'])
    api.adminApi.roles.list.mockResolvedValue([])
    renderRoute(<RolesPage />)
    expect(await screen.findByText('No roles yet')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'Create a role' })).toBeNull()
  })

  it('surfaces load errors', async () => {
    api.adminApi.roles.list.mockRejectedValue(apiError(403, 'forbidden here'))
    renderRoute(<RolesPage />)
    expect(await screen.findByText('forbidden here')).toBeTruthy()
  })
})

describe('RoleEditorPage', () => {
  function open(id: string) {
    return renderRoute(<RoleEditorPage />, { path: '/admin/roles/:id', url: `/admin/roles/${id}`, extra: ['/admin/roles', '/admin/users'] })
  }

  it('groups the permission catalog by feature in server order', async () => {
    open('new')
    await screen.findByText('New role')
    expect(screen.getByLabelText('All Crews permissions')).toBeTruthy()
    expect(screen.getByLabelText('All Runs permissions')).toBeTruthy()
    expect(screen.getByLabelText('All Admin permissions')).toBeTruthy()
    const features = screen.getAllByLabelText(/^All .* permissions$/).map((e) => e.getAttribute('aria-label'))
    expect(features).toEqual(['All Crews permissions', 'All Runs permissions', 'All Admin permissions'])
    expect(DEFS.length).toBe(5)
  })

  it('validates the name', async () => {
    open('new')
    await screen.findByText('New role')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(api.adminApi.roles.create).not.toHaveBeenCalled()
  })

  it('creates a role with chosen permissions and opens it', async () => {
    api.adminApi.roles.create.mockResolvedValue(makeRole({ id: 'r-new' }))
    open('new')
    await screen.findByText('New role')
    type('Name', '  Finance analyst ')
    type('Description', ' reads  ')
    fireEvent.click(perm('See crews'))
    fireEvent.click(perm('Change crews'))
    fireEvent.click(perm('Change crews'))
    fireEvent.click(screen.getByLabelText('All Runs permissions'))
    expect(screen.getByText(/Effective permissions · 2/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(loc()).toBe('/admin/roles/r-new'))
    expect(api.adminApi.roles.create.mock.calls[0][0] as RoleInput).toEqual({ name: 'Finance analyst', description: 'reads', permissions: ['crews.view', 'runs.view'] })
    expect(auth.reload).toHaveBeenCalled()
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Created role')
  })

  it('surfaces 422 field errors, including permissions', async () => {
    api.adminApi.roles.create.mockRejectedValue(apiError(422, 'bad', 'validation', ['name: already exists', 'permissions: unknown permission x']))
    open('new')
    await screen.findByText('New role')
    type('Name', 'Dup')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('already exists')).toBeTruthy()
    expect(screen.getByText('unknown permission x')).toBeTruthy()
    expect(auth.reload).not.toHaveBeenCalled()
  })

  it('edits an existing role, showing holders', async () => {
    api.adminApi.roles.update.mockImplementation(async (_id: string, b: RoleInput) => ({ ...ANALYST, ...b }))
    open('r-an')
    await screen.findByRole('heading', { name: 'Analyst' })
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Analyst')
    expect(perm('See crews').checked).toBe(true)
    expect(perm('Change crews').checked).toBe(false)
    expect(screen.getByText(/account holds this role/)).toBeTruthy()
    expect(screen.getByText(/Changes apply to them at once/)).toBeTruthy()
    expect(screen.getByRole('link', { name: /Manage people/ }).getAttribute('href')).toBe('/admin/users')
    type('Description', 'New text')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Saved role'))
    expect(api.adminApi.roles.update).toHaveBeenCalledWith('r-an', { name: 'Analyst', description: 'New text', permissions: ['crews.view', 'runs.view'] })
    expect(auth.reload).toHaveBeenCalledTimes(1)
  })

  it('pluralises holders and omits the manage-people link without admin.users', async () => {
    resetAll(['admin.roles'])
    api.adminApi.roles.list.mockResolvedValue([BUILTIN, EMPTYROLE])
    open('r-ee')
    await screen.findByRole('heading', { name: 'Empty' })
    expect(screen.getByText(/accounts hold this role/)).toBeTruthy()
    expect(screen.queryByRole('link', { name: /Manage people/ })).toBeNull()
    expect(screen.queryByText(/Changes apply to them at once/)).toBeNull()
  })

  describe('built-in Administrator', () => {
    it('is read-only: explained, fields locked, every permission shown, no save or delete', async () => {
      open('r-admin')
      await screen.findByRole('heading', { name: 'Administrator' })
      expect(screen.getByRole('note').textContent).toContain('always holds every permission')
      expect(isDisabled(screen.getByLabelText('Name'))).toBe(true)
      expect(isDisabled(screen.getByLabelText('Description'))).toBe(true)
      expect(perm('See crews').checked).toBe(true)
      expect(perm('Manage roles').checked).toBe(true)
      expect(isDisabled(perm('See crews'))).toBe(true)
      expect(screen.getByText(/Effective permissions · 5/)).toBeTruthy()
      expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
      expect(screen.getByText('read-only')).toBeTruthy()
    })

    it('never submits even if the form is submitted programmatically', async () => {
      const { container } = open('r-admin')
      await screen.findByRole('heading', { name: 'Administrator' })
      fireEvent.submit(container.querySelector('form')!)
      expect(api.adminApi.roles.update).not.toHaveBeenCalled()
    })

    it('stays read-only even for a person holding admin.roles', async () => {
      resetAll(['admin.roles'])
      api.adminApi.roles.list.mockResolvedValue([BUILTIN])
      open('r-admin')
      await screen.findByRole('heading', { name: 'Administrator' })
      expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
    })
  })

  describe('without admin.roles', () => {
    beforeEach(() => {
      resetAll(['admin.users'])
      api.adminApi.roles.list.mockResolvedValue([ANALYST, BUILTIN])
    })

    it('shows a read-only view naming the missing permission', async () => {
      open('r-an')
      await screen.findByRole('heading', { name: 'Analyst' })
      expect(screen.getByRole('note').textContent).toContain('admin.roles')
      expect(isDisabled(screen.getByLabelText('Name'))).toBe(true)
      expect(isDisabled(perm('See crews'))).toBe(true)
      expect(screen.queryByRole('button', { name: 'Save' })).toBeNull()
      expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    })

    it('does not submit the form', async () => {
      const { container } = open('r-an')
      await screen.findByRole('heading', { name: 'Analyst' })
      fireEvent.submit(container.querySelector('form')!)
      expect(api.adminApi.roles.update).not.toHaveBeenCalled()
      expect(api.adminApi.roles.create).not.toHaveBeenCalled()
    })
  })

  describe('deleting', () => {
    it('deletes after confirmation and returns to the list', async () => {
      api.adminApi.roles.remove.mockResolvedValue(undefined)
      open('r-ee')
      await screen.findByRole('heading', { name: 'Empty' })
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      expect(api.adminApi.roles.remove).not.toHaveBeenCalled()
      fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
      await waitFor(() => expect(loc()).toBe('/admin/roles'))
      expect(api.adminApi.roles.remove).toHaveBeenCalledWith('r-ee')
      expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Deleted Empty')
    })

    it('shows the server error for a role that is still in use and stays on the page', async () => {
      api.adminApi.roles.remove.mockRejectedValue(apiError(409, 'role is assigned to 1 account', 'role_in_use'))
      open('r-an')
      await screen.findByRole('heading', { name: 'Analyst' })
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
      await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not delete Analyst', expect.objectContaining({ status: 409, message: 'role is assigned to 1 account' })))
      expect(loc()).toBe('/admin/roles/r-an')
      expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy()
    })
  })

  describe('loading', () => {
    it('shows not-found for an unknown role', async () => {
      open('r-missing')
      expect(await screen.findByText('Role not found')).toBeTruthy()
      expect(screen.getByRole('link', { name: 'Back to roles' }).getAttribute('href')).toBe('/admin/roles')
    })

    it('surfaces a failure to load roles', async () => {
      api.adminApi.roles.list.mockRejectedValue(apiError(500, 'db down'))
      open('r-an')
      expect(await screen.findByText('db down')).toBeTruthy()
    })

    it('uses the server catalog labels and falls back to key-derived ones when it is unreadable', async () => {
      api.adminApi.permissions.mockRejectedValue(apiError(403, 'forbidden'))
      open('new')
      await screen.findByText('New role')
      expect(screen.getByLabelText('All Settings permissions')).toBeTruthy()
      expect(screen.getByLabelText('All Triggers permissions')).toBeTruthy()
    })
  })
})
