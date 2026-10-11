import { useState, type FormEvent } from 'react'
import { Save } from 'lucide-react'
import { settingsApi } from '../../api/endpoints'
import type { GroundingPeriod, Settings } from '../../api/types'
import { Button } from '../../components/Button'
import { FormErrors, NumberField, SelectField, TextField } from '../../components/Field'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { HistoryLink } from '../../components/misc'
import { LockableFieldset, ReadOnlyNote } from '../../components/ReadOnlyNote'
import { useAuth } from '../../lib/auth'
import { NO_ERRORS, parseFieldErrors, type FieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useUnsavedGuard } from '../../lib/useUnsavedGuard'

const FIELDS = ['default_budget_usd', 'hard_stop_multiplier', 'search_model', 'grounded_search_cost_per_1000', 'grounded_searches_free', 'grounding_free_period', 'max_concurrent_runs', 'compact_context_tokens'] as const

function validate(s: Settings): Record<string, string> {
  const errs: Record<string, string> = {}
  const bad = (n: number) => Number.isNaN(n)
  if (bad(s.default_budget_usd) || s.default_budget_usd <= 0) errs.default_budget_usd = 'Must be greater than 0'
  if (bad(s.hard_stop_multiplier) || s.hard_stop_multiplier < 1) errs.hard_stop_multiplier = 'Must be at least 1'
  if (!s.search_model.trim()) errs.search_model = 'Required'
  if (bad(s.grounded_search_cost_per_1000) || s.grounded_search_cost_per_1000 < 0) errs.grounded_search_cost_per_1000 = 'Must be ≥ 0'
  if (bad(s.grounded_searches_free) || s.grounded_searches_free < 0 || !Number.isInteger(s.grounded_searches_free)) errs.grounded_searches_free = 'Whole number ≥ 0'
  if (bad(s.max_concurrent_runs) || s.max_concurrent_runs < 1 || s.max_concurrent_runs > 10 || !Number.isInteger(s.max_concurrent_runs)) errs.max_concurrent_runs = 'Whole number from 1 to 10'
  const ct = s.compact_context_tokens
  if (bad(ct) || !Number.isInteger(ct) || (ct !== 0 && (ct < 10_000 || ct > 1_000_000))) errs.compact_context_tokens = '0 (never), or a whole number from 10,000 to 1,000,000'
  return errs
}

function SettingsForm({ initial, readOnly, onSaved }: { initial: Settings; readOnly: boolean; onSaved: (s: Settings) => void }) {
  const [value, setValue] = useState<Settings>(initial)
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [saving, setSaving] = useState(false)
  const dirty = JSON.stringify(value) !== JSON.stringify(initial)
  useUnsavedGuard(dirty)
  const num = (k: Exclude<keyof Settings, 'search_model' | 'grounding_free_period'>) => (v: number | null) => setValue((prev) => ({ ...prev, [k]: v ?? Number.NaN }))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (readOnly) return
    const local = validate(value)
    if (Object.keys(local).length > 0) return setErrors({ fields: local, general: [] })
    setSaving(true)
    try {
      const saved = await settingsApi.update({ ...value, search_model: value.search_model.trim() })
      setErrors(NO_ERRORS)
      notifySuccess('Settings saved')
      onSaved(saved)
    } catch (err) {
      setErrors(parseFieldErrors(err, FIELDS))
      notifyError('Could not save settings', err)
    } finally {
      setSaving(false)
    }
  }

  const f = errors.fields
  return (
    <form onSubmit={submit} noValidate className="flex max-w-3xl flex-col gap-4">
      <LockableFieldset locked={readOnly} className="flex flex-col gap-4">
      <Panel title="Budgets">
        <div className="grid gap-4 sm:grid-cols-2">
          <NumberField label="Default budget (USD)" value={value.default_budget_usd} onChange={num('default_budget_usd')} min={0} step="any" error={f.default_budget_usd} hint="Used when a crew has no budget of its own." />
          <NumberField label="Hard-stop multiplier" value={value.hard_stop_multiplier} onChange={num('hard_stop_multiplier')} min={1} step={0.1} error={f.hard_stop_multiplier} hint="All LLM calls stop at budget × this (≥ 1)." />
        </div>
      </Panel>
      <Panel title="Grounded search">
        <div className="grid gap-4 sm:grid-cols-3">
          <TextField label="Search model" value={value.search_model} onChange={(v) => setValue((prev) => ({ ...prev, search_model: v }))} error={f.search_model} className="sm:col-span-3" hint="Gemini model behind the google_search tool." />
          <NumberField label="Cost per 1,000 search queries (USD)" value={value.grounded_search_cost_per_1000} onChange={num('grounded_search_cost_per_1000')} min={0} step="any" error={f.grounded_search_cost_per_1000} />
          <NumberField label="Free search queries" value={value.grounded_searches_free} onChange={num('grounded_searches_free')} min={0} step={1} error={f.grounded_searches_free} hint="Shared across all runs." />
          <SelectField
            label="Free allowance resets"
            value={value.grounding_free_period}
            onChange={(v) => setValue((prev) => ({ ...prev, grounding_free_period: v as GroundingPeriod }))}
            options={[{ value: 'month', label: 'Monthly (Gemini 3)' }, { value: 'day', label: 'Daily (Gemini 2.5)' }]}
            error={f.grounding_free_period}
          />
        </div>
      </Panel>
      <Panel title="Execution">
        <NumberField label="Max concurrent runs" value={value.max_concurrent_runs} onChange={num('max_concurrent_runs')} min={1} max={10} step={1} error={f.max_concurrent_runs} hint="1 – 10. Extra runs wait in the queue." className="max-w-xs" />
        <NumberField label="Compact an agent's context above (tokens)" value={value.compact_context_tokens} onChange={num('compact_context_tokens')} min={0} max={1_000_000} step={1000} error={f.compact_context_tokens}
          hint="0 never compacts by itself. Otherwise, once an agent's conversation on a task passes this size, its steps so far are replaced by a summary, so later LLM calls resend fewer tokens. 10,000 – 1,000,000; 60,000 is a good start. Applies to runs that start after you save." className="max-w-md" />
      </Panel>
      </LockableFieldset>
      <FormErrors messages={errors.general} />
      {!readOnly && (
        <div className="flex items-center gap-2">
          <Button type="submit" variant="primary" icon={<Save className="size-4" />} busy={saving} disabled={!dirty}>Save settings</Button>
          {dirty && <Button variant="ghost" onClick={() => { setValue(initial); setErrors(NO_ERRORS) }}>Discard changes</Button>}
        </div>
      )}
    </form>
  )
}

export function SettingsPage() {
  const { can } = useAuth()
  const readOnly = !can('settings.manage')
  const res = useAsync((signal) => settingsApi.get(signal), [])
  return (
    <>
      <PageHeader eyebrow="Configure" title="Settings" description="Global defaults for budgets, search and concurrency." actions={can('history.view') ? <HistoryLink type="settings" /> : undefined} />
      {readOnly && <ReadOnlyNote perm="settings.manage" />}
      <AsyncView {...res}>
        {(settings) => <SettingsForm key={JSON.stringify(settings)} initial={settings} readOnly={readOnly} onSaved={(s) => res.setData(() => s)} />}
      </AsyncView>
    </>
  )
}
