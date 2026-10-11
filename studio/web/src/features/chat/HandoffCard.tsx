import { useState } from 'react'
import { Link } from 'react-router'
import { CircleStop, Hourglass, Loader2 } from 'lucide-react'
import { chatApi } from '../../api/endpoints.chat'
import type { ChatMessage } from '../../api/types.chat'
import { ConfirmButton } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { relative, usd } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'

type Props = {
  /** A hand-off's result message that has no text yet */
  message: ChatMessage
  now: number
  /** Re-reads the conversation (after cancelling, or when the card is out of date) */
  onStale: () => void
}

/** Minutes between two times, rounded: the time limit when an older server does not send it. */
function limitMinutes(from: string, to: string): number {
  return Math.max(1, Math.round((Date.parse(to) - Date.parse(from)) / 60_000))
}

/**
 * A hand-off that has not brought back a result: waiting for a slot, working in the background (with Cancel), or
 * stopped without finishing. A finished result is shown by MessageList as an ordinary message instead.
 */
export function HandoffCard({ message: m, now, onStale }: Props) {
  const { can } = useAuth()
  const [busy, setBusy] = useState(false)
  const h = m.handoff
  if (!h) return null
  const runLink = m.run_id && can('runs.view') ? <Link className="hover:text-accent" to={`/runs/${m.run_id}`}>run</Link> : null

  const cancel = async () => {
    setBusy(true)
    try {
      await chatApi.cancelHandoff(h.id)
      notifySuccess('Hand-off cancelled')
    } catch (err) {
      notifyError('Could not cancel the hand-off', err)
    } finally {
      setBusy(false)
      onStale()
    }
  }
  const cancelButton = <ConfirmButton icon={<CircleStop className="size-3.5" />} busy={busy} onConfirm={() => void cancel()}>Cancel</ConfirmButton>

  if (m.run_status === 'queued' || m.run_status === 'running') {
    const queued = m.run_status === 'queued'
    const minutes = h.timeout_minutes || limitMinutes(m.created_at, h.deadline_at)
    return (
      <section aria-label="Hand-off" className="rounded-md border border-blue/40 bg-blue-soft/30 p-3 text-[12.5px]" role="status">
        <div className="flex flex-wrap items-center gap-2">
          {queued ? <Hourglass className="size-3.5 text-blue" /> : <Loader2 className="size-3.5 animate-spin text-blue" />}
          <span className="font-medium">{queued ? `${h.worker_name} will start when a slot is free` : `${h.worker_name} is working in the background`}</span>
        </div>
        <p className="mt-1">{h.title}</p>
        <div className="mt-1 flex flex-wrap items-center gap-2 text-[11.5px] text-faint">
          {queued
            ? <span>up to {minutes} min once it starts · up to {usd(h.budget_usd)}</span>
            : <span>started {relative(h.started_at ?? m.created_at, now)} · up to {minutes} min · up to {usd(h.budget_usd)}</span>}
          {runLink}
          {cancelButton}
        </div>
      </section>
    )
  }
  if (m.run_status === 'failed' || m.run_status === 'cancelled') {
    return (
      <section aria-label="Hand-off" className="rounded-md border border-danger/40 bg-danger-soft/40 p-3 text-[12.5px]">
        <p className="font-medium text-danger">The hand-off did not finish</p>
        <p className="mt-1">{h.title}</p>
        <p className="mt-1 text-muted">{m.run_status === 'cancelled' ? 'Cancelled by you' : m.run_error || 'The run failed.'}</p>
        <div className="mt-1 flex flex-wrap items-center gap-2 text-[11.5px] text-faint">
          {m.cost_usd !== null && <span className="font-mono">{usd(m.cost_usd)} spent</span>}
          {runLink}
        </div>
      </section>
    )
  }
  return <p className="text-faint">No result came back.</p>
}
