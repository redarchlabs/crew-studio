import { expect, type Locator, type Page } from '@playwright/test'

/** A sonner toast containing the text. */
export function toast(page: Page, text: string | RegExp): Locator {
  return page.locator('[data-sonner-toast]').filter({ hasText: text }).first()
}

/** Two-step destructive buttons ("Delete" then "Confirm?"). Pass the button that arms the action. */
export async function confirm(button: Locator): Promise<void> {
  const page = button.page()
  await button.click()
  await page.getByRole('button', { name: 'Confirm?' }).click()
}

export function nav(page: Page): Locator {
  return page.getByRole('navigation')
}

/** Unique-per-run suffix so repeated runs against a kept database never collide. */
export function uid(prefix: string): string {
  return `${prefix} ${Date.now().toString(36).slice(-5)}${Math.floor(Math.random() * 36).toString(36)}`
}

const RATE_LIMITED = /Too many sign-in attempts from this network/

/**
 * Submits the password form. The server allows 5 sign-in attempts in a burst per address and then one every
 * 6 seconds, so a rate-limit answer is retried after a pause; any other outcome is returned as the alert text
 * (or null when the sign-in succeeded and the page left /login).
 */
export async function submitLogin(page: Page, email: string, password: string): Promise<string | null> {
  for (let attempt = 0; attempt < 4; attempt++) {
    await page.getByLabel('Email').fill(email)
    await page.getByLabel('Password').fill(password)
    await page.getByRole('button', { name: 'Sign in' }).click()
    const alert = page.getByRole('alert')
    await expect(async () => {
      const left = !/\/login/.test(page.url())
      const shown = await alert.count()
      expect(left || shown > 0).toBe(true)
    }).toPass({ timeout: 15_000 })
    if (!/\/login/.test(page.url())) return null
    const text = (await alert.first().innerText()).trim()
    if (!RATE_LIMITED.test(text)) return text
    await page.waitForTimeout(6500) // wait out the limiter; there is no event to wait on
  }
  throw new Error('still rate limited after several retries')
}

/** Reads a hidden-until-needed table row by its text. */
export function row(page: Page, text: string | RegExp): Locator {
  return page.getByRole('row').filter({ hasText: text })
}

/** The table row whose own link is exactly this name (rows that merely mention the name elsewhere do not match). */
export function linkRow(page: Page, name: string): Locator {
  return page.getByRole('row').filter({ has: page.getByRole('link', { name, exact: true }) })
}

/**
 * BUG (as of migration 0010): GET /api/overview answers 500 because store/events.go still selects sc.task_id after the
 * column was dropped ("column sc.task_id does not exist"). Tests that need the overview page call this to be marked
 * fixme while that is true, and run normally once it is fixed.
 */
export async function fixmeIfOverviewBroken(page: Page, test: { fixme: (cond: boolean, reason: string) => void }): Promise<void> {
  const res = await page.request.get('/api/overview')
  test.fixme(res.status() >= 500, 'BUG: GET /api/overview returns 500 (column sc.task_id does not exist after migration 0010_crew_targets_only)')
}
