import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { UserEditorPage } from './UserEditorPage'
import { ServiceAccountEditorPage } from './ServiceAccountEditorPage'
import { api, apiError, auth, isDisabled, makeArea, keyPage, makeIdentity, makeRole, makeUser, notifyMock, renderRoute, resetAll } from './testUtils'
import { invalidateAreas } from '../../lib/useAreas'
import type { UserInput } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const ADMIN_ROLE = makeRole({ id: 'r-admin', name: 'Administrator', builtin: true, permissions: [] })
const ANALYST = makeRole({ id: 'r-an', name: 'Analyst', permissions: ['crews.view', 'runs.view'], description: 'Reads things' })
const AREAS = [makeArea({ id: 'a-1', name: 'Finance' }), makeArea({ id: 'a-2', name: 'IT', description: 'Tech' })]

function renderUser(id: string) {
  return renderRoute(<UserEditorPage />, { path: '/admin/users/:id', url: `/admin/users/${id}`, extra: ['/admin/users', '/admin/service-accounts/:id'] })
}
function renderService(id: string) {
  return renderRoute(<ServiceAccountEditorPage />, { path: '/admin/service-accounts/:id', url: `/admin/service-accounts/${id}`, extra: ['/admin/service-accounts', '/admin/users/:id'] })
}

const type = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
const loc = () => screen.getByTestId('location').textContent
const option = (name: RegExp | string) => screen.getByRole('option', { name })
const permBox = (description: string) => within(screen.getByTitle(new RegExp(description))).getByRole('checkbox') as HTMLInputElement
const lastBody = (fn: ReturnType<typeof vi.fn>, idx = 0): UserInput => fn.mock.calls.at(-1)![idx]

beforeEach(() => {
  invalidateAreas()
  resetAll(['admin.users', 'admin.service_accounts', 'admin.roles'], { id: 'me-1' })
  api.adminApi.roles.list.mockResolvedValue([ANALYST, ADMIN_ROLE])
  api.areasApi.list.mockResolvedValue(AREAS)
})

describe('inviting a person', () => {
  async function open() {
    renderUser('new')
    await screen.findByText('Invite a person')
    await screen.findByRole('option', { name: /Finance/ })
  }

  it('starts empty with an active status and the create button', async () => {
    await open()
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('Status') as HTMLSelectElement).value).toBe('active')
    expect(screen.getByRole('button', { name: 'Create' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.queryByText('Linked sign-ins')).toBeNull()
    expect(screen.queryByText('Sessions')).toBeNull()
  })

  it('warns that someone without areas sees nothing', async () => {
    await open()
    expect(screen.getByText(/No functional areas: they will not see any crews/)).toBeTruthy()
    fireEvent.click(option(/Finance/))
    expect(screen.queryByText(/No functional areas: they will not see/)).toBeNull()
  })

  it('validates name and email without calling the API', async () => {
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(screen.getByText('Enter the email address they sign in with')).toBeTruthy()
    expect(api.adminApi.users.create).not.toHaveBeenCalled()
    expect(notifyMock.notifyError).toHaveBeenCalledWith('Check the person form', expect.any(Error))
  })

  it('creates with roles, areas and direct permissions, then opens the new person', async () => {
    api.adminApi.users.create.mockResolvedValue(makeUser({ id: 'u-new' }))
    await open()
    type('Name', '  Grace Hopper ')
    type('Email', ' grace@example.com ')
    fireEvent.click(option(/Analyst/))
    fireEvent.click(option(/Finance/))
    fireEvent.click(permBox('Change crews'))
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(loc()).toBe('/admin/users/u-new'))
    expect(lastBody(api.adminApi.users.create)).toEqual({
      kind: 'human', name: 'Grace Hopper', email: 'grace@example.com', status: 'active', all_areas: false,
      area_ids: ['a-1'], role_ids: ['r-an'], permissions: ['crews.edit'],
    })
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Created person')
  })

  it('creates a person with all areas', async () => {
    api.adminApi.users.create.mockResolvedValue(makeUser({ id: 'u-new' }))
    await open()
    type('Name', 'Grace')
    type('Email', 'g@example.com')
    fireEvent.click(screen.getByRole('switch', { name: /All areas/ }))
    expect(screen.queryByText(/No functional areas: they will not see/)).toBeNull()
    expect(isDisabled(option(/Finance/))).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(api.adminApi.users.create).toHaveBeenCalled())
    expect(lastBody(api.adminApi.users.create).all_areas).toBe(true)
  })

  it('keeps previously picked areas when All areas is toggled on and off', async () => {
    await open()
    fireEvent.click(option(/Finance/))
    const toggle = screen.getByRole('switch', { name: /All areas/ })
    fireEvent.click(toggle)
    fireEvent.click(toggle)
    expect(option(/Finance/).getAttribute('aria-selected')).toBe('true')
  })

  it('shows 422 field errors from the server next to the fields and as a toast', async () => {
    api.adminApi.users.create.mockRejectedValue(apiError(422, 'invalid', 'validation', ['email: already in use', 'role_ids: unknown role', 'weird: general problem']))
    await open()
    type('Name', 'Grace')
    type('Email', 'g@example.com')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('already in use')).toBeTruthy()
    expect(screen.getByText('unknown role')).toBeTruthy()
    expect(screen.getByText('weird: general problem')).toBeTruthy()
    expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not save person', expect.anything())
    expect(loc()).toBe('/admin/users/new')
  })

  it('keeps a 409 email conflict on screen and dismissible', async () => {
    api.adminApi.users.create.mockRejectedValue(apiError(409, 'email already registered', 'conflict'))
    await open()
    type('Name', 'Grace')
    type('Email', 'g@example.com')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect(await screen.findByText('That conflicts with another account')).toBeTruthy()
    expect(screen.getByText('email already registered')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText('That conflicts with another account')).toBeNull()
  })

  it('surfaces a 403 on create', async () => {
    api.adminApi.users.create.mockRejectedValue(apiError(403, 'forbidden', 'forbidden'))
    await open()
    type('Name', 'Grace')
    type('Email', 'g@example.com')
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not save person', expect.objectContaining({ status: 403 })))
    expect(loc()).toBe('/admin/users/new')
  })

  it('submits from the form (Enter key)', async () => {
    api.adminApi.users.create.mockResolvedValue(makeUser({ id: 'u-new' }))
    const { container } = renderUser('new')
    await screen.findByText('Invite a person')
    type('Name', 'Grace')
    type('Email', 'g@example.com')
    fireEvent.submit(container.querySelector('form')!)
    await waitFor(() => expect(api.adminApi.users.create).toHaveBeenCalledTimes(1))
  })
})

describe('editing a person', () => {
  const user = makeUser({
    id: 'u-2', name: 'Ada Lovelace', email: 'ada@example.com', role_ids: ['r-an'], area_ids: ['a-1'],
    permissions: ['crews.edit'], effective_permissions: ['crews.view', 'runs.view', 'crews.edit'],
    identities: [makeIdentity({ provider: 'microsoft', email: 'ada@corp.example' })],
  })

  async function open(u = user) {
    api.adminApi.users.get.mockResolvedValue(u)
    renderUser(u.id)
    await screen.findByRole('heading', { name: u.name })
    await screen.findByRole('option', { name: /Finance/ })
  }

  it('loads the account into the form', async () => {
    await open()
    expect((screen.getByLabelText('Name') as HTMLInputElement).value).toBe('Ada Lovelace')
    expect((screen.getByLabelText('Email') as HTMLInputElement).value).toBe('ada@example.com')
    expect(option(/Analyst/).getAttribute('aria-selected')).toBe('true')
    expect(option(/Finance/).getAttribute('aria-selected')).toBe('true')
    expect(permBox('Change crews').checked).toBe(true)
    expect(api.adminApi.users.get).toHaveBeenCalledWith('u-2', expect.anything())
  })

  it('enables save only after a change and sends the full update', async () => {
    api.adminApi.users.update.mockImplementation(async (_id: string, body: UserInput) => ({ ...user, ...body, email: body.email }))
    await open()
    const save = screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    type('Name', 'Ada King')
    expect(screen.getByText('unsaved')).toBeTruthy()
    expect(save.disabled).toBe(false)
    fireEvent.click(save)
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Saved person'))
    expect(api.adminApi.users.update).toHaveBeenCalledWith('u-2', expect.objectContaining({ name: 'Ada King', role_ids: ['r-an'], area_ids: ['a-1'], permissions: ['crews.edit'] }))
    expect(screen.getByRole('heading', { name: 'Ada King' })).toBeTruthy()
    // Editing someone else does not refresh my own permissions
    expect(auth.reload).not.toHaveBeenCalled()
    expect(screen.queryByText('unsaved')).toBeNull()
  })

  it('refreshes the signed-in principal after editing oneself', async () => {
    const me = makeUser({ id: 'me-1', name: 'Me Myself' })
    api.adminApi.users.update.mockImplementation(async (_id: string, body: UserInput) => ({ ...me, ...body }))
    await open(me)
    type('Name', 'Me Renamed')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(auth.reload).toHaveBeenCalledTimes(1))
  })

  it('lists roles with the built-in hint and updates the effective permissions as roles change', async () => {
    await open(makeUser({ id: 'u-3', name: 'Plain', role_ids: [], permissions: [], effective_permissions: [] }))
    expect(screen.getByText('Built-in · every permission')).toBeTruthy()
    expect(screen.getByText('Reads things')).toBeTruthy()
    expect(screen.getByText(/None yet\. Give them a role/)).toBeTruthy()
    fireEvent.click(option(/Analyst/))
    // Both Crews and Runs "View" are now effective, and the matrix labels them as inherited
    expect(screen.getByText(/Effective permissions · 2/)).toBeTruthy()
    expect(screen.getAllByText('via role').length).toBe(2)
    expect(screen.getByText('Includes unsaved changes.')).toBeTruthy()
  })

  it('granting the built-in Administrator role makes every permission effective', async () => {
    await open(makeUser({ id: 'u-3', name: 'Plain', role_ids: [], effective_permissions: [] }))
    fireEvent.click(option(/Administrator/))
    expect(screen.getByText('Effective permissions · 5')).toBeTruthy()
  })

  it('toggles a whole permission group with its tri-state checkbox', async () => {
    await open(makeUser({ id: 'u-3', name: 'Plain', role_ids: [], permissions: [] }))
    const crewsAll = screen.getByLabelText('All Crews permissions') as HTMLInputElement
    fireEvent.click(crewsAll)
    expect(permBox('See crews').checked).toBe(true)
    expect(permBox('Change crews').checked).toBe(true)
    fireEvent.click(permBox('Change crews'))
    expect(crewsAll.indeterminate).toBe(true)
    fireEvent.click(crewsAll)
    fireEvent.click(crewsAll)
    expect(permBox('See crews').checked).toBe(false)
  })

  it('disables the account (and says so when it is your own)', async () => {
    api.adminApi.users.update.mockImplementation(async (_id: string, body: UserInput) => ({ ...user, ...body }))
    await open()
    expect(screen.queryByText('You are about to disable your own account.')).toBeNull()
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'disabled' } })
    expect(screen.getByText(/Disabling signs them out everywhere/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(lastBody(api.adminApi.users.update, 1).status).toBe('disabled'))
  })

  it('warns when disabling oneself and offers no delete button for oneself', async () => {
    await open(makeUser({ id: 'me-1', name: 'Me Myself' }))
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'disabled' } })
    expect(screen.getByText('You are about to disable your own account.')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getByText(/This ends every session of yours/)).toBeTruthy()
  })

  it('ignores an unexpected status value', async () => {
    await open()
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'disabled' } })
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'active' } })
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true)
  })

  it('shows the linked single sign-in identities', async () => {
    await open()
    expect(screen.getByText('Linked sign-ins')).toBeTruthy()
    expect(screen.getByText('ada@corp.example')).toBeTruthy()
    expect(screen.getByText('microsoft')).toBeTruthy()
  })

  it('says when no sign-in is linked yet', async () => {
    await open(makeUser({ id: 'u-3', name: 'Plain', identities: [] }))
    expect(screen.getByText(/Not linked yet/)).toBeTruthy()
    expect(isDisabled(screen.getByLabelText('Email'))).toBe(false)
  })

  it('locks the email of a person with a linked single sign-on identity', async () => {
    await open()
    expect(isDisabled(screen.getByLabelText('Email'))).toBe(true)
  })

  it('surfaces a 409 last-administrator conflict on save with guidance', async () => {
    api.adminApi.users.update.mockRejectedValue(apiError(409, 'cannot remove the last administrator', 'last_admin'))
    await open()
    fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'disabled' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('This is the last administrator')).toBeTruthy()
    expect(screen.getByText(/Give that permission to another active person first/)).toBeTruthy()
  })

  it('clears a conflict after a later successful save', async () => {
    api.adminApi.users.update.mockRejectedValueOnce(apiError(409, 'conflict', 'x')).mockImplementation(async (_i: string, b: UserInput) => ({ ...user, ...b }))
    await open()
    type('Name', 'A1')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('That conflicts with another account')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.queryByText('That conflicts with another account')).toBeNull())
  })

  it('surfaces a 422 on update', async () => {
    api.adminApi.users.update.mockRejectedValue(apiError(422, 'bad', 'validation', ['area_ids: unknown area']))
    await open()
    type('Name', 'X')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('unknown area')).toBeTruthy()
  })

  describe('deleting', () => {
    it('needs a second click, then deletes and returns to the list', async () => {
      api.adminApi.users.remove.mockResolvedValue(undefined)
      await open()
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      expect(api.adminApi.users.remove).not.toHaveBeenCalled()
      fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
      await waitFor(() => expect(loc()).toBe('/admin/users'))
      expect(api.adminApi.users.remove).toHaveBeenCalledWith('u-2')
      expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Deleted Ada Lovelace')
    })

    it('shows the last-administrator conflict when the server refuses', async () => {
      api.adminApi.users.remove.mockRejectedValue(apiError(409, 'cannot delete the last admin', 'last_admin'))
      await open()
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
      expect(await screen.findByText('This is the last administrator')).toBeTruthy()
      expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not delete Ada Lovelace', expect.anything())
      expect(loc()).toBe('/admin/users/u-2')
    })

    it('reports a 403 delete failure', async () => {
      api.adminApi.users.remove.mockRejectedValue(apiError(403, 'forbidden'))
      await open()
      fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
      fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
      await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalled())
      expect(screen.queryByText('That conflicts with another account')).toBeNull()
    })
  })
})

describe('account loading and routing', () => {
  it('shows not-found for a 404', async () => {
    api.adminApi.users.get.mockRejectedValue(apiError(404, 'no such user', 'not_found'))
    renderUser('gone')
    expect(await screen.findByText('Account not found')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to the list' }).getAttribute('href')).toBe('/admin/users')
  })

  it('shows other load errors with retry that reloads both account and references', async () => {
    api.adminApi.users.get.mockRejectedValueOnce(apiError(500, 'kaput'))
    renderUser('u-2')
    expect(await screen.findByText('kaput')).toBeTruthy()
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-2', name: 'Back Again' }))
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('heading', { name: 'Back Again' })).toBeTruthy()
  })

  it('redirects a service account opened on the people screen to the right editor', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 's-1', kind: 'service', name: 'Bot' }))
    renderUser('s-1')
    await waitFor(() => expect(loc()).toBe('/admin/service-accounts/s-1'))
  })

  it('redirects a person opened on the service-account screen', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-9', kind: 'human' }))
    renderService('u-9')
    await waitFor(() => expect(loc()).toBe('/admin/users/u-9'))
  })

  it('falls back to a catalog built from the known permissions when /admin/permissions is unreadable', async () => {
    api.adminApi.permissions.mockRejectedValue(apiError(403, 'forbidden'))
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-2', name: 'Ada' }))
    renderUser('u-2')
    await screen.findByRole('heading', { name: 'Ada' })
    expect(screen.getByLabelText('All Crews permissions')).toBeTruthy()
    expect(screen.getByLabelText('All Work orders permissions')).toBeTruthy()
  })

  it('hides role assignment from someone who cannot list roles and keeps existing roles', async () => {
    resetAll(['admin.service_accounts'])
    api.areasApi.list.mockResolvedValue(AREAS)
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-2', name: 'Ada', role_ids: ['r-x'], effective_permissions: ['crews.view'] }))
    api.adminApi.users.update.mockImplementation(async (_i: string, b: UserInput) => makeUser({ ...b, id: 'u-2' }))
    renderUser('u-2')
    await screen.findByRole('heading', { name: 'Ada' })
    expect(api.adminApi.roles.list).not.toHaveBeenCalled()
    expect(screen.getByText(/Roles are hidden/)).toBeTruthy()
    expect(screen.queryByRole('option', { name: /Analyst/ })).toBeNull()
    // The server's figure is shown since roles cannot be resolved
    expect(screen.getByText('Effective permissions · 1')).toBeTruthy()
    type('Name', 'Ada 2')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(lastBody(api.adminApi.users.update, 1).role_ids).toEqual(['r-x']))
  })
})

describe('password panel', () => {
  const adminUser = makeUser({ id: 'u-2', name: 'Ada', effective_permissions: ['admin.users', 'crews.view'], has_password: false })
  async function open(u = adminUser) {
    api.adminApi.users.get.mockResolvedValue(u)
    renderUser(u.id)
    await screen.findByRole('heading', { name: u.name })
  }

  it('is offered only for people who hold admin.users themselves', async () => {
    await open(makeUser({ id: 'u-3', name: 'Plain', effective_permissions: ['crews.view'] }))
    expect(screen.queryByText('Administrator password')).toBeNull()
  })

  it('is never shown for service accounts', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 's-1', kind: 'service', name: 'Bot', effective_permissions: ['admin.users'] }))
    api.adminApi.users.apiKeys.mockResolvedValue(keyPage([]))
    renderService('s-1')
    await screen.findByRole('heading', { name: 'Bot' })
    expect(screen.queryByText('Administrator password')).toBeNull()
  })

  it('sets a password after validating length and confirmation', async () => {
    api.adminApi.users.setPassword.mockResolvedValue(undefined)
    await open()
    expect(screen.getByText(/No password is set\./)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Set password' }))
    expect(screen.getByText('Use at least 12 characters')).toBeTruthy()
    type('Password', 'x'.repeat(73))
    fireEvent.click(screen.getByRole('button', { name: 'Set password' }))
    expect(screen.getByText('Use at most 72 characters')).toBeTruthy()
    type('Password', 'a-long-enough-password')
    type('Confirm', 'different-password!!')
    fireEvent.click(screen.getByRole('button', { name: 'Set password' }))
    expect(screen.getByText('Passwords do not match')).toBeTruthy()
    expect(api.adminApi.users.setPassword).not.toHaveBeenCalled()
    type('Confirm', 'a-long-enough-password')
    fireEvent.click(screen.getByRole('button', { name: 'Set password' }))
    await waitFor(() => expect(api.adminApi.users.setPassword).toHaveBeenCalledWith('u-2', 'a-long-enough-password'))
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Password set')
    // The panel flips to "change" mode and wipes the typed secrets
    expect(await screen.findByRole('button', { name: 'Change password' })).toBeTruthy()
    expect((screen.getByLabelText('New password') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('Confirm') as HTMLInputElement).value).toBe('')
  })

  it('reports a server failure when setting the password and keeps the form', async () => {
    api.adminApi.users.setPassword.mockRejectedValue(apiError(422, 'too common', 'validation', ['password: too common']))
    await open()
    type('Password', 'a-long-enough-password')
    type('Confirm', 'a-long-enough-password')
    fireEvent.click(screen.getByRole('button', { name: 'Set password' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not set the password', expect.anything()))
    expect((screen.getByLabelText('Password') as HTMLInputElement).value).toBe('a-long-enough-password')
  })

  it('changes and then clears an existing password', async () => {
    api.adminApi.users.setPassword.mockResolvedValue(undefined)
    api.adminApi.users.clearPassword.mockResolvedValue(undefined)
    await open(makeUser({ ...adminUser, has_password: true }))
    expect(screen.getByText(/A password is set\./)).toBeTruthy()
    type('New password', 'another-long-password')
    type('Confirm', 'another-long-password')
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Password changed'))
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    expect(api.adminApi.users.clearPassword).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.adminApi.users.clearPassword).toHaveBeenCalledWith('u-2'))
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Password removed')
    expect(await screen.findByRole('button', { name: 'Set password' })).toBeTruthy()
  })

  it('reports a failure to clear the password and leaves it set', async () => {
    api.adminApi.users.clearPassword.mockRejectedValue(apiError(403, 'forbidden'))
    await open(makeUser({ ...adminUser, has_password: true }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not remove the password', expect.anything()))
    expect(screen.getByRole('button', { name: 'Change password' })).toBeTruthy()
  })
})

describe('session revocation', () => {
  async function open() {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-2', name: 'Ada' }))
    renderUser('u-2')
    await screen.findByRole('heading', { name: 'Ada' })
  }

  it.each([[1, 'Signed out of 1 session'], [3, 'Signed out of 3 sessions'], [0, 'Signed out of 0 sessions']])('ends sessions (%i)', async (ended, message) => {
    api.adminApi.users.revokeSessions.mockResolvedValue({ ended })
    await open()
    expect(screen.getByText(/API keys are not affected/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Sign out everywhere' }))
    expect(api.adminApi.users.revokeSessions).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith(message))
    expect(api.adminApi.users.revokeSessions).toHaveBeenCalledWith('u-2')
  })

  it('reports failure', async () => {
    api.adminApi.users.revokeSessions.mockRejectedValue(apiError(403, 'forbidden'))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Sign out everywhere' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not sign them out', expect.anything()))
  })
})

describe("a person's API keys", () => {
  it('lists the keys they created and lets an admin revoke them, but never create one for them', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-7', name: 'Pat' }))
    api.adminApi.users.apiKeys.mockResolvedValue(keyPage([{ id: 'k-9', user_id: 'u-7', name: 'laptop', prefix: 'pat99999' }]))
    api.adminApi.users.revokeApiKey.mockResolvedValue(undefined)
    renderUser('u-7')
    expect(await screen.findByText('csk_pat99999_…')).toBeTruthy()
    expect(screen.getByText('Personal API keys')).toBeTruthy()
    expect(api.adminApi.users.apiKeys).toHaveBeenCalledWith('u-7', { limit: 20, offset: 0 }, expect.anything())
    expect(screen.queryByRole('button', { name: 'New key' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Revoke' }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.adminApi.users.revokeApiKey).toHaveBeenCalledWith('u-7', 'k-9'))
  })

  it('shows "Cannot revoke" where the server says the admin lacks something the person holds', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-7', name: 'Pat' }))
    api.adminApi.users.apiKeys.mockResolvedValue(keyPage([{ id: 'k-9', user_id: 'u-7', prefix: 'pat99999', can_revoke: false }]))
    renderUser('u-7')
    expect(await screen.findByText('Cannot revoke')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull()
  })

  it('pages through many keys', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-7', name: 'Pat' }))
    api.adminApi.users.apiKeys.mockResolvedValue(keyPage([{ id: 'k-9', user_id: 'u-7', prefix: 'pat99999' }], { total: 45, limit: 20 }))
    renderUser('u-7')
    await screen.findByText('csk_pat99999_…')
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    await waitFor(() => expect(api.adminApi.users.apiKeys).toHaveBeenLastCalledWith('u-7', { limit: 20, offset: 20 }, expect.anything()))
  })

  it('says when they have none', async () => {
    api.adminApi.users.get.mockResolvedValue(makeUser({ id: 'u-8', name: 'Sam' }))
    renderUser('u-8')
    expect(await screen.findByText('This person has no API keys.')).toBeTruthy()
  })
})
