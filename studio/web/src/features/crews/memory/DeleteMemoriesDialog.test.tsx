import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import { renderRoute, setupApi } from '../testUtils'
import { DeleteMemoriesDialog } from './DeleteMemoriesDialog'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.memory')

beforeEach(() => setupApi())
afterEach(cleanup)

const count = () => screen.getByRole('button', { name: 'Count matches' })

describe('DeleteMemoriesDialog', () => {
  it('counts what a person-wide delete matches, then deletes it', async () => {
    vi.mocked(memoryApi.bulkDelete).mockResolvedValueOnce({ matched: 3, deleted: 0 }).mockResolvedValueOnce({ matched: 3, deleted: 3 })
    const onDeleted = vi.fn()
    renderRoute(<DeleteMemoriesDialog crewId="c1" onClose={vi.fn()} onDeleted={onDeleted} />)
    expect(await screen.findByText('Delete memories')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'people/sam/' } })
    fireEvent.change(screen.getByLabelText('Contains'), { target: { value: ' Sam Lee ' } })
    fireEvent.click(count())
    expect((await screen.findByTestId('memory-delete-count')).textContent).toMatch(/^3 memories match\./)
    expect(memoryApi.bulkDelete).toHaveBeenCalledWith('c1', { scope_prefix: '/people/sam', contains: 'Sam Lee', dry_run: true })
    fireEvent.click(screen.getByRole('button', { name: 'Delete 3 memories' }))
    await waitFor(() => expect(onDeleted).toHaveBeenCalledWith(3))
    expect(memoryApi.bulkDelete).toHaveBeenLastCalledWith('c1', { scope_prefix: '/people/sam', contains: 'Sam Lee', dry_run: false })
  })

  it('resets a scope, and recounts after the filters change', async () => {
    vi.mocked(memoryApi.bulkDelete).mockResolvedValueOnce({ matched: 1, deleted: 0 }).mockResolvedValueOnce({ matched: 0, deleted: 0 })
    renderRoute(<DeleteMemoriesDialog crewId="c1" initialScope="/projects" onClose={vi.fn()} onDeleted={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Count matches' }))
    expect((await screen.findByTestId('memory-delete-count')).textContent).toMatch(/^1 memory matches\./)
    expect(memoryApi.bulkDelete).toHaveBeenCalledWith('c1', { scope_prefix: '/projects', dry_run: true })
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: '' } })
    expect(screen.queryByTestId('memory-delete-count')).toBeNull()
    fireEvent.change(screen.getByLabelText('Contains'), { target: { value: 'nobody' } })
    fireEvent.click(count())
    expect((await screen.findByTestId('memory-delete-count')).textContent).toBe('No memories match.')
    expect(memoryApi.bulkDelete).toHaveBeenLastCalledWith('c1', { contains: 'nobody', dry_run: true })
    expect(count()).toBeTruthy()
  })

  it('asks for a filter, checks the scope, and shows errors', async () => {
    vi.mocked(memoryApi.bulkDelete)
      .mockRejectedValueOnce(new ApiError(403, { code: 'forbidden', message: 'No access.' }))
      .mockResolvedValueOnce({ matched: 2, deleted: 0 })
      .mockRejectedValueOnce(new ApiError(500, { code: 'internal', message: 'Boom.' }))
    renderRoute(<DeleteMemoriesDialog crewId="c1" onClose={vi.fn()} onDeleted={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Count matches' }))
    expect(await screen.findByText(/Enter a scope, some text, or both/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: '/Bad Scope' } })
    fireEvent.click(count())
    expect(await screen.findByText(/^Scope: Use lowercase/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: '/' } })
    fireEvent.click(count())
    expect(await screen.findByText('No access.')).toBeTruthy()
    fireEvent.click(count())
    fireEvent.click(await screen.findByRole('button', { name: 'Delete 2 memories' }))
    expect(await screen.findByText('Boom.')).toBeTruthy()
    // Changing a filter after a failed delete asks for a new count
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: '' } })
    expect(count()).toBeTruthy()
  })

  it('deletes a selection without filters', async () => {
    vi.mocked(memoryApi.bulkDelete).mockResolvedValueOnce({ matched: 2, deleted: 0 }).mockResolvedValueOnce({ matched: 2, deleted: 2 })
    const onDeleted = vi.fn()
    const onClose = vi.fn()
    renderRoute(<DeleteMemoriesDialog crewId="c1" ids={['m1', 'm2']} onClose={onClose} onDeleted={onDeleted} />)
    expect(await screen.findByText('Delete 2 selected memories')).toBeTruthy()
    expect(screen.queryByLabelText('Scope')).toBeNull()
    fireEvent.click(count())
    fireEvent.click(await screen.findByRole('button', { name: 'Delete 2 memories' }))
    await waitFor(() => expect(onDeleted).toHaveBeenCalledWith(2))
    expect(memoryApi.bulkDelete).toHaveBeenLastCalledWith('c1', { ids: ['m1', 'm2'], dry_run: false })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
  })
})
