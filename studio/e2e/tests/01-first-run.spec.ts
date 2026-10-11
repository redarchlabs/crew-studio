import { expect, test } from '@playwright/test'
import fs from 'node:fs'
import { ADMIN, statePath } from '../support/accounts.ts'
import { readState } from '../harness/state.ts'

// Order matters: the first-run screen only works once, so these tests build on each other.
test.describe.configure({ mode: 'serial' })

test.describe('first run', () => {
  let token: string
  test.beforeAll(() => {
    token = readState().setupToken
  })

  test('a visit before setup is sent to /setup', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveURL(/\/setup/)
    await expect(page.getByRole('heading', { name: 'Set up Crew Studio' })).toBeVisible()
    await page.goto('/login')
    await expect(page).toHaveURL(/\/setup/)
  })

  test('setup rejects a short password and a mismatched confirmation', async ({ page }) => {
    await page.goto(`/setup?token=${token}`)
    await expect(page.getByText('Setup token found in the link.')).toBeVisible()
    await page.getByLabel('Your name').fill(ADMIN.name)
    await page.getByLabel('Email').fill(ADMIN.email)
    await page.getByLabel('Password', { exact: true }).fill('short-pw')
    await page.getByLabel('Confirm password').fill('short-pw')
    await page.getByRole('button', { name: 'Create administrator' }).click()
    await expect(page.getByText('Use at least 12 characters')).toBeVisible()
    await expect(page).toHaveURL(/\/setup/)

    await page.getByLabel('Password', { exact: true }).fill(ADMIN.password)
    await page.getByLabel('Confirm password').fill(`${ADMIN.password}x`)
    await page.getByRole('button', { name: 'Create administrator' }).click()
    await expect(page.getByText('Passwords do not match')).toBeVisible()
  })

  test('the server enforces the password policy too', async ({ request }) => {
    const res = await request.post('/api/auth/setup', {
      data: { token, email: ADMIN.email, name: ADMIN.name, password: 'too-short' },
    })
    expect(res.status()).toBe(422)
    const body = await res.json()
    expect(JSON.stringify(body)).toContain('at least 12 characters')
  })

  test('a wrong setup token is refused and the token field appears', async ({ page }) => {
    await page.goto('/setup?token=not-the-token')
    await page.getByLabel('Your name').fill(ADMIN.name)
    await page.getByLabel('Email').fill(ADMIN.email)
    await page.getByLabel('Password', { exact: true }).fill(ADMIN.password)
    await page.getByLabel('Confirm password').fill(ADMIN.password)
    await page.getByRole('button', { name: 'Create administrator' }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'setup token is not valid' })).toBeVisible()
    await expect(page.getByLabel('Setup token')).toBeVisible()
  })

  test('setup with the printed token creates the administrator and signs them in', async ({ page }) => {
    await page.goto('/setup')
    // No token in the link: the field is shown and the person pastes it
    await page.getByLabel('Setup token').fill(token)
    await page.getByLabel('Your name').fill(ADMIN.name)
    await page.getByLabel('Email').fill(ADMIN.email)
    await page.getByLabel('Password', { exact: true }).fill(ADMIN.password)
    await page.getByLabel('Confirm password').fill(ADMIN.password)
    await page.getByRole('button', { name: 'Create administrator' }).click()

    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
    await expect(page.getByText(ADMIN.email)).toBeVisible()
    // Admins see every section of the sidebar
    for (const label of ['Work orders', 'Runs', 'Crews', 'People', 'Audit log', 'API explorer']) {
      await expect(page.getByRole('navigation').getByRole('link', { name: label })).toBeVisible()
    }
    await page.context().storageState({ path: statePath('admin') })
    expect(fs.existsSync(statePath('admin'))).toBe(true)
  })

  test('/setup is unusable afterwards', async ({ browser, request }) => {
    // Signed in: the screen bounces to the app
    const signedIn = await browser.newContext({ storageState: statePath('admin') })
    const page = await signedIn.newPage()
    await page.goto('/setup')
    await expect(page).toHaveURL(/\/$/)
    await signedIn.close()

    // Signed out: it bounces to the login page
    const anon = await browser.newContext()
    const anonPage = await anon.newPage()
    await anonPage.goto('/setup')
    await expect(anonPage).toHaveURL(/\/login/)
    await anon.close()

    // The API refuses a second setup, even with the old token
    const res = await request.post('/api/auth/setup', {
      data: { token, email: 'second@e2e.test', name: 'Second', password: 'Another-Passw0rd-1!' },
    })
    expect(res.status()).toBe(403)
    const status = await (await request.get('/api/auth/status')).json()
    expect(status.data.setup_required).toBe(false)
  })

  test('an unauthenticated visit to / redirects to /login, and a deep link is remembered', async ({ browser }) => {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    await page.goto('/')
    await expect(page).toHaveURL(/\/login\?return_to=%2F$/)
    await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
    await page.goto('/crews')
    await expect(page).toHaveURL(/\/login\?return_to=%2Fcrews/)
    await ctx.close()
  })

  test('sign in returns to the page asked for, and sign out ends the session', async ({ browser }) => {
    const ctx = await browser.newContext()
    const page = await ctx.newPage()
    await page.goto('/crews')
    await page.getByLabel('Email').fill(ADMIN.email)
    await page.getByLabel('Password').fill(ADMIN.password)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL(/\/crews$/)
    await expect(page.getByRole('heading', { name: 'Crews' })).toBeVisible()

    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL(/\/login/)
    // The session cookie is gone server-side as well
    const me = await page.request.get('/api/auth/me')
    expect(me.status()).toBe(401)
    await page.goto('/crews')
    await expect(page).toHaveURL(/\/login\?return_to=%2Fcrews/)
    await ctx.close()
  })
})
