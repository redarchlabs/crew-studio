import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'
import { ScrollText, Search, Trash2 } from 'lucide-react'
import { crewStateApi } from '../../../api/endpoints.crewstate'
import { Button } from '../../../components/Button'
import { Panel } from '../../../components/PageHeader'
import { Pagination } from '../../../components/Pagination'
import { AsyncView, EmptyState } from '../../../components/States'
import { Chip } from '../../../components/StatusBadge'
import { ConfirmButton } from '../../../components/misc'
import { dateTime } from '../../../lib/format'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'

const PAGE = 25

type Filters = { subject: string; action: string }

/** Compact one-line details, such as {"to":"sam"}. */
export function detailsText(details: Record<string, unknown>): string {
  const keys = Object.keys(details)
  return keys.length === 0 ? '' : JSON.stringify(details)
}

/** The crew's action log, newest first, filterable by subject and action (matches anywhere, any case). */
export function ActionLogPanel({ crewId, canManage }: { crewId: string; canManage: boolean }) {
  const [typed, setTyped] = useState<Filters>({ subject: '', action: '' })
  const [filters, setFilters] = useState<Filters>(typed)
  const [offset, setOffset] = useState(0)
  const [deleting, setDeleting] = useState<string | null>(null)
  const res = useAsync((signal) => crewStateApi.log(crewId, { ...filters, limit: PAGE, offset }, signal), [crewId, filters, offset])

  const apply = (e: FormEvent) => {
    e.preventDefault()
    setOffset(0)
    setFilters({ subject: typed.subject.trim(), action: typed.action.trim() })
  }

  const remove = async (id: string) => {
    setDeleting(id)
    try {
      await crewStateApi.deleteLogEntry(crewId, id)
      notifySuccess('Deleted the log entry')
      res.reload()
    } catch (err) {
      notifyError('Could not delete the log entry', err)
    } finally {
      setDeleting(null)
    }
  }

  return (
    <Panel title="Action log" bodyClassName="p-0">
      <form onSubmit={apply} className="flex flex-wrap items-end gap-2 border-b border-line px-4 py-2.5">
        <input aria-label="Subject filter" className="field-input max-w-56" placeholder="Subject, such as PRJ-15" value={typed.subject} onChange={(e) => setTyped({ ...typed, subject: e.target.value })} />
        <input aria-label="Action filter" className="field-input max-w-56" placeholder="Action, such as followup" value={typed.action} onChange={(e) => setTyped({ ...typed, action: e.target.value })} />
        <Button type="submit" size="sm" icon={<Search className="size-3.5" />}>Filter</Button>
      </form>
      <AsyncView {...res}>
        {({ items, meta }) =>
          items.length === 0 ? (
            <div className="p-4">
              <EmptyState icon={<ScrollText className="size-6" />} title={filters.subject || filters.action ? 'No matching entries' : 'Nothing logged yet'} hint="Agents with the Record Action tool log what they did here, so later runs can check it exactly." />
            </div>
          ) : (
            <>
              <div className="overflow-x-auto">
                <table className="w-full min-w-[760px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>When</th><th>Subject</th><th>Action</th><th>Schedule</th><th>Details</th><th>Run</th>{canManage && <th />}
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((e) => (
                      <tr key={e.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                        <td className="whitespace-nowrap text-[12.5px] text-muted">{dateTime(e.at)}</td>
                        <td className="font-mono text-[12px]">{e.subject}</td>
                        <td className="font-mono text-[12px]">{e.action}{e.dry_run && <Chip tone="warn" className="ml-1">dry run</Chip>}</td>
                        <td className="text-[12.5px]">{e.schedule_id ? e.schedule_name : <span className="text-faint">Crew-wide</span>}</td>
                        <td className="max-w-xs truncate font-mono text-[11.5px] text-muted" title={detailsText(e.details)}>{detailsText(e.details) || <span className="text-faint">—</span>}</td>
                        <td className="text-[12px]">{e.run_id ? <Link className="text-accent" to={`/runs/${e.run_id}`}>open</Link> : <span className="text-faint">—</span>}</td>
                        {canManage && <td className="text-right"><ConfirmButton icon={<Trash2 className="size-3.5" />} busy={deleting === e.id} onConfirm={() => void remove(e.id)}>Delete</ConfirmButton></td>}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pagination meta={meta} onOffset={setOffset} />
            </>
          )
        }
      </AsyncView>
    </Panel>
  )
}
