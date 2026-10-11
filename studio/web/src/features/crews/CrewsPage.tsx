import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { Briefcase, Play, Plus, Search, Workflow } from 'lucide-react'
import type { Crew } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { AreaFilterSelect, useAreaFilter } from '../../components/AreaFilter'
import { Button, LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { filterByArea } from '../../lib/areaFilter'
import { useAuth } from '../../lib/auth'
import { byId, sortByName } from '../../lib/catalog'
import { dateTime, usd } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { loadVisibleCatalog } from '../../lib/visibleCatalog'
import { PMO_PERMISSIONS } from '../pmo/pmoForm'
import { RunCrewModal } from './RunCrewModal'

export function CrewsPage() {
  const { can } = useAuth()
  const res = useAsync((signal) => loadVisibleCatalog(can, signal), [])
  const [running, setRunning] = useState<Crew | null>(null)
  const [query, setQuery] = useState('')
  const [areaId, setAreaId] = useAreaFilter()
  const canCreate = can('crews.create')
  const canRun = can('crews.run')
  const crews = useMemo(() => {
    const q = query.trim().toLowerCase()
    const all = filterByArea(sortByName(res.data?.crews ?? []), areaId)
    return q ? all.filter((c) => `${c.name} ${c.description}`.toLowerCase().includes(q)) : all
  }, [res.data, query, areaId])

  return (
    <>
      <PageHeader
        eyebrow="Build"
        title="Crews"
        description="A crew runs tasks with a team of agents, sequentially or under a manager."
        actions={
          <>
            {can('history.view') && <HistoryLink type="crew" />}
            {PMO_PERMISSIONS.every((p) => can(p)) && <LinkButton to="/pmo/setup" icon={<Briefcase className="size-4" />}>Set up a PMO</LinkButton>}
            {canCreate && <LinkButton to="/crews/new" variant="primary" icon={<Plus className="size-4" />}>New crew</LinkButton>}
          </>
        }
      />
      <AsyncView {...res}>
        {(cat) => {
          if (cat.crews.length === 0) {
            return (
              <EmptyState
                icon={<Workflow className="size-6" />}
                title="No crews yet"
                hint={canCreate ? 'Build a crew from your agents and tasks, then run it.' : 'No crews in your functional areas yet.'}
                action={canCreate ? <LinkButton to="/crews/new" variant="primary">Build a crew</LinkButton> : undefined}
              />
            )
          }
          const tasks = byId(cat.tasks)
          return (
            <>
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <div className="flex w-full max-w-sm items-center gap-2 rounded-md border border-line bg-raised px-2.5">
                  <Search className="size-3.5 text-faint" />
                  <input className="w-full bg-transparent py-1.5 outline-none" placeholder="Filter crews…" value={query} onChange={(e) => setQuery(e.target.value)} />
                </div>
                <AreaFilterSelect value={areaId} onChange={setAreaId} />
              </div>
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[920px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Crew</th><th>Areas</th><th>Process</th><th>Pipeline</th><th>Inputs</th><th className="text-right">Budget</th><th>Updated</th>{canRun && <th />}
                    </tr>
                  </thead>
                  <tbody>
                    {crews.map((c) => (
                      <tr key={c.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                        <td className="max-w-xs">
                          <Link to={`/crews/${c.id}`} className="font-medium hover:text-accent">{c.name}</Link>
                          {c.description && <div className="truncate text-[12px] text-muted">{c.description}</div>}
                        </td>
                        <td><AreaChips areaIds={c.area_ids ?? []} max={2} /></td>
                        <td><Chip tone={c.process === 'hierarchical' ? 'violet' : 'blue'}>{c.process}</Chip></td>
                        <td className="max-w-sm text-[12px] text-muted">
                          <span className="text-faint">{c.agent_ids.length} agents · </span>
                          {c.task_ids.map((id) => tasks.get(id)?.name ?? '?').join(' → ') || <span className="text-faint">no tasks</span>}
                        </td>
                        <td><div className="flex flex-wrap gap-1">{c.inputs.map((i) => <Chip key={i.key} tone="accent">{i.key}</Chip>)}{c.inputs.length === 0 && <span className="text-faint">—</span>}</div></td>
                        <td className="text-right font-mono tabular">{usd(c.budget_usd)}</td>
                        <td className="whitespace-nowrap text-[12px] text-muted">{dateTime(c.updated_at)}</td>
                        {canRun && <td className="text-right"><Button size="sm" icon={<Play className="size-3.5" />} onClick={() => setRunning(c)}>Run</Button></td>}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {crews.length === 0 && <div className="py-6 text-muted">{query ? <>No crews match “{query}”{areaId && ' in this area'}.</> : 'No crews in this area.'}</div>}
            </>
          )
        }}
      </AsyncView>
      {running && canRun && <RunCrewModal crew={running} onClose={() => setRunning(null)} />}
    </>
  )
}
