import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  accountApi, adminApi, agentsApi, areasApi, attachmentsApi, authApi, crewsApi, historyApi, llmsApi, overviewApi,
  pricingApi, runsApi, schedulesApi, settingsApi, tasksApi, toolsApi, triggersApi, workOrdersApi,
} from './endpoints'

type Call = { name: string; run: () => Promise<unknown>; method: string; url: string; body?: unknown }

function stub(data: unknown = {}) {
  const fn = vi.fn<typeof fetch>(() =>
    Promise.resolve(
      new Response(JSON.stringify({ success: true, data, error: null, meta: { total: 1, limit: 5, offset: 0 } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

const body = { a: 1 } as never
const signal = new AbortController().signal
const LIMITS = { allow_api: true, allow_mcp: false, permissions: ['crews.view' as const], area_ids: null }

const CALLS: Call[] = [
  // generic CRUD (llms/pricing/agents/tasks/roles/crews/schedules/triggers share the shape)
  ...(
    [
      ['llmsApi', llmsApi, '/llms'],
      ['pricingApi', pricingApi, '/pricing'],
      ['agentsApi', agentsApi, '/agents'],
      ['tasksApi', tasksApi, '/tasks'],
      ['crewsApi', crewsApi, '/crews'],
      ['schedulesApi', schedulesApi, '/schedules'],
      ['triggersApi', triggersApi, '/triggers'],
      ['adminApi.roles', adminApi.roles, '/admin/roles'],
    ] as const
  ).flatMap(([n, api, base]): Call[] => [
    { name: `${n}.list`, run: () => api.list(signal), method: 'GET', url: base },
    { name: `${n}.get`, run: () => api.get('x1', signal), method: 'GET', url: `${base}/x1` },
    { name: `${n}.create`, run: () => api.create(body), method: 'POST', url: base, body },
    { name: `${n}.update`, run: () => api.update('x1', body), method: 'PUT', url: `${base}/x1`, body },
    { name: `${n}.remove`, run: () => api.remove('x1'), method: 'DELETE', url: `${base}/x1` },
  ]),
  { name: 'tasksApi.crewDraft', run: () => tasksApi.crewDraft('t1'), method: 'GET', url: '/tasks/t1/crew-draft' },
  { name: 'crewsApi.validate', run: () => crewsApi.validate('c1'), method: 'GET', url: '/crews/c1/validate' },
  { name: 'crewsApi.startRun', run: () => crewsApi.startRun('c1', { inputs: { k: 'v' } }), method: 'POST', url: '/crews/c1/runs', body: { inputs: { k: 'v' } } },
  { name: 'toolsApi.list', run: () => toolsApi.list(), method: 'GET', url: '/tools' },
  { name: 'settingsApi.get', run: () => settingsApi.get(), method: 'GET', url: '/settings' },
  { name: 'settingsApi.update', run: () => settingsApi.update(body), method: 'PUT', url: '/settings', body },
  { name: 'historyApi.list', run: () => historyApi.list({ entity_type: 'agent', entity_id: 'a', limit: 10, offset: 20 }), method: 'GET', url: '/history?entity_type=agent&entity_id=a&limit=10&offset=20' },
  { name: 'runsApi.list', run: () => runsApi.list({ status: 'running', crew_id: '', limit: 5, offset: 0 }), method: 'GET', url: '/runs?status=running&limit=5&offset=0' },
  { name: 'runsApi.get', run: () => runsApi.get('r1'), method: 'GET', url: '/runs/r1' },
  { name: 'runsApi.cancel', run: () => runsApi.cancel('r1'), method: 'POST', url: '/runs/r1/cancel' },
  { name: 'runsApi.compact', run: () => runsApi.compact('r1'), method: 'POST', url: '/runs/r1/compact', body: {} },
  { name: 'runsApi.remove', run: () => runsApi.remove('r1'), method: 'DELETE', url: '/runs/r1' },
  { name: 'runsApi.interactions', run: () => runsApi.interactions('r1'), method: 'GET', url: '/runs/r1/interactions' },
  { name: 'runsApi.llmCalls', run: () => runsApi.llmCalls('r1'), method: 'GET', url: '/runs/r1/llm-calls' },
  { name: 'runsApi.events', run: () => runsApi.events('r1', 7), method: 'GET', url: '/runs/r1/events?after=7' },
  { name: 'runsApi.artifacts', run: () => runsApi.artifacts('r1'), method: 'GET', url: '/runs/r1/artifacts' },
  { name: 'runsApi.deliveries', run: () => runsApi.deliveries('r1'), method: 'GET', url: '/runs/r1/deliveries' },
  { name: 'runsApi.journal', run: () => runsApi.journal('r1'), method: 'GET', url: '/runs/r1/journal' },
  { name: 'overviewApi.get', run: () => overviewApi.get(), method: 'GET', url: '/overview' },
  { name: 'authApi.status', run: () => authApi.status(), method: 'GET', url: '/auth/status' },
  { name: 'authApi.me', run: () => authApi.me(), method: 'GET', url: '/auth/me' },
  { name: 'authApi.login', run: () => authApi.login('a@b.c', 'pw'), method: 'POST', url: '/auth/login', body: { email: 'a@b.c', password: 'pw' } },
  { name: 'authApi.setup', run: () => authApi.setup({ token: 't', email: 'e', name: 'n', password: 'p' }), method: 'POST', url: '/auth/setup', body: { token: 't', email: 'e', name: 'n', password: 'p' } },
  { name: 'authApi.logout', run: () => authApi.logout(), method: 'POST', url: '/auth/logout', body: {} },
  { name: 'areasApi.list', run: () => areasApi.list(), method: 'GET', url: '/areas' },
  { name: 'areasApi.create', run: () => areasApi.create({ name: 'n', description: '' }), method: 'POST', url: '/areas', body: { name: 'n', description: '' } },
  { name: 'areasApi.update', run: () => areasApi.update('a1', { name: 'n', description: '' }), method: 'PUT', url: '/areas/a1', body: { name: 'n', description: '' } },
  { name: 'areasApi.remove', run: () => areasApi.remove('a1'), method: 'DELETE', url: '/areas/a1' },
  { name: 'adminApi.permissions', run: () => adminApi.permissions(), method: 'GET', url: '/admin/permissions' },
  { name: 'adminApi.users.list', run: () => adminApi.users.list('service'), method: 'GET', url: '/admin/users?kind=service' },
  { name: 'adminApi.users.get', run: () => adminApi.users.get('u1'), method: 'GET', url: '/admin/users/u1' },
  { name: 'adminApi.users.create', run: () => adminApi.users.create(body), method: 'POST', url: '/admin/users', body },
  { name: 'adminApi.users.update', run: () => adminApi.users.update('u1', body), method: 'PUT', url: '/admin/users/u1', body },
  { name: 'adminApi.users.remove', run: () => adminApi.users.remove('u1'), method: 'DELETE', url: '/admin/users/u1' },
  { name: 'adminApi.users.setPassword', run: () => adminApi.users.setPassword('u1', 'pw'), method: 'PUT', url: '/admin/users/u1/password', body: { password: 'pw' } },
  { name: 'adminApi.users.clearPassword', run: () => adminApi.users.clearPassword('u1'), method: 'DELETE', url: '/admin/users/u1/password' },
  { name: 'adminApi.users.revokeSessions', run: () => adminApi.users.revokeSessions('u1'), method: 'POST', url: '/admin/users/u1/sessions/revoke', body: {} },
  { name: 'adminApi.apiKeys', run: () => adminApi.apiKeys({ limit: 50, offset: 0, q: 'erp', kind: 'service', status: 'active' }), method: 'GET', url: '/admin/api-keys?limit=50&offset=0&q=erp&kind=service&status=active' },
  { name: 'accountApi.apiKeys', run: () => accountApi.apiKeys({ limit: 20, offset: 20 }), method: 'GET', url: '/account/api-keys?limit=20&offset=20' },
  { name: 'accountApi.createApiKey', run: () => accountApi.createApiKey({ name: 'k', expires_at: null, ...LIMITS }), method: 'POST', url: '/account/api-keys', body: { name: 'k', expires_at: null, ...LIMITS } },
  { name: 'accountApi.revokeApiKey', run: () => accountApi.revokeApiKey('k1'), method: 'DELETE', url: '/account/api-keys/k1' },
  { name: 'adminApi.users.apiKeys', run: () => adminApi.users.apiKeys('u1', { limit: 20, offset: 0 }), method: 'GET', url: '/admin/users/u1/api-keys?limit=20&offset=0' },
  { name: 'adminApi.users.createApiKey', run: () => adminApi.users.createApiKey('u1', { name: 'k', expires_at: null, ...LIMITS }), method: 'POST', url: '/admin/users/u1/api-keys', body: { name: 'k', expires_at: null, ...LIMITS } },
  { name: 'adminApi.users.revokeApiKey', run: () => adminApi.users.revokeApiKey('u1', 'k1'), method: 'DELETE', url: '/admin/users/u1/api-keys/k1' },
  { name: 'adminApi.users.signingSecret', run: () => adminApi.users.signingSecret('u1'), method: 'GET', url: '/admin/users/u1/signing-secret' },
  { name: 'adminApi.users.rotateSigningSecret', run: () => adminApi.users.rotateSigningSecret('u1'), method: 'POST', url: '/admin/users/u1/signing-secret/rotate', body: {} },
  { name: 'adminApi.access.get', run: () => adminApi.access.get(), method: 'GET', url: '/admin/access' },
  { name: 'adminApi.access.update', run: () => adminApi.access.update(body), method: 'PUT', url: '/admin/access', body },
  { name: 'adminApi.audit', run: () => adminApi.audit({ limit: 10, offset: 0 }), method: 'GET', url: '/admin/audit?limit=10&offset=0' },
  { name: 'workOrdersApi.list', run: () => workOrdersApi.list({ q: 'x', status: 'draft', limit: 10, offset: 0 }), method: 'GET', url: '/work-orders?q=x&status=draft&limit=10&offset=0' },
  { name: 'workOrdersApi.list (crew)', run: () => workOrdersApi.list({ crew_id: 'c1', limit: 10, offset: 0 }), method: 'GET', url: '/work-orders?crew_id=c1&limit=10&offset=0' },
  { name: 'workOrdersApi.get', run: () => workOrdersApi.get('w1'), method: 'GET', url: '/work-orders/w1' },
  { name: 'workOrdersApi.create', run: () => workOrdersApi.create(body), method: 'POST', url: '/work-orders', body },
  { name: 'workOrdersApi.update', run: () => workOrdersApi.update('w1', body), method: 'PUT', url: '/work-orders/w1', body },
  { name: 'workOrdersApi.remove', run: () => workOrdersApi.remove('w1'), method: 'DELETE', url: '/work-orders/w1' },
  { name: 'workOrdersApi.submit (default body)', run: () => workOrdersApi.submit('w1'), method: 'POST', url: '/work-orders/w1/submit', body: {} },
  { name: 'workOrdersApi.submit (budget)', run: () => workOrdersApi.submit('w1', { budget_usd: 2 }), method: 'POST', url: '/work-orders/w1/submit', body: { budget_usd: 2 } },
  { name: 'workOrdersApi.cancel', run: () => workOrdersApi.cancel('w1'), method: 'POST', url: '/work-orders/w1/cancel', body: {} },
  { name: 'workOrdersApi.journal (no run)', run: () => workOrdersApi.journal('w1'), method: 'GET', url: '/work-orders/w1/journal' },
  { name: 'workOrdersApi.journal (run)', run: () => workOrdersApi.journal('w1', 'r9'), method: 'GET', url: '/work-orders/w1/journal?run_id=r9' },
  { name: 'workOrdersApi.removeAttachment', run: () => workOrdersApi.removeAttachment('w1', 'at1'), method: 'DELETE', url: '/work-orders/w1/attachments/at1' },
  { name: 'schedulesApi.runNow', run: () => schedulesApi.runNow('s1'), method: 'POST', url: '/schedules/s1/run-now', body: {} },
  { name: 'schedulesApi.preview', run: () => schedulesApi.preview('* * * * *', 'UTC'), method: 'POST', url: '/schedules/preview', body: { cron: '* * * * *', timezone: 'UTC' } },
  { name: 'triggersApi.rotate', run: () => triggersApi.rotate('t1'), method: 'POST', url: '/triggers/t1/rotate', body: {} },
]

describe('endpoint wiring', () => {
  it.each(CALLS)('$name -> $method $url', async ({ run, method, url, body: expected }) => {
    const fn = stub()
    await run()
    expect(fn).toHaveBeenCalledTimes(1)
    const [calledUrl, init] = fn.mock.calls[0]
    expect(calledUrl).toBe(`/api${url}`)
    expect(init?.method).toBe(method)
    if (expected === undefined) expect(init?.body).toBeUndefined()
    else expect(JSON.parse(init?.body as string)).toEqual(expected)
  })

  it.each(CALLS.filter((c) => /\.(list|permissions|llmCalls|events|artifacts|deliveries)$/.test(c.name) && !c.name.startsWith('historyApi') && !c.name.startsWith('runsApi.list') && !c.name.includes('audit') && !c.name.startsWith('workOrdersApi')))(
    '$name tolerates a null payload by returning []',
    async ({ run }) => {
      stub(null)
      const out = await run()
      expect(Array.isArray(out)).toBe(true)
      expect(out).toEqual([])
    },
  )

  it('passes the abort signal through to fetch', async () => {
    const ctrl = new AbortController()
    const fn = stub()
    await agentsApi.get('a1', ctrl.signal)
    expect(fn.mock.calls[0][1]?.signal).toBe(ctrl.signal)
  })
})

describe('paged endpoints', () => {
  it.each([
    ['accountApi.apiKeys', () => accountApi.apiKeys({ limit: 20, offset: 0 })],
    ['adminApi.apiKeys', () => adminApi.apiKeys({ limit: 50, offset: 0 })],
    ['adminApi.users.apiKeys', () => adminApi.users.apiKeys('u1', { limit: 20, offset: 0 })],
  ])('%s returns keys and meta from the envelope', async (_name, run) => {
    stub([{ id: 'k1' }])
    await expect(run()).resolves.toEqual({ items: [{ id: 'k1' }], meta: { total: 1, limit: 5, offset: 0 } })
  })


  it('historyApi.list returns items and meta from the envelope', async () => {
    stub([{ id: 'rev' }])
    await expect(historyApi.list({ limit: 5, offset: 0 })).resolves.toEqual({ items: [{ id: 'rev' }], meta: { total: 1, limit: 5, offset: 0 } })
  })
  it('workOrdersApi.list and adminApi.audit and runsApi.list page the same way', async () => {
    stub([{ id: 1 }])
    for (const p of [workOrdersApi.list({ limit: 5, offset: 0 }), adminApi.audit({ limit: 5, offset: 0 }), runsApi.list({ limit: 5, offset: 0 })]) {
      await expect(p).resolves.toMatchObject({ items: [{ id: 1 }], meta: { total: 1 } })
    }
  })
})

describe('workOrdersApi.upload', () => {
  it('posts each file as multipart "file" with the X-Requested-With header', async () => {
    const fn = stub([{ id: 'at1' }])
    const f1 = new File(['a'], 'a.txt', { type: 'text/plain' })
    const f2 = new File(['b'], 'b.png', { type: 'image/png' })
    await expect(workOrdersApi.upload('w1', [f1, f2])).resolves.toEqual([{ id: 'at1' }])
    const [url, init] = fn.mock.calls[0]
    expect(url).toBe('/api/work-orders/w1/attachments')
    expect(init?.method).toBe('POST')
    const headers = init?.headers as Record<string, string>
    expect(headers['X-Requested-With']).toBe('crewstudio')
    expect(headers['Content-Type']).toBeUndefined()
    const form = init?.body as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.getAll('file').map((f) => (f as File).name)).toEqual(['a.txt', 'b.png'])
  })

  it('returns [] when the server answers null', async () => {
    stub(null)
    await expect(workOrdersApi.upload('w1', [])).resolves.toEqual([])
  })
})

describe('pure URL helpers', () => {
  it('runsApi.streamUrl', () => {
    expect(runsApi.streamUrl('r1', 4)).toBe('/api/runs/r1/stream?after=4')
  })
  it('authApi.ssoLoginUrl encodes provider and return_to', () => {
    expect(authApi.ssoLoginUrl('git hub', '/runs?x=1&y=2')).toBe('/api/auth/git%20hub/login?return_to=%2Fruns%3Fx%3D1%26y%3D2')
  })
  it('attachmentsApi.contentUrl toggles download', () => {
    expect(attachmentsApi.contentUrl('a1')).toBe('/api/attachments/a1/content')
    expect(attachmentsApi.contentUrl('a1', true)).toBe('/api/attachments/a1/content?download=1')
  })
  it('triggersApi.hookUrl is absolute against the page origin', () => {
    expect(triggersApi.hookUrl('t1')).toBe(`${window.location.origin}/api/hooks/t1`)
  })
})
