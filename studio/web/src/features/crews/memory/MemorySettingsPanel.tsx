import { useState } from 'react'
import { CircleAlert } from 'lucide-react'
import { memoryApi } from '../../../api/endpoints.memory'
import type { MemoryMode, MemorySettingsInput, MemorySettingsView, RecallDepth } from '../../../api/types.memory'
import type { LLM } from '../../../api/types'
import { Button } from '../../../components/Button'
import { FormErrors, NumberField, SelectField, Toggle } from '../../../components/Field'
import { Panel } from '../../../components/PageHeader'
import { AsyncView } from '../../../components/States'
import { Collapsible } from '../../../components/misc'
import { sortByName } from '../../../lib/catalog'
import { dateTime } from '../../../lib/format'
import { parseFieldErrors } from '../../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'
import { MODE_LABELS, plural } from './memoryText'

type Props = {
  crewId: string
  llms: readonly LLM[]
  /** crews.edit and state.manage: the settings decide what memory costs */
  canEdit: boolean
}

const FIELDS = [
  'mode', 'llm_id', 'recall_depth', 'recall_limit', 'saves_per_run', 'retention_days', 'review_run_saves',
  'recency_weight', 'semantic_weight', 'importance_weight', 'recency_half_life_days',
] as const

const DEFAULT_RETENTION = 365

/** NumberField gives NaN for an empty field; the server refuses it with a field error */
const num = (x: number | null): number => x ?? Number.NaN

const MODE_HINTS: Record<MemoryMode, string> = {
  off: 'Runs neither recall nor save memories.',
  recall_only: 'Runs recall memories that people added or earlier runs saved, and save none.',
  explicit: 'Runs recall memories, and agents get a Remember tool to save the ones they choose. Saving costs no model call.',
  automatic: 'Runs recall memories, and after each task a model picks out what is worth keeping. That is one extra model call per task.',
}

const WARNING = 'flex items-start gap-2 rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px]'

const DEPTHS: { value: RecallDepth; label: string }[] = [
  { value: 'shallow', label: 'Shallow: a quick search, no model call' },
  { value: 'deep', label: 'Deep: a model refines the search when results are weak' },
]

const toInput = (s: MemorySettingsView['settings']): MemorySettingsInput => ({
  mode: s.mode, llm_id: s.llm_id, recall_depth: s.recall_depth, recall_limit: s.recall_limit, saves_per_run: s.saves_per_run,
  retention_days: s.retention_days, review_run_saves: s.review_run_saves, recency_weight: s.recency_weight,
  semantic_weight: s.semantic_weight, importance_weight: s.importance_weight, recency_half_life_days: s.recency_half_life_days,
})

/** How the crew uses memory: the mode, recall, saving limits, retention, review and ranking. */
export function MemorySettingsPanel({ crewId, llms, canEdit }: Props) {
  const res = useAsync((signal) => memoryApi.settings(crewId, signal), [crewId])
  return (
    <Panel title="Memory settings">
      <AsyncView {...res}>
        {(view) => (
          <MemorySettingsForm key={view.settings.updated_at ?? 'new'} crewId={crewId} view={view} llms={llms} canEdit={canEdit} onSaved={(next) => res.setData(() => next)} />
        )}
      </AsyncView>
    </Panel>
  )
}

type FormProps = Props & { view: MemorySettingsView; onSaved: (v: MemorySettingsView) => void }

function MemorySettingsForm({ crewId, view, llms, canEdit, onSaved }: FormProps) {
  const saved = view.settings
  const [v, setV] = useState<MemorySettingsInput>(toInput(saved))
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<{ fields: Record<string, string>; general: string[] }>({ fields: {}, general: [] })
  const set = <K extends keyof MemorySettingsInput>(key: K, value: MemorySettingsInput[K]) => setV((prev) => ({ ...prev, [key]: value }))
  const dirty = JSON.stringify(v) !== JSON.stringify(toInput(saved))
  const f = errors.fields
  const on = v.mode !== 'off'
  const saves = v.mode === 'explicit' || v.mode === 'automatic'
  const needsModel = v.mode === 'automatic' || (on && v.recall_depth === 'deep')

  const modeOptions = (Object.keys(MODE_LABELS) as MemoryMode[]).map((m) => ({ value: m, label: MODE_LABELS[m], disabled: m !== 'off' && !view.available }))
  const suggested = llms.find((l) => l.id === view.suggested_llm_id)
  const hiddenLlm = v.llm_id && !llms.some((l) => l.id === v.llm_id) ? [{ value: v.llm_id, label: 'Chosen model (not visible to you)' }] : []
  const llmOptions = [
    { value: '', label: suggested ? `The crew's cheapest model (now ${suggested.name})` : "The crew's cheapest model" },
    ...sortByName(llms).map((l) => ({ value: l.id, label: `${l.name} — ${l.model}` })),
    ...hiddenLlm,
  ]

  const save = async () => {
    setSaving(true)
    setErrors({ fields: {}, general: [] })
    try {
      onSaved(await memoryApi.saveSettings(crewId, v))
      notifySuccess('Saved the memory settings')
    } catch (err) {
      setErrors(parseFieldErrors(err, FIELDS))
      notifyError('Could not save the memory settings', err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <fieldset disabled={!canEdit} className="flex flex-col gap-3">
      {!view.available && (
        <p className={WARNING} role="status" data-testid="memory-unavailable">
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-warn" />
          <span>Memory cannot be turned on: {view.unavailable_reason || 'the server has no embedding model.'}</span>
        </p>
      )}
      {view.reembed_pending > 0 && (
        <p className={WARNING} role="status" data-testid="memory-reembed">
          <CircleAlert className="mt-0.5 size-4 shrink-0 text-warn" />
          <span>
            {plural(view.reembed_pending, 'memory', 'memories')} {view.reembed_pending === 1 ? 'is' : 'are'} being re-indexed for the new embedding model; until then recall may
            miss {view.reembed_pending === 1 ? 'it' : 'them'}.
          </span>
        </p>
      )}
      <SelectField label="Memory" value={v.mode} onChange={(x) => set('mode', x as MemoryMode)} options={modeOptions} error={f.mode} hint={MODE_HINTS[v.mode]} />
      {on && (
        <>
          <div className="grid gap-3 sm:grid-cols-2">
            <SelectField label="Recall" value={v.recall_depth} onChange={(x) => set('recall_depth', x as RecallDepth)} options={DEPTHS} error={f.recall_depth} />
            <NumberField label="Memories per recall" value={v.recall_limit} onChange={(x) => set('recall_limit', num(x))} min={1} max={20} error={f.recall_limit}
              hint="How many memories an agent sees before each task (1–20)." />
          </div>
          {needsModel && (
            <SelectField label="Memory model" value={v.llm_id ?? ''} onChange={(x) => set('llm_id', x || null)} options={llmOptions} error={f.llm_id}
              hint={v.mode === 'automatic' ? 'Picks out memories after each task, and refines deep recall. A cheap model is usually enough.' : 'Refines deep recall. A cheap model is usually enough.'} />
          )}
          {saves && (
            <NumberField label="Saves per run" value={v.saves_per_run} onChange={(x) => set('saves_per_run', num(x))} min={0} max={100} error={f.saves_per_run}
              hint="The most memories one run may save (0–100). Saves past this are skipped." />
          )}
          {saves && (
            <Toggle label="Review memories that runs save before later runs use them" checked={v.review_run_saves} onChange={(x) => set('review_run_saves', x)} disabled={!canEdit}
              hint="On by default. Text a crew reads, such as tickets, emails and web pages, can plant instructions that a run might save as a lesson. While this is on, every memory a run saves waits as pending until a person approves it, so a planted memory cannot reach later runs. Memories people add are active at once. Pending memories nobody approves are deleted after 30 days." />
          )}
          {saves && !v.review_run_saves && (
            <p className={`${WARNING} text-warn`} role="alert" data-testid="memory-review-off">
              <CircleAlert className="mt-0.5 size-4 shrink-0" />
              <span>Memories that runs save go live immediately, without review. Later runs recall them at once, including any instructions planted in text the crew read.</span>
            </p>
          )}
          <div className="grid gap-3 sm:grid-cols-2">
            <Toggle label="Delete old memories" checked={v.retention_days !== null} onChange={(x) => set('retention_days', x ? DEFAULT_RETENTION : null)} disabled={!canEdit}
              hint={v.retention_days === null ? 'Memories are kept until someone deletes them.' : 'Pinned memories are always kept.'} />
            {v.retention_days !== null && (
              <NumberField label="Keep memories for (days)" value={v.retention_days} onChange={(x) => set('retention_days', num(x))} min={1} max={3650} error={f.retention_days} />
            )}
          </div>
          <Collapsible title="Advanced: ranking" defaultOpen={!canEdit}>
            <div className="flex flex-col gap-3">
              <p className="text-[12px] text-muted">How recall ranks memories. The three weights are usually left at CrewAI&apos;s defaults (0.3, 0.5 and 0.2).</p>
              <div className="grid gap-3 sm:grid-cols-2">
                <NumberField label="Similarity weight" value={v.semantic_weight} onChange={(x) => set('semantic_weight', num(x))} min={0} max={1} step="any" error={f.semantic_weight} />
                <NumberField label="Recency weight" value={v.recency_weight} onChange={(x) => set('recency_weight', num(x))} min={0} max={1} step="any" error={f.recency_weight} />
                <NumberField label="Importance weight" value={v.importance_weight} onChange={(x) => set('importance_weight', num(x))} min={0} max={1} step="any" error={f.importance_weight} />
                <NumberField label="Recency half-life (days)" value={v.recency_half_life_days} onChange={(x) => set('recency_half_life_days', num(x))} min={1} error={f.recency_half_life_days}
                  hint="After this many days a memory counts half as recent." />
              </div>
            </div>
          </Collapsible>
        </>
      )}
      <FormErrors messages={errors.general} />
      {saved.updated_at && <p className="text-[11.5px] text-faint">Last changed by {saved.updated_by_name || 'someone'} · {dateTime(saved.updated_at)}</p>}
      {canEdit && (
        <div className="flex justify-end">
          <Button size="sm" variant="primary" busy={saving} disabled={!dirty} onClick={() => void save()}>Save memory settings</Button>
        </div>
      )}
    </fieldset>
  )
}
