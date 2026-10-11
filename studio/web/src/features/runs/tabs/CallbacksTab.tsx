import { Send } from 'lucide-react'
import { runsApi } from '../../../api/endpoints'
import type { Delivery } from '../../../api/types'
import { EmptyState, ErrorState, Loading } from '../../../components/States'
import { Chip, type Tone } from '../../../components/StatusBadge'
import { dateTime, relative } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { useNow } from '../../../lib/useNow'
import { usePollWhile } from '../../../lib/usePollWhile'
import { fromNow } from '../../schedules/time'

const TONE: Record<Delivery['status'], Tone> = { pending: 'blue', delivered: 'ok', failed: 'danger' }

/** Callback deliveries for a run (POSTed to callback_url when it finishes, with retries). */
export function CallbacksTab({ runId, live }: { runId: string; live: boolean }) {
  const res = useAsync((signal) => runsApi.deliveries(runId, signal), [runId])
  const pending = res.data?.some((d) => d.status === 'pending') ?? false
  const now = useNow(pending, 5000)
  usePollWhile(live || pending, res.reload, 5000)

  if (res.error && !res.data) return <ErrorState error={res.error} onRetry={res.reload} title="Could not load callback deliveries" />
  if (!res.data) return <Loading label="Loading deliveries…" />
  if (res.data.length === 0) {
    return <EmptyState icon={<Send className="size-6" />} title="No callbacks" hint="Runs started by a trigger with a callback URL, or by a service account, POST their result when they finish." />
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-line bg-panel">
      <table className="w-full min-w-[760px] text-left">
        <thead className="border-b border-line">
          <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
            <th>URL</th><th>Status</th><th className="text-right">Attempts</th><th>Last response</th><th>Next attempt</th><th>Delivered</th>
          </tr>
        </thead>
        <tbody>
          {res.data.map((d) => (
            <tr key={d.id} className="border-b border-line align-top last:border-0 [&>td]:px-3 [&>td]:py-2">
              <td className="max-w-xs"><span className="block truncate font-mono text-[11.5px]" title={d.url}>{d.url}</span><span className="font-mono text-[10.5px] text-faint">{d.id}</span></td>
              <td><Chip tone={TONE[d.status]}>{d.status}</Chip></td>
              <td className="text-right font-mono tabular">{d.attempts}</td>
              <td className="max-w-xs text-[12px]">
                {d.last_status_code !== null && <span className="font-mono">{d.last_status_code}</span>}
                {d.last_error && <span className="block break-words text-danger">{d.last_error}</span>}
                {d.last_status_code === null && !d.last_error && <span className="text-faint">—</span>}
              </td>
              <td className="whitespace-nowrap text-[12px] text-muted" title={dateTime(d.next_attempt_at)}>{d.status === 'pending' ? fromNow(d.next_attempt_at, now) : '—'}</td>
              <td className="whitespace-nowrap text-[12px] text-muted" title={dateTime(d.delivered_at)}>{d.delivered_at ? relative(d.delivered_at, now) : '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

