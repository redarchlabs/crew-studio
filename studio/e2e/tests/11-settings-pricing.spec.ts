import { expect, test } from '@playwright/test'
import { statePath } from '../support/accounts.ts'
import { apiAs } from '../support/api.ts'
import { confirm, row, toast } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })

test.describe('settings', () => {
  test('values are validated, saved and persisted', async ({ page }) => {
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    const budget = page.getByLabel('Default budget (USD)')
    const concurrent = page.getByLabel('Max concurrent runs')
    const save = page.getByRole('button', { name: 'Save settings' })
    await expect(save).toBeDisabled()
    const original = await budget.inputValue()
    const originalConcurrent = await concurrent.inputValue()

    await concurrent.fill('11')
    await save.click()
    await expect(page.getByRole('alert').filter({ hasText: 'Whole number from 1 to 10' })).toBeVisible()
    await concurrent.fill('0')
    await budget.fill('0')
    await save.click()
    await expect(page.getByRole('alert').filter({ hasText: 'Must be greater than 0' })).toBeVisible()

    await budget.fill('3.5')
    await concurrent.fill('4')
    await page.getByLabel('Hard-stop multiplier').fill('1.5')
    await page.getByLabel('Free allowance resets').selectOption('day')
    await save.click()
    await expect(toast(page, 'Settings saved')).toBeVisible()

    await page.reload()
    await expect(page.getByLabel('Default budget (USD)')).toHaveValue('3.5')
    await expect(page.getByLabel('Max concurrent runs')).toHaveValue('4')
    await expect(page.getByLabel('Hard-stop multiplier')).toHaveValue('1.5')
    await expect(page.getByLabel('Free allowance resets')).toHaveValue('day')

    // history records the change
    await page.getByRole('main').getByRole('link', { name: 'History' }).click()
    await expect(page).toHaveURL(/entity_type=settings/)
    await expect(page.getByRole('row').filter({ hasText: 'update' }).first()).toBeVisible()

    // restore
    await page.goto('/settings')
    await page.getByLabel('Default budget (USD)').fill(original)
    await page.getByLabel('Max concurrent runs').fill(originalConcurrent)
    await page.getByLabel('Hard-stop multiplier').fill('2')
    await page.getByLabel('Free allowance resets').selectOption('month')
    await page.getByRole('button', { name: 'Save settings' }).click()
    await expect(toast(page, 'Settings saved')).toBeVisible()
  })

  test('a builder can see settings but not change them', async ({ browser }) => {
    const ctx = await browser.newContext({ storageState: statePath('builder') })
    const page = await ctx.newPage()
    await page.goto('/settings')
    await expect(page.getByRole('note')).toContainText('settings.manage')
    await expect(page.getByLabel('Default budget (USD)')).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Save settings' })).toHaveCount(0)
    const api = await apiAs('builder')
    expect((await api.put('/api/settings', { data: {} })).status()).toBe(403)
    await api.dispose()
    await ctx.close()
  })
})

test.describe('model pricing', () => {
  const MODEL = 'e2e-test-model'

  test('seeded prices and the LLMs that use them are listed', async ({ page }) => {
    await page.goto('/pricing')
    await expect(page.getByRole('heading', { name: 'Model pricing' })).toBeVisible()
    const luna = row(page, 'gpt-6-luna').first()
    await expect(luna).toContainText('$0.10')
    await expect(luna).toContainText('$0.50')
    await expect(luna).toContainText(/\d+ LLMs?/)
  })

  test('add, validate, edit and delete a price', async ({ page }) => {
    await page.goto('/pricing')
    await page.getByRole('button', { name: 'Add price' }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: 'model: Required' })).toBeVisible()

    await page.getByLabel('Model', { exact: true }).fill(MODEL)
    await page.getByLabel('Input per 1M tokens', { exact: true }).fill('1.25')
    await page.getByLabel('Cached input per 1M tokens', { exact: true }).fill('0.125')
    await page.getByLabel('Output per 1M tokens', { exact: true }).fill('10')
    await page.getByLabel('Notes').fill('https://example.com/pricing')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, `Added price for ${MODEL}`)).toBeVisible()
    const r = row(page, MODEL)
    await expect(r).toContainText('$1.25')
    await expect(r).toContainText('$10.00')
    await expect(r).toContainText('unused')
    await expect(r.getByRole('link', { name: 'https://example.com/pricing' })).toHaveAttribute('href', 'https://example.com/pricing')

    // duplicate model is refused
    await page.getByRole('button', { name: 'Add price' }).click()
    await page.getByLabel('Model', { exact: true }).fill(MODEL)
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Could not save price')).toBeVisible()
    await page.getByRole('button', { name: 'Cancel' }).click()

    // edit by button, save with Enter
    await page.getByRole('button', { name: `Edit ${MODEL}` }).click()
    await page.getByLabel('Output per 1M tokens', { exact: true }).fill('12.5')
    await page.getByLabel('Output per 1M tokens', { exact: true }).press('Enter')
    await expect(toast(page, `Updated ${MODEL}`)).toBeVisible()
    await page.reload()
    await expect(row(page, MODEL)).toContainText('$12.50')

    // an LLM using it shows up as "used by"
    const llm = await (await apiAs('admin')).post('/api/llms', { data: { name: 'E2E Priced LLM', model: `openai/${MODEL}`, temperature: null, max_completion_tokens: null, reasoning_effort: null, api: 'completions', vision: false } })
    expect(llm.status()).toBe(201)
    await page.reload()
    await expect(row(page, MODEL)).toContainText('1 LLM')
    await page.goto('/llms')
    await expect(row(page, 'E2E Priced LLM')).toContainText(MODEL)
    const id = ((await llm.json()).data as { id: string }).id
    await (await apiAs('admin')).delete(`/api/llms/${id}`)

    await page.goto('/pricing')
    await confirm(row(page, MODEL).getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${MODEL}`)).toBeVisible()
    await expect(row(page, MODEL)).toHaveCount(0)
  })

  test('an LLM without a price warns that runs cannot be costed', async ({ page }) => {
    const api = await apiAs('admin')
    const res = await api.post('/api/llms', { data: { name: 'E2E Unpriced', model: 'openai/e2e-unpriced-model', temperature: null, max_completion_tokens: null, reasoning_effort: null, api: 'completions', vision: false } })
    const id = ((await res.json()).data as { id: string }).id
    await page.goto('/llms')
    await expect(row(page, 'E2E Unpriced')).toContainText('No price entry')
    await api.delete(`/api/llms/${id}`)
    await api.dispose()
  })
})
