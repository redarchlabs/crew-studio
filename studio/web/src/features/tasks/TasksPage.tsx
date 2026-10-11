import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { ListChecks, Plus, Search } from 'lucide-react'
import { AreaChips } from '../../components/AreaChips'
import { AreaFilterSelect, useAreaFilter } from '../../components/AreaFilter'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { filterByArea } from '../../lib/areaFilter'
import { useAuth } from '../../lib/auth'
import { byId, sortByName } from '../../lib/catalog'
import { truncate } from '../../lib/format'
import { findPlaceholders } from '../../lib/placeholders'
import { crewsUsingTask } from '../../lib/usage'
import { useAsync } from '../../lib/useAsync'
import { loadVisibleCatalog } from '../../lib/visibleCatalog'

export function TasksPage() {
  const { can } = useAuth()
  const res = useAsync((signal) => loadVisibleCatalog(can, signal), [])
  const [query, setQuery] = useState('')
  const [areaId, setAreaId] = useAreaFilter()
  const canCreate = can('tasks.create')
  const tasks = useMemo(() => {
    const q = query.trim().toLowerCase()
    const all = filterByArea(sortByName(res.data?.tasks ?? []), areaId)
    return q ? all.filter((t) => `${t.name} ${t.description}`.toLowerCase().includes(q)) : all
  }, [res.data, query, areaId])

  return (
    <>
      <PageHeader
        eyebrow="Build"
        title="Tasks"
        description="What gets done: a description, the expected output, the agent responsible and the tasks whose output it reads."
        actions={
          <>
            {can('history.view') && <HistoryLink type="task" />}
            {canCreate && <LinkButton to="/tasks/new" variant="primary" icon={<Plus className="size-4" />}>New task</LinkButton>}
          </>
        }
      />
      <AsyncView {...res}>
        {(cat) => {
          if (cat.tasks.length === 0) {
            return (
              <EmptyState
                icon={<ListChecks className="size-6" />}
                title="No tasks yet"
                hint={canCreate ? 'Tasks describe the work; crews run them in order.' : 'No tasks in your functional areas yet.'}
                action={canCreate ? <LinkButton to="/tasks/new" variant="primary">Create a task</LinkButton> : undefined}
              />
            )
          }
          const agents = byId(cat.agents)
          const taskNames = byId(cat.tasks)
          return (
            <>
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <div className="flex w-full max-w-sm items-center gap-2 rounded-md border border-line bg-raised px-2.5">
                  <Search className="size-3.5 text-faint" />
                  <input className="w-full bg-transparent py-1.5 outline-none" placeholder="Filter tasks…" value={query} onChange={(e) => setQuery(e.target.value)} />
                </div>
                <AreaFilterSelect value={areaId} onChange={setAreaId} />
              </div>
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[920px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Task</th><th>Areas</th><th>Agent</th><th>Context</th><th>Inputs</th><th>Crews</th>
                    </tr>
                  </thead>
                  <tbody>
                    {tasks.map((t) => {
                      const agent = t.agent_id ? agents.get(t.agent_id) : undefined
                      const crews = crewsUsingTask(t.id, cat.crews)
                      const keys = findPlaceholders(`${t.description}\n${t.expected_output}`)
                      return (
                        <tr key={t.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                          <td className="max-w-md">
                            <Link to={`/tasks/${t.id}`} className="font-medium hover:text-accent">{t.name}</Link>
                            <div className="text-[12px] text-muted">{truncate(t.description, 140) || <span className="text-faint">No description</span>}</div>
                          </td>
                          <td><AreaChips areaIds={t.area_ids ?? []} max={2} /></td>
                          <td className="whitespace-nowrap">{agent ? <Link className="hover:text-accent" to={`/agents/${agent.id}`}>{agent.name}</Link> : t.agent_id ? <span className="text-faint">Not visible</span> : <span className="text-warn">Unassigned</span>}</td>
                          <td className="text-[12px] text-muted">{t.context_task_ids.map((id) => taskNames.get(id)?.name ?? 'missing').join(', ') || <span className="text-faint">—</span>}</td>
                          <td><div className="flex flex-wrap gap-1">{keys.map((k) => <Chip key={k} tone="accent">{k}</Chip>)}{keys.length === 0 && <span className="text-faint">—</span>}</div></td>
                          <td className="text-[12px] text-muted">{crews.map((c) => c.name).join(', ') || <span className="text-faint">—</span>}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
              {tasks.length === 0 && <div className="py-6 text-muted">{query ? <>No tasks match “{query}”{areaId && ' in this area'}.</> : 'No tasks in this area.'}</div>}
            </>
          )
        }}
      </AsyncView>
    </>
  )
}
