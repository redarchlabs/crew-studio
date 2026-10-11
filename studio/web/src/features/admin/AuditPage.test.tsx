import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { AuditPage } from './AuditPage'
import { api, apiError, makeAudit, renderRoute, resetAll } from './testUtils'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)

function page(items: ReturnType<typeof makeAudit>[], meta: { total: number; limit: number; offset: number }) {
  return { items, meta }
}
const loc = () => screen.getByTestId('location').textContent

beforeEach(() => {
  resetAll(['admin.audit'])
})

function open(url = '/admin/audit') {
  return renderRoute(<AuditPage />, { path: '/admin/audit', url })
}

describe('AuditPage', () => {
  it('requests the first page of 50', async () => {
    api.adminApi.audit.mockResolvedValue(page([makeAudit()], { total: 1, limit: 50, offset: 0 }))
    open()
    await screen.findByText('Signed in')
    expect(api.adminApi.audit).toHaveBeenCalledWith({ limit: 50, offset: 0 }, expect.anything())
  })

  it('renders friendly action labels, falling back to the raw action', async () => {
    api.adminApi.audit.mockResolvedValue(page([
      makeAudit({ id: 1, action: 'login_failed', actor_name: 'ghost', ip: '1.2.3.4' }),
      makeAudit({ id: 2, action: 'sessions_revoked' }),
      makeAudit({ id: 3, action: 'brand_new_action', actor_name: '', ip: '' }),
    ], { total: 3, limit: 50, offset: 0 }))
    open()
    expect(await screen.findByText('Sign-in failed')).toBeTruthy()
    expect(screen.getByText('Signed out everywhere')).toBeTruthy()
    expect(screen.getByText('brand_new_action')).toBeTruthy()
    expect(screen.getByText('1.2.3.4')).toBeTruthy()
    expect(screen.getByText('unknown')).toBeTruthy()
    expect(screen.getByText('—', { selector: 'td' })).toBeTruthy()
  })

  it('renders details as sorted key/value pairs, truncating long values', async () => {
    const long = { a: 'x'.repeat(200) }
    api.adminApi.audit.mockResolvedValue(page([
      makeAudit({ id: 1, details: { reason: 'bad password', attempts: 3, locked: true, nothing: null, extra: { nested: long } } }),
      makeAudit({ id: 2, details: {} }),
    ], { total: 2, limit: 50, offset: 0 }))
    open()
    await screen.findByText('bad password')
    const row = screen.getAllByRole('row')[1]
    const pairs = Array.from(row.querySelectorAll('[title]')).map((e) => e.getAttribute('title')).filter((t) => t?.includes(': '))
    expect(pairs.map((p) => p!.split(':')[0])).toEqual(['attempts', 'extra', 'locked', 'nothing', 'reason'])
    expect(within(row).getByText('3')).toBeTruthy()
    expect(within(row).getByText('true')).toBeTruthy()
    expect(within(row).getByText('—')).toBeTruthy()
    const extra = pairs.find((p) => p!.startsWith('extra'))!
    expect(extra.endsWith('…')).toBe(true)
    // Rows without details show a dash
    expect(within(screen.getAllByRole('row')[2]).getAllByText('—').length).toBeGreaterThan(0)
  })

  it('does not interpret HTML in actor names or details (no injection)', async () => {
    api.adminApi.audit.mockResolvedValue(page([makeAudit({ actor_name: '<img src=x onerror=alert(1)>', details: { note: '<script>alert(1)</script>' } })], { total: 1, limit: 50, offset: 0 }))
    const { container } = open()
    await screen.findByText('<img src=x onerror=alert(1)>')
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('script')).toBeNull()
  })

  it('shows an empty state', async () => {
    api.adminApi.audit.mockResolvedValue(page([], { total: 0, limit: 50, offset: 0 }))
    open()
    expect(await screen.findByText('Nothing recorded yet')).toBeTruthy()
  })

  it('surfaces a 403 with retry', async () => {
    api.adminApi.audit.mockRejectedValueOnce(apiError(403, 'needs admin.audit', 'forbidden'))
    open()
    expect(await screen.findByText('needs admin.audit')).toBeTruthy()
    api.adminApi.audit.mockResolvedValue(page([makeAudit()], { total: 1, limit: 50, offset: 0 }))
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Signed in')).toBeTruthy()
  })
})

describe('AuditPage pagination', () => {
  const many = (n: number, start = 1) => Array.from({ length: n }, (_, i) => makeAudit({ id: start + i, actor_name: `Person ${start + i}` }))

  it('pages forward and back through the offset query parameter', async () => {
    api.adminApi.audit.mockImplementation(async (q: { offset: number }) => page(many(50, q.offset + 1), { total: 120, limit: 50, offset: q.offset }))
    open()
    await screen.findByText('Person 1')
    expect(screen.getByText('1–50 of 120')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    await screen.findByText('Person 51')
    expect(loc()).toBe('/admin/audit?offset=50')
    expect(api.adminApi.audit).toHaveBeenLastCalledWith({ limit: 50, offset: 50 }, expect.anything())

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
    await screen.findByText('Person 101')
    expect(screen.getByText('101–120 of 120')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }))
    await screen.findByText('Person 51')
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }))
    await screen.findByText('Person 1')
    // Back on the first page the parameter disappears
    await waitFor(() => expect(loc()).toBe('/admin/audit'))
  })

  it.each([
    ['?offset=-20', 0],
    ['?offset=abc', 0],
    ['?offset=12.9', 12],
    ['?offset=100', 100],
  ])('sanitises the offset in %s to %i', async (qs, expected) => {
    api.adminApi.audit.mockResolvedValue(page([makeAudit()], { total: 500, limit: 50, offset: expected }))
    open(`/admin/audit${qs}`)
    await screen.findByText('Signed in')
    expect(api.adminApi.audit).toHaveBeenCalledWith({ limit: 50, offset: expected }, expect.anything())
  })
})
