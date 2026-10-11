import { Plus, Trash2 } from 'lucide-react'
import { Button, IconButton } from '../../../components/Button'
import { NumberField, TextField } from '../../../components/Field'
import { emptyStep, splitList, WEEKDAYS, type LadderForm, type SkipRow, type StepRow } from './watchForm'

type Props = {
  ladder: LadderForm
  onChange: (ladder: LadderForm) => void
  trackedFields: string
  errors: Record<string, string>
}

const cell = 'field-input py-1 text-[12.5px]'

/** The follow-up ladder: a date field, steps with offsets, who to notify, repeats, and quiet days. */
export function LadderEditor({ ladder, onChange, trackedFields, errors }: Props) {
  const tracked = splitList(trackedFields)
  const setStep = (i: number, patch: Partial<StepRow>) => onChange({ ...ladder, steps: ladder.steps.map((s, j) => (j === i ? { ...s, ...patch } : s)) })
  const setSkip = (i: number, patch: Partial<SkipRow>) => onChange({ ...ladder, skip: ladder.skip.map((s, j) => (j === i ? { ...s, ...patch } : s)) })
  const toggleDay = (day: string) =>
    onChange({ ...ladder, quiet_days: ladder.quiet_days.includes(day) ? ladder.quiet_days.filter((d) => d !== day) : [...ladder.quiet_days, day] })

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <TextField label="Date field" value={ladder.date_field} onChange={(v) => onChange({ ...ladder, date_field: v })} error={errors.date_field} list="watch-tracked-fields" placeholder="due_date" hint="A tracked field holding each item's date." />
        <TextField label="Time zone" value={ladder.timezone} onChange={(v) => onChange({ ...ladder, timezone: v })} placeholder="the schedule's" hint="Empty uses the schedule's time zone." />
        <NumberField label="Most per check" value={ladder.max_per_tick} onChange={(v) => onChange({ ...ladder, max_per_tick: v })} nullable min={1} max={200} hint="Default 25; the rest wait for the next check." />
      </div>
      <datalist id="watch-tracked-fields">{tracked.map((t) => <option key={t} value={t} />)}</datalist>

      <div>
        <div className="mb-1 text-[12px] font-medium text-muted">Steps</div>
        <table className="w-full text-left">
          <thead>
            <tr className="eyebrow [&>th]:pb-1 [&>th]:pr-2 [&>th]:font-semibold"><th>Name</th><th>Offset</th><th>Notify</th><th>Repeat</th><th /></tr>
          </thead>
          <tbody>
            {ladder.steps.map((s, i) => (
              <tr key={i} className="[&>td]:pb-1 [&>td]:pr-2 align-top">
                <td><input aria-label={`Step ${i + 1} name`} className={cell} value={s.name} onChange={(e) => setStep(i, { name: e.target.value })} placeholder="due_soon" /></td>
                <td><input aria-label={`Step ${i + 1} offset`} className={`${cell} w-20`} value={s.offset} onChange={(e) => setStep(i, { offset: e.target.value })} placeholder="-3d" /></td>
                <td><input aria-label={`Step ${i + 1} notify`} className={cell} value={s.notify} onChange={(e) => setStep(i, { notify: e.target.value })} placeholder="owner" /></td>
                <td><input aria-label={`Step ${i + 1} repeat`} className={`${cell} w-20`} value={s.repeat} onChange={(e) => setStep(i, { repeat: e.target.value })} placeholder="—" /></td>
                <td><IconButton label={`Remove step ${i + 1}`} icon={<Trash2 className="size-3.5" />} onClick={() => onChange({ ...ladder, steps: ladder.steps.filter((_, j) => j !== i) })} /></td>
              </tr>
            ))}
          </tbody>
        </table>
        {Object.entries(errors).filter(([k]) => k.startsWith('step')).map(([k, v]) => <div key={k} className="text-[12px] text-danger" role="alert">{k.startsWith('step:') ? `Step ${Number(k.slice(5)) + 1}: ` : ''}{v}</div>)}
        <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => onChange({ ...ladder, steps: [...ladder.steps, emptyStep()] })}>Add step</Button>
        <p className="mt-1 text-[11.5px] text-faint">Offsets count from the date: -3d is three days before, +2d two days after, 1w a week after. When several steps have passed, only the latest is due. Notify is a role (owner, lead) or a tracked field holding the person.</p>
      </div>

      <div>
        <div className="mb-1 text-[12px] font-medium text-muted">Skip items when</div>
        {ladder.skip.map((row, i) => (
          <div key={i} className="mb-1 flex flex-wrap items-center gap-2 text-[12.5px]">
            <input aria-label={`Skip rule ${i + 1} field`} className={`${cell} w-40`} value={row.field} onChange={(e) => setSkip(i, { field: e.target.value })} placeholder="status" list="watch-tracked-fields" />
            <span className="text-muted">is one of</span>
            <input aria-label={`Skip rule ${i + 1} values`} className={`${cell} min-w-0 flex-1`} value={row.values} onChange={(e) => setSkip(i, { values: e.target.value })} placeholder="Done, Canceled" />
            <IconButton label={`Remove skip rule ${i + 1}`} icon={<Trash2 className="size-3.5" />} onClick={() => onChange({ ...ladder, skip: ladder.skip.filter((_, j) => j !== i) })} />
          </div>
        ))}
        {errors.skip && <div className="text-[12px] text-danger" role="alert">{errors.skip}</div>}
        <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => onChange({ ...ladder, skip: [...ladder.skip, { field: '', values: '' }] })}>Add rule</Button>
      </div>

      <div>
        <div className="mb-1 text-[12px] font-medium text-muted">Quiet days</div>
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="Quiet days">
          {WEEKDAYS.map((d) => (
            <button key={d} type="button" aria-pressed={ladder.quiet_days.includes(d)} onClick={() => toggleDay(d)}
              className={`rounded border px-2 py-0.5 text-[12px] ${ladder.quiet_days.includes(d) ? 'border-accent/60 bg-accent-soft' : 'border-line bg-raised'}`}>{d}</button>
          ))}
        </div>
        {errors.quiet_days && <div className="text-[12px] text-danger" role="alert">{errors.quiet_days}</div>}
        <p className="mt-1 text-[11.5px] text-faint">Steps falling on a quiet day move to the next working day, and nothing is due on a quiet day.</p>
      </div>
    </div>
  )
}
