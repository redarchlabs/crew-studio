import { useState } from 'react'
import { Link } from 'react-router'
import { Bot, CalendarClock, ClipboardList, Cpu, DollarSign, ListChecks, Play, Workflow, type LucideIcon } from 'lucide-react'
import { crewsApi, overviewApi } from '../../api/endpoints'
import type { Crew, Overview, Permission, WorkOrder } from '../../api/types'
import { Button } from '../../components/Button'
import { SelectField } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView, EmptyState, Loading } from '../../components/States'
import { Chip, StatusBadge } from '../../components/StatusBadge'
import { useAuth } from '../../lib/auth'
import { sortByName } from '../../lib/catalog'
import { int, relative, usd } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { usePollWhile } from '../../lib/usePollWhile'
import { useNow } from '../../lib/useNow'
import { RunCrewForm } from '../crews/RunCrewModal'
import { RunsTable } from '../runs/RunsTable'
import { isTerminal } from '../runs/timeline/payload'

type Tile = { key: keyof Overview['counts']; label: string; to: string; icon: LucideIcon; perm: Permission }

// Each tile links to a list page, so it is shown only to people who can open that page
const TILES: Tile[] = [
  { key: 'work_orders', label: 'Work orders', to: '/work-orders', icon: ClipboardList, perm: 'work_orders.view' },
  { key: 'runs', label: 'Runs', to: '/runs', icon: Play, perm: 'runs.view' },
  { key: 'schedules', label: 'Schedules', to: '/schedules', icon: CalendarClock, perm: 'schedules.view' },
  { key: 'crews', label: 'Crews', to: '/crews', icon: Workflow, perm: 'crews.view' },
  { key: 'agents', label: 'Agents', to: '/agents', icon: Bot, perm: 'agents.view' },
  { key: 'tasks', label: 'Tasks', to: '/tasks', icon: ListChecks, perm: 'tasks.view' },
  { key: 'llms', label: 'LLMs', to: '/llms', icon: Cpu, perm: 'llms.view' },
  { key: 'pricing', label: 'Prices', to: '/pricing', icon: DollarSign, perm: 'pricing.view' },
]

function WorkOrderStatus({ status }: { status: WorkOrder['status'] }) {
  if (status === 'draft') return <Chip>draft</Chip>
  return <StatusBadge status={status} />
}

function RecentWorkOrders({ items }: { items: readonly WorkOrder[] }) {
  if (items.length === 0) return <div className="p-4"><EmptyState title="No work orders yet" hint="Work orders appear here when someone asks a crew to do something." /></div>
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-left">
        <thead className="border-b border-line">
          <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
            <th>Work order</th><th>Status</th><th>Target</th><th className="text-right">Cost</th><th>Updated</th>
          </tr>
        </thead>
        <tbody>
          {items.map((wo) => (
            <tr key={wo.id} className="border-b border-line last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-1.5">
              <td className="max-w-sm">
                <Link to={`/work-orders/${wo.id}`} className="flex min-w-0 items-baseline gap-2 hover:text-accent">
                  <span className="shrink-0 font-mono text-[11.5px] text-faint">WO-{wo.number}</span>
                  <span className="truncate font-medium">{wo.title}</span>
                </Link>
              </td>
              <td><WorkOrderStatus status={wo.status} /></td>
              <td className="max-w-48 truncate text-[12px] text-muted" title={wo.target_name}>{wo.target_name || <span className="text-faint">—</span>}</td>
              <td className="text-right font-mono tabular">{usd(wo.cost_usd)}</td>
              <td className="whitespace-nowrap text-[12px] text-muted">{relative(wo.updated_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function QuickRunModal({ onClose }: { onClose: () => void }) {
  const crews = useAsync((signal) => crewsApi.list(signal), [])
  const [crew, setCrew] = useState<Crew | null>(null)
  return (
    <Modal title="Run a crew" onClose={onClose}>
      {crews.loading && !crews.data ? (
        <Loading />
      ) : (crews.data ?? []).length === 0 ? (
        <EmptyState title="No crews to run" hint={<Link className="text-accent" to="/crews/new">Build your first crew</Link>} />
      ) : (
        <div className="flex flex-col gap-4">
          <SelectField
            label="Crew"
            value={crew?.id ?? ''}
            onChange={(id) => setCrew(crews.data?.find((c) => c.id === id) ?? null)}
            options={[{ value: '', label: 'Choose a crew…' }, ...sortByName(crews.data ?? []).map((c) => ({ value: c.id, label: c.name }))]}
          />
          {crew && <RunCrewForm key={crew.id} crew={crew} onCancel={onClose} />}
        </div>
      )}
    </Modal>
  )
}

function OverviewBody({ data }: { data: Overview }) {
  const { can } = useAuth()
  const recentRuns = data.recent_runs ?? []
  const recentWorkOrders = data.recent_work_orders ?? []
  const anyActive = recentRuns.some((r) => !isTerminal(r.status))
  const now = useNow(anyActive)
  const tiles = TILES.filter((t) => can(t.perm))
  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <div className="col-span-2 rounded-lg border border-accent/40 bg-accent-soft p-4 sm:col-span-3 lg:col-span-1">
          <div className="eyebrow">Spend · 30 days</div>
          <div className="mt-1 font-mono text-[24px] font-semibold tabular text-accent">{usd(data.spend_last_30d_usd)}</div>
          {(data.chat_spend_last_30d_usd ?? 0) > 0 && <div className="text-[11.5px] text-muted">incl. {usd(data.chat_spend_last_30d_usd ?? 0)} in chat</div>}
        </div>
        {tiles.map(({ key, label, to, icon: Icon }) => (
          <Link key={key} to={to} className="group rounded-lg border border-line bg-panel p-4 transition-colors hover:border-line-strong">
            <div className="flex items-center justify-between">
              <span className="eyebrow">{label}</span>
              <Icon className="size-4 text-faint group-hover:text-accent" />
            </div>
            <div className="mt-1 font-mono text-[22px] font-semibold tabular">{int(data.counts?.[key])}</div>
          </Link>
        ))}
      </div>
      {can('work_orders.view') && (
        <Panel title="Recent work orders" actions={<Link to="/work-orders" className="text-[12px] text-muted hover:text-accent">All work orders →</Link>} bodyClassName="p-0">
          <RecentWorkOrders items={recentWorkOrders} />
        </Panel>
      )}
      {can('runs.view') && (
        <Panel title="Recent runs" actions={<Link to="/runs" className="text-[12px] text-muted hover:text-accent">All runs →</Link>} bodyClassName="p-0">
          {recentRuns.length === 0 ? (
            <div className="p-4"><EmptyState title="No runs yet" hint="Run a crew to see it here." /></div>
          ) : (
            <div className="[&>div]:rounded-none [&>div]:border-0">
              <RunsTable runs={recentRuns} now={now} compact />
            </div>
          )}
        </Panel>
      )}
    </div>
  )
}

function anyInProgress(data: Overview | undefined): boolean {
  if (!data) return false
  const runs = (data.recent_runs ?? []).some((r) => !isTerminal(r.status))
  const workOrders = (data.recent_work_orders ?? []).some((w) => w.status === 'queued' || w.status === 'running')
  return runs || workOrders
}

export function OverviewPage() {
  const { can } = useAuth()
  // Quick run picks from the crew list, so it needs crews.view as well as crews.run
  const canQuickRun = can('crews.run') && can('crews.view')
  const res = useAsync((signal) => overviewApi.get(signal), [])
  const [running, setRunning] = useState(false)
  // Keep statuses and spend current while a recent run or work order is still in progress
  usePollWhile(anyInProgress(res.data), res.reload)
  return (
    <>
      <PageHeader
        eyebrow="Console"
        title="Overview"
        description="What is configured, what ran recently and what it cost."
        actions={canQuickRun ? <Button variant="primary" icon={<Play className="size-4" />} onClick={() => setRunning(true)}>Run a crew</Button> : undefined}
      />
      <AsyncView {...res}>{(data) => <OverviewBody data={data} />}</AsyncView>
      {running && canQuickRun && <QuickRunModal onClose={() => setRunning(false)} />}
    </>
  )
}
