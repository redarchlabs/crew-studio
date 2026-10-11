import { Link } from 'react-router'
import { ExternalLink } from 'lucide-react'
import type { Run, RunStatus } from '../../../api/types'
import { useAuth } from '../../../lib/auth'
import { relative, usd } from '../../../lib/format'

const DOT: Record<RunStatus, string> = {
  queued: 'bg-blue pulse-dot',
  running: 'bg-accent pulse-dot',
  succeeded: 'bg-ok',
  failed: 'bg-danger',
  cancelled: 'bg-faint',
}

type AttemptSelectorProps = {
  /** Newest first, as the API returns them */
  runs: readonly Run[]
  selectedId: string | null
  onSelect: (runId: string) => void
  /** Live status of the selected attempt (may be ahead of the list) */
  liveStatus: RunStatus | null
}

/** "Attempt 3 of 3" plus a scrollable row of attempts, newest first; every tab follows the selection. */
export function AttemptSelector({ runs, selectedId, onSelect, liveStatus }: AttemptSelectorProps) {
  const { can } = useAuth()
  const index = runs.findIndex((r) => r.id === selectedId)
  const number = index < 0 ? runs.length : runs.length - index
  return (
    <div className="mb-3 flex flex-col gap-1.5">
      <div className="flex items-center gap-2 text-[12px]">
        <span className="font-medium">Attempt {number} of {runs.length}</span>
        {selectedId && can('runs.view') && (
          <Link to={`/runs/${selectedId}`} className="inline-flex items-center gap-1 text-muted hover:text-accent">
            Open run <ExternalLink className="size-3" />
          </Link>
        )}
      </div>
      <div role="tablist" aria-label="Attempts" className="flex gap-1.5 overflow-x-auto pb-1">
        {runs.map((r, i) => {
          const on = r.id === selectedId
          const status = on && liveStatus ? liveStatus : r.status
          return (
            <button
              key={r.id}
              type="button"
              role="tab"
              aria-selected={on}
              onClick={() => onSelect(r.id)}
              className={`flex shrink-0 items-center gap-2 rounded-md border px-2.5 py-1 text-left text-[12px] transition-colors ${on ? 'border-accent/60 bg-accent-soft' : 'border-line bg-panel hover:border-line-strong'}`}
            >
              <span className={`size-2 rounded-full ${DOT[status]}`} title={status} />
              <span className="font-medium">#{runs.length - i}</span>
              <span className="text-muted">{status}</span>
              <span className="font-mono text-[11px] text-faint">{usd(r.cost_usd)} · {relative(r.created_at)}</span>
            </button>
          )
        })}
      </div>
    </div>
  )
}
