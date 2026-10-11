import { expect, test, type Page } from '@playwright/test'
import { psql } from '../harness/db.ts'
import { readState } from '../harness/state.ts'
import { LIB } from '../support/accounts.ts'
import { apiAs } from '../support/api.ts'
import { ensureLibrary } from '../support/library.ts'
import { confirm, row, toast } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })
test.beforeAll(async () => {
  await ensureLibrary()
})

const NAME = 'E2E Weekday digest'
/** "Next run" as the list shows it: in 30s, in 24m, in 5h or in 2d, depending on how far off 09:00 is when the suite runs. */
const NEXT_RUN = /in \d+[smhd]|due now/
/** The plain-words description under the cron field (a span; the preset <option>s share some of the text). */
const words = (page: Page, text: string | RegExp) => page.locator('span').filter({ hasText: text }).first()
const nextRuns = (page: Page) => page.getByText('Next 5 runs').locator('xpath=..').getByRole('listitem')

async function chooseCrew(page: Page, name: string): Promise<void> {
  await page.getByRole('button', { name: /Choose a crew/ }).click()
  await page.getByPlaceholder('Search…').fill(name)
  await page.getByRole('option', { name: new RegExp(`^${name}\\b`) }).first().click()
}

test.describe('creating a schedule', () => {
  test('required fields are enforced', async ({ page }) => {
    await page.goto('/schedules/new')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'Give the schedule a name.' })).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: 'Choose a crew to run.' })).toBeVisible()
  })

  test('the cron field describes the expression and previews the next five runs in the chosen zone', async ({ page }) => {
    await page.goto('/schedules/new')
    await page.getByLabel('Time zone').selectOption('America/New_York')
    await expect(words(page, /^Weekdays at 09:00$/)).toBeVisible()
    await expect(nextRuns(page)).toHaveCount(5)
    await expect(nextRuns(page).first()).toContainText(/09:00\s?AM\s+E[SD]T/)
    // every preview row is a weekday morning
    for (const li of await nextRuns(page).all()) await expect(li).toContainText(/^(Mon|Tue|Wed|Thu|Fri)/)

    await page.getByLabel('Time zone').selectOption('Asia/Tokyo')
    await expect(nextRuns(page).first()).toContainText(/GMT\+9/)
    await expect(nextRuns(page).first()).toContainText(/09:00\s?AM/)

    await page.getByLabel('Repeats').selectOption({ label: 'Every Monday at 09:00' })
    await expect(words(page, /^Every Monday at 09:00$/)).toBeVisible()
    await expect(nextRuns(page).first()).toContainText(/^Mon/)

    await page.getByLabel('Cron expression').fill('*/15 9-17 * * 1-5')
    await expect(words(page, /^Every 15 minutes, 09:00–17:59, weekdays$/)).toBeVisible()
    await expect(nextRuns(page)).toHaveCount(5)
  })

  test('an invalid expression and one that fires too often are refused', async ({ page }) => {
    await page.goto('/schedules/new')
    const cron = page.getByLabel('Cron expression')
    await cron.fill('nope')
    await expect(page.getByRole('alert').filter({ hasText: 'Use 5 fields' })).toBeVisible()
    await expect(page.getByText('Fix the expression to see upcoming runs.')).toBeVisible()
    await cron.fill('*/2 * * * *')
    await expect(page.getByRole('alert').filter({ hasText: 'more often than every 5 minutes' })).toBeVisible()
    await cron.fill('61 * * * *')
    await expect(page.getByRole('alert').filter({ hasText: 'Use 5 fields' })).toBeVisible()

    // Save is blocked while it is wrong
    await page.getByLabel('Name', { exact: true }).fill('Bad cron')
    await chooseCrew(page, LIB.crew)
    await cron.fill('*/2 * * * *')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'more often than every 5 minutes' })).toBeVisible()
    await expect(page).toHaveURL(/\/schedules\/new$/)
  })

  test('the server enforces the same rules', async () => {
    const api = await apiAs('admin')
    const tooOften = await api.post('/api/schedules/preview', { data: { cron: '*/2 * * * *', timezone: 'UTC' } })
    expect(tooOften.status()).toBe(422)
    expect(JSON.stringify(await tooOften.json())).toMatch(/5 minutes/)
    expect((await api.post('/api/schedules/preview', { data: { cron: 'banana', timezone: 'UTC' } })).status()).toBe(422)
    expect((await api.post('/api/schedules/preview', { data: { cron: '0 9 * * *', timezone: 'Mars/Olympus' } })).status()).toBe(422)
    const ok = await api.post('/api/schedules/preview', { data: { cron: '0 9 * * *', timezone: 'UTC' } })
    expect(ok.status()).toBe(200)
    expect(((await ok.json()).data.next as string[]).length).toBe(5)
    await api.dispose()
  })

  test('create a schedule', async ({ page }) => {
    await page.goto('/schedules/new')
    await page.getByLabel('Name', { exact: true }).fill(NAME)
    await page.getByLabel('Description').fill('Created by the e2e suite')
    await chooseCrew(page, LIB.crew)
    await expect(page.getByLabel(/Topic/)).toHaveValue(LIB.topicDefault)
    await page.getByLabel('Time zone').selectOption('America/New_York')
    await page.getByLabel('Repeats').selectOption({ label: 'Weekdays at 09:00' })
    await page.getByRole('radio', { name: /Allow/ }).click()
    await page.getByLabel('Brief', { exact: true }).fill('Scheduled brief for the digest.')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created schedule')).toBeVisible()
    await expect(page).toHaveURL(/\/schedules\/[0-9a-f-]{36}$/)
    await expect(page.getByRole('heading', { name: NAME })).toBeVisible()
    await expect(page.getByText('E2E Admin').first()).toBeVisible() // runs as

    await page.goto('/schedules')
    const r = row(page, NAME)
    await expect(r).toContainText('Weekdays at 09:00')
    await expect(r).toContainText('0 9 * * 1-5 · America/New_York')
    await expect(r).toContainText(LIB.crew)
    await expect(r).toContainText(NEXT_RUN) // next run
    await expect(r).toContainText('never') // last run
    await expect(r.getByRole('switch')).toHaveAttribute('aria-checked', 'true')
  })
})

test.describe('managing a schedule', () => {
  test('pause and resume from the list', async ({ page }) => {
    await page.goto('/schedules')
    const r = row(page, NAME)
    await r.getByRole('switch').click()
    await expect(toast(page, `Paused ${NAME}`)).toBeVisible()
    await expect(r.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
    await expect(r).toContainText('paused') // no next run while paused
    await page.reload()
    await expect(row(page, NAME).getByRole('switch')).toHaveAttribute('aria-checked', 'false')

    await row(page, NAME).getByRole('switch').click()
    await expect(toast(page, `Enabled ${NAME}`)).toBeVisible()
    await expect(row(page, NAME)).toContainText(NEXT_RUN)
  })

  test('edit the schedule: change the cron and see it described', async ({ page }) => {
    await page.goto('/schedules')
    await page.getByRole('link', { name: NAME }).click()
    await page.getByLabel('Repeats').selectOption({ label: 'Every day at 09:00' })
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved schedule')).toBeVisible()
    await page.goto('/schedules')
    await expect(row(page, NAME)).toContainText('Every day at 09:00')
    await expect(row(page, NAME)).toContainText('0 9 * * *')
  })

  test('run now creates and submits a work order that completes', async ({ page }) => {
    await page.goto('/schedules')
    await page.getByRole('link', { name: NAME }).click()
    await page.getByRole('button', { name: 'Run now' }).click()
    await expect(toast(page, /Started WO-\d+/)).toBeVisible()
    await expect(page).toHaveURL(/\/work-orders\/[0-9a-f-]{36}/)
    const header = page.getByRole('main').locator('header')
    await expect(header).toContainText('schedule') // source chip
    await expect(header.getByText('succeeded', { exact: true }).first()).toBeVisible({ timeout: 120_000 })
    await page.getByRole('tab', { name: /^Brief/ }).click()
    await expect(page.getByText('Scheduled brief for the digest.')).toBeVisible()

    // the schedule shows its last run and its recent work orders
    await header.getByRole('link', { name: 'schedule' }).click()
    await expect(page.getByRole('heading', { name: NAME })).toBeVisible()
    const recent = page.locator('section').filter({ hasText: 'Recent work orders' })
    await expect(recent.getByRole('row').filter({ hasText: NAME })).toBeVisible()
  })

  test('a schedule that is due fires on its own and records the work order', async ({ page }) => {
    test.setTimeout(150_000)
    // The scheduler wakes every 20 s; make the schedule overdue instead of waiting for 09:00
    psql(readState().dbName, `UPDATE schedules SET next_run_at = now() - interval '1 minute' WHERE name = '${NAME}'`)
    await page.goto('/schedules')
    await expect(async () => {
      await page.reload()
      await expect(row(page, NAME)).not.toContainText('never')
      await expect(row(page, NAME).getByRole('link', { name: 'Last work order' })).toBeVisible()
    }).toPass({ timeout: 60_000, intervals: [3000] })
    // it moved on to the next 09:00 and made a work order named after the schedule
    await expect(row(page, NAME)).toContainText(NEXT_RUN)
    await row(page, NAME).getByRole('link', { name: 'Last work order' }).click()
    await expect(page.getByRole('main').locator('header')).toContainText(NAME)
    await expect(page.getByRole('main').locator('header').getByText('succeeded', { exact: true }).first()).toBeVisible({ timeout: 120_000 })
  })

  test('delete the schedule', async ({ page }) => {
    await page.goto('/schedules')
    await page.getByRole('link', { name: NAME }).click()
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${NAME}`)).toBeVisible()
    await expect(page).toHaveURL(/\/schedules$/)
    await expect(page.getByRole('link', { name: NAME })).toHaveCount(0)
  })
})
