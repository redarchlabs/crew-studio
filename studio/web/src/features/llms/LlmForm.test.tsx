import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { llmsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeLlm, makePrice } from '../../lib/testFixtures'
import { renderRoute, setupApi } from '../crews/testUtils'
import { LlmForm } from './LlmForm'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

beforeEach(() => setupApi())
afterEach(cleanup)

const prices = [makePrice({ id: 'p', model: 'gpt-6', input_per_mtok: 2, output_per_mtok: 9 })]

function open(llm = null as ReturnType<typeof makeLlm> | null, extra: { showPrices?: boolean } = {}) {
  const onClose = vi.fn()
  const onSaved = vi.fn()
  renderRoute(<LlmForm llm={llm} prices={prices} onClose={onClose} onSaved={onSaved} {...extra} />)
  return { onClose, onSaved }
}
const input = async (label: string) => (await screen.findByLabelText(label)) as HTMLInputElement
const submit = () => fireEvent.click(screen.getByRole('button', { name: /^(Create|Save)$/ }))
const fillBasics = async (name = 'My LLM', model = 'openai/gpt-6') => {
  fireEvent.change(await input('Name'), { target: { value: name } })
  fireEvent.change(screen.getByLabelText('Model'), { target: { value: model } })
}

describe('LlmForm: create', () => {
  it('renders an empty New LLM dialog with defaults', async () => {
    open()
    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect(screen.getByText('New LLM')).toBeTruthy()
    expect((await input('Name')).value).toBe('')
    expect((screen.getByLabelText('API') as HTMLSelectElement).value).toBe('completions')
    expect((screen.getByLabelText('Reasoning effort') as HTMLSelectElement).value).toBe('')
    expect(screen.getByRole('switch', { name: /Vision/ }).getAttribute('aria-checked')).toBe('false')
  })

  it('requires name and model', async () => {
    open()
    await input('Name')
    submit()
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(screen.getByText('Model is required')).toBeTruthy()
    expect(llmsApi.create).not.toHaveBeenCalled()
  })

  it('creates with trimmed values and defaults then reports saved', async () => {
    const saved = makeLlm({ id: 'n1' })
    vi.mocked(llmsApi.create).mockResolvedValue(saved)
    const { onSaved } = open()
    await fillBasics('  My LLM ', ' openai/gpt-6 ')
    submit()
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(saved))
    expect(llmsApi.create).toHaveBeenCalledWith({ name: 'My LLM', model: 'openai/gpt-6', temperature: null, max_completion_tokens: null, reasoning_effort: null, api: 'completions', vision: false })
    expect(toast.success).toHaveBeenCalledWith('LLM created')
  })

  it('sends temperature, tokens, effort and vision', async () => {
    vi.mocked(llmsApi.create).mockResolvedValue(makeLlm({ id: 'n1' }))
    open()
    await fillBasics()
    fireEvent.change(screen.getByLabelText('Temperature'), { target: { value: '0.7' } })
    fireEvent.change(screen.getByLabelText('Max completion tokens'), { target: { value: '4096' } })
    fireEvent.change(screen.getByLabelText('Reasoning effort'), { target: { value: 'high' } })
    fireEvent.click(screen.getByRole('switch', { name: /Vision/ }))
    expect(screen.getByRole('switch', { name: /Vision/ }).getAttribute('aria-checked')).toBe('true')
    submit()
    await waitFor(() => expect(llmsApi.create).toHaveBeenCalled())
    expect(vi.mocked(llmsApi.create).mock.calls[0][0]).toMatchObject({ temperature: 0.7, max_completion_tokens: 4096, reasoning_effort: 'high', vision: true })
  })

  it('toggling vision off again and clearing effort sends defaults', async () => {
    vi.mocked(llmsApi.create).mockResolvedValue(makeLlm({ id: 'n1' }))
    open()
    await fillBasics()
    const vision = screen.getByRole('switch', { name: /Vision/ })
    fireEvent.click(vision)
    fireEvent.click(vision)
    fireEvent.change(screen.getByLabelText('Reasoning effort'), { target: { value: 'low' } })
    fireEvent.change(screen.getByLabelText('Reasoning effort'), { target: { value: '' } })
    submit()
    await waitFor(() => expect(llmsApi.create).toHaveBeenCalled())
    expect(vi.mocked(llmsApi.create).mock.calls[0][0]).toMatchObject({ vision: false, reasoning_effort: null })
  })

  it('supports api=responses for openai models', async () => {
    vi.mocked(llmsApi.create).mockResolvedValue(makeLlm({ id: 'n1' }))
    open()
    await fillBasics('R', 'openai/gpt-6.1-sol')
    fireEvent.change(screen.getByLabelText('API'), { target: { value: 'responses' } })
    submit()
    await waitFor(() => expect(llmsApi.create).toHaveBeenCalled())
    expect(vi.mocked(llmsApi.create).mock.calls[0][0].api).toBe('responses')
  })

  it('rejects api=responses for non-openai models', async () => {
    open()
    await fillBasics('R', 'anthropic/claude')
    fireEvent.change(screen.getByLabelText('API'), { target: { value: 'responses' } })
    submit()
    expect(await screen.findByText('The Responses API is only for openai/ models')).toBeTruthy()
    expect(llmsApi.create).not.toHaveBeenCalled()
  })

  it.each([['-0.1'], ['2.5']])('rejects temperature %s', async (t) => {
    open()
    await fillBasics()
    fireEvent.change(screen.getByLabelText('Temperature'), { target: { value: t } })
    submit()
    expect(await screen.findByText(/Use 0 – 2/)).toBeTruthy()
    expect(llmsApi.create).not.toHaveBeenCalled()
  })

  it.each([['0', 0], ['2', 2]])('accepts the boundary temperature %s', async (t, n) => {
    vi.mocked(llmsApi.create).mockResolvedValue(makeLlm({ id: 'n1' }))
    open()
    await fillBasics()
    fireEvent.change(screen.getByLabelText('Temperature'), { target: { value: t } })
    submit()
    await waitFor(() => expect(llmsApi.create).toHaveBeenCalled())
    expect(vi.mocked(llmsApi.create).mock.calls[0][0].temperature).toBe(n)
  })

  it.each([['0'], ['1.5'], ['-5']])('rejects max tokens %s', async (t) => {
    open()
    await fillBasics()
    fireEvent.change(screen.getByLabelText('Max completion tokens'), { target: { value: t } })
    submit()
    expect(await screen.findByText(/positive whole number/)).toBeTruthy()
    expect(llmsApi.create).not.toHaveBeenCalled()
  })

  it('clearing a numeric field returns it to the provider default', async () => {
    vi.mocked(llmsApi.create).mockResolvedValue(makeLlm({ id: 'n1' }))
    open()
    await fillBasics()
    fireEvent.change(screen.getByLabelText('Temperature'), { target: { value: '1' } })
    fireEvent.change(screen.getByLabelText('Temperature'), { target: { value: '' } })
    submit()
    await waitFor(() => expect(llmsApi.create).toHaveBeenCalled())
    expect(vi.mocked(llmsApi.create).mock.calls[0][0].temperature).toBeNull()
  })

  it('maps 422 details onto fields and reports a toast', async () => {
    vi.mocked(llmsApi.create).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'bad', details: ['model: unknown provider', 'extra note'] }))
    const { onSaved } = open()
    await fillBasics()
    submit()
    expect(await screen.findByText('unknown provider')).toBeTruthy()
    expect(screen.getByText('extra note')).toBeTruthy()
    expect(toast.error).toHaveBeenCalledWith('Could not create LLM', expect.any(Object))
    expect(onSaved).not.toHaveBeenCalled()
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('cancel closes without saving', async () => {
    const { onClose } = open()
    await input('Name')
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onClose).toHaveBeenCalled()
    expect(llmsApi.create).not.toHaveBeenCalled()
  })
})

describe('LlmForm: edit and price badge', () => {
  it('prefills an existing llm and updates it', async () => {
    const llm = makeLlm({ id: 'l1', name: 'Fast', model: 'openai/gpt-6', temperature: 0.2, max_completion_tokens: 100, reasoning_effort: 'low', api: 'responses', vision: true })
    vi.mocked(llmsApi.update).mockResolvedValue({ ...llm, name: 'Faster' })
    const { onSaved } = open(llm)
    expect(await screen.findByText('Edit Fast')).toBeTruthy()
    expect((await input('Name')).value).toBe('Fast')
    expect((screen.getByLabelText('Temperature') as HTMLInputElement).value).toBe('0.2')
    expect((screen.getByLabelText('Max completion tokens') as HTMLInputElement).value).toBe('100')
    expect((screen.getByLabelText('API') as HTMLSelectElement).value).toBe('responses')
    expect(screen.getByRole('switch', { name: /Vision/ }).getAttribute('aria-checked')).toBe('true')
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Faster' } })
    submit()
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(llmsApi.update).toHaveBeenCalledWith('l1', expect.objectContaining({ name: 'Faster', api: 'responses', vision: true, reasoning_effort: 'low' }))
    expect(toast.success).toHaveBeenCalledWith('LLM updated')
  })

  it('reports update failures with the update wording', async () => {
    vi.mocked(llmsApi.update).mockRejectedValue(new Error('offline'))
    open(makeLlm({ id: 'l1', name: 'Fast' }))
    await input('Name')
    submit()
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Could not update LLM', { description: 'offline' }))
  })

  it('treats a legacy llm without api/vision as completions and no vision', async () => {
    const legacy = { ...makeLlm({ id: 'l1' }), api: undefined, vision: undefined } as unknown as ReturnType<typeof makeLlm>
    open(legacy)
    expect((await screen.findByLabelText('API') as HTMLSelectElement).value).toBe('completions')
    expect(screen.getByRole('switch', { name: /Vision/ }).getAttribute('aria-checked')).toBe('false')
  })

  it('shows the live price match as the model is typed', async () => {
    open()
    fireEvent.change(await input('Model'), { target: { value: 'openai/gpt-6' } })
    expect(screen.getByText('(matched without provider prefix)')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'openai/zzz' } })
    expect(screen.getByText(/No price entry/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: '' } })
    expect(screen.queryByText(/No price entry/)).toBeNull()
  })

  it('hides the price match without showPrices', async () => {
    open(null, { showPrices: false })
    fireEvent.change(await input('Model'), { target: { value: 'openai/zzz' } })
    expect(screen.queryByText(/No price entry/)).toBeNull()
  })
})
