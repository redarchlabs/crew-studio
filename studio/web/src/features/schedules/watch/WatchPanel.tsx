import { useState } from 'react'
import { Eye, Radar, Save, Trash2 } from 'lucide-react'
import { ApiError } from '../../../api/client'
import { watchesApi } from '../../../api/endpoints.watch'
import type { Schedule } from '../../../api/types'
import type { Watch, WatchPreview, WatchView } from '../../../api/types.watch'
import { Button } from '../../../components/Button'
import { FormErrors, Toggle } from '../../../components/Field'
import { Panel } from '../../../components/PageHeader'
import { AsyncView } from '../../../components/States'
import { Chip } from '../../../components/StatusBadge'
import { ConfirmButton, JsonBlock } from '../../../components/misc'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'
import { LadderEditor } from './LadderEditor'
import { RollupsView } from './RollupsView'
import { WatchCapsFields } from './TrackerSourceFields'
import { ChangesView, FollowupsView } from './WatchInputsView'
import { WatchSourceFields } from './WatchSourceFields'
import { WatchChecks, WatchSavings, WatchStatusLine } from './WatchStatus'
import { effectiveTracked, emptyWatchForm, hasItems, RUN_WHEN, toWatchForm, toWatchInput, validateWatchForm, type WatchForm } from './watchForm'

/** One line describing where a watch reads from. */
export function sourceSummary(w: Watch): string {
  const s = w.source
  if (s.kind === 'http_json') return `${w.connection_name || 'HTTP'} ${s.path ?? ''}`
  if (s.kind === 'web_page') return s.selector ? `${s.url} (${s.selector})` : (s.url ?? '')
  if (s.kind === 'tracker') return `${w.connection_name || 'tracker'} ${s.project ?? ''}${s.jql ? ` (${s.jql})` : ''}`
  if (s.kind === 'tracking_list') return "the crew's tracking list"
  return `workspace: ${s.file ?? ''}`
}

function problems(err: unknown): string[] {
  if (err instanceof ApiError) return [err.message, ...err.details]
  return [err instanceof Error ? err.message : 'Something went wrong.']
}

function PreviewResult({ preview }: { preview: WatchPreview }) {
  return (
    <div className="flex flex-col gap-3 rounded-md border border-line bg-raised p-3" data-testid="watch-preview">
      <div className="flex flex-wrap items-center gap-2 text-[12.5px]">
        <Chip tone={preview.would_run ? 'ok' : 'neutral'}>{preview.would_run ? 'would start a run' : 'would not run'}</Chip>
        <span className="text-muted">{preview.reason} · {preview.item_count} items fetched · nothing was saved</span>
      </div>
      <ChangesView changes={preview.changes} />
      {preview.rollups && <div><div className="eyebrow mb-1">Roll-ups</div><RollupsView rollups={preview.rollups.items} /></div>}
      {preview.followups_due && <div><div className="eyebrow mb-1">Follow-ups due</div><FollowupsView followups={preview.followups_due} /></div>}
      {preview.sample.length > 0 && <div><div className="eyebrow mb-1">First items as read (to check the fields)</div><JsonBlock value={preview.sample} className="max-h-60" /></div>}
    </div>
  )
}

function RunWhenPicker({ value, onChange }: { value: WatchForm['run_when']; onChange: (v: WatchForm['run_when']) => void }) {
  return (
    <div>
      <div className="mb-1.5 text-[12px] font-medium text-muted">Start a run when</div>
      <div role="radiogroup" aria-label="Start a run when" className="grid gap-2 sm:grid-cols-3">
        {RUN_WHEN.map((o) => (
          <button key={o.value} type="button" role="radio" aria-checked={value === o.value} onClick={() => onChange(o.value)}
            className={`rounded-md border px-3 py-2 text-left ${value === o.value ? 'border-accent/60 bg-accent-soft' : 'border-line bg-raised hover:border-line-strong'}`}>
            <div className="text-[13px] font-medium">{o.label}</div>
            <div className="text-[11.5px] text-muted">{o.hint}</div>
          </button>
        ))}
      </div>
    </div>
  )
}

type EditorProps = {
  schedule: Schedule
  view: WatchView
  canClear: boolean
  onSaved: (v: WatchView | null) => void
  /** The snapshot was cleared: update the view without resetting the form */
  onCleared: () => void
}

function WatchEditor({ schedule, view, canClear, onSaved, onCleared }: EditorProps) {
  const [form, setForm] = useState<WatchForm>(() => (view.watch ? toWatchForm(view.watch) : emptyWatchForm()))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [serverErrors, setServerErrors] = useState<string[]>([])
  const [busy, setBusy] = useState<'save' | 'preview' | 'remove' | 'clear' | null>(null)
  const [preview, setPreview] = useState<WatchPreview | null>(null)
  const set = <K extends keyof WatchForm>(key: K, value: WatchForm[K]) => {
    setForm((f) => ({ ...f, [key]: value }))
    setPreview(null) // a preview of the old definition would mislead
  }
  const ladderErrors = Object.fromEntries(Object.entries(errors).filter(([k]) => k.startsWith('ladder:')).map(([k, v]) => [k.slice(7), v]))

  const checked = (): boolean => {
    const found = validateWatchForm(form)
    setErrors(found)
    setServerErrors([])
    return Object.keys(found).length === 0
  }
  const run = async (kind: 'save' | 'preview' | 'remove' | 'clear', action: () => Promise<void>) => {
    setBusy(kind)
    try {
      await action()
    } catch (err) {
      setServerErrors(problems(err))
      notifyError(kind === 'preview' ? 'The preview failed' : `Could not ${kind === 'clear' ? 'clear the snapshot' : `${kind} the watch`}`, err)
    } finally {
      setBusy(null)
    }
  }
  const save = () => checked() && void run('save', async () => {
    onSaved(await watchesApi.save(schedule.id, toWatchInput(form)))
    notifySuccess('Saved the watch')
  })
  const doPreview = () => checked() && void run('preview', async () => {
    setPreview(null)
    setPreview(await watchesApi.preview(schedule.id, toWatchInput(form)))
  })
  const remove = () => void run('remove', async () => {
    await watchesApi.remove(schedule.id)
    notifySuccess('Removed the watch')
    onSaved(null)
  })
  const clear = () => void run('clear', async () => {
    await watchesApi.clearSnapshot(schedule.id)
    notifySuccess('Cleared the snapshot; the next check is the first')
    onCleared()
  })
  const kindInfo = view.source_kinds.find((k) => k.kind === form.kind)

  return (
    <div className="flex flex-col gap-4">
      <WatchSourceFields form={form} set={set} kinds={view.source_kinds} connections={view.connections} crewId={schedule.crew_id} errors={errors} />
      <RunWhenPicker value={form.run_when} onChange={(v) => set('run_when', v)} />
      <Toggle label="Dry run" checked={form.dry_run} onChange={(v) => set('dry_run', v)}
        hint="The crew writes what it would post instead of posting it, and its log entries are marked dry run. They do not count as sent once you turn this off." />
      {hasItems(form.kind) && kindInfo?.followups !== false && (
        <div className="flex flex-col gap-3 rounded-md border border-line p-3">
          <Toggle label="Follow-up ladder" checked={form.ladder_on} onChange={(v) => set('ladder_on', v)} hint="Remind people as an item's date approaches and passes. Code decides who is due; the crew writes the message." />
          {form.ladder_on && <LadderEditor ladder={form.ladder} onChange={(l) => set('ladder', l)} trackedFields={effectiveTracked(form).join(', ')} errors={ladderErrors} />}
        </div>
      )}
      <details className="rounded-md border border-line p-3">
        <summary className="cursor-pointer text-[12.5px] font-medium text-muted">Tracker tool limits</summary>
        <div className="mt-3"><WatchCapsFields form={form} set={set} errors={errors} /></div>
      </details>
      <FormErrors messages={serverErrors} />
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="primary" icon={<Save className="size-4" />} busy={busy === 'save'} onClick={save}>{view.watch ? 'Save watch' : 'Add watch'}</Button>
        <Button icon={<Eye className="size-4" />} busy={busy === 'preview'} onClick={doPreview} title="Fetch the source now and show what a check would see. Saves nothing.">Preview</Button>
        {view.watch && <ConfirmButton icon={<Trash2 className="size-3.5" />} busy={busy === 'remove'} onConfirm={remove}>Remove watch</ConfirmButton>}
        {view.watch && canClear && view.watch.snapshot_at && <ConfirmButton busy={busy === 'clear'} onConfirm={clear}>Clear snapshot</ConfirmButton>}
      </div>
      {preview && <PreviewResult preview={preview} />}
    </div>
  )
}

function ReadOnlyWatch({ watch }: { watch: Watch }) {
  return (
    <div className="text-[12.5px] text-muted">
      Reads <span className="font-medium text-fg">{sourceSummary(watch)}</span>
      {hasItems(watch.source.kind) && <> · id <span className="font-mono">{watch.id_field}</span> · tracks <span className="font-mono">{watch.tracked_fields.join(', ')}</span></>}
      {' '}· runs when {RUN_WHEN.find((r) => r.value === watch.run_when)?.label.toLowerCase()}
      {watch.followups && <> · follow-ups: {watch.followups.steps.map((s) => s.name).join(', ')}</>}
    </div>
  )
}

type Props = { schedule: Schedule; canManage: boolean; canClear: boolean }

/** The schedule's watch: status and savings, its recent checks, and (for managers) the editor with Preview. */
export function WatchPanel({ schedule, canManage, canClear }: Props) {
  const res = useAsync((signal) => watchesApi.get(schedule.id, signal), [schedule.id])
  const [editing, setEditing] = useState(false)
  const [version, setVersion] = useState(0)
  const onCleared = () =>
    res.setData((v) => v && { ...v, watch: v.watch && { ...v.watch, snapshot_at: null, snapshot_items: 0 } })
  const onSaved = (v: WatchView | null) => {
    if (v) {
      res.setData(() => v)
      setVersion((n) => n + 1)
    } else {
      setEditing(false)
      res.reload()
    }
  }
  return (
    <Panel title={<span className="inline-flex items-center gap-2"><Radar className="size-4 text-faint" />Watch</span>}>
      <AsyncView {...res}>
        {(view) => (
          <div className="flex flex-col gap-4">
            <p className="text-[12.5px] text-muted">
              A watch checks a source before each run and only starts the work order when something changed (or a follow-up is due). The run gets the
              changes as its <code className="font-mono">changes</code> and <code className="font-mono">followups_due</code> inputs.
            </p>
            {view.watch && (
              <>
                <WatchStatusLine watch={view.watch} />
                {!canManage && <ReadOnlyWatch watch={view.watch} />}
                {view.stats && <WatchSavings stats={view.stats} />}
              </>
            )}
            {canManage && (view.watch || editing ? (
              <WatchEditor key={`${view.watch?.id ?? 'new'}-${version}`} schedule={schedule} view={view} canClear={canClear} onSaved={onSaved} onCleared={onCleared} />
            ) : (
              <div><Button icon={<Radar className="size-4" />} onClick={() => setEditing(true)}>Add a watch</Button></div>
            ))}
            {!canManage && !view.watch && <p className="text-[12.5px] text-faint">This schedule has no watch; it starts a work order every time.</p>}
            {view.watch && <div><div className="eyebrow mb-1">Recent checks</div><WatchChecks checks={view.checks} /></div>}
          </div>
        )}
      </AsyncView>
    </Panel>
  )
}
