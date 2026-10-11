import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { request as pwRequest } from '@playwright/test'
import { BASE_URL } from '../harness/paths.ts'
import { LIB } from '../support/accounts.ts'
import { apiAnon, apiAs, findByName, unwrap } from '../support/api.ts'
import { ensureLibrary, type Library } from '../support/library.ts'
import { confirm, row, toast } from '../support/ui.ts'

test.describe.configure({ mode: 'serial' })

const NAME = 'E2E Webhook'
const RUN_TIMEOUT = 120_000
type Accepted = { work_order_id: string; number: number; run_id: string; status: string; status_url: string }
type Named = { id: string; name: string }

let lib: Library
test.beforeAll(async () => {
  lib = await ensureLibrary()
})

const state = { id: '', token: '', hookUrl: '' }

async function fire(token: string | null, body: unknown, headers: Record<string, string> = {}) {
  const api = await pwRequest.newContext({ baseURL: BASE_URL, storageState: { cookies: [], origins: [] } })
  const res = await api.post(state.hookUrl, {
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...headers },
    data: body as object,
  })
  const json = (await res.json()) as { success: boolean; data: Accepted | null; error: { code: string; message: string } | null }
  await api.dispose()
  return { status: res.status(), json }
}

async function waitSucceeded(page: Page, workOrderId: string): Promise<void> {
  await page.goto(`/work-orders/${workOrderId}`)
  await expect(page.getByRole('main').locator('header').getByText('succeeded', { exact: true }).first()).toBeVisible({ timeout: RUN_TIMEOUT })
}

test.describe('webhook triggers', () => {
  test('create a trigger; the token is shown once', async ({ page }) => {
    await page.goto('/triggers/new')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(page.getByRole('alert').first()).toBeVisible() // name / target missing

    await page.getByLabel('Name', { exact: true }).fill(NAME)
    await page.getByLabel('Description').fill('Created by the e2e suite')
    await page.getByRole('button', { name: /Choose a crew/ }).click()
    await page.getByPlaceholder('Search…').fill(LIB.crew)
    await page.getByRole('option', { name: new RegExp(`^${LIB.crew}\\b`) }).first().click()
    await page.getByRole('button', { name: /Map all \d+ crew inputs/ }).click()
    await expect(page.getByLabel('Input key')).toHaveValue('topic')
    await page.getByLabel('Payload path').fill('request.topic')
    await page.getByLabel('Brief', { exact: true }).fill('Webhook brief.')
    await page.getByRole('button', { name: 'Create', exact: true }).click()
    await expect(toast(page, 'Created trigger')).toBeVisible()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: 'Trigger token' })).toBeVisible()
    await expect(dialog.getByText('It is shown only once')).toBeVisible()
    const blocks = dialog.locator('pre')
    state.token = (await blocks.nth(0).innerText()).trim()
    state.hookUrl = (await blocks.nth(1).innerText()).trim()
    expect(state.token).toMatch(/^cst_[A-Za-z0-9]+_[A-Za-z0-9_-]+$/)
    expect(state.hookUrl).toMatch(/\/api\/hooks\/[0-9a-f-]{36}$/)
    const curl = await blocks.nth(2).innerText()
    expect(curl).toContain('Authorization: Bearer')
    expect(curl).toContain('Idempotency-Key')
    expect(await blocks.nth(3).innerText()).toContain('request')
    await dialog.getByRole('button', { name: 'I have saved the token' }).click()

    await expect(page).toHaveURL(/\/triggers\/[0-9a-f-]{36}$/)
    state.id = /triggers\/([0-9a-f-]{36})/.exec(page.url())![1]
    // afterwards only the prefix is visible
    await page.reload()
    await expect(page.getByText(state.token)).toHaveCount(0)
    await expect(page.getByText(`cst_${state.token.split('_')[1]}_…`).first()).toBeVisible()
    await expect(page.getByText(state.hookUrl.replace(BASE_URL, ''), { exact: false }).first()).toBeVisible()
  })

  test('firing with the token creates a work order that runs to completion', async ({ page }) => {
    const { status, json } = await fire(state.token, { request: { topic: 'tidal power' }, other: 'x' }, { 'Idempotency-Key': 'e2e-key-1' })
    expect(status).toBe(202)
    expect(json.success).toBe(true)
    const accepted = json.data!
    expect(accepted.number).toBeGreaterThan(0)
    expect(accepted.status_url).toBe(`/api/work-orders/${accepted.work_order_id}`)

    await waitSucceeded(page, accepted.work_order_id)
    const header = page.getByRole('main').locator('header')
    await expect(header).toContainText(`WO-${accepted.number}`)
    await expect(header).toContainText(NAME)
    await expect(header).toContainText('webhook') // source chip
    await page.getByRole('tab', { name: /^Brief/ }).click()
    await expect(page.getByText('Webhook brief.')).toBeVisible()
    await expect(page.getByText('tidal power').first()).toBeVisible() // mapped input and the payload in the brief
    await expect(page.getByText('topic').first()).toBeVisible()

    // it shows up in the work order list and on the trigger
    await page.goto('/work-orders')
    await expect(page.getByRole('row').filter({ hasText: `WO-${accepted.number}` })).toContainText('webhook')
    await page.goto(`/triggers/${state.id}`)
    await expect(page.locator('section').filter({ hasText: 'Recent work orders' }).getByRole('row').filter({ hasText: NAME })).toBeVisible()
    await expect(page.getByText(/Fired 1 times/)).toBeVisible()
  })

  test('the same Idempotency-Key returns the same work order; a new key creates another', async ({ page }) => {
    const first = await fire(state.token, { request: { topic: 'geothermal' } }, { 'Idempotency-Key': 'e2e-key-2' })
    const again = await fire(state.token, { request: { topic: 'geothermal' } }, { 'Idempotency-Key': 'e2e-key-2' })
    expect(first.status).toBe(202)
    expect(again.status).toBe(202)
    expect(again.json.data!.work_order_id).toBe(first.json.data!.work_order_id)
    expect(again.json.data!.number).toBe(first.json.data!.number)

    const other = await fire(state.token, { request: { topic: 'geothermal' } }, { 'Idempotency-Key': 'e2e-key-3' })
    expect(other.json.data!.work_order_id).not.toBe(first.json.data!.work_order_id)

    // only two new work orders exist, not three
    await page.goto(`/work-orders?q=${encodeURIComponent(NAME)}`)
    await expect(page.getByRole('row').filter({ hasText: NAME })).toHaveCount(3) // key-1, key-2, key-3
    await expect(page.getByText(/Fired 3 times/)).toHaveCount(0)
    await page.goto(`/triggers/${state.id}`)
    await expect(page.getByText(/Fired 3 times/)).toBeVisible()
  })

  test('a missing, wrong or malformed token is refused with 401', async () => {
    const missing = await fire(null, { request: { topic: 'x' } })
    expect(missing.status).toBe(401)
    expect(missing.json.error?.code).toBe('unauthenticated')
    const wrong = await fire('cst_wrong_tokenvalue', { request: { topic: 'x' } })
    expect(wrong.status).toBe(401)
    const swapped = await fire(`${state.token}x`, { request: { topic: 'x' } })
    expect(swapped.status).toBe(401)
    // a Studio API key is not a trigger token
    const apiKey = await fire('csk_abc_def', { request: { topic: 'x' } })
    expect(apiKey.status).toBe(401)
    // the other accepted header works
    const alt = await fire(null, { request: { topic: 'wave power' } }, { 'X-CrewStudio-Token': state.token, 'Idempotency-Key': 'e2e-key-alt' })
    expect(alt.status).toBe(202)
  })

  test('a body that is not JSON is rejected', async () => {
    const api = await pwRequest.newContext({ baseURL: BASE_URL, storageState: { cookies: [], origins: [] } })
    const res = await api.post(state.hookUrl, { headers: { Authorization: `Bearer ${state.token}`, 'Content-Type': 'application/json' }, data: Buffer.from('{not json') })
    expect(res.status()).toBe(400)
    expect((await res.json()).error.code).toBe('bad_json')
    const noType = await api.post(state.hookUrl, { headers: { Authorization: `Bearer ${state.token}`, 'Content-Type': 'text/plain' }, data: Buffer.from('hello') })
    expect(noType.status()).toBe(415)
    await api.dispose()
  })

  test('rotating the token invalidates the old one', async ({ page }) => {
    await page.goto(`/triggers/${state.id}`)
    await confirm(page.getByRole('button', { name: 'Rotate token' }))
    await expect(toast(page, 'Token rotated; the old token no longer works')).toBeVisible()
    const dialog = page.getByRole('dialog')
    const next = (await dialog.locator('pre').nth(0).innerText()).trim()
    expect(next).toMatch(/^cst_/)
    expect(next).not.toBe(state.token)
    await dialog.getByRole('button', { name: 'I have saved the token' }).click()

    const old = await fire(state.token, { request: { topic: 'x' } })
    expect(old.status).toBe(401)
    const fresh = await fire(next, { request: { topic: 'hydro power' } }, { 'Idempotency-Key': 'e2e-key-4' })
    expect(fresh.status).toBe(202)
    state.token = next
    await waitSucceeded(page, fresh.json.data!.work_order_id)
  })

  test('a disabled trigger answers 409 and creates nothing', async ({ page }) => {
    await page.goto(`/triggers/${state.id}`)
    await page.getByRole('switch', { name: /Enabled/ }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved trigger')).toBeVisible()
    const res = await fire(state.token, { request: { topic: 'x' } }, { 'Idempotency-Key': 'e2e-key-off' })
    expect(res.status).toBe(409)
    expect(res.json.error?.code).toBe('disabled')
    await page.goto('/triggers')
    await expect(row(page, NAME)).toContainText('disabled')

    await page.getByRole('link', { name: NAME }).click()
    await page.getByRole('switch', { name: /Enabled/ }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(toast(page, 'Saved trigger')).toBeVisible()
    expect((await fire(state.token, { request: { topic: 'solar' } }, { 'Idempotency-Key': 'e2e-key-on' })).status).toBe(202)
  })

  test('the trigger list shows counts and the token prefix; then the trigger is deleted', async ({ page }) => {
    await page.goto('/triggers')
    const r = row(page, NAME)
    await expect(r).toContainText('enabled')
    await expect(r).toContainText(`cst_${state.token.split('_')[1]}_…`)
    await expect(r).toContainText(LIB.crew)
    // wait for the runs started above so deleting does not race a queued work order
    await expect(async () => {
      const api = await apiAs('admin')
      const data = await unwrap<{ status: string }[] | { items: { status: string }[] }>(await api.get('/api/work-orders?source=webhook&limit=50'))
      await api.dispose()
      const items = Array.isArray(data) ? data : data.items
      expect(items.every((w) => !['queued', 'running'].includes(w.status))).toBe(true)
    }).toPass({ timeout: RUN_TIMEOUT, intervals: [1000] })

    await page.getByRole('link', { name: NAME }).click()
    await confirm(page.getByRole('button', { name: 'Delete' }))
    await expect(toast(page, `Deleted ${NAME}`)).toBeVisible()
    await expect(page).toHaveURL(/\/triggers$/)
    const gone = await fire(state.token, { request: { topic: 'x' } })
    expect(gone.status).toBe(401)
  })
})

test.describe('API keys (service accounts)', () => {
  async function serviceKey(api: APIRequestContext, name: string, opts: { roles?: string[]; permissions?: string[]; areas: string[] }): Promise<{ secret: string; keyId: string; userId: string }> {
    const roles = await unwrap<Named[]>(await api.get('/api/admin/roles'))
    const user = await unwrap<Named>(
      await api.post('/api/admin/users', {
        data: { kind: 'service', name, status: 'active', all_areas: false, area_ids: opts.areas, role_ids: (opts.roles ?? []).map((r) => roles.find((x) => x.name === r)!.id), permissions: opts.permissions ?? [] },
      }),
      201,
    )
    const key = await unwrap<{ key: { id: string }; secret: string }>(await api.post(`/api/admin/users/${user.id}/api-keys`, { data: { name: 'e2e key' } }), 201)
    return { secret: key.secret, keyId: key.key.id, userId: user.id }
  }

  test('a key with crews.view lists crews, scoped to the account\'s areas', async () => {
    const admin = await apiAs('admin')
    const finance = await findByName<Named>(admin, '/api/areas', 'Finance')
    const { secret } = await serviceKey(admin, 'E2E Reader', { roles: ['Viewer'], areas: [lib.areaId, finance.id] })
    const api = await apiAnon(secret)
    const res = await api.get('/api/crews')
    expect(res.status()).toBe(200)
    const crews = (await res.json()).data as Named[]
    const names = crews.map((c) => c.name).sort()
    expect(names).toContain(LIB.crew)
    expect(names).toContain('PE Due Diligence Crew')
    expect(names).not.toContain('AI Research Crew') // IT is not one of its areas

    const me = await unwrap<{ kind: string; via: string; permissions: string[] }>(await api.get('/api/auth/me'))
    expect(me.kind).toBe('service')
    expect(me.via).toBe('api_key')
    expect(me.permissions).toContain('crews.view')
    // read-only: writes are refused
    expect((await api.post('/api/crews', { data: { name: 'nope' } })).status()).toBe(403)
    expect((await api.get('/api/admin/users')).status()).toBe(403)
    await api.dispose()
    await admin.dispose()
  })

  test('a key without the permission gets 403 naming it', async () => {
    const admin = await apiAs('admin')
    const { secret } = await serviceKey(admin, 'E2E Narrow', { permissions: ['overview.view'], areas: [lib.areaId] })
    const api = await apiAnon(secret)
    const res = await api.get('/api/crews')
    expect(res.status()).toBe(403)
    const body = await res.json()
    expect(body.error.code).toBe('forbidden')
    expect(JSON.stringify(body.error.details)).toContain('crews.view')
    expect((await api.get('/api/overview')).status()).toBe(200)
    await api.dispose()
    await admin.dispose()
  })

  test('a revoked, unknown or non-key bearer token is refused with 401', async () => {
    const admin = await apiAs('admin')
    const { secret, keyId, userId } = await serviceKey(admin, 'E2E Revoked', { roles: ['Viewer'], areas: [lib.areaId] })
    const api = await apiAnon(secret)
    expect((await api.get('/api/crews')).status()).toBe(200)
    expect((await admin.delete(`/api/admin/users/${userId}/api-keys/${keyId}`)).status()).toBe(204)
    const revoked = await api.get('/api/crews')
    expect(revoked.status()).toBe(401)
    expect(JSON.stringify(await revoked.json())).toContain('revoked')
    await api.dispose()

    const forged = await apiAnon('csk_forged_0123456789abcdef')
    expect((await forged.get('/api/crews')).status()).toBe(401)
    await forged.dispose()
    const notAKey = await apiAnon('not-an-api-key')
    expect((await notAKey.get('/api/crews')).status()).toBe(401) // falls through as anonymous
    await notAKey.dispose()
    const anon = await apiAnon()
    expect((await anon.get('/api/crews')).status()).toBe(401)
    await anon.dispose()
    await admin.dispose()
  })

  test('an operator key can start a work order, which appears in the UI under the service account', async ({ page }) => {
    const admin = await apiAs('admin')
    const { secret } = await serviceKey(admin, 'E2E Operator Bot', { roles: ['Operator'], areas: [lib.areaId] })
    await admin.dispose()
    const api = await apiAnon(secret)
    const created = await api.post('/api/work-orders', {
      data: { title: 'Created through the API', brief: 'API brief', crew_id: lib.crewId, inputs: { topic: 'api topic' }, budget_usd: null, submit: true },
    })
    expect(created.status()).toBe(201)
    const wo = (await created.json()).data as { id: string; number: number; source: string }
    expect(wo.source).toBe('api')
    await api.dispose()

    await page.goto(`/work-orders/${wo.id}`)
    const header = page.getByRole('main').locator('header')
    await expect(header).toContainText('E2E Operator Bot')
    await expect(header).toContainText('api')
    await expect(header.getByText('succeeded', { exact: true }).first()).toBeVisible({ timeout: RUN_TIMEOUT })
  })
})
