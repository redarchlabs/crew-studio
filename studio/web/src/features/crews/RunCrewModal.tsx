import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'
import { Play } from 'lucide-react'
import { crewsApi } from '../../api/endpoints'
import type { Crew } from '../../api/types'
import { ApiError } from '../../api/client'
import { Button } from '../../components/Button'
import { FormErrors, NumberField, TextAreaField } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { ReadOnlyNote } from '../../components/ReadOnlyNote'
import { useAuth } from '../../lib/auth'
import { usd } from '../../lib/format'
import { notifyError } from '../../lib/notify'

type RunCrewFormProps = { crew: Crew; onCancel: () => void; formId?: string }

function initialValues(crew: Crew): Record<string, string> {
  return Object.fromEntries(crew.inputs.map((i) => [i.key, i.default]))
}

/** Input fields for each crew input plus an optional budget override; starts a run and opens it. */
export function RunCrewForm({ crew, onCancel, formId = 'run-crew-form' }: RunCrewFormProps) {
  const navigate = useNavigate()
  const [values, setValues] = useState<Record<string, string>>(() => initialValues(crew))
  const [budget, setBudget] = useState<number | null>(null)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [serverErrors, setServerErrors] = useState<string[]>([])
  const [starting, setStarting] = useState(false)
  const { can } = useAuth()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const missing = Object.fromEntries(crew.inputs.filter((i) => i.required && !(values[i.key] ?? '').trim()).map((i) => [i.key, `${i.label || i.key} is required`]))
    const budgetErr: Record<string, string> = budget !== null && (Number.isNaN(budget) || budget <= 0) ? { __budget: 'Budget must be greater than 0' } : {}
    const all = { ...missing, ...budgetErr }
    setErrors(all)
    if (Object.keys(all).length > 0) return
    setStarting(true)
    setServerErrors([])
    try {
      const run = await crewsApi.startRun(crew.id, { inputs: values, ...(budget !== null ? { budget_usd: budget } : {}) })
      navigate(`/runs/${run.id}`)
    } catch (err) {
      if (err instanceof ApiError && err.details.length > 0) setServerErrors(err.details)
      notifyError('Could not start the run', err)
      setStarting(false)
    }
  }

  if (!can('crews.run')) return <ReadOnlyNote>You need <code className="font-mono">crews.run</code> to start runs.</ReadOnlyNote>

  return (
    <form id={formId} className="flex flex-col gap-3" onSubmit={submit} noValidate>
      {crew.inputs.length === 0 && <p className="text-muted">This crew takes no inputs.</p>}
      {crew.inputs.map((i) => (
        <TextAreaField
          key={i.key}
          rows={i.default.length > 80 ? 3 : 1}
          label={
            <>
              {i.label || i.key} <span className="font-mono text-faint">{`{${i.key}}`}</span>
              {i.required && <span className="text-danger"> *</span>}
            </>
          }
          value={values[i.key] ?? ''}
          onChange={(v) => setValues((prev) => ({ ...prev, [i.key]: v }))}
          error={errors[i.key]}
        />
      ))}
      <NumberField label="Budget override (USD)" value={budget} onChange={setBudget} nullable min={0} step="any" error={errors.__budget} hint={`Leave empty to use the crew budget of ${usd(crew.budget_usd)}.`} />
      <FormErrors messages={serverErrors} />
      <div className="flex justify-end gap-2 pt-1">
        <Button variant="ghost" onClick={onCancel}>Cancel</Button>
        <Button type="submit" variant="primary" icon={<Play className="size-4" />} busy={starting}>Start run</Button>
      </div>
    </form>
  )
}

export function RunCrewModal({ crew, onClose }: { crew: Crew; onClose: () => void }) {
  return (
    <Modal title={`Run ${crew.name}`} onClose={onClose}>
      <RunCrewForm crew={crew} onCancel={onClose} />
    </Modal>
  )
}
