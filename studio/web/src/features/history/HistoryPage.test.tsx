import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { historyApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import type { Revision } from '../../api/types'
import { makeRevision, renderAt, setPerms } from '../runs/testUtils'
import { HistoryPage } from './HistoryPage'

vi.mock('../../api/endpoints', () => ({ historyApi: { list: vi.fn() } }))
vi.mock('../../lib/auth', async () => (await import('../runs/testUtils')).authMockModule())

const list = vi.mocked(historyApi.list)
const page = (items: Revision[], over: { total?: number; limit?: number; offset?: number } = {}) => ({ items, meta: { total: over.total ?? items.length, limit: over.limit ?? 50, offset: over.offset ?? 0 } })
const show = (route = '/history') => renderAt(<HistoryPage />, { route, path: '/history' })
const typeFilter = () => screen.getByLabelText('Filter by entity type') as HTMLSelectElement
const optionLabels = () => within(typeFilter()).getAllByRole('option').map((o) => o.textContent)

beforeEach(() => {
  setPerms('history.view')
  list.mockResolvedValue(page([makeRevision({ id: 1 })]))
})
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('HistoryPage list', () => {
  it('loads revisions with the default page size', async () => {
    show()
    expect(screen.getByRole('status')).toBeTruthy()
    expect(await screen.findByText('Analyst')).toBeTruthy()
    expect(list).toHaveBeenCalledWith({ entity_type: undefined, entity_id: undefined, limit: 50, offset: 0 }, expect.anything())
    expect(screen.getByText('Alice')).toBeTruthy()
    expect(screen.getByText('update')).toBeTruthy()
  })

  it('shows an empty state', async () => {
    list.mockResolvedValue(page([]))
    show()
    expect(await screen.findByText('No revisions')).toBeTruthy()
  })

  it('shows an error with retry', async () => {
    list.mockRejectedValueOnce(new ApiError(403, { code: 'forbidden', message: 'not allowed' }))
    show()
    expect(await screen.findByText('not allowed')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(await screen.findByText('Analyst')).toBeTruthy()
  })

  it('shows a dash when the actor is unknown and uses the action tone', async () => {
    list.mockResolvedValue(page([makeRevision({ id: 1, actor_name: '', action: 'delete' }), makeRevision({ id: 2, action: 'create' })]))
    show()
    await screen.findAllByText('Analyst')
    expect(screen.getByText('delete')).toBeTruthy()
    expect(screen.getByText('create')).toBeTruthy()
    expect(screen.getByText('—')).toBeTruthy()
  })

  it('names entities from their snapshot and links to their page', async () => {
    list.mockResolvedValue(page([
      makeRevision({ id: 1, entity_type: 'agent', entity_id: 'a1', snapshot: { name: 'Analyst' } }),
      makeRevision({ id: 2, entity_type: 'work_order', entity_id: 'w1', snapshot: { number: 4, title: 'Report' } }),
    ]))
    show()
    const link = (await screen.findByText('Analyst')).closest('a')!
    expect(link.getAttribute('href')).toBe('/agents/a1')
    expect(screen.getByText('WO-4 · Report').closest('a')?.getAttribute('href')).toBe('/work-orders/w1')
    expect(screen.getAllByText('open')).toHaveLength(2)
  })

  it('does not link deleted entities', async () => {
    list.mockResolvedValue(page([makeRevision({ id: 1, action: 'delete' })]))
    show()
    const name = await screen.findByText('Analyst')
    expect(name.closest('a')).toBeNull()
    expect(screen.queryByText('open')).toBeNull()
  })

  it('expands a revision to show its snapshot and collapses it again', async () => {
    list.mockResolvedValue(page([makeRevision({ id: 7, snapshot: { name: 'Analyst', secret_field: 'xyz' } })]))
    show()
    await screen.findByText('Analyst')
    const row = screen.getByText('Alice').closest('tr')!
    expect(row.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(row)
    expect(screen.getByText(/"secret_field": "xyz"/)).toBeTruthy()
    expect(row.getAttribute('aria-expanded')).toBe('true')
    fireEvent.click(row)
    expect(screen.queryByText(/"secret_field"/)).toBeNull()
  })

  it('opening the entity link does not toggle the row', async () => {
    show()
    const link = (await screen.findByText('Analyst')).closest('a')!
    fireEvent.click(link)
    expect(screen.queryByText(/"name": "Analyst"/)).toBeNull()
  })

  it('only one revision is expanded at a time', async () => {
    list.mockResolvedValue(page([makeRevision({ id: 1, snapshot: { name: 'One', f: 1 } }), makeRevision({ id: 2, snapshot: { name: 'Two', f: 2 } })]))
    show()
    await screen.findByText('One')
    fireEvent.click(screen.getByText('One').closest('tr')!)
    fireEvent.click(screen.getByText('Two').closest('tr')!)
    expect(screen.queryByText(/"f": 1/)).toBeNull()
    expect(screen.getByText(/"f": 2/)).toBeTruthy()
  })
})

describe('HistoryPage filters and pagination', () => {
  it('offers all types to history viewers and filters the query by type', async () => {
    const h = show()
    await screen.findByText('Analyst')
    expect(optionLabels()).toContain('All types')
    expect(optionLabels()).toContain('Agent')
    expect(optionLabels()).not.toContain('Role')
    fireEvent.change(typeFilter(), { target: { value: 'crew' } })
    await waitFor(() => expect(list).toHaveBeenLastCalledWith({ entity_type: 'crew', entity_id: undefined, limit: 50, offset: 0 }, expect.anything()))
    expect(h.search()).toBe('?entity_type=crew')
    fireEvent.change(typeFilter(), { target: { value: '' } })
    await waitFor(() => expect(h.search()).toBe(''))
  })

  it('reads the type and entity id from the URL and can clear the entity filter', async () => {
    const h = show('/history?entity_type=agent&entity_id=abc-123&offset=50')
    await screen.findByText('Analyst')
    expect(list).toHaveBeenCalledWith({ entity_type: 'agent', entity_id: 'abc-123', limit: 50, offset: 50 }, expect.anything())
    expect(screen.getByText('abc-123')).toBeTruthy()
    expect(screen.getByText(/Showing one agent/)).toBeTruthy()
    fireEvent.click(screen.getByText('clear'))
    await waitFor(() => expect(h.search()).toBe('?entity_type=agent'))
    expect(screen.queryByText(/Showing one/)).toBeNull()
  })

  it('the "only this" action filters to one entity and resets the offset', async () => {
    const h = show('/history?offset=50')
    await screen.findByText('Analyst')
    fireEvent.click(screen.getByText('only this'))
    await waitFor(() => expect(h.search()).toBe('?entity_type=agent&entity_id=abcdef123456'))
    expect(list).toHaveBeenLastCalledWith({ entity_type: 'agent', entity_id: 'abcdef123456', limit: 50, offset: 0 }, expect.anything())
  })

  it('changing the type drops the entity filter', async () => {
    const h = show('/history?entity_type=agent&entity_id=x')
    await screen.findByText('Analyst')
    fireEvent.change(typeFilter(), { target: { value: 'task' } })
    await waitFor(() => expect(h.search()).toBe('?entity_type=task'))
  })

  it('describes an entity filter without a known type generically', async () => {
    show('/history?entity_id=zzz')
    expect(await screen.findByText(/Showing one entity/)).toBeTruthy()
  })

  it('ignores a type the person cannot read instead of requesting it', async () => {
    show('/history?entity_type=role')
    await screen.findByText('Analyst')
    expect(list).toHaveBeenCalledWith({ entity_type: undefined, entity_id: undefined, limit: 50, offset: 0 }, expect.anything())
    expect(typeFilter().value).toBe('')
  })

  it('treats junk offsets as zero', async () => {
    show('/history?offset=abc')
    await screen.findByText('Analyst')
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ offset: 0 }), expect.anything())
    cleanup()
    show('/history?offset=-9')
    await waitFor(() => expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 0 }), expect.anything()))
  })

  it('paginates', async () => {
    list.mockImplementation((q) => Promise.resolve(page([makeRevision({ id: 1 })], { total: 120, limit: 50, offset: q.offset })))
    const h = show()
    expect(await screen.findByText('1–50 of 120')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Next page'))
    await waitFor(() => expect(h.search()).toBe('?offset=50'))
    await screen.findByText('51–100 of 120')
    fireEvent.click(screen.getByLabelText('Next page'))
    await waitFor(() => expect(h.search()).toBe('?offset=100'))
    await screen.findByText('101–120 of 120')
    expect((screen.getByLabelText('Next page') as HTMLButtonElement).disabled).toBe(true)
  })

  it('keeps the type and entity filters when paginating', async () => {
    list.mockImplementation((q) => Promise.resolve(page([makeRevision({ id: 1 })], { total: 120, limit: 50, offset: q.offset })))
    const h = show('/history?entity_type=agent&entity_id=e1')
    await screen.findByText('1–50 of 120')
    fireEvent.click(screen.getByLabelText('Next page'))
    await waitFor(() => expect(h.search()).toBe('?entity_type=agent&entity_id=e1&offset=50'))
  })
})

describe('HistoryPage audit gating', () => {
  it('history viewers without admin.audit get the standard description and no admin types', async () => {
    show()
    await screen.findByText('Analyst')
    expect(screen.getByText(/of your LLMs, prices, settings/)).toBeTruthy()
    for (const label of ['Role', 'Functional area', 'Person / service account', 'API key', 'Sign-in settings']) expect(optionLabels()).not.toContain(label)
  })

  it('admin.audit adds the admin types and the audit description', async () => {
    setPerms('history.view', 'admin.audit')
    show()
    await screen.findByText('Analyst')
    expect(screen.getByText(/including people, roles, areas, keys and sign-in settings/)).toBeTruthy()
    for (const label of ['Role', 'Functional area', 'Person / service account', 'API key', 'Sign-in settings']) expect(optionLabels()).toContain(label)
  })

  it('allows deep-linking to an admin type for auditors', async () => {
    setPerms('history.view', 'admin.audit')
    show('/history?entity_type=role')
    await screen.findByText('Analyst')
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ entity_type: 'role' }), expect.anything())
    expect(typeFilter().value).toBe('role')
  })

  it('auditors without history.view must pick a type, defaulting to the first admin one', async () => {
    setPerms('admin.audit')
    show()
    await screen.findByText('Analyst')
    expect(optionLabels()).not.toContain('All types')
    expect(optionLabels()).not.toContain('Agent')
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ entity_type: 'area' }), expect.anything())
    expect(typeFilter().value).toBe('area')
  })

  it('auditors without history.view cannot request business types via the URL', async () => {
    setPerms('admin.audit')
    show('/history?entity_type=agent')
    await screen.findByText('Analyst')
    expect(list).toHaveBeenCalledWith(expect.objectContaining({ entity_type: 'area' }), expect.anything())
  })

  it('links user revisions to service accounts or users by snapshot kind', async () => {
    setPerms('history.view', 'admin.audit')
    list.mockResolvedValue(page([
      makeRevision({ id: 1, entity_type: 'user', entity_id: 'u1', snapshot: { email: 'a@x.com', kind: 'human' } }),
      makeRevision({ id: 2, entity_type: 'user', entity_id: 'u2', snapshot: { name: 'Bot', kind: 'service' } }),
    ]))
    show()
    expect((await screen.findByText('a@x.com')).closest('a')?.getAttribute('href')).toBe('/admin/users/u1')
    expect(screen.getByText('Bot').closest('a')?.getAttribute('href')).toBe('/admin/service-accounts/u2')
  })
})
