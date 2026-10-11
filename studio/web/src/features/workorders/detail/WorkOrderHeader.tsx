import { Link } from 'react-router'
import { ChevronLeft, CircleStop, Pencil, Radio, RotateCcw, Send, StepForward, Trash2 } from 'lucide-react'
import type { ResumeHint, Run, RunStatus, WorkOrder, WorkOrderStatus } from '../../../api/types'
import { AreaChips } from '../../../components/AreaChips'
import { Button } from '../../../components/Button'
import { ConfirmButton, HistoryLink, Stat } from '../../../components/misc'
import { useAuth } from '../../../lib/auth'
import { between, dateTime, durationMs, int } from '../../../lib/format'
import { useNow } from '../../../lib/useNow'
import { BudgetBar } from '../../runs/BudgetBar'
import { resumeLabel } from '../../runs/ResumeRunModal'
import type { SpendTally } from '../../runs/timeline/spendTally'
import type { StreamState } from '../../runs/timeline/useRunTimeline'
import { CrewLabel, isActive, SourceChip, WoBadge, WorkOrderStatusBadge } from '../workOrderLabels'

export type HeaderBusy = 'submit' | 'cancel' | 'delete' | null

type WorkOrderHeaderProps = {
  workOrder: WorkOrder
  status: WorkOrderStatus
  run: Run | null
  runStatus: RunStatus | null
  spent: number | null
  budgetState: string | null
  /** Live spend and tokens from the attempt's events, shown until the run's final usage arrives. */
  tally: SpendTally
  stream: StreamState
  busy: HeaderBusy
  onSubmit: () => void
  onCancel: () => void
  onEdit: () => void
  onDelete: () => void
  /** Set when the latest attempt failed or was cancelled part-way and can be resumed */
  resumable?: ResumeHint | null
  onResume?: () => void
}

/** Title row with actions, then who/what/where, then the selected attempt's spend, duration and tokens. */
export function WorkOrderHeader({ workOrder: wo, status, run, runStatus, spent, budgetState, tally, stream, busy, onSubmit, onCancel, onEdit, onDelete, resumable, onResume }: WorkOrderHeaderProps) {
  const { can } = useAuth()
  const active = isActive(status)
  const runActive = isActive(runStatus)
  const now = useNow(runActive)
  const duration = run ? between(run.started_at, runActive ? null : run.finished_at, now) : null
  const liveTokens = tally.promptTokens + tally.completionTokens
  const tokens = run?.usage ? run.usage.prompt_tokens + run.usage.completion_tokens : liveTokens > 0 ? liveTokens : null
  const tokensSub = run?.usage
    ? `${int(run.usage.successful_requests)} requests`
    : liveTokens > 0
      ? `${int(tally.llmCalls)} LLM calls so far`
      : runActive
        ? 'final usage pending'
        : undefined
  const busyAny = busy !== null

  return (
    <header className="mb-4 flex flex-col gap-3 border-b border-line pb-4">
      <Link to="/work-orders" className="inline-flex items-center gap-1 self-start text-[12px] text-muted hover:text-fg">
        <ChevronLeft className="size-3.5" /> Work orders
      </Link>
      <div className="flex flex-wrap items-start gap-x-3 gap-y-2">
        <div className="flex min-w-0 basis-full flex-wrap items-center gap-2 md:basis-0 md:flex-1">
          <WoBadge number={wo.number} className="text-[13px]" />
          <h1 className="min-w-0 text-[20px] font-semibold leading-tight tracking-tight">{wo.title}</h1>
          <WorkOrderStatusBadge status={status} />
          {stream === 'live' && <span className="inline-flex items-center gap-1 font-mono text-[11px] text-ok"><Radio className="size-3" /> live</span>}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <HistoryLink type="work_order" id={wo.id} />
          {can('work_orders.delete') && (
            <ConfirmButton icon={<Trash2 className="size-3.5" />} size="md" busy={busy === 'delete'} disabled={active || busyAny} onConfirm={onDelete}>Delete</ConfirmButton>
          )}
          {can('work_orders.create') && (
            <Button icon={<Pencil className="size-4" />} disabled={active || busyAny} title={active ? 'Wait for the current attempt to finish, or cancel it' : 'Edit the work order'} onClick={onEdit}>Edit</Button>
          )}
          {active && can('work_orders.cancel') && (
            <Button variant="danger" icon={<CircleStop className="size-4" />} busy={busy === 'cancel'} disabled={busyAny} onClick={onCancel}>Cancel</Button>
          )}
          {!active && can('work_orders.create') && resumable && onResume && (
            <Button variant="primary" icon={<StepForward className="size-4" />} disabled={busyAny} title={`Reuses what the attempt finished and runs ${resumable.task_name} and the rest`} onClick={onResume}>
              {resumeLabel(resumable)}
            </Button>
          )}
          {!active && can('work_orders.create') && (
            <Button variant={resumable && onResume ? 'secondary' : 'primary'} icon={status === 'draft' ? <Send className="size-4" /> : <RotateCcw className="size-4" />} busy={busy === 'submit'} disabled={busyAny} onClick={onSubmit}>
              {status === 'draft' ? 'Submit' : 'Re-run'}
            </Button>
          )}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[12.5px] text-muted">
        <CrewLabel name={wo.target_name} crewId={wo.crew_id} link />
        <SourceChip source={wo.source} sourceId={wo.source_id} link />
        <AreaChips areaIds={wo.area_ids} />
        <span>by {wo.created_by_name || 'unknown'} · {dateTime(wo.created_at)}</span>
        {wo.submitted_at && <span className="text-faint">submitted {dateTime(wo.submitted_at)}</span>}
      </div>
      {run && (
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-[minmax(240px,1.4fr)_repeat(3,minmax(0,1fr))]">
          <div className="col-span-2 lg:col-span-1">
            <BudgetBar spent={spent ?? 0} budget={run.budget_usd} hardStopMultiplier={run.spec?.settings.hard_stop_multiplier} state={budgetState} />
          </div>
          <Stat label="Duration" value={durationMs(duration)} sub={run.started_at ? `started ${dateTime(run.started_at)}` : 'not started'} />
          <Stat label="Tokens" value={int(tokens)} sub={tokensSub} />
          <Stat label="Attempts" value={wo.run_count} sub={wo.finished_at ? `last finished ${dateTime(wo.finished_at)}` : undefined} />
        </div>
      )}
    </header>
  )
}
