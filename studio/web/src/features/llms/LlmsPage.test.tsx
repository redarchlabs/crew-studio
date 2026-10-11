import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { llmsApi, pricingApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeLlm, makePrice } from '../../lib/testFixtures'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { LlmsPage } from './LlmsPage'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const llms = [
  makeLlm({ id: 'l2', name: 'Zeta', model: 'openai/gpt-6', temperature: 0.5, max_completion_tokens: 2048, reasoning_effort: 'high', api: 'responses' }),
  makeLlm({ id: 'l1', name: 'Alpha', model: 'anthropic/none' }),
]

beforeEach(() => {
  setupApi()
  vi.mocked(llmsApi.list).mockResolvedValue(llms)
  vi.mocked(pricingApi.list).mockResolvedValue([makePrice({ id: 'p', model: 'gpt-6' })])
})
afterEach(cleanup)

const names = () => screen.getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('cell')[0].textContent)
const row = (name: string) => screen.getByText(name).closest('tr') as HTMLElement

describe('LlmsPage', () => {
  it('lists llms sorted by name with defaults and responses badge', async () => {
    renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    expect(names()).toEqual(['Alpha', 'Zeta'])
    const alpha = row('Alpha')
    expect(within(alpha).getByText('anthropic/none')).toBeTruthy()
    expect(within(alpha).getAllByText('default').length).toBe(3)
    const zeta = row('Zeta')
    expect(within(zeta).getByText('high')).toBeTruthy()
    expect(within(zeta).getByText('responses')).toBeTruthy()
    expect(within(zeta).getByText('0.5')).toBeTruthy()
    expect(within(zeta).getByText('2048')).toBeTruthy()
  })

  it('shows price match status per llm', async () => {
    renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    expect(within(row('Alpha')).getByText(/No price entry/)).toBeTruthy()
    expect(within(row('Zeta')).getByText('gpt-6')).toBeTruthy()
  })

  it('omits the price column and does not request prices without pricing.view', async () => {
    renderRoute(<LlmsPage />, { perms: without('pricing.view') })
    await screen.findByText('Alpha')
    expect(screen.queryByText('Price entry')).toBeNull()
    expect(pricingApi.list).not.toHaveBeenCalled()
  })

  it('is read-only without llms.manage', async () => {
    renderRoute(<LlmsPage />, { perms: without('llms.manage') })
    await screen.findByText('Alpha')
    expect(screen.getByRole('note').textContent).toMatch(/llms\.manage/)
    expect(screen.queryByRole('button', { name: 'New LLM' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Edit Alpha/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Alpha' })).toBeNull()
  })

  it('ignores ?edit= without llms.manage', async () => {
    renderRoute(<LlmsPage />, { url: '/?edit=l1', perms: without('llms.manage') })
    await screen.findByText('Alpha')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('opens the editor from a name click and from the ?edit= param', async () => {
    const { router, unmount } = renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Alpha' }))
    expect(await screen.findByText('Edit Alpha')).toBeTruthy()
    expect(router.state.location.search).toBe('?edit=l1')
    unmount()
    renderRoute(<LlmsPage />, { url: '/?edit=l2' })
    expect(await screen.findByText('Edit Zeta')).toBeTruthy()
  })

  it('opens from the pencil button and closing clears the param', async () => {
    const { router } = renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Edit Zeta' }))
    await screen.findByText('Edit Zeta')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(router.state.location.search).toBe('')
  })

  it('ignores an unknown ?edit= id', async () => {
    renderRoute(<LlmsPage />, { url: '/?edit=nope' })
    await screen.findByText('Alpha')
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('creates an llm from the New LLM dialog and inserts it sorted', async () => {
    vi.mocked(llmsApi.create).mockResolvedValue(makeLlm({ id: 'l3', name: 'Mid', model: 'openai/mid' }))
    renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    fireEvent.click(screen.getByRole('button', { name: 'New LLM' }))
    fireEvent.change(await screen.findByLabelText('Name'), { target: { value: 'Mid' } })
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'openai/mid' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(names()).toEqual(['Alpha', 'Mid', 'Zeta']))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('replaces an edited llm in place', async () => {
    vi.mocked(llmsApi.update).mockResolvedValue({ ...llms[1], name: 'Omega' })
    renderRoute(<LlmsPage />, { url: '/?edit=l1' })
    await screen.findByText('Edit Alpha')
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Omega' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(names()).toEqual(['Omega', 'Zeta']))
  })

  it('deletes with two clicks and removes the row', async () => {
    vi.mocked(llmsApi.remove).mockResolvedValue(undefined)
    renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    const del = within(row('Alpha')).getByRole('button', { name: 'Delete' })
    fireEvent.click(del)
    expect(llmsApi.remove).not.toHaveBeenCalled()
    fireEvent.click(within(row('Alpha')).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(screen.queryByText('Alpha')).toBeNull())
    expect(llmsApi.remove).toHaveBeenCalledWith('l1')
    expect(toast.success).toHaveBeenCalledWith('LLM deleted')
  })

  it('keeps the row and shows a toast when delete conflicts (409)', async () => {
    vi.mocked(llmsApi.remove).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'LLM is used by agent Ann' }))
    renderRoute(<LlmsPage />)
    await screen.findByText('Alpha')
    fireEvent.click(within(row('Alpha')).getByRole('button', { name: 'Delete' }))
    fireEvent.click(within(row('Alpha')).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete Alpha', { description: 'LLM is used by agent Ann' }))
    expect(screen.getByText('Alpha')).toBeTruthy()
  })

  it('shows the empty state with an add action for managers', async () => {
    vi.mocked(llmsApi.list).mockResolvedValue([])
    renderRoute(<LlmsPage />)
    expect(await screen.findByText('No LLMs yet')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add an LLM' }))
    expect(await screen.findByText('New LLM', { selector: 'h2' })).toBeTruthy()
  })

  it('empty state for non-managers has no action', async () => {
    vi.mocked(llmsApi.list).mockResolvedValue([])
    renderRoute(<LlmsPage />, { perms: without('llms.manage') })
    expect(await screen.findByText(/An administrator has not added/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Add an LLM' })).toBeNull()
  })

  it('shows an error and retries', async () => {
    vi.mocked(llmsApi.list).mockRejectedValueOnce(new ApiError(500, { code: 'x', message: 'llm list failed' }))
    renderRoute(<LlmsPage />)
    expect(await screen.findByText('llm list failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('Alpha')).toBeTruthy()
  })

  it('gates the history link', async () => {
    renderRoute(<LlmsPage />, { perms: without('history.view') })
    await screen.findByText('Alpha')
    expect(screen.queryByText('History')).toBeNull()
  })
})
