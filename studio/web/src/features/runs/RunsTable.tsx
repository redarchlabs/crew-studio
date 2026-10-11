import { Link } from 'react-router'
import { CircleStop, Trash2 } from 'lucide-react'
import type { Run, TriggerType } from '../../api/types'
import { Button } from '../../components/Button'
import { Chip, StatusBadge, type Tone } from '../../components/StatusBadge'
import { ConfirmButton } from '../../components/misc'
import { between, dateTime, durationMs, relative, usd } from '../../lib/format'
import { TRIGGER_LABELS } from './runsQuery'
import { isTerminal } from './timeline/payload'

const TRIGGER_TONE: Record<TriggerType, Tone> = {
  manual: 'neutral',
  api: 'blue',
  schedule: 'violet',
  webhook: 'teal',
  work_order: 'accent',
}

/** Chip naming how a run started; work-order runs link to their work order. */
export function TriggerChip({ run }: { run: Pick<Run, 'trigger_type' | 'work_order_id'> }) {
  const type = run.trigger_type ?? 'manual'
  const chip = <Chip tone={TRIGGER_TONE[type] ?? 'neutral'}>{TRIGGER_LABELS[type] ?? type}</Chip>
  if (!run.work_order_id) return chip
  return (
    <Link to={`/work-orders/${run.work_order_id}`} className="hover:opacity-80" title="Open the work order">
      {chip}
    </Link>
  )
}

type RunsTableProps = {
  runs: readonly Run[]
  now: number
  compact?: boolean
  /** Omit when the person lacks runs.cancel */
  onCancel?: (run: Run) => void
  /** Omit when the person lacks runs.delete */
  onDelete?: (run: Run) => void
  busyId?: string | null
}

export function RunsTable({ runs, now, compact = false, onCancel, onDelete, busyId }: RunsTableProps) {
  const actions = !compact && Boolean(onCancel || onDelete)
  return (
    <div className="overflow-x-auto rounded-lg border border-line bg-panel">
      <table className="w-full min-w-[860px] text-left">
        <thead className="border-b border-line">
          <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
            <th>Crew</th><th>Status</th><th>Trigger</th><th>Started by</th><th className="text-right">Cost</th><th className="text-right">Budget</th><th className="text-right">Duration</th><th>Started</th>{actions && <th />}
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => {
            const finished = isTerminal(r.status)
            return (
              <tr key={r.id} className="border-b border-line last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-1.5">
                <td>
                  <Link to={`/runs/${r.id}`} className="mr-1.5 font-medium hover:text-accent">{r.crew_name}</Link>
                  {r.kind === 'chat' && <Chip tone="accent" title="One turn of a crew chat">chat</Chip>}
                  {r.kind === 'handoff' && <Chip tone="blue" title="A hand-off from an agent chat, worked in the background">hand-off</Chip>}
                  <span className="ml-2 font-mono text-[10.5px] text-faint">{r.id.slice(0, 8)}</span>
                </td>
                <td><StatusBadge status={r.status} /></td>
                <td><TriggerChip run={r} /></td>
                <td className="max-w-40 truncate text-[12px] text-muted" title={r.created_by_name || undefined}>{r.created_by_name || <span className="text-faint">—</span>}</td>
                <td className="text-right font-mono tabular">{usd(r.cost_usd)}</td>
                <td className="text-right font-mono tabular text-muted">{usd(r.budget_usd)}</td>
                <td className="text-right font-mono tabular text-muted">{durationMs(between(r.started_at, finished ? r.finished_at : null, now))}</td>
                <td className="whitespace-nowrap text-[12px] text-muted" title={dateTime(r.started_at ?? r.created_at)}>{relative(r.started_at ?? r.created_at, now)}</td>
                {actions && (
                  <td className="whitespace-nowrap text-right">
                    {!finished && onCancel && <Button size="sm" variant="ghost" icon={<CircleStop className="size-3.5" />} busy={busyId === r.id} onClick={() => onCancel(r)}>Cancel</Button>}
                    {finished && onDelete && <ConfirmButton icon={<Trash2 className="size-3.5" />} busy={busyId === r.id} onConfirm={() => onDelete(r)}>Delete</ConfirmButton>}
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
