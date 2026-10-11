import { useSearchParams } from 'react-router'
import { ScrollText } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import { PageHeader } from '../../components/PageHeader'
import { Pagination } from '../../components/Pagination'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { dateTime } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { AUDIT_ACTION_LABELS, AUDIT_ACTION_TONES, detailPairs } from './auditFormat'

const PAGE_SIZE = 50

function Details({ details }: { details: Record<string, unknown> }) {
  const pairs = detailPairs(details)
  if (pairs.length === 0) return <span className="text-faint">—</span>
  return (
    <span className="flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-[11.5px]">
      {pairs.map(({ key, value }) => (
        <span key={key} className="max-w-full truncate" title={`${key}: ${value}`}>
          <span className="text-faint">{key}:</span> <span className="text-muted">{value}</span>
        </span>
      ))}
    </span>
  )
}

export function AuditPage() {
  const [params, setParams] = useSearchParams()
  const offset = Math.max(0, Math.floor(Number(params.get('offset')) || 0))
  const res = useAsync((signal) => adminApi.audit({ limit: PAGE_SIZE, offset }, signal), [offset])

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Audit log"
        description="Sign-ins, sign-outs, failed attempts, forced sign-outs, and API keys created, revoked or refused, newest first. Changes to people, roles and settings are in History."
      />
      <AsyncView {...res}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState icon={<ScrollText className="size-6" />} title="Nothing recorded yet" hint="Sign-in activity appears here." />
          ) : (
            <>
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[760px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Time</th><th>Action</th><th>Who</th><th>IP</th><th>Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {page.items.map((ev) => (
                      <tr key={ev.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-1.5">
                        <td className="whitespace-nowrap text-[12px] text-muted">{dateTime(ev.created_at)}</td>
                        <td className="whitespace-nowrap"><Chip tone={AUDIT_ACTION_TONES[ev.action] ?? 'neutral'} title={ev.action}>{AUDIT_ACTION_LABELS[ev.action] ?? ev.action}</Chip></td>
                        <td className="max-w-48 truncate" title={ev.actor_name || undefined}>{ev.actor_name || <span className="text-faint">unknown</span>}</td>
                        <td className="whitespace-nowrap font-mono text-[12px] text-muted">{ev.ip || '—'}</td>
                        <td className="max-w-lg"><Details details={ev.details} /></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pagination
                meta={page.meta}
                onOffset={(o) => setParams(o > 0 ? { offset: String(o) } : {})}
              />
            </>
          )
        }
      </AsyncView>
    </>
  )
}
