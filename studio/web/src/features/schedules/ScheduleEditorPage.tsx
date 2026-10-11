import { useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { Play, TriangleAlert, UserRound } from 'lucide-react'
import { schedulesApi } from '../../api/endpoints'
import type { OverlapPolicy, Schedule, ScheduleInput } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { Button } from '../../components/Button'
import { EditorHeader } from '../../components/EditorHeader'
import { FormErrors, TextAreaField, TextField, Toggle } from '../../components/Field'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { useAuth } from '../../lib/auth'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { BriefEditor } from '../workorders/composer/BriefEditor'
import { hasTarget, inputErrors } from '../workorders/composer/composerForm'
import { RecentWorkOrders } from '../workorders/RecentWorkOrders'
import { BudgetField } from '../workorders/target/BudgetField'
import { InputsFields } from '../workorders/target/InputsFields'
import { deriveTargetInputs, valuesForInputs, type TargetRef } from '../workorders/target/targetInputs'
import { TargetPicker } from '../workorders/target/TargetPicker'
import { loadTargetCatalog, type TargetCatalogData } from '../workorders/target/useTargetCatalog'
import { woNumber } from '../workorders/workOrderLabels'
import { CronField } from './CronField'
import { WatchPanel } from './watch/WatchPanel'
import { ReportDestinationsPanel } from '../delivery/ReportDestinationsPanel'
import { emptySchedule, normalizeSchedule, SCHEDULE_FIELDS, toScheduleInput, validateSchedule } from './scheduleForm'

const OVERLAP: { value: OverlapPolicy; label: string; hint: string }[] = [
  { value: 'skip', label: 'Skip', hint: 'Do not start while the previous work order is still running.' },
  { value: 'allow', label: 'Allow', hint: 'Start a new work order even if the last one is still running.' },
]

function ScheduleEditor({ schedule, catalog }: { schedule: Schedule | null; catalog: TargetCatalogData }) {
  const navigate = useNavigate()
  const { can, me } = useAuth()
  const manage = can('schedules.manage')
  const [current, setCurrent] = useState<Schedule | null>(schedule)
  const [deleting, setDeleting] = useState(false)
  const [running, setRunning] = useState(false)
  const [savedTick, setSavedTick] = useState(0)
  const initial = useMemo(() => (schedule ? toScheduleInput(schedule) : emptySchedule()), [schedule])
  const derivedFor = (v: ScheduleInput) => deriveTargetInputs(v, catalog)
  const form = useEntityForm<ScheduleInput, Schedule>({
    entityId: current?.id ?? null,
    initial,
    fields: SCHEDULE_FIELDS,
    label: 'schedule',
    validate: (v) => validateSchedule(v, derivedFor(v).inputs),
    normalize: normalizeSchedule,
    create: schedulesApi.create,
    update: schedulesApi.update,
  })
  useUnsavedGuard(form.dirty && manage)
  const { value: v, set, errors } = form
  const f = errors.fields
  const derived = useMemo(() => deriveTargetInputs(v, catalog), [v, catalog])
  const crew = catalog.crews.find((c) => c.id === v.crew_id)

  const setTarget = (t: TargetRef) => form.update((prev) => {
    const next = { ...prev, ...t }
    return { ...next, inputs: valuesForInputs(deriveTargetInputs(next, catalog).inputs, prev.inputs) }
  })

  const onSave = async () => {
    const saved = await form.save()
    if (!saved) return
    setCurrent(saved)
    setSavedTick((t) => t + 1)
    if (!current) navigate(`/schedules/${saved.id}`, { replace: true, state: SKIP_GUARD })
  }
  const onDelete = async () => {
    if (!current) return
    setDeleting(true)
    try {
      await schedulesApi.remove(current.id)
      notifySuccess(`Deleted ${current.name}`)
      navigate('/schedules', { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${current.name}`, err)
      setDeleting(false)
    }
  }
  const runNow = async () => {
    if (!current) return
    setRunning(true)
    try {
      const wo = await schedulesApi.runNow(current.id)
      notifySuccess(`Started ${woNumber(wo.number)}`)
      navigate(`/work-orders/${wo.id}`, { state: SKIP_GUARD })
    } catch (err) {
      notifyError('Could not run the schedule now', err)
      setRunning(false)
    }
  }

  const canRunNow = manage && can('work_orders.create')
  const header = manage ? (
    <EditorHeader
      backTo="/schedules"
      backLabel="Schedules"
      title={current ? current.name : 'New schedule'}
      isNew={!current}
      dirty={form.dirty}
      saving={form.saving}
      entityType="schedule"
      entityId={current?.id}
      onSave={() => void onSave()}
      onDelete={() => void onDelete()}
      deleting={deleting}
      extra={current && canRunNow && (
        <Button icon={<Play className="size-4" />} busy={running} disabled={form.dirty} title={form.dirty ? 'Save your changes first' : 'Create and submit a work order now'} onClick={() => void runNow()}>Run now</Button>
      )}
    />
  ) : (
    <PageHeader eyebrow="Schedule" title={current?.name ?? 'Schedule'} description={<span>Read only: changing schedules needs <code className="font-mono text-[12px]">schedules.manage</code>.</span>} actions={<Link className="text-accent" to="/schedules">Back to schedules</Link>} />
  )

  return (
    <>
      {header}
      <fieldset disabled={!manage} className="m-0 min-w-0 border-0 p-0">
        <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
          <div className="flex min-w-0 flex-col gap-4">
            {current?.last_error && (
              <div className="flex items-start gap-2 rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px] text-warn" role="alert">
                <TriangleAlert className="mt-0.5 size-4 shrink-0" />
                <span><span className="font-medium">Last problem:</span> {current.last_error}</span>
              </div>
            )}
            <Panel title="Schedule">
              <div className="flex flex-col gap-3">
                <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder="Weekly vendor digest" />
                <TextAreaField label="Description" rows={2} value={v.description} onChange={(x) => set('description', x)} error={f.description} />
                <TargetPicker value={v} onChange={setTarget} catalog={catalog} error={f.target ?? f.crew_id} />
                <Toggle label="Enabled" checked={v.enabled} onChange={(x) => set('enabled', x)} hint="Paused schedules keep their settings but do not fire." />
              </div>
            </Panel>
            <Panel title="When">
              <CronField cron={v.cron} timezone={v.timezone} onCron={(x) => set('cron', x)} onTimezone={(x) => set('timezone', x)} cronError={f.cron} timezoneError={f.timezone} />
              <div className="mt-4">
                <div className="mb-1.5 text-[12px] font-medium text-muted">If the previous work order is still running</div>
                <div role="radiogroup" className="grid gap-2 sm:grid-cols-2">
                  {OVERLAP.map((o) => (
                    <button key={o.value} type="button" role="radio" aria-checked={v.overlap_policy === o.value} onClick={() => set('overlap_policy', o.value)} className={`rounded-md border px-3 py-2 text-left ${v.overlap_policy === o.value ? 'border-accent/60 bg-accent-soft' : 'border-line bg-raised hover:border-line-strong'}`}>
                      <div className="text-[13px] font-medium">{o.label}</div>
                      <div className="text-[11.5px] text-muted">{o.hint}</div>
                    </button>
                  ))}
                </div>
              </div>
            </Panel>
            <Panel title="Brief">
              <BriefEditor value={v.brief} onChange={(x) => set('brief', x)} error={f.brief} rows={8} hint="Becomes each work order's brief." />
            </Panel>
            <FormErrors messages={errors.general} />
          </div>
          <aside className="flex min-w-0 flex-col gap-4">
            <Panel title="Inputs">
              <InputsFields derived={derived} values={v.inputs} onChange={(x) => set('inputs', x)} errors={inputErrors(f)} noTarget={!hasTarget(v)} />
              {f.inputs && <div className="mt-2 text-[12px] text-danger">{f.inputs}</div>}
            </Panel>
            <Panel title="Budget">
              <BudgetField value={v.budget_usd} onChange={(x) => set('budget_usd', x)} crewBudget={crew?.budget_usd} error={f.budget_usd} />
            </Panel>
            <Panel title="Runs as">
              <div className="flex items-center gap-2 text-[13px]">
                <UserRound className="size-4 text-faint" />
                <span className="font-medium">{current?.run_as_name || me?.name || 'You'}</span>
                {current && <AreaChips areaIds={current.area_ids} max={2} />}
              </div>
              <p className="mt-1.5 text-[12px] text-muted">
                Work orders from this schedule run with the permissions and areas of the person who last saved it.
                {manage && current && current.run_as !== me?.id && <> Saving makes <span className="font-medium text-fg">you</span> the run-as person.</>}
                {' '}If that person loses access, the schedule pauses and records why.
              </p>
              {current?.enabled === false && <Chip tone="neutral" className="mt-2">paused</Chip>}
            </Panel>
          </aside>
        </div>
      </fieldset>
      {current ? (
        <>
          {current.inputs.run_tasks && (
            <p className="mt-4 text-[12.5px] text-muted" data-testid="run-tasks-note">
              This schedule runs only part of its crew: the task set in its <code className="font-mono">run_tasks</code> input.
            </p>
          )}
          <div className="mt-5"><WatchPanel schedule={current} canManage={manage} canClear={can('state.manage')} /></div>
          <div className="mt-5"><ReportDestinationsPanel schedule={current} canManage={manage} /></div>
        </>
      ) : (
        manage && <p className="mt-4 text-[12.5px] text-muted">Save the schedule to add a watch, so it only runs when something changed.</p>
      )}
      {current && <div className="mt-5"><RecentWorkOrders source="schedule" sourceId={current.id} refreshKey={savedTick} /></div>}
    </>
  )
}

export function ScheduleEditorPage() {
  const { id = 'new' } = useParams()
  const res = useAsync(async (signal) => {
    const [catalog, schedule] = await Promise.all([loadTargetCatalog(signal), id === 'new' ? Promise.resolve(null) : schedulesApi.get(id, signal)])
    return { catalog, schedule }
  }, [id])
  return (
    <AsyncView {...res}>
      {({ catalog, schedule }) =>
        id !== 'new' && !schedule ? (
          <EmptyState title="Schedule not found" action={<Link className="text-accent" to="/schedules">Back to schedules</Link>} />
        ) : (
          <ScheduleEditor key={id} schedule={schedule} catalog={catalog} />
        )
      }
    </AsyncView>
  )
}
