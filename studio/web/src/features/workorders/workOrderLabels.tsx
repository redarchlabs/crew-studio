import { Link } from 'react-router'
import { CalendarClock, Code, Hand, MessagesSquare, Webhook, Workflow, type LucideIcon } from 'lucide-react'
import type { RunStatus, WorkOrderSource, WorkOrderStatus } from '../../api/types'
import { RUN_STATUSES } from '../../api/types'
import { Chip, StatusBadge, type Tone } from '../../components/StatusBadge'

export const WORK_ORDER_STATUSES: readonly WorkOrderStatus[] = ['draft', ...RUN_STATUSES]
export const WORK_ORDER_SOURCES: readonly WorkOrderSource[] = ['manual', 'schedule', 'webhook', 'api', 'chat']

export function woNumber(n: number): string {
  return `WO-${n}`
}

export function isActive(status: WorkOrderStatus | RunStatus | null | undefined): boolean {
  return status === 'queued' || status === 'running'
}

/** Draft gets a neutral badge; every other status is a run status. */
export function WorkOrderStatusBadge({ status }: { status: WorkOrderStatus }) {
  if (status !== 'draft') return <StatusBadge status={status} />
  return (
    <span className="inline-flex items-center gap-1.5 rounded border border-dashed border-line-strong px-1.5 py-0.5 font-mono text-[11px] font-medium uppercase tracking-wide text-muted">
      <span className="size-1.5 rounded-full border border-current" />
      draft
    </span>
  )
}

export function WoBadge({ number, className = '' }: { number: number; className?: string }) {
  return <span className={`whitespace-nowrap rounded border border-line bg-panel-2 px-1.5 py-px font-mono text-[11.5px] font-medium text-fg ${className}`}>{woNumber(number)}</span>
}

const SOURCE: Record<WorkOrderSource, { icon: LucideIcon; tone: Tone; label: string }> = {
  manual: { icon: Hand, tone: 'neutral', label: 'manual' },
  schedule: { icon: CalendarClock, tone: 'blue', label: 'schedule' },
  webhook: { icon: Webhook, tone: 'violet', label: 'webhook' },
  api: { icon: Code, tone: 'teal', label: 'api' },
  chat: { icon: MessagesSquare, tone: 'accent', label: 'chat' },
}

/** Where the work order came from; links to the schedule, trigger or (for its owner) the chat when known. */
export function SourceChip({ source, sourceId, link = false }: { source: WorkOrderSource; sourceId?: string | null; link?: boolean }) {
  const s = SOURCE[source]
  const Icon = s.icon
  const chip = (
    <Chip tone={s.tone} title={`Created by ${s.label}`}>
      <Icon className="size-3" />
      {s.label}
    </Chip>
  )
  const to = !sourceId ? null : source === 'schedule' ? `/schedules/${sourceId}` : source === 'webhook' ? `/triggers/${sourceId}` : source === 'chat' ? `/chat/${sourceId}` : null
  return link && to ? <Link to={to} className="hover:opacity-80">{chip}</Link> : chip
}

type CrewLabelProps = { name: string; crewId: string | null; link?: boolean }

/** Crew icon plus the target crew's name, optionally linking to the crew; a deleted crew keeps its old name. */
export function CrewLabel({ name, crewId, link = false }: CrewLabelProps) {
  const body = (
    <>
      <Workflow className="size-3.5 shrink-0 text-accent" aria-label="crew" />
      <span className="truncate">{name || <span className="text-faint">(deleted)</span>}</span>
    </>
  )
  return link && crewId ? (
    <Link to={`/crews/${crewId}`} className="inline-flex min-w-0 items-center gap-1.5 hover:text-accent">{body}</Link>
  ) : (
    <span className="inline-flex min-w-0 items-center gap-1.5" title={crewId ? undefined : 'This crew was deleted'}>{body}</span>
  )
}
