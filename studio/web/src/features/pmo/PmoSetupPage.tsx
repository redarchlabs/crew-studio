import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { ArrowLeft, ArrowRight, Briefcase, Check } from 'lucide-react'
import { ApiError } from '../../api/client'
import { pmoApi } from '../../api/endpoints.delivery'
import type { PMOOptions } from '../../api/types.delivery'
import { trackerKindLabel } from '../../api/types.trackers'
import { AreaPicker, defaultAreaIds } from '../../components/AreaChips'
import { Button } from '../../components/Button'
import { FormErrors, SelectField, TextField, Toggle } from '../../components/Field'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { DestinationRows } from '../delivery/DestinationRows'
import { KIND_LABELS } from '../delivery/destinationsForm'
import { browserTimeZone, timeZones } from '../schedules/scheduleForm'
import { connectionsApi } from '../../api/endpoints.crewstate'
import { loadFolders } from '../connections/km2Folders'
import { emptyForm, inAreas, km2DestinationProblems, nameProblem, rowErrors, STEPS, suggestedName, toRequest, validateStep, type PMOForm, type Step } from './pmoForm'

function problems(err: unknown): string[] {
  if (err instanceof ApiError) return [err.message, ...err.details]
  return [err instanceof Error ? err.message : 'Something went wrong.']
}

function StepList({ step }: { step: Step }) {
  return (
    <ol className="mb-4 flex flex-wrap gap-2 text-[12.5px]" aria-label="Steps">
      {STEPS.map((label, i) => (
        <li key={label} aria-current={i === step ? 'step' : undefined}
          className={`rounded-full border px-3 py-1 ${i === step ? 'border-accent/60 bg-accent-soft text-accent' : i < step ? 'border-line text-fg' : 'border-line text-faint'}`}>
          {i < step ? <Check className="mr-1 inline size-3" /> : null}{i + 1}. {label}
        </li>
      ))}
    </ol>
  )
}

type StepProps = { form: PMOForm; opts: PMOOptions; errors: Record<string, string>; set: (change: Partial<PMOForm>) => void }

function Basics({ form, errors, set }: StepProps) {
  return (
    <div className="flex flex-col gap-3">
      <TextField label="Name" value={form.name} onChange={(v) => set({ name: v })} error={errors.name}
        hint="The crew is called this; its agents, tasks and schedules are named after it, such as “Atlas PMO: daily sweep”." />
      <AreaPicker value={form.area_ids} onChange={(v) => set({ area_ids: v })} error={errors.area_ids} />
      <SelectField label="Time zone" value={form.timezone} onChange={(v) => set({ timezone: v })} error={errors.timezone}
        options={timeZones().map((z) => ({ value: z, label: z }))}
        hint="The daily sweep runs on weekdays at 07:00 and the weekly report on Mondays at 08:00 in this time zone; follow-up dates count in it too." />
    </div>
  )
}

function Tracker({ form, opts, errors, set }: StepProps) {
  const trackers = inAreas(opts.trackers, form.area_ids)
  if (trackers.length === 0) {
    return <EmptyState title="No tracker connection" hint="Add a Jira, Linear or KM2 issues connection on Connections in the areas you chose (see the Jira, Linear and KM2 help), then come back." action={<Link className="text-accent" to="/connections">Open Connections</Link>} />
  }
  return (
    <div className="flex flex-col gap-3">
      <SelectField label="Tracker connection" value={form.tracker_connection_id} onChange={(v) => set({ tracker_connection_id: v,
        // A name still as suggested follows the tracker
        ...(form.name === suggestedName(opts.trackers.find((t) => t.id === form.tracker_connection_id)) ? { name: suggestedName(opts.trackers.find((t) => t.id === v)) } : {}) })}
        error={errors.tracker_connection_id}
        options={[{ value: '', label: 'Choose…' }, ...trackers.map((t) => ({ value: t.id, label: `${t.name} (${trackerKindLabel(t.kind ?? '')})` }))]} />
      <p className="text-[12.5px] text-muted">
        After setup, tell the coordinator in Chat what to track, such as “track PRJ-10 and its sub-tasks as Project Atlas”. The sweep then follows
        those items and their child tickets.
      </p>
    </div>
  )
}

function Destinations({ form, opts, errors, set }: StepProps) {
  const conns = inAreas(opts.destinations, form.area_ids).map((c) => ({ id: c.id, name: c.name, kind: c.kind ?? '' }))
  return (
    <div className="flex flex-col gap-3">
      <p className="text-[12.5px] text-muted">The weekly report always goes to the work order. Choose where else it goes; you can change this later on the weekly schedule.</p>
      <DestinationRows rows={form.destinations} connections={conns} errors={rowErrors(errors)} onChange={(rows) => set({ destinations: rows })} />
      {conns.length === 0 && <p className="text-[12px] text-faint">No Slack, Teams, email or KM2 connection in these areas yet; add them on Connections.</p>}
    </div>
  )
}

function Review({ form, opts, errors, set }: StepProps) {
  const llms = [{ value: '', label: 'Choose…' }, ...opts.llms.map((l) => ({ value: l.id, label: `${l.name} (${l.kind ?? ''})` }))]
  const tracker = opts.trackers.find((t) => t.id === form.tracker_connection_id)
  const warnings = [...new Set([form.coordinator_llm_id, form.reporter_llm_id])]
    .map((id) => opts.llms.find((l) => l.id === id))
    .filter((l) => l?.warning)
    .map((l) => `${l!.name}: ${l!.warning}`)
  return (
    <div className="flex flex-col gap-3">
      <SelectField label="PMO Coordinator model" value={form.coordinator_llm_id} onChange={(v) => set({ coordinator_llm_id: v })} options={llms}
        error={errors.coordinator_llm_id} hint="It runs the daily sweep and answers chat, so a fast, inexpensive model is usually right." />
      <SelectField label="Status Reporter model" value={form.reporter_llm_id} onChange={(v) => set({ reporter_llm_id: v })} options={llms}
        error={errors.reporter_llm_id} hint="It writes the weekly reports; a stronger writer pays off here." />
      {warnings.length > 0 && (
        <ul className="rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12px] text-warn" role="status" data-testid="llm-warnings">
          {warnings.map((w) => <li key={w}>{w}</li>)}
        </ul>
      )}
      <Toggle label="Turn the schedules on" checked={form.enabled} onChange={(v) => set({ enabled: v })}
        hint="Both start in dry run either way: nothing is posted or sent until you turn dry run off on each schedule's Watch panel." />
      <ul className="list-disc pl-5 text-[12.5px] text-muted" data-testid="pmo-summary">
        <li>Crew <strong className="text-fg">{form.name.trim()}</strong> with a coordinator (chat front desk) and a status reporter</li>
        <li>Follows <strong className="text-fg">{tracker?.name ?? 'the tracker'}</strong>; daily sweep on weekdays at 07:00 ({form.timezone}) with the follow-up ladder</li>
        <li>Weekly report on Mondays at 08:00 to the work order{form.destinations.map((d) => `, ${KIND_LABELS[d.kind]}`).join('')}</li>
      </ul>
    </div>
  )
}

const STEP_VIEWS = [Basics, Tracker, Destinations, Review]

function Wizard({ opts }: { opts: PMOOptions }) {
  const { me } = useAuth()
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>(0)
  const [form, setForm] = useState<PMOForm>(() => emptyForm(defaultAreaIds(me), browserTimeZone(), opts))
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [general, setGeneral] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const set = (change: Partial<PMOForm>) => setForm((f) => ({ ...f, ...change }))
  const View = STEP_VIEWS[step]

  const destinationProblems = () => km2DestinationProblems(form.destinations,
    Object.fromEntries(opts.destinations.map((c) => [c.id, c.name])), (id) => loadFolders(id, () => connectionsApi.km2Folders(id)))
  const next = async () => {
    const found = { ...validateStep(step, form), ...(step === 2 ? await destinationProblems() : {}) }
    setErrors(found)
    if (Object.keys(found).length === 0) setStep((s) => (s + 1) as Step)
  }
  const install = async () => {
    const found = validateStep(3, form)
    setErrors(found)
    setGeneral([])
    if (Object.keys(found).length > 0) return
    // A KM2 destination fixed on step 3 may have lost its default folder since; check again before setting up
    const rows = await destinationProblems()
    if (Object.keys(rows).length > 0) {
      setErrors(rows)
      setStep(2)
      return
    }
    setSaving(true)
    try {
      const out = await pmoApi.install(toRequest(form))
      notifySuccess(`${out.crew.name} is set up; tell it what to track in Chat`)
      navigate(`/crews/${out.crew.id}`)
    } catch (err) {
      const name = err instanceof ApiError ? nameProblem(err.details) : undefined
      if (name) {
        setErrors({ name: `Name: ${name}` })
        setStep(0)
      } else {
        setGeneral(problems(err))
      }
    } finally {
      setSaving(false)
    }
  }
  return (
    <Panel>
      <StepList step={step} />
      <View form={form} opts={opts} errors={errors} set={set} />
      <FormErrors messages={general} />
      <div className="mt-4 flex justify-between gap-2">
        <Button variant="ghost" icon={<ArrowLeft className="size-4" />} disabled={step === 0} onClick={() => { setErrors({}); setStep((s) => (s - 1) as Step) }}>Back</Button>
        {step < 3
          ? <Button variant="primary" icon={<ArrowRight className="size-4" />} onClick={() => void next()}>Next</Button>
          : <Button variant="primary" icon={<Check className="size-4" />} busy={saving} onClick={() => void install()}>Set up the PMO</Button>}
      </div>
    </Panel>
  )
}

/** The PMO setup wizard: copies the PMO template with its schedules, watches, chat and report destinations. */
export function PmoSetupPage() {
  const res = useAsync((signal) => pmoApi.options(signal), [])
  return (
    <>
      <PageHeader
        eyebrow="Automate"
        title={<span className="inline-flex items-center gap-2"><Briefcase className="size-5" />Set up a PMO</span>}
        description={<>A ready-made crew that follows up on due and overdue work in Jira, Linear or KM2 issues and writes a weekly status report per project. It comments and suggests; it never changes fields or creates issues. See <Link className="text-accent" to="/help/pmo">Set up a PMO</Link>.</>}
      />
      <AsyncView {...res}>{(opts) => <Wizard opts={opts} />}</AsyncView>
    </>
  )
}

export default PmoSetupPage
