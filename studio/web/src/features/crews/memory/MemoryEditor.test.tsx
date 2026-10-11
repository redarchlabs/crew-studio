import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import { renderRoute, setupApi } from '../testUtils'
import { MemoryEditor } from './MemoryEditor'
import { makeMemory } from './testUtils'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.memory')

beforeEach(() => setupApi())
afterEach(cleanup)

const add = () => screen.getByRole('button', { name: 'Add memory' })

describe('MemoryEditor', () => {
  it('adds a memory in the scope being browsed', async () => {
    const onSaved = vi.fn()
    const created = makeMemory({ id: 'new' })
    vi.mocked(memoryApi.create).mockResolvedValue(created)
    renderRoute(<MemoryEditor crewId="c1" memory={null} defaultScope="/people" onClose={vi.fn()} onSaved={onSaved} />)
    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect((screen.getByLabelText('Scope') as HTMLInputElement).value).toBe('/people')
    fireEvent.change(screen.getByLabelText('Memory'), { target: { value: '  The vendor status page lags real outages  ' } })
    expect(screen.getByText(/^40\/2000 characters/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: 'vendors/acme/' } })
    fireEvent.change(screen.getByLabelText('Categories (optional)'), { target: { value: 'vendor, outages' } })
    fireEvent.change(screen.getByLabelText('Importance'), { target: { value: '0.8' } })
    fireEvent.click(screen.getByRole('switch', { name: /Pin this memory/ }))
    fireEvent.click(add())
    await waitFor(() => expect(memoryApi.create).toHaveBeenCalledWith('c1', {
      content: 'The vendor status page lags real outages', scope: '/vendors/acme', categories: ['vendor', 'outages'], importance: 0.8, pinned: true,
    }))
    expect(onSaved).toHaveBeenCalledWith(created)
  })

  it('checks the fields before sending', async () => {
    renderRoute(<MemoryEditor crewId="c1" memory={null} onClose={vi.fn()} onSaved={vi.fn()} />)
    await screen.findByRole('dialog')
    expect((screen.getByLabelText('Scope') as HTMLInputElement).value).toBe('/')
    fireEvent.change(screen.getByLabelText('Scope'), { target: { value: '/People' } })
    fireEvent.change(screen.getByLabelText('Categories (optional)'), { target: { value: 'Bad!' } })
    fireEvent.change(screen.getByLabelText('Importance'), { target: { value: '' } })
    fireEvent.click(add())
    expect(await screen.findByText('Write what the crew should remember.')).toBeTruthy()
    expect(screen.getByText(/Use lowercase letters/)).toBeTruthy()
    expect(screen.getByText(/Categories use lowercase/)).toBeTruthy()
    expect(screen.getByText('Use a number from 0 to 1.')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Memory'), { target: { value: 'x'.repeat(2001) } })
    fireEvent.change(screen.getByLabelText('Importance'), { target: { value: '2' } })
    fireEvent.click(add())
    expect(await screen.findByText('Use at most 2000 characters.')).toBeTruthy()
    expect(screen.getByText('Use a number from 0 to 1.')).toBeTruthy()
    expect(memoryApi.create).not.toHaveBeenCalled()
  })

  it('edits a pending memory and shows server errors', async () => {
    const memory = makeMemory({ status: 'pending', categories: ['people', 'timing'], pinned: true })
    vi.mocked(memoryApi.update)
      .mockRejectedValueOnce(new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['content: has control characters', 'metadata: too big'] }))
      .mockRejectedValueOnce(new ApiError(403, { code: 'forbidden', message: 'You may not change memories.' }))
      .mockResolvedValueOnce(memory)
    const onSaved = vi.fn()
    renderRoute(<MemoryEditor crewId="c1" memory={memory} onClose={vi.fn()} onSaved={onSaved} />)
    expect(await screen.findByText('Edit memory')).toBeTruthy()
    expect(screen.getByText(/Saving does not approve it/)).toBeTruthy()
    expect((screen.getByLabelText('Categories (optional)') as HTMLInputElement).value).toBe('people, timing')
    const save = screen.getByRole('button', { name: 'Save memory' })
    fireEvent.click(save)
    expect(await screen.findByText('has control characters')).toBeTruthy()
    expect(screen.getByText('metadata: too big')).toBeTruthy()
    fireEvent.click(save)
    expect(await screen.findByText('You may not change memories.')).toBeTruthy()
    fireEvent.click(save)
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(memory))
    expect(memoryApi.update).toHaveBeenLastCalledWith('c1', memory.id, {
      content: memory.content, scope: '/people/sam', categories: ['people', 'timing'], importance: 0.5, pinned: true,
    })
  })

  it('shows a duplicate on the memory field, when adding and when editing', async () => {
    const dup = new ApiError(409, { code: 'memory_exists', message: 'A memory in this scope already says the same thing.' })
    vi.mocked(memoryApi.create).mockRejectedValueOnce(dup)
    const { unmount } = renderRoute(<MemoryEditor crewId="c1" memory={null} onClose={vi.fn()} onSaved={vi.fn()} />)
    await screen.findByRole('dialog')
    fireEvent.change(screen.getByLabelText('Memory'), { target: { value: 'Sam usually replies late on Fridays' } })
    fireEvent.click(add())
    expect(await screen.findByText('That memory already exists in this scope.')).toBeTruthy()
    expect(screen.getByLabelText('Memory').getAttribute('aria-invalid')).toBe('true')
    unmount()
    vi.mocked(memoryApi.update).mockRejectedValueOnce(dup)
    renderRoute(<MemoryEditor crewId="c1" memory={makeMemory()} onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Save memory' }))
    expect(await screen.findByText('That memory already exists in this scope.')).toBeTruthy()
  })

  it('closes on Cancel', async () => {
    const onClose = vi.fn()
    renderRoute(<MemoryEditor crewId="c1" memory={null} onClose={onClose} onSaved={vi.fn()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
  })
})
