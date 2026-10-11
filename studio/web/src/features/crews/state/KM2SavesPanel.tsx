import { Link } from 'react-router'
import { BookUp } from 'lucide-react'
import { crewStateApi } from '../../../api/endpoints.crewstate'
import type { ActionEntry } from '../../../api/types.crewstate'
import { Panel } from '../../../components/PageHeader'
import { Chip } from '../../../components/StatusBadge'
import { dateTime } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'

const SHOWN = 10
export const KM2_SAVE_ACTION = 'km2:save'

const text = (v: unknown): string => (typeof v === 'string' ? v : '')

/** What a km2:save entry says happened: a dry run, KM2's outcome, or nothing known. */
export function saveOutcome(e: ActionEntry): { label: string; tone: 'warn' | 'ok' | 'neutral' | 'danger' } {
  if (e.dry_run || e.details.would_save === true) return { label: 'dry run, not sent', tone: 'warn' }
  const outcome = text(e.details.outcome)
  if (outcome === 'sending') return { label: 'sending', tone: 'neutral' }
  if (outcome === 'unknown') return { label: 'no answer from KM2', tone: 'danger' }
  if (outcome === 'created' || outcome === 'updated') return { label: outcome, tone: 'ok' }
  return { label: outcome.replace('_', ' ') || 'saved', tone: 'neutral' }
}

/**
 * The crew's latest KM2 saves (km_save_document), read from its action log. Hidden until the crew has saved
 * something, so crews without KM2 show nothing.
 */
export function KM2SavesPanel({ crewId }: { crewId: string }) {
  const res = useAsync((signal) => crewStateApi.log(crewId, { action: KM2_SAVE_ACTION, limit: SHOWN, offset: 0 }, signal), [crewId])
  const items = (res.data?.items ?? []).filter((e) => e.action === KM2_SAVE_ACTION)
  if (items.length === 0) return null
  const total = res.data?.meta.total ?? items.length
  return (
    <Panel title={<span className="inline-flex items-center gap-1.5"><BookUp className="size-3.5" /> Recent KM2 saves</span>} bodyClassName="p-0">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[640px] text-left" aria-label="Recent KM2 saves">
          <thead className="border-b border-line">
            <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
              <th>When</th><th>Document</th><th>Connection and folder</th><th>Outcome</th><th>Run</th>
            </tr>
          </thead>
          <tbody>
            {items.map((e) => {
              const outcome = saveOutcome(e)
              return (
                <tr key={e.id} className="border-b border-line align-top last:border-0 [&>td]:px-3 [&>td]:py-2">
                  <td className="whitespace-nowrap text-[12.5px] text-muted">{dateTime(e.at)}</td>
                  <td className="max-w-xs">
                    <div className="truncate text-[13px]" title={text(e.details.title)}>{text(e.details.title) || '—'}</div>
                    <div className="truncate font-mono text-[11.5px] text-muted">{text(e.details.external_ref) || e.subject}</div>
                  </td>
                  <td className="text-[12.5px]">
                    <div>{text(e.details.connection) || '—'}</div>
                    <div className="truncate font-mono text-[11px] text-faint">{text(e.details.folder) || text(e.details.folder_id)}</div>
                  </td>
                  <td><Chip tone={outcome.tone}>{outcome.label}</Chip></td>
                  <td className="text-[12px]">{e.run_id ? <Link className="text-accent" to={`/runs/${e.run_id}`}>open</Link> : <span className="text-faint">—</span>}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {total > items.length && (
        <p className="border-t border-line px-4 py-2 text-[12px] text-muted">Showing the latest {items.length} of {total}. Filter the action log by km2:save for all of them.</p>
      )}
    </Panel>
  )
}
