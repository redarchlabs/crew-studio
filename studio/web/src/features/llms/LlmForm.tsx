import { useState, type FormEvent } from 'react'
import { llmsApi } from '../../api/endpoints'
import { REASONING_EFFORTS, type LLM, type LLMApi, type LLMInput, type ModelPrice, type ReasoningEffort } from '../../api/types'
import { Button } from '../../components/Button'
import { FormErrors, NumberField, SelectField, TextField, Toggle } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { NO_ERRORS, parseFieldErrors, type FieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { PriceMatchBadge } from './PriceMatchBadge'

const FIELDS = ['name', 'model', 'temperature', 'max_completion_tokens', 'reasoning_effort', 'api', 'vision'] as const

const EMPTY: LLMInput = { name: '', model: '', temperature: null, max_completion_tokens: null, reasoning_effort: null, api: 'completions', vision: false }

const EFFORT_OPTIONS = [{ value: '', label: 'Model default' }, ...REASONING_EFFORTS.map((e) => ({ value: e, label: e }))]
const API_OPTIONS = [
  { value: 'completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses (OpenAI reasoning models)' },
]

function toInput(llm: LLM | null): LLMInput {
  if (!llm) return EMPTY
  const { name, model, temperature, max_completion_tokens, reasoning_effort, api, vision } = llm
  return { name, model, temperature, max_completion_tokens, reasoning_effort, api: api || 'completions', vision: !!vision }
}

function validate(v: LLMInput): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.name.trim()) errs.name = 'Name is required'
  if (!v.model.trim()) errs.model = 'Model is required'
  if (v.temperature !== null && (Number.isNaN(v.temperature) || v.temperature < 0 || v.temperature > 2)) errs.temperature = 'Use 0 – 2, or leave empty for the provider default'
  if (v.max_completion_tokens !== null && (!Number.isInteger(v.max_completion_tokens) || v.max_completion_tokens < 1)) errs.max_completion_tokens = 'Use a positive whole number, or leave empty'
  if (v.api === 'responses' && !v.model.trim().startsWith('openai/')) errs.api = 'The Responses API is only for openai/ models'
  return errs
}

type LlmFormProps = { llm: LLM | null; prices: readonly ModelPrice[]; showPrices?: boolean; onClose: () => void; onSaved: (llm: LLM) => void }

export function LlmForm({ llm, prices, showPrices = true, onClose, onSaved }: LlmFormProps) {
  const [value, setValue] = useState<LLMInput>(() => toInput(llm))
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [saving, setSaving] = useState(false)
  const set = <K extends keyof LLMInput>(key: K, v: LLMInput[K]) => setValue((prev) => ({ ...prev, [key]: v }))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const local = validate(value)
    if (Object.keys(local).length > 0) {
      setErrors({ fields: local, general: [] })
      return
    }
    setSaving(true)
    const body: LLMInput = { ...value, name: value.name.trim(), model: value.model.trim() }
    try {
      const saved = llm ? await llmsApi.update(llm.id, body) : await llmsApi.create(body)
      notifySuccess(llm ? 'LLM updated' : 'LLM created')
      onSaved(saved)
    } catch (err) {
      setErrors(parseFieldErrors(err, FIELDS))
      notifyError(llm ? 'Could not update LLM' : 'Could not create LLM', err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title={llm ? `Edit ${llm.name}` : 'New LLM'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" form="llm-form" busy={saving}>{llm ? 'Save' : 'Create'}</Button>
        </>
      }
    >
      <form id="llm-form" className="flex flex-col gap-3" onSubmit={submit} noValidate>
        <TextField label="Name" value={value.name} onChange={(v) => set('name', v)} error={errors.fields.name} placeholder="GPT-6 Luna (research)" autoFocus />
        <TextField
          label="Model"
          value={value.model}
          onChange={(v) => set('model', v)}
          error={errors.fields.model}
          placeholder="openai/gpt-6-luna"
          className="font-mono"
          hint="LiteLLM-style id: provider/model"
        />
        <div className="-mt-1 min-h-5">{showPrices && value.model.trim() && <PriceMatchBadge model={value.model} prices={prices} verbose />}</div>
        <div className="grid grid-cols-2 gap-3">
          <NumberField label="Temperature" value={value.temperature} onChange={(v) => set('temperature', v)} nullable min={0} max={2} step={0.1} error={errors.fields.temperature} hint="Empty = provider default" />
          <NumberField label="Max completion tokens" value={value.max_completion_tokens} onChange={(v) => set('max_completion_tokens', v)} nullable min={1} step={1} error={errors.fields.max_completion_tokens} hint="Empty = provider default" />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <SelectField
            label="Reasoning effort"
            value={value.reasoning_effort ?? ''}
            onChange={(v) => set('reasoning_effort', v === '' ? null : (v as ReasoningEffort))}
            options={EFFORT_OPTIONS}
            error={errors.fields.reasoning_effort}
            hint="Reasoning models only. Claude: effort; OpenAI: reasoning_effort."
          />
          <SelectField
            label="API"
            value={value.api}
            onChange={(v) => set('api', v as LLMApi)}
            options={API_OPTIONS}
            error={errors.fields.api}
            hint="gpt-6-astra and gpt-6.1-sol need Responses for tool calling."
          />
        </div>
        <Toggle
          label="Vision: accepts images and PDFs"
          checked={value.vision}
          onChange={(v) => set('vision', v)}
          hint="Work order screenshots and PDFs go straight to agents on this model; others get the file names instead."
        />
        <FormErrors messages={errors.general} />
      </form>
    </Modal>
  )
}
