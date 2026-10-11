import { expect, test } from '@playwright/test'
import { killGroup } from '../harness/proc.ts'
import { startServer, waitHealthy } from '../harness/server.ts'
import { ADMIN, ROLE_USERS, SEED, statePath } from '../support/accounts.ts'
import { apiAnon, apiAs, findByName, unwrap } from '../support/api.ts'
import { submitLogin, toast } from '../support/ui.ts'

const WRONG = "That email and password don't match an administrator account."

test.describe('local login', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a wrong password shows the same message as an unknown account', async ({ page }) => {
    await page.goto('/login')
    expect(await submitLogin(page, ADMIN.email, 'definitely-not-the-password')).toBe(WRONG)
    expect(await submitLogin(page, 'nobody@e2e.test', 'definitely-not-the-password')).toBe(WRONG)
    await expect(page).toHaveURL(/\/login/)
  })

  test('empty fields are caught before the server is asked', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByRole('alert')).toContainText('Enter your email and password.')
  })

  test('only administrators can use a password; a viewer cannot be given one', async ({ page }) => {
    const admin = await apiAs('admin')
    const viewer = await findByName<{ id: string; name: string }>(admin, '/api/admin/users', ROLE_USERS.viewer.name)
    const res = await admin.put(`/api/admin/users/${viewer.id}/password`, { data: { password: 'Viewer-Passw0rd-123!' } })
    expect(res.status()).toBe(422)
    expect(JSON.stringify(await res.json())).toContain('only administrators')
    await admin.dispose()

    await page.goto('/login')
    expect(await submitLogin(page, ROLE_USERS.viewer.email, 'Viewer-Passw0rd-123!')).toBe(WRONG)
  })

  test('five wrong passwords lock the account without revealing it to a guesser', async ({ page, browser }) => {
    // A throw-away administrator, so the main account is never locked
    const admin = await apiAs('admin')
    const user = await unwrap<{ id: string }>(
      await admin.post('/api/admin/users', {
        data: { kind: 'human', email: 'lockme@e2e.test', name: 'Lock Me', status: 'active', all_areas: true, area_ids: [], role_ids: [SEED.administratorRoleId], permissions: [] },
      }),
      201,
    )
    const password = 'Lockout-Passw0rd-1!'
    expect((await admin.put(`/api/admin/users/${user.id}/password`, { data: { password } })).status()).toBe(204)

    await page.goto('/login')
    for (let i = 1; i <= 5; i++) expect(await submitLogin(page, 'lockme@e2e.test', `wrong-password-${i}`)).toBe(WRONG)
    // Locked now: another wrong guess still gets the generic answer, nothing says the account exists or is locked
    const guess = await submitLogin(page, 'lockme@e2e.test', 'wrong-password-6')
    expect(guess).toBe(WRONG)
    expect(guess).not.toMatch(/lock/i)
    // The right password is refused too while locked, and the person is told why
    expect(await submitLogin(page, 'lockme@e2e.test', password)).toContain('locked for 15 minutes')
    await expect(page).toHaveURL(/\/login/)

    // Every failure is in the audit log
    const ctx = await browser.newContext({ storageState: statePath('admin') })
    const auditPage = await ctx.newPage()
    await auditPage.goto('/admin/audit')
    await expect(auditPage.getByRole('row').filter({ hasText: 'Sign-in failed' }).filter({ hasText: 'lockme@e2e.test' }).first()).toBeVisible()
    await ctx.close()
    // Remove the throw-away administrator: the last-administrator checks in the admin spec need exactly one
    expect((await admin.delete(`/api/admin/users/${user.id}`)).status()).toBe(204)
    await admin.dispose()
  })

  test('the sign-in settings page reports password sign-in as enabled', async ({ browser }) => {
    const ctx = await browser.newContext({ storageState: statePath('admin') })
    const page = await ctx.newPage()
    await page.goto('/admin/access')
    await expect(page.getByRole('heading', { name: 'Sign-in settings' })).toBeVisible()
    await expect(page.getByText('Administrator password sign-in')).toBeVisible()
    await expect(page.getByText('enabled', { exact: true })).toBeVisible()
    await expect(page.getByText('No single sign-on provider configured')).toBeVisible()
    await ctx.close()
  })

  test('with password sign-in turned off the form is gone and the API refuses it', async ({ page }) => {
    // STUDIO_LOCAL_LOGIN is a server setting, not a switch in Admin > Access, so a second instance runs with it off
    const port = 8093
    const pid = await startServer({ port, overrides: { STUDIO_LOCAL_LOGIN: 'false' }, logName: 'server-no-local-login.log' })
    try {
      await waitHealthy(port)
      const origin = `http://127.0.0.1:${port}`
      await page.goto(`${origin}/login`)
      await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
      await expect(page.getByText('No sign-in method is configured yet.')).toBeVisible()
      await expect(page.getByLabel('Password')).toHaveCount(0)
      await expect(page.getByRole('button', { name: 'Sign in' })).toHaveCount(0)

      const api = await apiAnon()
      const res = await api.post(`${origin}/api/auth/login`, { data: { email: ADMIN.email, password: ADMIN.password } })
      expect(res.status()).toBe(404)
      const status = await (await api.get(`${origin}/api/auth/status`)).json()
      expect(status.data.local_login).toBe(false)
      await api.dispose()
    } finally {
      await killGroup(pid)
    }
  })
})

test.describe('sign-in settings (Admin > Sign-in settings)', () => {
  test('auto-provisioning and callback hosts can be changed and saved', async ({ page }) => {
    await page.goto('/admin/access')
    const save = page.getByRole('button', { name: 'Save sign-in settings' })
    await expect(save).toBeDisabled()

    await page.getByRole('switch').first().click()
    await page.getByLabel('Role for automatically created accounts').selectOption({ label: 'Viewer' })
    await expect(save).toBeEnabled()
    await save.click()
    await expect(toast(page, 'Sign-in settings saved')).toBeVisible()

    await page.reload()
    await expect(page.getByRole('switch').first()).toHaveAttribute('aria-checked', 'true')
    await expect(page.getByLabel('Role for automatically created accounts')).toHaveValue(/.+/)

    // put it back so later specs start from the defaults
    await page.getByLabel('Role for automatically created accounts').selectOption('')
    await page.getByRole('switch').first().click()
    await save.click()
    await expect(toast(page, 'Sign-in settings saved')).toBeVisible()
  })
})
