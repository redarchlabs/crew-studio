import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { crewsApi, tasksApi, workOrdersApi } from '../../api/endpoints'
import { makeCrew, makeTask } from '../../lib/testFixtures'
import { makeRun, makeWorkOrder, renderRoute } from './testUtils'
import { NewWorkOrderPage } from './NewWorkOrderPage'

vi.mock('../../api/endpoints', async () => (await import('./testUtils')).endpointsMock())
vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

beforeEach(() => {
  vi.mocked(tasksApi.list).mockReset().mockResolvedValue([makeTask({ id: 't1', name: 'Research' })])
  vi.mocked(crewsApi.list).mockReset().mockResolvedValue([
    makeCrew({ id: 'c1', name: 'Crew One', task_ids: ['t1'], inputs: [{ key: 'region', label: 'Region', default: 'EMEA', required: true }] }),
    makeCrew({ id: 'c2', name: 'Crew Two', inputs: [{ key: 'topic', label: '', default: '', required: true }] }),
  ])
  vi.mocked(workOrdersApi.create).mockReset()
  vi.mocked(workOrdersApi.submit).mockReset()
})
afterEach(cleanup)

describe('NewWorkOrderPage', () => {
  it('shows a loading state, then the composer', async () => {
    renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new' })
    expect(screen.getByRole('status')).toBeTruthy()
    expect(await screen.findByRole('heading', { name: 'New work order' })).toBeTruthy()
  })

  it('shows an error if the catalog fails to load', async () => {
    vi.mocked(tasksApi.list).mockImplementation(() => Promise.reject(new Error('catalog down')))
    renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new' })
    expect(await screen.findByText('catalog down')).toBeTruthy()
  })

  it('preselects a crew from ?crew=', async () => {
    renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new', url: '/work-orders/new?crew=c1' })
    expect(await screen.findByText('Crew One')).toBeTruthy()
    expect((screen.getByLabelText(/Region/) as HTMLTextAreaElement).value).toBe('EMEA')
    expect(screen.queryByRole('radio')).toBeNull()
  })

  it('still accepts ?crew_id=', async () => {
    renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new', url: '/work-orders/new?crew_id=c2' })
    expect(await screen.findByText('Crew Two')).toBeTruthy()
    expect(screen.getByLabelText(/Topic/)).toBeTruthy()
  })

  it('ignores a ?task_id= deep link and starts with no crew', async () => {
    renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new', url: '/work-orders/new?task_id=t1' })
    expect(await screen.findByText('Choose a crew…')).toBeTruthy()
  })

  it('submits and navigates to the new work order, skipping the unsaved guard', async () => {
    vi.mocked(workOrdersApi.create).mockResolvedValue(makeWorkOrder({ id: 'created', number: 30 }))
    vi.mocked(workOrdersApi.submit).mockResolvedValue(makeRun())
    const view = renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new', url: '/work-orders/new?crew=c2' })
    await screen.findByText('Crew Two')
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Go' } })
    fireEvent.change(screen.getByLabelText(/Topic/), { target: { value: 'AI' } })
    fireEvent.click(screen.getByRole('button', { name: /Submit/ }))
    await waitFor(() => expect(view.location().pathname).toBe('/work-orders/created'))
    expect(view.location().state).toEqual({ skipUnsavedGuard: true })
    expect(screen.getByTestId('nav').textContent).toBe('NAV:/work-orders/created')
  })

  it('warns before leaving with unsaved edits and stays when declined', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const view = renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new' })
    await screen.findByRole('heading', { name: 'New work order' })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'dirty' } })
    fireEvent.click(screen.getByRole('link', { name: /Work orders/ }))
    await waitFor(() => expect(confirm).toHaveBeenCalled())
    expect(view.location().pathname).toBe('/work-orders/new')
    confirm.mockRestore()
  })

  it('leaves without a warning when nothing changed', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const view = renderRoute(<NewWorkOrderPage />, { path: '/work-orders/new' })
    await screen.findByRole('heading', { name: 'New work order' })
    fireEvent.click(screen.getByRole('link', { name: /Work orders/ }))
    await waitFor(() => expect(view.location().pathname).toBe('/work-orders'))
    expect(confirm).not.toHaveBeenCalled()
    confirm.mockRestore()
  })
})
