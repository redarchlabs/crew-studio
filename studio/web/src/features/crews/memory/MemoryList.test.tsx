import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import type { MemoryRecord } from '../../../api/types.memory'
import { renderRoute, setupApi } from '../testUtils'
import { MemoryList } from './MemoryList'
import { makeMemory } from './testUtils'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.memory')

const LATER = '2026-10-04T09:00:00Z'
const page = (items: MemoryRecord[], total = items.length) => ({ items, meta: { total, limit: 25, offset: 0 } })

const fromRun = makeMemory()
const fromPerson = makeMemory({
  id: 'm2', content: 'Atlas dates slipped three times', scope: '/projects/atlas', categories: [], source: 'user:Ada', run_id: null, written_by: 'u1',
  agent_role: '', task_name: '', pinned: true, km2_document_id: 'doc-1', updated_at: LATER,
})
const pending = makeMemory({ id: 'm3', content: 'Vendor page lags outages', scope: '/', status: 'pending', source: 'run:gone', run_id: null })

beforeEach(() => {
  setupApi()
  vi.mocked(memoryApi.list).mockResolvedValue(page([fromRun, fromPerson, pending]))
  vi.mocked(memoryApi.scopes).mockResolvedValue([{ scope: '/people/sam', count: 1 }, { scope: '/projects/atlas', count: 1 }, { scope: '/', count: 1 }])
})
afterEach(cleanup)

const render = (canManage = true) => renderRoute(<MemoryList crewId="c1" canManage={canManage} />)
const row = (target: string | HTMLElement) => within((typeof target === 'string' ? screen.getByText(target) : target).closest('li') as HTMLElement)
const lastQuery = () => vi.mocked(memoryApi.list).mock.lastCall?.[1]

describe('MemoryList', () => {
  it('shows each memory with where it came from, read-only without state.manage', async () => {
    render(false)
    const run = row(await screen.findByText(fromRun.content))
    expect(run.getByRole('link', { name: 'r1bbbbbb' }).getAttribute('href')).toBe('/runs/r1bbbbbbbbbb')
    expect(run.getByText(/task Daily sweep · agent Coordinator/)).toBeTruthy()
    expect(run.getByText('/people/sam')).toBeTruthy()
    expect(run.getByText('people')).toBeTruthy()
    expect(run.queryByText(/edited/)).toBeNull()
    const person = row(fromPerson.content)
    expect(person.getByText(/Added by Ada/)).toBeTruthy()
    expect(person.getByText(/edited/)).toBeTruthy()
    expect(person.getByText('pinned')).toBeTruthy()
    expect(person.getByText('in KM2').getAttribute('title')).toBe('KM2 document doc-1')
    const waiting = row(pending.content)
    expect(waiting.getByText('waiting for review')).toBeTruthy()
    expect(waiting.getByText(/Saved by a run that was since deleted/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Approve' })).toBeNull()
    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.queryByRole('button', { name: /Add a memory/ })).toBeNull()
  })

  it('searches, filters by scope, status and pin, and pages', async () => {
    vi.mocked(memoryApi.list).mockResolvedValue(page([fromRun], 30))
    render()
    await screen.findByText(fromRun.content)
    expect(lastQuery()).toEqual({ q: '', scope: '', status: '', pinned: false, limit: 25, offset: 0 })
    const tree = within(screen.getByRole('navigation', { name: 'Scopes' }))
    expect(tree.getByRole('button', { name: /All scopes\s*3/ }).getAttribute('aria-current')).toBe('true')
    fireEvent.click(tree.getByRole('button', { name: /\/projects\s*1/ }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ scope: '/projects' }))
    expect(tree.getByRole('button', { name: /\/projects\s*1/ }).getAttribute('aria-current')).toBe('true')
    fireEvent.change(screen.getByLabelText('Search memories'), { target: { value: ' fridays ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ q: 'fridays', scope: '/projects' }))
    fireEvent.change(screen.getByLabelText('Status filter'), { target: { value: 'pending' } })
    await waitFor(() => expect(lastQuery()).toMatchObject({ status: 'pending' }))
    fireEvent.click(screen.getByRole('switch', { name: /Pinned only/ }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ pinned: true }))
    fireEvent.click(await screen.findByRole('button', { name: 'Next page' }))
    await waitFor(() => expect(lastQuery()).toMatchObject({ offset: 25 }))
  })

  it('shows empty states', async () => {
    vi.mocked(memoryApi.list).mockResolvedValue(page([]))
    vi.mocked(memoryApi.scopes).mockResolvedValue([])
    render()
    expect(await screen.findByText('No memories yet')).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: /Pinned only/ }))
    expect(await screen.findByText('No matching memories')).toBeTruthy()
  })

  it('approves, pins, unpins and deletes, reporting failures', async () => {
    vi.mocked(memoryApi.approve).mockResolvedValue({ ...pending, status: 'active' })
    vi.mocked(memoryApi.update).mockResolvedValue(fromRun)
    vi.mocked(memoryApi.remove).mockRejectedValueOnce(new ApiError(500, { code: 'x', message: 'Nope' })).mockResolvedValueOnce(undefined)
    render()
    fireEvent.click(row(await screen.findByText(pending.content)).getByRole('button', { name: 'Approve' }))
    await waitFor(() => expect(memoryApi.approve).toHaveBeenCalledWith('c1', 'm3'))
    await waitFor(() => expect(memoryApi.scopes).toHaveBeenCalledTimes(2))
    fireEvent.click(row(fromRun.content).getByRole('button', { name: 'Pin' }))
    await waitFor(() => expect(memoryApi.update).toHaveBeenCalledWith('c1', fromRun.id, { pinned: true }))
    fireEvent.click(await waitFor(() => row(fromPerson.content).getByRole('button', { name: 'Unpin' })))
    await waitFor(() => expect(memoryApi.update).toHaveBeenLastCalledWith('c1', 'm2', { pinned: false }))
    const del = () => row(fromRun.content).getByRole('button', { name: /Delete|Confirm/ })
    await waitFor(() => expect((del() as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(del())
    fireEvent.click(del())
    await waitFor(() => expect(memoryApi.remove).toHaveBeenCalledTimes(1))
    await waitFor(() => expect((del() as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(del())
    fireEvent.click(del())
    await waitFor(() => expect(memoryApi.remove).toHaveBeenCalledTimes(2))
  })

  it('adds and edits memories in a dialog', async () => {
    vi.mocked(memoryApi.create).mockResolvedValue(fromRun)
    render()
    await screen.findByText(fromRun.content)
    fireEvent.click(within(screen.getByRole('navigation', { name: 'Scopes' })).getByRole('button', { name: /\/people\s*1/ }))
    fireEvent.click(screen.getByRole('button', { name: /Add a memory/ }))
    const dialog = within(await screen.findByRole('dialog'))
    expect((dialog.getByLabelText('Scope') as HTMLInputElement).value).toBe('/people')
    fireEvent.change(dialog.getByLabelText('Memory'), { target: { value: 'New lesson' } })
    fireEvent.click(dialog.getByRole('button', { name: 'Add memory' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(memoryApi.create).toHaveBeenCalled()
    fireEvent.click(await waitFor(() => row(fromRun.content).getByRole('button', { name: 'Edit' })))
    expect(await screen.findByText('Edit memory')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('promotes and deletes a selection, and deletes by scope or text', async () => {
    vi.mocked(memoryApi.promote).mockResolvedValue({ outcome: 'created', document_id: 'doc-9', external_ref: '', folder_id: 'f1' })
    vi.mocked(memoryApi.bulkDelete).mockResolvedValue({ matched: 1, deleted: 1 })
    render()
    await screen.findByText(fromRun.content)
    fireEvent.click(screen.getByRole('checkbox', { name: /Select memory m1aaaaaa/ }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Select memory m2/ }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Select memory m2/ }))
    const bar = within(screen.getByTestId('memory-selection'))
    expect(bar.getByText('1 memory selected')).toBeTruthy()
    fireEvent.click(bar.getByRole('button', { name: 'Promote to KM2' }))
    fireEvent.change(await screen.findByLabelText('Title'), { target: { value: 'Lessons' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save to KM2' }))
    expect(await screen.findByTestId('memory-promote-result')).toBeTruthy()
    expect(memoryApi.promote).toHaveBeenCalledWith('c1', { memory_ids: [fromRun.id], title: 'Lessons' })
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(screen.queryByTestId('memory-selection')).toBeNull()

    fireEvent.click(await waitFor(() => screen.getByRole('checkbox', { name: /Select memory m3/ })))
    fireEvent.click(within(screen.getByTestId('memory-selection')).getByRole('button', { name: 'Delete selected' }))
    expect(await screen.findByText('Delete 1 selected memory')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Count matches' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete 1 memory' }))
    await waitFor(() => expect(memoryApi.bulkDelete).toHaveBeenLastCalledWith('c1', { ids: ['m3'], dry_run: false }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: /Delete memories…/ }))
    expect(await screen.findByLabelText('Contains')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    fireEvent.click(await waitFor(() => screen.getByRole('checkbox', { name: /Select memory m3/ })))
    fireEvent.click(within(screen.getByTestId('memory-selection')).getByRole('button', { name: 'Clear' }))
    expect(screen.queryByTestId('memory-selection')).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: /Select memory m1aaaaaa/ }))
    fireEvent.click(within(screen.getByTestId('memory-selection')).getByRole('button', { name: 'Promote to KM2' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('will not promote a selection that includes a memory waiting for review', async () => {
    render()
    await screen.findByText(pending.content)
    fireEvent.click(screen.getByRole('checkbox', { name: /Select memory m1aaaaaa/ }))
    const bar = () => within(screen.getByTestId('memory-selection'))
    const promote = () => bar().getByRole('button', { name: 'Promote to KM2' }) as HTMLButtonElement
    expect(promote().disabled).toBe(false)
    expect(bar().queryByText('Approve pending memories before promoting them to KM2.')).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: /Select memory m3/ }))
    expect(promote().disabled).toBe(true)
    expect(bar().getByText('Approve pending memories before promoting them to KM2.')).toBeTruthy()
    fireEvent.click(promote())
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
