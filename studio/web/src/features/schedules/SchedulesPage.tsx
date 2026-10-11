import { useState } from 'react'
import { Link } from 'react-router'
import { CalendarClock, Plus, TriangleAlert } from 'lucide-react'
import { schedulesApi } from '../../api/endpoints'
import type { Schedule } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { LinkButton } from '../../components/Button'
import { Toggle } from '../../components/Field'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { Can, useAuth } from '../../lib/auth'
import { dateTime, relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useNow } from '../../lib/useNow'
import { CrewLabel } from '../workorders/workOrderLabels'
import { cronToWords } from './cron'
import { toScheduleInput } from './scheduleForm'
import { fromNow, inZone } from './time'

export function SchedulesPage() {
  const res = useAsync((signal) => schedulesApi.list(signal), [])
  const { can } = useAuth()
  const manage = can('schedules.manage')
  const now = useNow(true, 30_000)
  const [busy, setBusy] = useState<string | null>(null)

  // Saving makes you the run-as person, so the toggle explains that on hover
  const toggle = async (s: Schedule, enabled: boolean) => {
    setBusy(s.id)
    try {
      const saved = await schedulesApi.update(s.id, { ...toScheduleInput(s), enabled })
      res.setData((prev) => prev?.map((x) => (x.id === saved.id ? saved : x)))
      notifySuccess(`${enabled ? 'Enabled' : 'Paused'} ${s.name}`)
    } catch (err) {
      notifyError(`Could not update ${s.name}`, err)
    } finally {
      setBusy(null)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Automate"
        title="Schedules"
        description="Create a work order for a crew on a cron schedule. Each one runs as the person who last saved it."
        actions={<><HistoryLink type="schedule" /><Can perm="schedules.manage"><LinkButton to="/schedules/new" variant="primary" icon={<Plus className="size-4" />}>New schedule</LinkButton></Can></>}
      />
      <AsyncView {...res}>
        {(list) =>
          list.length === 0 ? (
            <EmptyState icon={<CalendarClock className="size-6" />} title="No schedules yet" hint="A schedule creates and submits a work order at the times you choose." action={manage && <LinkButton to="/schedules/new" variant="primary">Create a schedule</LinkButton>} />
          ) : (
            <div className="overflow-x-auto rounded-lg border border-line bg-panel">
              <table className="w-full min-w-[1020px] text-left">
                <thead className="border-b border-line">
                  <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                    <th>Schedule</th><th>Crew</th><th>When</th><th>Next run</th><th>Last run</th><th>Run as</th><th>Enabled</th>
                  </tr>
                </thead>
                <tbody>
                  {[...list].sort((a, b) => a.name.localeCompare(b.name)).map((s) => (
                    <tr key={s.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                      <td className="max-w-xs">
                        <Link to={`/schedules/${s.id}`} className="font-medium hover:text-accent">{s.name}</Link>
                        {s.description && <div className="truncate text-[12px] text-muted">{s.description}</div>}
                        <div className="mt-1"><AreaChips areaIds={s.area_ids} max={2} /></div>
                        {s.last_error && (
                          <div className="mt-1 flex items-start gap-1 text-[11.5px] text-warn" title={s.last_error}>
                            <TriangleAlert className="mt-0.5 size-3 shrink-0" /><span className="line-clamp-2">{s.last_error}</span>
                          </div>
                        )}
                      </td>
                      <td className="max-w-[200px] text-[12.5px]"><CrewLabel name={s.target_name} crewId={s.crew_id} link /></td>
                      <td className="max-w-[240px]">
                        <div className="text-[12.5px]">{cronToWords(s.cron) ?? s.cron}</div>
                        <div className="font-mono text-[11px] text-faint">{s.cron} · {s.timezone}</div>
                      </td>
                      <td className="whitespace-nowrap text-[12.5px]">
                        {s.enabled && s.next_run_at ? <><div>{fromNow(s.next_run_at, now)}</div><div className="font-mono text-[11px] text-faint">{inZone(s.next_run_at, s.timezone)}</div></> : <span className="text-faint">{s.enabled ? '—' : 'paused'}</span>}
                      </td>
                      <td className="whitespace-nowrap text-[12.5px]">
                        {s.last_run_at ? <span title={dateTime(s.last_run_at)}>{relative(s.last_run_at, now)}</span> : <span className="text-faint">never</span>}
                        {s.last_work_order_id && <div><Link className="text-[11.5px] text-accent" to={`/work-orders/${s.last_work_order_id}`}>Last work order</Link></div>}
                      </td>
                      <td className="whitespace-nowrap text-[12px] text-muted">{s.run_as_name || '—'}</td>
                      <td className="relative" title={manage ? 'Changing this saves the schedule, which makes you its run-as person' : undefined}>
                        {manage ? <Toggle label={<span className="sr-only">Enabled</span>} checked={s.enabled} disabled={busy === s.id} onChange={(on) => void toggle(s, on)} /> : <Chip tone={s.enabled ? 'ok' : 'neutral'}>{s.enabled ? 'on' : 'paused'}</Chip>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </AsyncView>
    </>
  )
}
