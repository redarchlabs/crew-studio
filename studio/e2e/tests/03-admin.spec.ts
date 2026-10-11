import { expect, test, type Page } from '@playwright/test'
import { ADMIN } from '../support/accounts.ts'
import { apiAnon, apiAs, findByName, unwrap } from '../support/api.ts'
import { insertSession, cookieState } from '../support/sessions.ts'
import { confirm, nav, row, toast } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })

const LAST_ADMIN_MESSAGE = 'At least one active person must keep the Manage users permission.'
const AREA = 'QA Sandbox'
const AREA_RENAMED = 'QA Lab'
const UI_USERS = [
  { name: 'Ui Builder', email: 'ui-builder@e2e.test', role: 'Builder', areas: ['Finance'] },
  { name: 'Ui Operator', email: 'ui-operator@e2e.test', role: 'Operator', areas: ['IT'] },
  { name: 'Ui Viewer', email: 'ui-viewer@e2e.test', role: 'Viewer', areas: ['Finance', 'IT'] },
]

/** Picks options in a MultiSelect inside the panel with this title. */
async function pick(page: Page, panel: string, names: (string | RegExp)[], on = true): Promise<void> {
  const scope = page.locator('section').filter({ has: page.getByText(panel, { exact: true }) })
  for (const name of names) {
    const opt = scope.getByRole('option', { name: typeof name === 'string' ? new RegExp(`^${name}\\b`) : name })
    if ((await opt.getAttribute('aria-selected')) !== String(on)) await opt.click()
  }
}

async function invite(page: Page, u: (typeof UI_USERS)[number]): Promise<void> {
  await page.goto('/admin/users/new')
  await page.getByLabel('Name').fill(u.name)
  await page.getByLabel('Email').fill(u.email)
  await pick(page, 'Roles and areas', [u.role, ...u.areas])
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(toast(page, 'Created person')).toBeVisible()
  await expect(page).toHaveURL(/\/admin\/users\/[0-9a-f-]{36}$/)
  await expect(page.getByRole('heading', { name: u.name })).toBeVisible()
}

test.describe('the last administrator', () => {
  test('cannot be disabled, demoted or deleted', async ({ page }) => {
    await page.goto('/admin/users')
    await page.getByRole('link', { name: ADMIN.name }).click()
    await expect(page.getByRole('heading', { name: ADMIN.name })).toBeVisible()
    // You cannot even offer to delete yourself
    await expect(page.getByRole('button', { name: 'Delete' })).toHaveCount(0)
    const me = await unwrap<{ id: string }>(await page.request.get('/api/auth/me'))
    const del = await page.request.delete(`/api/admin/users/${me.id}`)
    expect(del.status()).toBe(422)
    expect(JSON.stringify(await del.json())).toContain('cannot delete your own account')

    // Disabling the only administrator is refused with an explanation that stays on screen
    await page.getByLabel('Status').selectOption('disabled')
    await expect(page.getByText('You are about to disable your own account.')).toBeVisible()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: LAST_ADMIN_MESSAGE })).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Status')).toHaveValue('active')

    // So is taking the Administrator role away
    await pick(page, 'Roles and areas', ['Administrator'], false)
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: LAST_ADMIN_MESSAGE })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('option', { name: /^Administrator/ })).toHaveAttribute('aria-selected', 'true')
  })
})

test.describe('the last administrator', () => {
  test('the banner is titled "This is the last administrator"', async ({ page }) => {
    await page.goto('/admin/users')
    await page.getByRole('link', { name: ADMIN.name }).click()
    // The account must have loaded (and the change registered) before Save is enabled; a change made while the
    // editor is still loading is replaced by the loaded account, so retry until it sticks
    await expect(page.getByLabel('Name')).toHaveValue(ADMIN.name)
    await expect(async () => {
      await page.getByLabel('Status').selectOption('disabled')
      await expect(page.getByText('You are about to disable your own account.')).toBeVisible({ timeout: 1000 })
    }).toPass({ timeout: 10_000 })
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'This is the last administrator' })).toBeVisible()
    await expect(page.getByRole('alert')).toContainText('Give that permission to another active person first')
  })
})

test.describe('functional areas', () => {
  test('create, reject a duplicate, rename and delete an empty area', async ({ page }) => {
    await page.goto('/admin/areas')
    await expect(page.getByRole('heading', { name: 'Functional areas' })).toBeVisible()
    for (const seeded of ['Finance', 'IT', 'Training', 'HR']) await expect(row(page, seeded).first()).toBeVisible()

    await page.getByRole('button', { name: 'Add area' }).click()
    await page.getByLabel('Area name').fill(AREA)
    await page.getByLabel('Area description').fill('Created by the e2e suite')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, `Added ${AREA}`)).toBeVisible()
    await expect(row(page, AREA)).toContainText('0 crews · 0 agents · 0 tasks · 0 accounts')

    await page.getByRole('button', { name: 'Add area' }).click()
    await page.getByLabel('Area name').fill('finance')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByText('Another area already has this name')).toBeVisible()
    await page.getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('button', { name: `Rename ${AREA}` }).click()
    await page.getByLabel('Area name').fill(AREA_RENAMED)
    await page.getByLabel('Area name').press('Enter')
    await expect(toast(page, `Saved ${AREA_RENAMED}`)).toBeVisible()
    await expect(row(page, AREA_RENAMED)).toBeVisible()
    await expect(row(page, AREA)).toHaveCount(0)

    await confirm(row(page, AREA_RENAMED).getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${AREA_RENAMED}`)).toBeVisible()
    await expect(row(page, AREA_RENAMED)).toHaveCount(0)
  })

  test('an area that still holds crews cannot be deleted', async ({ page }) => {
    await page.goto('/admin/areas')
    const finance = row(page, 'Finance').first()
    await expect(finance).toContainText(/1 crew · \d+ agents · \d+ tasks/)
    await expect(finance.getByRole('button', { name: 'Delete' })).toBeDisabled()
    await expect(finance.locator('span[title]')).toHaveAttribute('title', /Holds \d+ crews, agents or tasks/)

    // The server refuses too, whatever the UI does
    const areas = await unwrap<{ id: string; name: string }[]>(await page.request.get('/api/areas'))
    const id = areas.find((a) => a.name === 'Finance')!.id
    const res = await page.request.delete(`/api/areas/${id}`)
    expect(res.status()).toBe(409)
    expect(JSON.stringify(await res.json())).toContain('Move them to another area first')
  })
})

test.describe('people', () => {
  test('invite a builder, an operator and a viewer with their areas', async ({ page }) => {
    for (const u of UI_USERS) await invite(page, u)

    await page.goto('/admin/users')
    for (const u of UI_USERS) {
      const r = row(page, u.email)
      await expect(r).toContainText(u.name)
      await expect(r).toContainText(u.role)
      for (const area of u.areas) await expect(r).toContainText(area)
      await expect(r).toContainText('not linked yet')
    }
    // The role decides what each can do: the operator holds run permissions but no edit ones
    await page.getByRole('link', { name: 'Ui Operator' }).click()
    const effective = page.locator('section').filter({ hasText: 'Effective permissions' })
    await expect(effective).toContainText('Run crews')
    await expect(effective).not.toContainText('Edit crews')
    await expect(page.getByText('No functional areas:')).toHaveCount(0)
  })

  test('the people list can be filtered', async ({ page }) => {
    await page.goto('/admin/users')
    await page.getByPlaceholder('Filter by name or email…').fill('ui-viewer')
    await expect(page.getByRole('row').filter({ hasText: 'ui-viewer@e2e.test' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: 'ui-builder@e2e.test' })).toHaveCount(0)
  })

  test('the form refuses a bad email and an unfinished person warns about areas', async ({ page }) => {
    await page.goto('/admin/users/new')
    await page.getByLabel('Name').fill('Nobody Yet')
    await page.getByLabel('Email').fill('not-an-email')
    await expect(page.getByText('No functional areas: they will not see any crews')).toBeVisible()
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Enter the email address they sign in with' })).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/users\/new$/)
  })

  test('a duplicate email is refused', async ({ page }) => {
    await page.goto('/admin/users/new')
    await page.getByLabel('Name').fill('Copycat')
    await page.getByLabel('Email').fill(UI_USERS[0].email)
    await pick(page, 'Roles and areas', ['Viewer', 'Finance'])
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'That conflicts with another account' })).toBeVisible()
  })

  test('an administrator gets a password; weak ones are refused; it can sign in; it can be removed', async ({ page, browser }) => {
    await page.goto('/admin/users/new')
    await page.getByLabel('Name').fill('Second Admin')
    await page.getByLabel('Email').fill('second-admin@e2e.test')
    await pick(page, 'Roles and areas', ['Administrator'])
    await page.getByRole('switch', { name: /All areas/ }).click()
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Second Admin' })).toBeVisible()

    const panel = page.locator('section').filter({ hasText: 'Administrator password' })
    await expect(panel).toContainText('No password is set.')
    await panel.getByLabel('Password', { exact: true }).fill('too-short')
    await panel.getByLabel('Confirm').fill('too-short')
    await panel.getByRole('button', { name: 'Set password' }).click()
    await expect(panel.getByText('Use at least 12 characters')).toBeVisible()

    const password = 'Second-Admin-Passw0rd!'
    await panel.getByLabel('Password', { exact: true }).fill(password)
    await panel.getByLabel('Confirm').fill(`${password}?`)
    await panel.getByRole('button', { name: 'Set password' }).click()
    await expect(panel.getByText('Passwords do not match')).toBeVisible()
    await panel.getByLabel('Confirm').fill(password)
    await panel.getByRole('button', { name: 'Set password' }).click()
    await expect(toast(page, 'Password set')).toBeVisible()
    await expect(panel).toContainText('A password is set.')

    // The new administrator can use it
    const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const other = await ctx.newPage()
    await other.goto('/login')
    await other.getByLabel('Email').fill('second-admin@e2e.test')
    await other.getByLabel('Password').fill(password)
    await other.getByRole('button', { name: 'Sign in' }).click()
    await expect(other.getByRole('heading', { name: 'Overview' })).toBeVisible()
    await ctx.close()

    // Remove it again: password sign-in stops working
    await confirm(panel.getByRole('button', { name: 'Remove' }))
    await expect(toast(page, 'Password removed')).toBeVisible()
    await expect(panel).toContainText('No password is set.')
    // With two administrators the other one is no longer the last, so this account can now be deleted
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, 'Deleted Second Admin')).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/users$/)
  })

  test('disabling a person blocks their session', async ({ page, browser }) => {
    const admin = await apiAs('admin')
    const u = await unwrap<{ id: string }>(
      await admin.post('/api/admin/users', { data: { kind: 'human', email: 'to-disable@e2e.test', name: 'To Disable', status: 'active', all_areas: false, area_ids: [], role_ids: [], permissions: [] } }),
      201,
    )
    const ctx = await browser.newContext({ storageState: cookieState(insertSession(u.id)) as never })
    const them = await ctx.newPage()
    expect((await them.request.get('/api/auth/me')).status()).toBe(200)

    await page.goto(`/admin/users/${u.id}`)
    await page.getByLabel('Status').selectOption('disabled')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved person')).toBeVisible()
    expect((await them.request.get('/api/auth/me')).status()).toBe(401)
    await ctx.close()
    await admin.dispose()
  })
})

test.describe('roles', () => {
  const ROLE = 'E2E Auditor'

  test('the built-in Administrator role is read-only', async ({ page }) => {
    await page.goto('/admin/roles')
    await expect(row(page, 'Administrator')).toContainText('built-in')
    await page.getByRole('link', { name: 'Administrator' }).click()
    await expect(page.getByText(/always holds every permission/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save' })).toHaveCount(0)
    await expect(page.getByLabel('View crews')).toBeDisabled()
  })

  test('create a custom role, edit its permissions, give it to someone, then delete it', async ({ page }) => {
    await page.goto('/admin/roles/new')
    await page.getByLabel('Name').fill(ROLE)
    await page.getByLabel('Description').fill('Reads runs and history')
    await page.getByLabel('View runs').check()
    await page.getByLabel('View history').check()
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created role')).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/roles\/[0-9a-f-]{36}$/)
    await expect(page.locator('section').filter({ hasText: 'Effective permissions · 2' })).toBeVisible()

    // Edit: add a permission with the per-feature "all" box and drop one
    await page.getByLabel('All Crews permissions').check()
    await page.getByLabel('View history').uncheck()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved role')).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Run crews')).toBeChecked()
    await expect(page.getByLabel('View history')).not.toBeChecked()
    // The six Crews permissions (view, create, edit, delete, run, chat) plus View runs
    await expect(page.locator('section').filter({ hasText: 'Effective permissions · 7' })).toBeVisible()

    // A role that grants the admin screens cannot be given to someone without them... but any holder works for our own list
    await expect(page.getByText('0 accounts hold this role.')).toBeVisible()
    const admin = await apiAs('admin')
    const viewer = await findByName<{ id: string; name: string }>(admin, '/api/admin/users', UI_USERS[2].name)
    const roles = await unwrap<{ id: string; name: string }[]>(await admin.get('/api/admin/roles'))
    const roleId = roles.find((r) => r.name === ROLE)!.id
    const full = await unwrap<Record<string, unknown>>(await admin.get(`/api/admin/users/${viewer.id}`))
    await unwrap(await admin.put(`/api/admin/users/${viewer.id}`, { data: { ...full, role_ids: [...(full.role_ids as string[]), roleId] } }), 200)
    await page.reload()
    await expect(page.getByText('1 account holds this role.')).toBeVisible()

    // The viewer now also runs crews
    const effective = await unwrap<{ effective_permissions: string[] }>(await admin.get(`/api/admin/users/${viewer.id}`))
    expect(effective.effective_permissions).toContain('crews.run')
    await admin.dispose()

    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${ROLE}`)).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/roles$/)
    await expect(page.getByRole('link', { name: ROLE })).toHaveCount(0)
  })

  test('a role needs a name', async ({ page }) => {
    await page.goto('/admin/roles/new')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByText(/Name is required/i).first()).toBeVisible()
  })
})

test.describe('service accounts and API keys', () => {
  test('create an account, create a key (secret shown once), use it, revoke it', async ({ page }) => {
    await page.goto('/admin/service-accounts/new')
    await page.getByLabel('Name').fill('E2E Integration')
    await pick(page, 'Roles and areas', ['Viewer', 'Finance'])
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created service account')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'E2E Integration' })).toBeVisible()

    const keys = page.locator('section').filter({ has: page.getByText('API keys', { exact: true }) }).first()
    await expect(keys).toContainText('No API keys yet')
    await keys.getByRole('button', { name: 'New key' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: 'Create key' }).click()
    await expect(dialog.getByText('Give the key a name')).toBeVisible()
    await dialog.getByLabel('Name').fill('ci pipeline')
    await dialog.getByRole('button', { name: 'Create key' }).click()

    const secretDialog = page.getByRole('dialog')
    await expect(secretDialog.getByRole('alert')).toContainText(/cannot be shown again|will not be shown again/)
    const secret = (await secretDialog.locator('code').first().innerText()).trim()
    expect(secret).toMatch(/^csk_[A-Za-z0-9]+_/)
    await secretDialog.getByRole('button', { name: 'I have saved it' }).click()

    // After closing (and after a reload) only the masked prefix remains
    await page.reload()
    const table = page.locator('section').filter({ has: page.getByText('API keys', { exact: true }) }).first()
    await expect(table.getByRole('row').filter({ hasText: 'ci pipeline' })).toContainText('active')
    await expect(table).not.toContainText(secret)

    // The key authenticates as the service account
    const api = await apiAnon(secret)
    const me = await unwrap<{ kind: string; name: string; via: string }>(await api.get('/api/auth/me'))
    expect(me).toMatchObject({ kind: 'service', name: 'E2E Integration', via: 'api_key' })

    await confirm(table.getByRole('row').filter({ hasText: 'ci pipeline' }).getByRole('button', { name: 'Revoke' }))
    await expect(toast(page, 'Revoked ci pipeline')).toBeVisible()
    await expect(table.getByRole('row').filter({ hasText: 'ci pipeline' })).toContainText('revoked')
    expect((await api.get('/api/auth/me')).status()).toBe(401)
    await api.dispose()

    await page.goto('/admin/service-accounts')
    await expect(row(page, 'E2E Integration')).toContainText('Viewer')
  })

  test('a key limited to the REST API and read-only permissions gets only those', async ({ page }) => {
    const admin = await apiAs('admin')
    const sa = await unwrap<{ id: string; effective_permissions: string[] }>(
      await admin.post('/api/admin/users', { data: { kind: 'service', name: 'E2E Limited', status: 'active', all_areas: true, area_ids: [], role_ids: [],
        permissions: ['crews.view', 'crews.edit', 'agents.view'] } }),
      201,
    )
    await admin.dispose()
    await page.goto(`/admin/service-accounts/${sa.id}`)
    await page.getByRole('button', { name: 'New key' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name').fill('read only')
    await dialog.getByLabel('MCP', { exact: true }).uncheck()
    await dialog.getByRole('group', { name: 'Permissions' }).getByLabel('Only these').check()
    await dialog.getByRole('button', { name: 'Read only' }).click()
    await dialog.getByRole('button', { name: 'Create key' }).click()
    const secret = (await page.getByRole('dialog').locator('code').first().innerText()).trim()
    await page.getByRole('dialog').getByRole('button', { name: 'I have saved it' }).click()

    const keyRow = page.locator('section').filter({ has: page.getByText('API keys', { exact: true }) }).first().getByRole('row').filter({ hasText: 'read only' })
    await expect(keyRow).toContainText('API only')
    await expect(keyRow).toContainText('2 permissions')

    const api = await apiAnon(secret)
    const me = await unwrap<{ permissions: string[] }>(await api.get('/api/auth/me'))
    expect(me.permissions).toEqual(['crews.view', 'agents.view'])
    expect((await api.get('/api/crews')).status()).toBe(200)
    expect((await api.post('/api/crews', { data: { name: 'not allowed' } })).status()).toBe(403)
    const mcp = await api.post('/mcp', { data: { jsonrpc: '2.0', id: 1, method: 'initialize', params: {} }, headers: { Accept: 'application/json, text/event-stream' } })
    expect(mcp.status()).toBe(403)
    await api.dispose()
  })

  test('an API key needs a name', async ({ page }) => {
    const admin = await apiAs('admin')
    const sa = await unwrap<{ id: string }>(
      await admin.post('/api/admin/users', { data: { kind: 'service', name: 'Keyless', status: 'active', all_areas: false, area_ids: [], role_ids: [], permissions: [] } }),
      201,
    )
    await admin.dispose()
    await page.goto(`/admin/service-accounts/${sa.id}`)
    await page.getByRole('button', { name: 'New key' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Create key' }).click()
    await expect(page.getByRole('dialog').getByRole('alert')).toContainText('Give the key a name')
  })
})

test.describe('sessions, audit log and history', () => {
  test('signing a person out everywhere ends their session and is audited', async ({ page, browser }) => {
    const admin = await apiAs('admin')
    const u = await unwrap<{ id: string }>(
      await admin.post('/api/admin/users', { data: { kind: 'human', email: 'revoke-me@e2e.test', name: 'Revoke Me', status: 'active', all_areas: false, area_ids: [], role_ids: [], permissions: [] } }),
      201,
    )
    const ctx = await browser.newContext({ storageState: cookieState(insertSession(u.id)) as never })
    const them = await ctx.newPage()
    await them.goto('/')
    expect((await them.request.get('/api/auth/me')).status()).toBe(200)

    await page.goto(`/admin/users/${u.id}`)
    const sessions = page.locator('section').filter({ hasText: 'Ends every browser session at once' })
    await confirm(sessions.getByRole('button', { name: 'Sign out everywhere' }))
    await expect(toast(page, 'Signed out of 1 session')).toBeVisible()
    expect((await them.request.get('/api/auth/me')).status()).toBe(401)
    // Their open page falls back to the login screen on the next request
    await them.reload()
    await expect(them).toHaveURL(/\/login/)
    await ctx.close()
    await admin.dispose()

    await page.goto('/admin/audit')
    await expect(row(page, 'Signed out everywhere').first()).toContainText(ADMIN.name)
  })

  test('the audit log lists sign-ins, failures and sign-outs', async ({ page }) => {
    // Make sure there is a failure and a sign-in to see, whatever ran before
    const anon = await apiAnon()
    expect((await anon.post('/api/auth/login', { data: { email: 'audit-probe@e2e.test', password: 'wrong-password-123' } })).status()).toBe(401)
    await anon.dispose()
    await page.goto('/admin/audit')
    await expect(page.getByRole('heading', { name: 'Audit log' })).toBeVisible()
    for (const label of ['First-run setup', 'Signed in', 'Signed out', 'Sign-in failed']) await expect(row(page, label).first()).toBeVisible()
  })

  test('history records who created areas, people, roles and API keys', async ({ page }) => {
    await page.goto('/history')
    await page.getByLabel('Filter by entity type').selectOption('area')
    await expect(row(page, AREA).filter({ hasText: 'create' })).toBeVisible()
    await expect(row(page, AREA_RENAMED).filter({ hasText: 'update' })).toBeVisible()
    await expect(row(page, AREA_RENAMED).filter({ hasText: 'delete' })).toBeVisible()
    await expect(row(page, AREA).first()).toContainText(ADMIN.name)

    await page.getByLabel('Filter by entity type').selectOption('user')
    await expect(row(page, 'Ui Builder').filter({ hasText: 'create' })).toBeVisible()
    // expand a revision to see its snapshot
    await row(page, 'Ui Builder').first().click()
    await expect(page.locator('pre').first()).toContainText('ui-builder@e2e.test')

    await page.getByLabel('Filter by entity type').selectOption('role')
    await expect(row(page, 'E2E Auditor').first()).toBeVisible()
    await page.getByLabel('Filter by entity type').selectOption('api_key')
    await expect(row(page, 'ci pipeline').filter({ hasText: 'create' })).toBeVisible()
  })

  test('the admin sections are in the sidebar for an administrator', async ({ page }) => {
    await page.goto('/')
    for (const label of ['People', 'Service accounts', 'Roles', 'Functional areas', 'Sign-in settings', 'Audit log']) {
      await expect(nav(page).getByRole('link', { name: label })).toBeVisible()
    }
  })
})
