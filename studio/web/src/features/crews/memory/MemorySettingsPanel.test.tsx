import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import type { LLM } from '../../../api/types'
import { renderRoute, setupApi } from '../testUtils'
import { MemorySettingsPanel } from './MemorySettingsPanel'
import { makeSettingsView, TS } from './testUtils'

vi.mock('../../../api/endpoints')
vi.mock('../../../api/endpoints.memory')

const llm = (id: string, name: string) => ({ id, name, model: `openai/${id}` }) as LLM
const LLMS = [llm('l2', 'Smart'), llm('l1', 'Cheap')]

beforeEach(() => {
  setupApi()
  vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView())
})
afterEach(cleanup)

const render = (canEdit = true, llms = LLMS) => renderRoute(<MemorySettingsPanel crewId="crew-1" llms={llms} canEdit={canEdit} />)
const save = () => screen.getByRole('button', { name: 'Save memory settings' }) as HTMLButtonElement

describe('MemorySettingsPanel', () => {
  it('turns on automatic memory with a model, limits, retention and ranking', async () => {
    vi.mocked(memoryApi.saveSettings).mockResolvedValue(makeSettingsView({}, { mode: 'automatic', updated_by_name: 'Tester', updated_at: TS }))
    render()
    const mode = await screen.findByLabelText('Memory')
    expect(save().disabled).toBe(true)
    expect(screen.getByText('Runs neither recall nor save memories.')).toBeTruthy()
    expect(screen.queryByLabelText('Recall')).toBeNull()

    fireEvent.change(mode, { target: { value: 'recall_only' } })
    expect(screen.queryByLabelText('Memory model')).toBeNull()
    expect(screen.queryByLabelText('Saves per run')).toBeNull()
    fireEvent.change(screen.getByLabelText('Recall'), { target: { value: 'deep' } })
    expect(screen.getByLabelText('Memory model')).toBeTruthy()
    expect(screen.getByText('Refines deep recall. A cheap model is usually enough.')).toBeTruthy()

    fireEvent.change(mode, { target: { value: 'automatic' } })
    expect(screen.getByText(/one extra model call per task/)).toBeTruthy()
    expect(screen.getByRole('option', { name: "The crew's cheapest model (now Cheap)" })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Memory model'), { target: { value: 'l2' } })
    fireEvent.change(screen.getByLabelText('Memories per recall'), { target: { value: '8' } })
    fireEvent.change(screen.getByLabelText('Saves per run'), { target: { value: '10' } })
    expect(screen.getByText(/can plant instructions/)).toBeTruthy()
    expect(screen.queryByTestId('memory-review-off')).toBeNull()
    fireEvent.click(screen.getByRole('switch', { name: /Review memories that runs save/ }))
    expect(screen.getByTestId('memory-review-off').textContent).toContain('go live immediately')
    fireEvent.change(screen.getByLabelText('Keep memories for (days)'), { target: { value: '90' } })
    fireEvent.click(screen.getByRole('button', { name: /Advanced: ranking/ }))
    fireEvent.change(screen.getByLabelText('Similarity weight'), { target: { value: '0.6' } })
    fireEvent.change(screen.getByLabelText('Recency weight'), { target: { value: '0.2' } })
    fireEvent.change(screen.getByLabelText('Importance weight'), { target: { value: '0.2' } })
    fireEvent.change(screen.getByLabelText('Recency half-life (days)'), { target: { value: '14' } })
    fireEvent.click(save())
    await waitFor(() => expect(memoryApi.saveSettings).toHaveBeenCalledWith('crew-1', {
      mode: 'automatic', llm_id: 'l2', recall_depth: 'deep', recall_limit: 8, saves_per_run: 10, retention_days: 90, review_run_saves: false,
      recency_weight: 0.2, semantic_weight: 0.6, importance_weight: 0.2, recency_half_life_days: 14,
    }))
    expect(await screen.findByText(/Last changed by Tester/)).toBeTruthy()
  })

  it('keeps memories forever, and empties number fields until they are valid', async () => {
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({ suggested_llm_id: null }, { mode: 'explicit', retention_days: null, updated_at: TS }))
    vi.mocked(memoryApi.saveSettings).mockRejectedValue(new ApiError(422, { code: 'validation_failed', message: 'Fix', details: ['recall_limit: use 1 to 20', 'weights: must add up to 1'] }))
    render()
    expect(await screen.findByText('Memories are kept until someone deletes them.')).toBeTruthy()
    expect(screen.getByText(/Last changed by someone/)).toBeTruthy()
    expect(screen.queryByLabelText('Keep memories for (days)')).toBeNull()
    fireEvent.click(screen.getByRole('switch', { name: /Delete old memories/ }))
    expect((screen.getByLabelText('Keep memories for (days)') as HTMLInputElement).value).toBe('365')
    expect(screen.getByText('Pinned memories are always kept.')).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: /Delete old memories/ }))
    for (const label of ['Memories per recall', 'Saves per run']) fireEvent.change(screen.getByLabelText(label), { target: { value: '' } })
    fireEvent.click(screen.getByRole('switch', { name: /Delete old memories/ }))
    fireEvent.change(screen.getByLabelText('Keep memories for (days)'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: /Advanced: ranking/ }))
    for (const label of ['Similarity weight', 'Recency weight', 'Importance weight', 'Recency half-life (days)']) {
      fireEvent.change(screen.getByLabelText(label), { target: { value: '' } })
    }
    fireEvent.change(screen.getByLabelText('Recall'), { target: { value: 'deep' } })
    expect(screen.getByRole('option', { name: "The crew's cheapest model" })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Memory model'), { target: { value: 'l1' } })
    fireEvent.change(screen.getByLabelText('Memory model'), { target: { value: '' } })
    fireEvent.click(save())
    expect(await screen.findByText('use 1 to 20')).toBeTruthy()
    expect(screen.getByText('weights: must add up to 1')).toBeTruthy()
  })

  it('offers review of run saves in explicit and automatic modes, and warns while it is off', async () => {
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({}, { mode: 'explicit', review_run_saves: false }))
    render()
    const toggle = await screen.findByRole('switch', { name: /Review memories that runs save before later runs use them/ })
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    expect(screen.getByTestId('memory-review-off').textContent).toContain('Memories that runs save go live immediately, without review')
    fireEvent.change(screen.getByLabelText('Memory'), { target: { value: 'recall_only' } })
    expect(screen.queryByRole('switch', { name: /Review memories that runs save/ })).toBeNull()
    expect(screen.queryByTestId('memory-review-off')).toBeNull()
    fireEvent.change(screen.getByLabelText('Memory'), { target: { value: 'automatic' } })
    expect(screen.getByTestId('memory-review-off')).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: /Review memories that runs save/ }))
    expect(screen.queryByTestId('memory-review-off')).toBeNull()
  })

  it('says how many memories are waiting to be re-indexed for a new embedding model', async () => {
    render()
    await screen.findByLabelText('Memory')
    expect(screen.queryByTestId('memory-reembed')).toBeNull()
    cleanup()
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({ reembed_pending: 1 }))
    render()
    expect((await screen.findByTestId('memory-reembed')).textContent).toBe('1 memory is being re-indexed for the new embedding model; until then recall may miss it.')
    cleanup()
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({ reembed_pending: 3 }))
    render()
    expect((await screen.findByTestId('memory-reembed')).textContent).toBe('3 memories are being re-indexed for the new embedding model; until then recall may miss them.')
  })

  it('explains why memory cannot be turned on and only offers Off', async () => {
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({ available: false, unavailable_reason: 'STUDIO_MEMORY_EMBEDDINGS is off.' }))
    render()
    expect((await screen.findByTestId('memory-unavailable')).textContent).toContain('STUDIO_MEMORY_EMBEDDINGS is off.')
    expect((screen.getByRole('option', { name: 'Automatic' }) as HTMLOptionElement).disabled).toBe(true)
    expect((screen.getByRole('option', { name: 'Off' }) as HTMLOptionElement).disabled).toBe(false)
    cleanup()
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({ available: false, unavailable_reason: '' }))
    render()
    expect((await screen.findByTestId('memory-unavailable')).textContent).toContain('the server has no embedding model.')
  })

  it('is read-only without crews.edit and state.manage, with the ranking shown', async () => {
    vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView({}, { mode: 'automatic', llm_id: 'hidden' }))
    render(false)
    expect(await screen.findByRole('option', { name: 'Chosen model (not visible to you)' })).toBeTruthy()
    expect(screen.getByLabelText('Similarity weight')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Save memory settings' })).toBeNull()
  })
})
