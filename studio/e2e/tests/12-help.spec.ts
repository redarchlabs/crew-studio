import { expect, test } from '@playwright/test'
import { statePath } from '../support/accounts.ts'
import { apiAs, unwrap } from '../support/api.ts'
import { nav } from '../support/ui.ts'

type Contents = { categories: { title: string; articles: { slug: string; headings: { id: string }[] }[] }[] }
type Results = { mode: string; results: { slug: string; section_id: string }[] }

const GATED_CREW_SECTIONS = ['Create and edit crews', 'Delete a crew', 'Run a crew']

test.describe('help for a read-only viewer', () => {
  test.use({ storageState: statePath('viewer') })

  test('shows the screens they can use and hides admin help and actions they cannot take', async ({ page }) => {
    await page.goto('/')
    await nav(page).getByRole('link', { name: 'Help', exact: true }).click()
    const toc = page.getByRole('navigation', { name: 'Help contents' })
    await expect(toc.getByRole('link', { name: 'Crews', exact: true })).toBeVisible()
    await expect(toc.getByText('Admin', { exact: true })).toHaveCount(0)
    await expect(toc.getByRole('link', { name: 'People', exact: true })).toHaveCount(0)

    await toc.getByRole('link', { name: 'Crews', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Crews', level: 2 })).toBeVisible()
    await expect(page.getByRole('img', { name: /crews/i }).first()).toBeVisible()
    for (const title of GATED_CREW_SECTIONS) await expect(page.getByRole('heading', { name: title })).toHaveCount(0)
  })

  test('a hidden article is "not found", and its screenshots and search results are withheld', async ({ page }) => {
    await page.goto('/help/admin-people')
    await expect(page.getByText('No help article here')).toBeVisible()

    const api = await apiAs('viewer')
    expect((await api.get('/api/help/articles/admin-people')).status()).toBe(404)
    expect((await api.get('/api/help/images/admin-people.png')).status()).toBe(404)
    expect((await api.get('/api/help/images/run-crew-modal.png')).status()).toBe(404)
    expect((await api.get('/api/help/images/crews.png')).status()).toBe(200)
    const contents = await unwrap<Contents>(await api.get('/api/help'))
    expect(contents.categories.map((c) => c.title)).not.toContain('Admin')
    const found = await unwrap<Results>(await api.get('/api/help/search?q=invite+a+person+and+give+them+roles'))
    expect(found.results.filter((r) => r.slug.startsWith('admin-'))).toEqual([])
    const run = await unwrap<Results>(await api.get('/api/help/search?q=start+a+run+with+a+budget+override'))
    expect(run.results.filter((r) => r.slug === 'crews' && r.section_id === 'run-a-crew')).toEqual([])
    await api.dispose()
  })
})

test.describe('help for an administrator', () => {
  test('includes admin articles, gated sections and search across them', async ({ page }) => {
    await page.goto('/help/crews')
    for (const title of GATED_CREW_SECTIONS) await expect(page.getByRole('heading', { name: title })).toBeVisible()

    await page.getByRole('searchbox', { name: 'Search help' }).fill('invite a person and give them roles')
    const results = page.getByRole('region', { name: 'Search results' })
    await expect(results.getByRole('link').first()).toBeVisible()
    await results.getByRole('link', { name: /People/ }).first().click()
    await expect(page).toHaveURL(/\/help\/admin-people/)
    await expect(page.getByRole('heading', { name: 'People', level: 2 })).toBeVisible()
  })
})
