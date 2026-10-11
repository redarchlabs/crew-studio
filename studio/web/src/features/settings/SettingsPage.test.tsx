import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { SettingsPage } from './SettingsPage'
import { api, apiError, isDisabled, notifyMock, renderRoute, resetAll } from '../admin/testUtils'
import type { Settings } from '../../api/types'

vi.mock('../../api/endpoints', async () => (await import('../admin/testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('../admin/testUtils')).authMock)
vi.mock('../../lib/notify', async () => (await import('../admin/testUtils')).notifyMock)

const SETTINGS: Settings = {
  default_budget_usd: 5,
  hard_stop_multiplier: 1.5,
  search_model: 'gemini-3-flash',
  grounded_search_cost_per_1000: 14,
  grounded_searches_free: 5000,
  grounding_free_period: 'month',
  max_concurrent_runs: 3,
  compact_context_tokens: 0,
}

const input = (label: string) => screen.getByLabelText(label) as HTMLInputElement
const change = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
const saveBtn = () => screen.queryByRole('button', { name: 'Save settings' }) as HTMLButtonElement | null

async function renderPage() {
  const view = renderRoute(<SettingsPage />, { path: '/settings' })
  await screen.findByLabelText('Default budget (USD)')
  return view
}

beforeEach(() => {
  resetAll(['settings.view', 'settings.manage'])
  api.settingsApi.get.mockResolvedValue(SETTINGS)
})

describe('SettingsPage loading', () => {
  it('shows the loaded values', async () => {
    await renderPage()
    expect(input('Default budget (USD)').value).toBe('5')
    expect(input('Hard-stop multiplier').value).toBe('1.5')
    expect(input('Search model').value).toBe('gemini-3-flash')
    expect(input('Max concurrent runs').value).toBe('3')
    expect((screen.getByLabelText('Free allowance resets') as HTMLSelectElement).value).toBe('month')
  })

  it('shows a loading state first', () => {
    api.settingsApi.get.mockReturnValue(new Promise(() => {}))
    renderRoute(<SettingsPage />, { path: '/settings' })
    expect(screen.getByRole('status')).toBeTruthy()
  })

  it('shows the error and retries', async () => {
    api.settingsApi.get.mockRejectedValueOnce(apiError(500, 'cannot read settings', 'internal'))
    renderRoute(<SettingsPage />, { path: '/settings' })
    expect(await screen.findByText('cannot read settings')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await screen.findByLabelText('Default budget (USD)')
    expect(api.settingsApi.get).toHaveBeenCalledTimes(2)
  })
})

describe('SettingsPage permissions', () => {
  it('offers history only with history.view', async () => {
    await renderPage()
    expect(screen.queryByText('History')).toBeNull()
  })

  it('links to history when permitted', async () => {
    resetAll(['settings.manage', 'history.view'])
    api.settingsApi.get.mockResolvedValue(SETTINGS)
    await renderPage()
    expect(screen.getByText('History').closest('a')?.getAttribute('href')).toContain('entity_type=settings')
  })

  it('is read-only without settings.manage: fields disabled, no save button, explanation shown', async () => {
    resetAll(['settings.view'])
    api.settingsApi.get.mockResolvedValue(SETTINGS)
    await renderPage()
    expect(screen.getByRole('note').textContent).toContain('settings.manage')
    expect(isDisabled(input('Default budget (USD)'))).toBe(true)
    expect(isDisabled(input('Search model'))).toBe(true)
    expect(saveBtn()).toBeNull()
  })

  it('does not submit a read-only form even when the form is submitted directly', async () => {
    resetAll(['settings.view'])
    api.settingsApi.get.mockResolvedValue(SETTINGS)
    const { container } = await renderPage()
    fireEvent.submit(container.querySelector('form')!)
    expect(api.settingsApi.update).not.toHaveBeenCalled()
  })
})

describe('SettingsPage editing', () => {
  it('disables save until something changes, and offers discard', async () => {
    await renderPage()
    expect(saveBtn()!.disabled).toBe(true)
    expect(screen.queryByRole('button', { name: 'Discard changes' })).toBeNull()
    change('Default budget (USD)', '8')
    expect(saveBtn()!.disabled).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
    expect(input('Default budget (USD)').value).toBe('5')
    expect(saveBtn()!.disabled).toBe(true)
  })

  it('saves edited values (trimming the search model) and reports success', async () => {
    api.settingsApi.update.mockImplementation(async (s: Settings) => s)
    await renderPage()
    change('Default budget (USD)', '12.5')
    change('Hard-stop multiplier', '2')
    change('Search model', '  gemini-x  ')
    change('Cost per 1,000 search queries (USD)', '35')
    change('Free search queries', '100')
    change('Free allowance resets', 'day')
    change('Max concurrent runs', '10')
    change("Compact an agent's context above (tokens)", '60000')
    fireEvent.click(saveBtn()!)
    await waitFor(() => expect(notifyMock.notifySuccess).toHaveBeenCalledWith('Settings saved'))
    expect(api.settingsApi.update).toHaveBeenCalledWith({
      default_budget_usd: 12.5,
      hard_stop_multiplier: 2,
      search_model: 'gemini-x',
      grounded_search_cost_per_1000: 35,
      grounded_searches_free: 100,
      grounding_free_period: 'day',
      max_concurrent_runs: 10,
      compact_context_tokens: 60000,
    })
    // Saved values become the new baseline: nothing left to save
    await waitFor(() => expect(saveBtn()!.disabled).toBe(true))
  })

  it('refuses a compaction limit outside the range', async () => {
    await renderPage()
    change("Compact an agent's context above (tokens)", '5000')
    fireEvent.click(saveBtn()!)
    expect(await screen.findByText(/0 \(never\), or a whole number from 10,000/)).toBeTruthy()
    expect(api.settingsApi.update).not.toHaveBeenCalled()
  })

  it('submits on Enter via the form', async () => {
    api.settingsApi.update.mockImplementation(async (s: Settings) => s)
    const { container } = await renderPage()
    change('Default budget (USD)', '9')
    fireEvent.submit(container.querySelector('form')!)
    await waitFor(() => expect(api.settingsApi.update).toHaveBeenCalledTimes(1))
  })

  it.each([
    ['Default budget (USD)', '0', 'Must be greater than 0'],
    ['Default budget (USD)', '', 'Must be greater than 0'],
    ['Hard-stop multiplier', '0.9', 'Must be at least 1'],
    ['Search model', '   ', 'Required'],
    ['Cost per 1,000 search queries (USD)', '-1', 'Must be ≥ 0'],
    ['Free search queries', '1.5', 'Whole number ≥ 0'],
    ['Free search queries', '-2', 'Whole number ≥ 0'],
    ['Max concurrent runs', '0', 'Whole number from 1 to 10'],
    ['Max concurrent runs', '11', 'Whole number from 1 to 10'],
    ['Max concurrent runs', '2.5', 'Whole number from 1 to 10'],
  ])('rejects %s = "%s" locally', async (label, value, message) => {
    await renderPage()
    change(label, value)
    fireEvent.click(saveBtn()!)
    expect(await screen.findByText(message)).toBeTruthy()
    expect(api.settingsApi.update).not.toHaveBeenCalled()
  })

  it('accepts boundary values', async () => {
    api.settingsApi.update.mockImplementation(async (s: Settings) => s)
    await renderPage()
    change('Hard-stop multiplier', '1')
    change('Free search queries', '0')
    change('Max concurrent runs', '1')
    fireEvent.click(saveBtn()!)
    await waitFor(() => expect(api.settingsApi.update).toHaveBeenCalled())
  })

  it('maps 422 details onto fields and toasts the failure', async () => {
    api.settingsApi.update.mockRejectedValue(apiError(422, 'invalid', 'validation', ['max_concurrent_runs: must be at most 10', 'something else entirely']))
    await renderPage()
    change('Default budget (USD)', '6')
    fireEvent.click(saveBtn()!)
    expect(await screen.findByText('must be at most 10')).toBeTruthy()
    expect(screen.getByText('something else entirely')).toBeTruthy()
    expect(notifyMock.notifyError).toHaveBeenCalledWith('Could not save settings', expect.anything())
    // Still dirty so the person can fix and resubmit
    expect(saveBtn()!.disabled).toBe(false)
  })

  it('reports a 403 from the server without losing the edits', async () => {
    api.settingsApi.update.mockRejectedValue(apiError(403, 'forbidden', 'forbidden'))
    await renderPage()
    change('Default budget (USD)', '6')
    fireEvent.click(saveBtn()!)
    await waitFor(() => expect(notifyMock.notifyError).toHaveBeenCalled())
    expect(input('Default budget (USD)').value).toBe('6')
    expect(notifyMock.notifySuccess).not.toHaveBeenCalled()
  })
})
