import { useEffect, useMemo, useState } from 'react'
import { CalendarClock, LoaderCircle } from 'lucide-react'
import { ApiError, errorMessage } from '../../api/client'
import { schedulesApi } from '../../api/endpoints'
import { Field, SelectField } from '../../components/Field'
import { useNow } from '../../lib/useNow'
import { CRON_PRESETS, CUSTOM_PRESET, cronProblem, cronToWords, presetFor } from './cron'
import { timeZones } from './scheduleForm'
import { fromNow, inZone } from './time'

const PREVIEW_DEBOUNCE_MS = 450

type CronFieldProps = {
  cron: string
  timezone: string
  onCron: (cron: string) => void
  onTimezone: (tz: string) => void
  cronError?: string
  timezoneError?: string
}

const PRESET_OPTIONS = [...CRON_PRESETS.map((p) => ({ value: p.key, label: p.label })), { value: CUSTOM_PRESET, label: 'Custom…' }]

/** Preset or custom cron, the expression in words, a time zone, and the server's next 5 run times. */
export function CronField({ cron, timezone, onCron, onTimezone, cronError, timezoneError }: CronFieldProps) {
  const preset = presetFor(cron)
  const [custom, setCustom] = useState(preset === CUSTOM_PRESET)
  const zones = useMemo(() => {
    const all = timeZones()
    return all.includes(timezone) || !timezone ? all : [timezone, ...all]
  }, [timezone])
  const words = cronToWords(cron)
  const local = cronProblem(cron)

  return (
    <div className="flex flex-col gap-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <SelectField
          label="Repeats"
          value={custom ? CUSTOM_PRESET : preset}
          options={PRESET_OPTIONS}
          onChange={(key) => {
            setCustom(key === CUSTOM_PRESET)
            const p = CRON_PRESETS.find((x) => x.key === key)
            if (p) onCron(p.cron)
          }}
        />
        <SelectField label="Time zone" value={timezone} options={zones.map((z) => ({ value: z, label: z.replace(/_/g, ' ') }))} onChange={onTimezone} error={timezoneError} />
      </div>
      <Field label="Cron expression" error={cronError ?? (custom ? (local ?? undefined) : undefined)} hint="5 fields: minute hour day-of-month month weekday, or @hourly, @daily, @weekly, @monthly. At most every 5 minutes.">
        {(id, invalid) => (
          <input id={id} className="field-input font-mono" aria-invalid={invalid} value={cron} spellCheck={false} placeholder="0 9 * * 1-5" onChange={(e) => { setCustom(true); onCron(e.target.value) }} />
        )}
      </Field>
      <div className="flex items-start gap-2 rounded-md bg-panel-2 px-3 py-2 text-[12.5px]">
        <CalendarClock className="mt-0.5 size-4 shrink-0 text-accent" />
        <span>{words ? <><span className="font-medium">{words}</span> <span className="text-muted">({timezone || 'UTC'})</span></> : <span className="text-faint">Cannot describe this expression.</span>}</span>
      </div>
      <NextRuns cron={cron} timezone={timezone} skip={local !== null} />
    </div>
  )
}

type PreviewState = { loading: boolean; next: string[]; error: string | null }

/** Debounced POST /schedules/preview; shows the 422 reason when the server rejects the expression. */
function NextRuns({ cron, timezone, skip }: { cron: string; timezone: string; skip: boolean }) {
  const [state, setState] = useState<PreviewState>({ loading: false, next: [], error: null })
  const now = useNow(true, 30_000)
  useEffect(() => {
    if (skip || !timezone) {
      setState({ loading: false, next: [], error: null })
      return
    }
    let alive = true
    setState((s) => ({ ...s, loading: true }))
    const t = setTimeout(() => {
      schedulesApi
        .preview(cron.trim(), timezone)
        .then((res) => alive && setState({ loading: false, next: res.next ?? [], error: null }))
        .catch((err: unknown) => {
          const detail = err instanceof ApiError && err.details.length > 0 ? err.details.join(' · ') : errorMessage(err)
          if (alive) setState({ loading: false, next: [], error: detail })
        })
    }, PREVIEW_DEBOUNCE_MS)
    return () => {
      alive = false
      clearTimeout(t)
    }
  }, [cron, timezone, skip])

  const local = Intl.DateTimeFormat().resolvedOptions().timeZone
  return (
    <div className="rounded-md border border-line bg-raised px-3 py-2">
      <div className="eyebrow mb-1 flex items-center gap-1.5">Next 5 runs {state.loading && <LoaderCircle className="size-3 animate-spin text-accent" />}</div>
      {state.error && <div className="text-[12px] text-danger" role="alert">{state.error}</div>}
      {!state.error && state.next.length === 0 && <div className="text-[12px] text-faint">{skip ? 'Fix the expression to see upcoming runs.' : state.loading ? 'Working out the next runs…' : 'No upcoming runs.'}</div>}
      <ol className="flex flex-col gap-0.5">
        {state.next.map((t) => (
          <li key={t} className="flex flex-wrap items-baseline gap-x-2 font-mono text-[12px] tabular">
            <span>{inZone(t, timezone)}</span>
            {local !== timezone && <span className="text-faint">· {inZone(t, local)}</span>}
            <span className="ml-auto text-[11px] text-faint">{fromNow(t, now)}</span>
          </li>
        ))}
      </ol>
    </div>
  )
}
