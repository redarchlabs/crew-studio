import { useState } from 'react'
import { Save, Send } from 'lucide-react'
import { ApiError } from '../../api/client'
import { destinationsApi } from '../../api/endpoints.delivery'
import type { Schedule } from '../../api/types'
import type { DestinationsView } from '../../api/types.delivery'
import { Button } from '../../components/Button'
import { FormErrors } from '../../components/Field'
import { Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { DestinationRows } from './DestinationRows'
import { describeDestination, toInputs, toRows, validateRows, type DestinationRow } from './destinationsForm'

function problems(err: unknown): string[] {
  if (err instanceof ApiError) return [err.message, ...err.details]
  return [err instanceof Error ? err.message : 'Something went wrong.']
}

function Editor({ schedule, view, onSaved }: { schedule: Schedule; view: DestinationsView; onSaved: (v: DestinationsView) => void }) {
  const [rows, setRows] = useState<DestinationRow[]>(() => toRows(view.destinations))
  const [errors, setErrors] = useState<Record<number, string>>({})
  const [general, setGeneral] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const save = async () => {
    const found = validateRows(rows)
    setErrors(found)
    setGeneral([])
    if (Object.keys(found).length > 0) return
    setSaving(true)
    try {
      const saved = await destinationsApi.put(schedule.id, toInputs(rows))
      notifySuccess('Report destinations saved')
      onSaved(saved)
    } catch (err) {
      setGeneral(problems(err))
    } finally {
      setSaving(false)
    }
  }
  return (
    <div className="flex flex-col gap-3">
      <DestinationRows rows={rows} connections={view.connections} errors={errors} onChange={setRows} />
      {view.connections.length === 0 && (
        <p className="text-[12px] text-faint">
          No Slack, Teams, email or KM2 connection is available: add one on Connections and let the crew use it on its State tab.
        </p>
      )}
      <FormErrors messages={general} />
      <div><Button variant="primary" icon={<Save className="size-4" />} busy={saving} onClick={() => void save()}>Save destinations</Button></div>
    </div>
  )
}

/** Where the reports of a schedule's runs go (Deliver Report). */
export function ReportDestinationsPanel({ schedule, canManage }: { schedule: Schedule; canManage: boolean }) {
  const res = useAsync((signal) => destinationsApi.get(schedule.id, signal), [schedule.id])
  const [version, setVersion] = useState(0)
  return (
    <Panel title={<span className="inline-flex items-center gap-2"><Send className="size-4 text-faint" />Report destinations</span>}>
      <AsyncView {...res}>
        {(view) => (
          <div className="flex flex-col gap-3">
            <p className="text-[12.5px] text-muted">
              When a run of this schedule delivers a report (the Deliver Report tool), Crew Studio sends it to the work order and to each destination
              below. In a dry run nothing is sent; the work order shows where it would have gone.
            </p>
            {canManage ? (
              <Editor key={version} schedule={schedule} view={view} onSaved={(v) => { res.setData(() => v); setVersion((n) => n + 1) }} />
            ) : view.destinations.length === 0 ? (
              <p className="text-[12.5px] text-faint">Reports go only to the work order.</p>
            ) : (
              <ul className="list-disc pl-5 text-[13px]">{view.destinations.map((d) => <li key={d.id}>{describeDestination(d)}</li>)}</ul>
            )}
          </div>
        )}
      </AsyncView>
    </Panel>
  )
}
