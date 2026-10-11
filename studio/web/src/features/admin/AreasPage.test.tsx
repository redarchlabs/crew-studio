import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { AreasPage } from './AreasPage'
import { api, apiError, makeArea, makeMe, notifyMock, renderRoute, resetAll, setAuth } from './testUtils'
import { invalidateAreas } from '../../lib/useAreas'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('./testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('./testUtils')).notifyMock)

const USAGE0 = { crews: 0, agents: 0, tasks: 0, users: 0 }
const FIN = makeArea({ id: 'a-fin', name: 'Finance', description: 'Money', usage: { crews: 2, agents: 1, tasks: 0, users: 3 } })
const HR = makeArea({ id: 'a-hr', name: 'HR', usage: { ...USAGE0, users: 1 } })

beforeEach(() => {
  invalidateAreas()
  resetAll(['admin.areas'])
  api.areasApi.list.mockResolvedValue([HR, FIN])
})

const nameInput = () => screen.getByLabelText('Area name') as HTMLInputElement
const dataRows = () => screen.getAllByRole('row').slice(1)

async function open() {
  renderRoute(<AreasPage />)
  await screen.findByText('Finance')
}

describe('AreasPage listing', () => {
  it('lists areas sorted by name with usage summaries', async () => {
    await open()
    expect(dataRows().map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['Finance', 'HR'])
    expect(screen.getByText('2 crews · 1 agent · 0 tasks · 3 accounts')).toBeTruthy()
    expect(screen.getByText('0 crews · 0 agents · 0 tasks · 1 account')).toBeTruthy()
  })

  it('shows a dash when usage is not returned', async () => {
    api.areasApi.list.mockResolvedValue([makeArea({ id: 'a-x', name: 'Ops' })])
    renderRoute(<AreasPage />)
    await screen.findByText('Ops')
    expect(within(dataRows()[0]).getAllByText('—').length).toBeGreaterThan(0)
  })

  it('shows an empty state whose action opens the add row', async () => {
    api.areasApi.list.mockResolvedValue([])
    renderRoute(<AreasPage />)
    expect(await screen.findByText('No functional areas yet')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add the first area' }))
    expect(nameInput()).toBeTruthy()
    expect(screen.queryByText('No functional areas yet')).toBeNull()
  })

  it('surfaces load errors', async () => {
    api.areasApi.list.mockRejectedValue(apiError(403, 'no areas for you'))
    renderRoute(<AreasPage />)
    expect(await screen.findByText('no areas for you')).toBeTruthy()
  })

  it('shows the history link only with admin.audit', async () => {
    await open()
    expect(screen.queryByText('History')).toBeNull()
    setAuth({ me: makeMe({ permissions: ['admin.areas', 'admin.audit'] }) })
    renderRoute(<AreasPage />)
    await waitFor(() => expect(screen.getByText('History')).toBeTruthy())
  })
})

describe('creating an area', () => {
  it('adds an area and keeps the list sorted', async () => {
    api.areasApi.create.mockResolvedValue(makeArea({ id: 'a-it', name: 'IT', description: 'Tech' }))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    expect((screen.getByRole('button', { name: 'Add area' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(nameInput(), { target: { value: '  IT ' } })
    fireEvent.change(screen.getByLabelText('Area description'), { target: { value: ' Tech ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Added IT'))
    expect(api.areasApi.create).toHaveBeenCalledWith({ name: 'IT', description: 'Tech' })
    expect(dataRows().map((r) => within(r).getAllByRole('cell')[0].textContent)).toEqual(['Finance', 'HR', 'IT'])
    // A fresh area has zero usage, so it can be deleted straight away
    expect(screen.getByText('0 crews · 0 agents · 0 tasks · 0 accounts')).toBeTruthy()
  })

  it('saves with Enter and cancels with Escape', async () => {
    api.areasApi.create.mockResolvedValue(makeArea({ id: 'a-it', name: 'IT' }))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.keyDown(nameInput(), { key: 'Escape' })
    expect(screen.queryByLabelText('Area name')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.change(nameInput(), { target: { value: 'IT' } })
    fireEvent.keyDown(nameInput(), { key: 'Enter' })
    await waitFor(() => expect(api.areasApi.create).toHaveBeenCalledTimes(1))
  })

  it('cancel discards the draft', async () => {
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByLabelText('Area name')).toBeNull()
    expect(api.areasApi.create).not.toHaveBeenCalled()
  })

  it('rejects an empty name and a case-insensitive duplicate locally', async () => {
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getByText('Name is required')).toBeTruthy()
    fireEvent.change(nameInput(), { target: { value: ' finance ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getByText('Another area already has this name')).toBeTruthy()
    expect(api.areasApi.create).not.toHaveBeenCalled()
  })

  it('shows 422 field errors from the server and keeps the row open', async () => {
    api.areasApi.create.mockRejectedValue(apiError(422, 'bad', 'validation', ['name: too long', 'description: too long too']))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.change(nameInput(), { target: { value: 'Zed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('too long')).toBeTruthy()
    expect(screen.getByText('too long too')).toBeTruthy()
    expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not save the area', expect.anything())
    expect(nameInput().value).toBe('Zed')
  })

  it('shows general (unmapped) errors above the table', async () => {
    api.areasApi.create.mockRejectedValue(apiError(422, 'bad', 'validation', ['something odd']))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.change(nameInput(), { target: { value: 'Zed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('something odd')).toBeTruthy()
  })

  it('reports a 403 on create', async () => {
    api.areasApi.create.mockRejectedValue(apiError(403, 'forbidden'))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Add area' }))
    fireEvent.change(nameInput(), { target: { value: 'Zed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalled())
    expect(screen.getByLabelText('Area name')).toBeTruthy()
  })
})

describe('renaming an area', () => {
  it('renames via the pencil button, allowing the area to keep its own name', async () => {
    api.areasApi.update.mockResolvedValue(makeArea({ id: 'a-fin', name: 'Finance & Ops', description: 'Money' }))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Rename Finance' }))
    expect(nameInput().value).toBe('Finance')
    // Saving the unchanged name is not a duplicate of itself
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.areasApi.update).toHaveBeenCalledWith('a-fin', { name: 'Finance', description: 'Money' }))
    expect(await screen.findByRole('button', { name: 'Rename Finance & Ops' })).toBeTruthy()
  })

  it('keeps usage counts after a rename when the server omits them', async () => {
    api.areasApi.update.mockResolvedValue(makeArea({ id: 'a-fin', name: 'Finance 2', description: 'Money' }))
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Rename Finance' }))
    fireEvent.change(nameInput(), { target: { value: 'Finance 2' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Finance 2')).toBeTruthy()
    expect(screen.getByText('2 crews · 1 agent · 0 tasks · 3 accounts')).toBeTruthy()
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Saved Finance 2')
  })

  it('rejects renaming onto another area name', async () => {
    await open()
    fireEvent.click(screen.getByRole('button', { name: 'Rename HR' }))
    fireEvent.change(nameInput(), { target: { value: 'FINANCE' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(screen.getByText('Another area already has this name')).toBeTruthy()
    expect(api.areasApi.update).not.toHaveBeenCalled()
  })

  it('double-click enters edit mode and clears stale errors', async () => {
    await open()
    fireEvent.doubleClick(screen.getByText('Finance').closest('tr')!)
    expect(nameInput().value).toBe('Finance')
  })
})

describe('deleting an area', () => {
  it('blocks deleting an area that still holds crews, agents or tasks', async () => {
    await open()
    const row = screen.getByText('Finance').closest('tr')!
    const del = within(row).getByRole('button', { name: 'Delete' }) as HTMLButtonElement
    expect(del.disabled).toBe(true)
    expect(del.parentElement?.getAttribute('title')).toMatch(/Holds 3 crews, agents or tasks/)
    fireEvent.click(del)
    expect(api.areasApi.remove).not.toHaveBeenCalled()
  })

  it('deletes an area that only has accounts, after confirming', async () => {
    api.areasApi.remove.mockResolvedValue(undefined)
    await open()
    const row = screen.getByText('HR').closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: 'Delete' }))
    expect(api.areasApi.remove).not.toHaveBeenCalled()
    fireEvent.click(within(row).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(api.areasApi.remove).toHaveBeenCalledWith('a-hr'))
    await waitFor(() => expect(screen.queryByText('HR')).toBeNull())
    expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Deleted HR')
  })

  it('shows the server error when a non-empty delete is refused (stale counts) and reloads', async () => {
    api.areasApi.remove.mockRejectedValue(apiError(409, 'area still holds 1 crew', 'area_in_use'))
    await open()
    const row = screen.getByText('HR').closest('tr')!
    fireEvent.click(within(row).getByRole('button', { name: 'Delete' }))
    fireEvent.click(within(row).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not delete HR', expect.objectContaining({ status: 409, message: 'area still holds 1 crew' })))
    await waitFor(() => expect(api.areasApi.list.mock.calls.length).toBeGreaterThanOrEqual(2))
    expect(screen.getByText('HR')).toBeTruthy()
  })
})
