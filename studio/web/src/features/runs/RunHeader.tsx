import { Link } from 'react-router'
import { ChevronLeft, CircleStop, Radio } from 'lucide-react'
import type { Run, RunStatus } from '../../api/types'
import { Button } from '../../components/Button'
import { StatusBadge } from '../../components/StatusBadge'
import { Stat } from '../../components/misc'
import { between, dateTime, durationMs, int } from '../../lib/format'
import { useNow } from '../../lib/useNow'
import { BudgetBar } from './BudgetBar'
import type { StreamState } from './timeline/useRunTimeline'

type RunHeaderProps = {
  run: Run
  status: RunStatus
  spent: number
  budgetState: string | null
  stream: StreamState
  cancelling: boolean
  onCancel: () => void
}

const STREAM_LABEL: Record<StreamState, string> = {
  idle: '',
  loading: 'loading events',
  connecting: 'connecting',
  live: 'live',
  reconnecting: 'reconnecting…',
  closed: '',
}

export function RunHeader({ run, status, spent, budgetState, stream, cancelling, onCancel }: RunHeaderProps) {
  const active = status === 'running' || status === 'queued'
  const now = useNow(active)
  const duration = between(run.started_at ?? (status === 'running' ? run.created_at : null), active ? null : run.finished_at, now)
  const inputs = Object.entries(run.inputs)
  const tokens = run.usage ? run.usage.prompt_tokens + run.usage.completion_tokens : null

  return (
    <header className="mb-4 flex flex-col gap-3 border-b border-line pb-4">
      <Link to="/runs" className="inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
        <ChevronLeft className="size-3.5" /> Runs
      </Link>
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="text-[20px] font-semibold tracking-tight">
          {run.crew_id ? <Link className="hover:text-accent" to={`/crews/${run.crew_id}`}>{run.crew_name}</Link> : run.crew_name}
        </h1>
        <StatusBadge status={status} />
        {STREAM_LABEL[stream] && (
          <span className={`inline-flex items-center gap-1 font-mono text-[11px] ${stream === 'live' ? 'text-ok' : 'text-faint'}`}>
            <Radio className="size-3" /> {STREAM_LABEL[stream]}
          </span>
        )}
        <span className="font-mono text-[11px] text-faint">{run.id}</span>
        {active && (
          <Button className="ml-auto" variant="danger" icon={<CircleStop className="size-4" />} busy={cancelling} onClick={onCancel}>
            Cancel run
          </Button>
        )}
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-[minmax(240px,1.4fr)_repeat(3,minmax(0,1fr))]">
        <BudgetBar spent={spent} budget={run.budget_usd} hardStopMultiplier={run.spec?.settings.hard_stop_multiplier} state={budgetState} />
        <Stat label="Duration" value={durationMs(duration)} sub={`started ${dateTime(run.started_at ?? run.created_at)}`} />
        <Stat label="Tokens" value={int(tokens)} sub={run.usage ? `${int(run.usage.successful_requests)} requests` : 'final usage pending'} />
        <Stat label="Process" value={run.spec?.crew.process ?? '—'} sub={run.spec ? `${run.spec.agents.length} agents · ${run.spec.tasks.length} tasks` : undefined} />
      </div>
      {inputs.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {inputs.map(([k, val]) => (
            <span key={k} className="max-w-full truncate rounded border border-line bg-panel px-2 py-0.5 text-[12px]" title={val}>
              <span className="font-mono text-accent">{k}</span> <span className="text-muted">{val || '""'}</span>
            </span>
          ))}
        </div>
      )}
      {run.error && status !== 'running' && <div className="rounded-md border border-danger/40 bg-danger-soft px-3 py-2 text-[12.5px] text-danger">{run.error}</div>}
    </header>
  )
}
