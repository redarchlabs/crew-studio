import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { llmsApi, pricingApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeLlm, makePrice } from '../../lib/testFixtures'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { PricingPage } from './PricingPage'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const prices = [
  makePrice({ id: 'p2', model: 'zeta', input_per_mtok: 3, output_per_mtok: 12 }),
  makePrice({ id: 'p1', model: 'alpha', input_per_mtok: 1, output_per_mtok: 4 }),
]

beforeEach(() => {
  setupApi()
  vi.mocked(pricingApi.list).mockResolvedValue(prices)
  vi.mocked(llmsApi.list).mockResolvedValue([makeLlm({ id: 'l1', name: 'First', model: 'openai/alpha' }), makeLlm({ id: 'l2', name: 'Second', model: 'zeta' }), makeLlm({ id: 'l3', name: 'Third', model: 'alpha' })])
})
afterEach(cleanup)

const models = () => screen.getAllByRole('row').slice(1).map((r) => within(r).getAllByRole('cell')[0].textContent)
const row = (model: string) => screen.getByText(model).closest('tr') as HTMLElement

describe('PricingPage', () => {
  it('lists prices sorted by model with usage counts', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    expect(models()).toEqual(['alpha', 'zeta'])
    expect(within(row('alpha')).getByText('2 LLMs')).toBeTruthy()
    expect(within(row('zeta')).getByText('1 LLM')).toBeTruthy()
  })

  it('shows unused prices', async () => {
    vi.mocked(llmsApi.list).mockResolvedValue([])
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    expect(screen.getAllByText('unused')).toHaveLength(2)
  })

  it('omits usage and does not load llms without llms.view', async () => {
    renderRoute(<PricingPage />, { perms: without('llms.view') })
    await screen.findByText('alpha')
    expect(screen.queryByText('Used by')).toBeNull()
    expect(llmsApi.list).not.toHaveBeenCalled()
  })

  it('is read-only without pricing.manage', async () => {
    renderRoute(<PricingPage />, { perms: without('pricing.manage') })
    await screen.findByText('alpha')
    expect(screen.getByRole('note').textContent).toMatch(/pricing\.manage/)
    expect(screen.queryByRole('button', { name: 'Add price' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Edit/ })).toBeNull()
    expect(screen.queryByText(/double-click/)).toBeNull()
    fireEvent.doubleClick(screen.getByText('alpha'))
    expect(screen.queryByLabelText('Model')).toBeNull()
  })

  it('empty state offers the first price to managers', async () => {
    vi.mocked(pricingApi.list).mockResolvedValue([])
    renderRoute(<PricingPage />)
    expect(await screen.findByText('No prices yet')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Add the first price' }))
    expect(await screen.findByLabelText('Model')).toBeTruthy()
    expect(screen.queryByText('No prices yet')).toBeNull()
  })

  it('empty state has no action for non-managers', async () => {
    vi.mocked(pricingApi.list).mockResolvedValue([])
    renderRoute(<PricingPage />, { perms: without('pricing.manage') })
    expect(await screen.findByText('No prices yet')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Add the first price' })).toBeNull()
  })

  it('shows an error and retries', async () => {
    vi.mocked(pricingApi.list).mockRejectedValueOnce(new ApiError(500, { code: 'x', message: 'pricing failed' }))
    renderRoute(<PricingPage />)
    expect(await screen.findByText('pricing failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('alpha')).toBeTruthy()
  })

  it('gates the history link', async () => {
    renderRoute(<PricingPage />, { perms: without('history.view') })
    await screen.findByText('alpha')
    expect(screen.queryByText('History')).toBeNull()
  })
})

describe('PricingPage: add', () => {
  it('adds a price and keeps the list sorted', async () => {
    vi.mocked(pricingApi.create).mockResolvedValue(makePrice({ id: 'p3', model: 'mid', input_per_mtok: 2 }))
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Add price' }))
    expect((screen.getByRole('button', { name: 'Add price' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: '  mid ' } })
    fireEvent.change(screen.getByLabelText('Input per 1M tokens'), { target: { value: '2' } })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: '  note ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(models()).toEqual(['alpha', 'mid', 'zeta']))
    expect(pricingApi.create).toHaveBeenCalledWith({ model: 'mid', input_per_mtok: 2, cached_input_per_mtok: 0, cache_write_per_mtok: null, output_per_mtok: 0, notes: 'note' })
    expect(toast.success).toHaveBeenCalledWith('Added price for mid')
    expect((screen.getByRole('button', { name: 'Add price' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('validates locally and lists all problems without calling the API', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Add price' }))
    fireEvent.change(screen.getByLabelText('Output per 1M tokens'), { target: { value: '-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('model: Required')).toBeTruthy()
    expect(screen.getByText('output_per_mtok: Must be ≥ 0')).toBeTruthy()
    expect(pricingApi.create).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Model').getAttribute('aria-invalid')).toBe('true')
  })

  it('rejects an empty numeric field (NaN)', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Add price' }))
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'm' } })
    fireEvent.change(screen.getByLabelText('Input per 1M tokens'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('input_per_mtok: Must be ≥ 0')).toBeTruthy()
  })

  it('maps server 422 details onto fields and keeps the row open', async () => {
    vi.mocked(pricingApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['model: already priced', 'whatever'] }))
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Add price' }))
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'alpha' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByLabelText('Model').getAttribute('title')).toBe('already priced'))
    expect(screen.getByText('whatever')).toBeTruthy()
    expect(toast.error).toHaveBeenCalledWith('Could not save price', expect.any(Object))
  })

  it('Escape cancels the new row', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Add price' }))
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Escape' })
    expect(screen.queryByLabelText('Model')).toBeNull()
  })

  it('Enter submits from the model input', async () => {
    vi.mocked(pricingApi.create).mockResolvedValue(makePrice({ id: 'p9', model: 'new' }))
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Add price' }))
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'new' } })
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Enter' })
    await waitFor(() => expect(pricingApi.create).toHaveBeenCalled())
  })
})

describe('PricingPage: edit and delete', () => {
  it('edits a price through the pencil button and updates the row', async () => {
    vi.mocked(pricingApi.update).mockResolvedValue({ ...prices[1], input_per_mtok: 1.5 })
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Edit alpha' }))
    expect((screen.getByLabelText('Input per 1M tokens') as HTMLInputElement).value).toBe('1')
    fireEvent.change(screen.getByLabelText('Input per 1M tokens'), { target: { value: '1.5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(within(row('alpha')).getByText('$1.50')).toBeTruthy())
    expect(pricingApi.update).toHaveBeenCalledWith('p1', expect.objectContaining({ model: 'alpha', input_per_mtok: 1.5 }))
    expect(toast.success).toHaveBeenCalledWith('Updated alpha')
  })

  it('double-click starts editing', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.doubleClick(screen.getByText('zeta'))
    expect((await screen.findByLabelText('Model') as HTMLInputElement).value).toBe('zeta')
  })

  it('switching to another row discards the first edit and clears errors', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Edit alpha' }))
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('model: Required')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Edit zeta' }))
    expect(screen.queryByText('model: Required')).toBeNull()
    expect((screen.getByLabelText('Model') as HTMLInputElement).value).toBe('zeta')
    expect(screen.getAllByLabelText('Model')).toHaveLength(1)
  })

  it('Escape cancels an edit and restores the view row', async () => {
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Edit alpha' }))
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Escape' })
    expect(screen.queryByLabelText('Model')).toBeNull()
    expect(screen.getByText('alpha')).toBeTruthy()
  })

  it('reports an update failure and keeps editing', async () => {
    vi.mocked(pricingApi.update).mockRejectedValue(new Error('offline'))
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(screen.getByRole('button', { name: 'Edit alpha' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not save price', { description: 'offline' }))
    expect(screen.getByLabelText('Model')).toBeTruthy()
  })

  it('deletes after confirmation', async () => {
    vi.mocked(pricingApi.remove).mockResolvedValue(undefined)
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(within(row('alpha')).getByRole('button', { name: 'Delete' }))
    fireEvent.click(within(row('alpha')).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(models()).toEqual(['zeta']))
    expect(pricingApi.remove).toHaveBeenCalledWith('p1')
    expect(toast.success).toHaveBeenCalledWith('Deleted alpha')
  })

  it('keeps the row when delete fails', async () => {
    vi.mocked(pricingApi.remove).mockRejectedValue(new ApiError(409, { code: 'conflict', message: 'price in use' }))
    renderRoute(<PricingPage />)
    await screen.findByText('alpha')
    fireEvent.click(within(row('alpha')).getByRole('button', { name: 'Delete' }))
    fireEvent.click(within(row('alpha')).getByRole('button', { name: 'Confirm?' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not delete alpha', { description: 'price in use' }))
    expect(models()).toEqual(['alpha', 'zeta'])
  })
})
