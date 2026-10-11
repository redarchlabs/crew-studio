import { useState } from 'react'
import { humanApi } from '../../api/endpoints.human'
import type { EscalationOnTimeout, EscalationSettings, EscalationSettingsInput } from '../../api/types.human'
import { Button } from '../../components/Button'
import { NumberField, SelectField, TextAreaField, Toggle } from '../../components/Field'
import { Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { dateTime } from '../../lib/format'
import { parseFieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'

const FIELDS = ['enabled', 'rules', 'timeout_minutes', 'on_timeout', 'max_questions'] as const

const RULES_PLACEHOLDER = `- Ask before assuming a budget, deadline or region the brief does not give.
- Ask when two good sources disagree on a number that matters.
- Ask before anything is sent outside the company.`

const ON_TIMEOUT: { value: EscalationOnTimeout; label: string }[] = [
  { value: 'proceed', label: 'Carry on and state the assumption' },
  { value: 'fail', label: 'Stop the run' },
]

const toInput = (s: EscalationSettings): EscalationSettingsInput => ({
  enabled: s.enabled, rules: s.rules, timeout_minutes: s.timeout_minutes, on_timeout: s.on_timeout, max_questions: s.max_questions,
})

/** When the crew's agents stop and ask the person who started the run, and what happens if nobody answers. */
export function EscalationPanel({ crewId, canEdit }: { crewId: string; canEdit: boolean }) {
  const res = useAsync((signal) => humanApi.escalation(crewId, signal), [crewId])
  return (
    <Panel title="Human input">
      <AsyncView {...res}>
        {(s) => <EscalationForm key={s.updated_at ?? 'new'} crewId={crewId} saved={s} canEdit={canEdit} onSaved={(next) => res.setData(() => next)} />}
      </AsyncView>
    </Panel>
  )
}

type FormProps = { crewId: string; saved: EscalationSettings; canEdit: boolean; onSaved: (s: EscalationSettings) => void }

function EscalationForm({ crewId, saved, canEdit, onSaved }: FormProps) {
  const [v, setV] = useState<EscalationSettingsInput>(toInput(saved))
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = <K extends keyof EscalationSettingsInput>(key: K, value: EscalationSettingsInput[K]) => setV((prev) => ({ ...prev, [key]: value }))
  const dirty = JSON.stringify(v) !== JSON.stringify(toInput(saved))

  const save = async () => {
    setSaving(true)
    setErrors({})
    try {
      onSaved(await humanApi.saveEscalation(crewId, v))
      notifySuccess('Saved the human input settings')
    } catch (err) {
      setErrors(parseFieldErrors(err, FIELDS).fields)
      notifyError('Could not save the human input settings', err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <fieldset disabled={!canEdit} className="flex flex-col gap-3">
      <Toggle label="Agents may ask a person" checked={v.enabled} onChange={(x) => set('enabled', x)} disabled={!canEdit}
        hint="The person who started the run is asked on the work order, and sees a badge on Work orders. The run waits for the answer." />
      <TextAreaField label="When to ask" value={v.rules} onChange={(x) => set('rules', x)} rows={5} placeholder={RULES_PLACEHOLDER}
        error={errors.rules} hint="Plain language. Every agent gets these rules and an Ask a human tool; outside them, agents decide for themselves." />
      <div className="grid gap-3 sm:grid-cols-3">
        <NumberField label="Wait for an answer (minutes)" value={v.timeout_minutes} onChange={(x) => set('timeout_minutes', x ?? Number.NaN)}
          min={1} max={180} error={errors.timeout_minutes} />
        <SelectField label="If nobody answers" value={v.on_timeout} onChange={(x) => set('on_timeout', x as EscalationOnTimeout)} options={ON_TIMEOUT}
          error={errors.on_timeout} />
        <NumberField label="Questions per run" value={v.max_questions} onChange={(x) => set('max_questions', x ?? Number.NaN)}
          min={1} max={20} error={errors.max_questions} hint="Stops a looping agent from flooding you." />
      </div>
      {saved.updated_at && <p className="text-[11.5px] text-faint">Last changed by {saved.updated_by_name || 'someone'} · {dateTime(saved.updated_at)}</p>}
      {canEdit && (
        <div className="flex justify-end">
          <Button size="sm" variant="primary" busy={saving} disabled={!dirty} onClick={() => void save()}>Save human input settings</Button>
        </div>
      )}
    </fieldset>
  )
}
