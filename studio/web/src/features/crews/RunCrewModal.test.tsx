import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { crewsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import type { Permission } from '../../api/types'
import { makeCrew } from '../../lib/testFixtures'
import { RunCrewModal } from './RunCrewModal'
import { renderRoute, setupApi, without } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

const crew = makeCrew({
  id: 'c1',
  name: 'Research',
  budget_usd: 2.5,
  inputs: [
    { key: 'company', label: 'Company', default: 'ACME', required: true },
    { key: 'notes', label: '', default: '', required: false },
    { key: 'long', label: 'Long', default: 'x'.repeat(100), required: false },
  ],
})

beforeEach(() => setupApi())
afterEach(cleanup)

function open(c = crew, onClose = vi.fn(), perms?: Permission[]) {
  return { onClose, ...renderRoute(<RunCrewModal crew={c} onClose={onClose} />, { perms }) }
}

describe('RunCrewModal', () => {
  it('renders a dialog titled with the crew and pre-fills defaults', async () => {
    open()
    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect(screen.getByText('Run Research')).toBeTruthy()
    expect((screen.getByLabelText(/Company/) as HTMLTextAreaElement).value).toBe('ACME')
    expect((screen.getByLabelText(/Long/) as HTMLTextAreaElement).rows).toBe(3)
    expect((screen.getByLabelText(/Company/) as HTMLTextAreaElement).rows).toBe(1)
    expect(screen.getByText(/crew budget of \$2\.50/)).toBeTruthy()
  })

  it('falls back to the key as label when label is empty', async () => {
    open()
    expect(await screen.findByLabelText(/notes/)).toBeTruthy()
  })

  it('says when the crew takes no inputs', async () => {
    open(makeCrew({ inputs: [] }))
    expect(await screen.findByText('This crew takes no inputs.')).toBeTruthy()
  })

  it('blocks submit when a required input is blank', async () => {
    open()
    fireEvent.change(await screen.findByLabelText(/Company/), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start run' }))
    expect(await screen.findByText('Company is required')).toBeTruthy()
    expect(crewsApi.startRun).not.toHaveBeenCalled()
  })

  it('validates the budget override', async () => {
    open()
    fireEvent.change(await screen.findByLabelText(/Budget override/), { target: { value: '0' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start run' }))
    expect(await screen.findByText('Budget must be greater than 0')).toBeTruthy()
    expect(crewsApi.startRun).not.toHaveBeenCalled()
  })

  it('starts the run with inputs only and navigates to it', async () => {
    vi.mocked(crewsApi.startRun).mockResolvedValue({ id: 'run-9' } as never)
    const { router } = open()
    fireEvent.change(await screen.findByLabelText(/Company/), { target: { value: 'Globex' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start run' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/runs/run-9'))
    expect(crewsApi.startRun).toHaveBeenCalledWith('c1', { inputs: { company: 'Globex', notes: '', long: 'x'.repeat(100) } })
  })

  it('includes a budget override when given', async () => {
    vi.mocked(crewsApi.startRun).mockResolvedValue({ id: 'r' } as never)
    open()
    fireEvent.change(await screen.findByLabelText(/Budget override/), { target: { value: '7.5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Start run' }))
    await waitFor(() => expect(crewsApi.startRun).toHaveBeenCalled())
    expect(vi.mocked(crewsApi.startRun).mock.calls[0][1]).toMatchObject({ budget_usd: 7.5 })
  })

  it('shows 422 problems, notifies and lets the user retry', async () => {
    const { toast } = await import('sonner')
    vi.mocked(crewsApi.startRun).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['company: too short', 'budget: low'] }))
    const { router } = open()
    fireEvent.click(await screen.findByRole('button', { name: 'Start run' }))
    expect(await screen.findByText('company: too short')).toBeTruthy()
    expect(screen.getByText('budget: low')).toBeTruthy()
    expect(toast.error).toHaveBeenCalledWith('Could not start the run', expect.any(Object))
    expect(router.state.location.pathname).toBe('/')
    expect((screen.getByRole('button', { name: 'Start run' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('handles non-422 errors without a problems list', async () => {
    vi.mocked(crewsApi.startRun).mockRejectedValue(new Error('network down'))
    open()
    fireEvent.click(await screen.findByRole('button', { name: 'Start run' }))
    await waitFor(() => expect((screen.getByRole('button', { name: 'Start run' }) as HTMLButtonElement).disabled).toBe(false))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('cancel closes', async () => {
    const { onClose } = open()
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
  })

  it('Escape closes the modal', async () => {
    const { onClose } = open()
    await screen.findByRole('dialog')
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })

  it('without crews.run shows a read-only note and no form', async () => {
    open(crew, vi.fn(), without('crews.run'))
    expect(await screen.findByText(/You need/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Start run' })).toBeNull()
  })
})
