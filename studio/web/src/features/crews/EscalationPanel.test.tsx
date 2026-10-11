import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../api/client'
import { humanApi } from '../../api/endpoints.human'
import type { EscalationSettings } from '../../api/types.human'
import { EscalationPanel } from './EscalationPanel'
import { renderRoute, setupApi } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.human')

const TS = '2026-10-06T07:00:00Z'
const settings = (over: Partial<EscalationSettings> = {}): EscalationSettings => ({
  crew_id: 'crew-1', enabled: false, rules: '', timeout_minutes: 30, on_timeout: 'proceed', max_questions: 5, updated_by_name: '', updated_at: null, ...over,
})

beforeEach(() => {
  setupApi()
  vi.mocked(humanApi.escalation).mockResolvedValue(settings())
})
afterEach(cleanup)

describe('EscalationPanel', () => {
  it('turns asking on with rules, a wait and what happens on timeout', async () => {
    vi.mocked(humanApi.saveEscalation).mockResolvedValue(settings({ enabled: true, rules: 'Ask before assuming a budget.', on_timeout: 'fail', updated_by_name: 'Tester', updated_at: TS }))
    renderRoute(<EscalationPanel crewId="crew-1" canEdit />)
    const save = await screen.findByRole('button', { name: 'Save human input settings' })
    expect((save as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getByRole('switch'))
    fireEvent.change(screen.getByLabelText('When to ask'), { target: { value: 'Ask before assuming a budget.' } })
    fireEvent.change(screen.getByLabelText('Wait for an answer (minutes)'), { target: { value: '15' } })
    fireEvent.change(screen.getByLabelText('If nobody answers'), { target: { value: 'fail' } })
    fireEvent.change(screen.getByLabelText('Questions per run'), { target: { value: '3' } })
    fireEvent.click(save)
    await waitFor(() => expect(humanApi.saveEscalation).toHaveBeenCalledWith('crew-1',
      { enabled: true, rules: 'Ask before assuming a budget.', timeout_minutes: 15, on_timeout: 'fail', max_questions: 3 }))
    expect(await screen.findByText(/Last changed by Tester/)).toBeTruthy()
  })

  it('shows field errors from the server', async () => {
    vi.mocked(humanApi.saveEscalation).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['rules: say when the agents should ask a person'] }))
    renderRoute(<EscalationPanel crewId="crew-1" canEdit />)
    fireEvent.click(await screen.findByRole('switch'))
    fireEvent.click(screen.getByRole('button', { name: 'Save human input settings' }))
    expect(await screen.findByText('say when the agents should ask a person')).toBeTruthy()
  })

  it('is read-only without edit access', async () => {
    renderRoute(<EscalationPanel crewId="crew-1" canEdit={false} />)
    await screen.findByLabelText('When to ask')
    expect(screen.queryByRole('button', { name: 'Save human input settings' })).toBeNull()
    expect((screen.getByLabelText('When to ask') as HTMLTextAreaElement).closest('fieldset')?.disabled).toBe(true)
  })
})
