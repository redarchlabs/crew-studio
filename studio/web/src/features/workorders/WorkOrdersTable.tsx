import { Link } from 'react-router'
import type { WorkOrder } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { dateTime, relative, usd } from '../../lib/format'
import { CrewLabel, SourceChip, WoBadge, WorkOrderStatusBadge } from './workOrderLabels'

type WorkOrdersTableProps = {
  items: readonly WorkOrder[]
  now: number
  /** Fewer columns, for "recent work orders" panels */
  compact?: boolean
}

/** Table on wide screens, stacked cards on phones. */
export function WorkOrdersTable({ items, now, compact = false }: WorkOrdersTableProps) {
  return (
    <>
      <div className={`overflow-x-auto rounded-lg border border-line bg-panel ${compact ? '' : 'hidden md:block'}`}>
        <table className={`w-full text-left ${compact ? 'min-w-[520px]' : 'min-w-[980px]'}`}>
          <thead className="border-b border-line">
            <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
              <th>Work order</th>
              {!compact && <th>Crew</th>}
              <th>Status</th>
              {!compact && <th>Source</th>}
              {!compact && <th>Areas</th>}
              {!compact && <th>Created by</th>}
              <th className="text-right">Cost</th>
              <th>Updated</th>
            </tr>
          </thead>
          <tbody>
            {items.map((wo) => (
              <tr key={wo.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                <td className="max-w-sm">
                  <Link to={`/work-orders/${wo.id}`} className="group flex min-w-0 items-center gap-2">
                    <WoBadge number={wo.number} />
                    <span className="truncate font-medium group-hover:text-accent">{wo.title}</span>
                  </Link>
                  {wo.run_count > 1 && <div className="mt-0.5 text-[11px] text-faint">{wo.run_count} attempts</div>}
                </td>
                {!compact && <td className="max-w-[220px] text-[12.5px]"><CrewLabel name={wo.target_name} crewId={wo.crew_id} link /></td>}
                <td><WorkOrderStatusBadge status={wo.status} /></td>
                {!compact && <td><SourceChip source={wo.source} /></td>}
                {!compact && <td><AreaChips areaIds={wo.area_ids} max={2} /></td>}
                {!compact && <td className="whitespace-nowrap text-[12px] text-muted">{wo.created_by_name || '—'}</td>}
                <td className="text-right font-mono tabular">{usd(wo.cost_usd)}</td>
                <td className="whitespace-nowrap text-[12px] text-muted" title={dateTime(wo.updated_at)}>{relative(wo.updated_at, now)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {!compact && (
        <ul className="flex flex-col gap-2 md:hidden">
          {items.map((wo) => (
            <li key={wo.id}>
              <Link to={`/work-orders/${wo.id}`} className="block rounded-lg border border-line bg-panel p-3 hover:border-line-strong">
                <div className="flex items-center gap-2">
                  <WoBadge number={wo.number} />
                  <WorkOrderStatusBadge status={wo.status} />
                  <span className="ml-auto font-mono text-[12px] tabular">{usd(wo.cost_usd)}</span>
                </div>
                <div className="mt-1.5 font-medium">{wo.title}</div>
                <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px] text-muted">
                  <CrewLabel name={wo.target_name} crewId={wo.crew_id} />
                  <SourceChip source={wo.source} />
                  <AreaChips areaIds={wo.area_ids} max={2} />
                </div>
                <div className="mt-1 text-[11.5px] text-faint">{wo.created_by_name || 'Unknown'} · updated {relative(wo.updated_at, now)}</div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}
