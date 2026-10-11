import { useMemo, useState } from 'react'
import { Bot, Plus, Search } from 'lucide-react'
import { AreaFilterSelect, useAreaFilter } from '../../components/AreaFilter'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { HistoryLink } from '../../components/misc'
import { filterByArea } from '../../lib/areaFilter'
import { useAuth } from '../../lib/auth'
import { byId, sortByName } from '../../lib/catalog'
import { crewsUsingAgent, tasksUsingAgent } from '../../lib/usage'
import { useAsync } from '../../lib/useAsync'
import { loadVisibleCatalog } from '../../lib/visibleCatalog'
import { AgentCard } from './AgentCard'

export function AgentsPage() {
  const { can } = useAuth()
  const res = useAsync((signal) => loadVisibleCatalog(can, signal), [])
  const [query, setQuery] = useState('')
  const [areaId, setAreaId] = useAreaFilter()
  const canCreate = can('agents.create')
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const agents = filterByArea(sortByName(res.data?.agents ?? []), areaId)
    return q ? agents.filter((a) => `${a.name} ${a.role} ${a.goal}`.toLowerCase().includes(q)) : agents
  }, [res.data, query, areaId])

  return (
    <>
      <PageHeader
        eyebrow="Build"
        title="Agents"
        description="Who does the work: a role, a goal, a backstory, an LLM and tools."
        actions={
          <>
            {can('history.view') && <HistoryLink type="agent" />}
            {canCreate && <LinkButton to="/agents/new" variant="primary" icon={<Plus className="size-4" />}>New agent</LinkButton>}
          </>
        }
      />
      <AsyncView {...res}>
        {(cat) => {
          if (cat.agents.length === 0) {
            return (
              <EmptyState
                icon={<Bot className="size-6" />}
                title="No agents yet"
                hint={canCreate ? 'Create an agent, then assign it to tasks and crews.' : 'No agents in your functional areas yet.'}
                action={canCreate ? <LinkButton to="/agents/new" variant="primary">Create an agent</LinkButton> : undefined}
              />
            )
          }
          const llms = byId(cat.llms)
          const tools = new Map(cat.tools.map((t) => [t.key, t]))
          return (
            <>
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <div className="flex w-full max-w-sm items-center gap-2 rounded-md border border-line bg-raised px-2.5">
                  <Search className="size-3.5 text-faint" />
                  <input className="w-full bg-transparent py-1.5 outline-none" placeholder="Filter agents…" value={query} onChange={(e) => setQuery(e.target.value)} />
                </div>
                <AreaFilterSelect value={areaId} onChange={setAreaId} />
              </div>
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
                {filtered.map((a) => (
                  <AgentCard key={a.id} agent={a} llm={a.llm_id ? llms.get(a.llm_id) : undefined} tools={tools} crews={crewsUsingAgent(a.id, cat.crews, cat.tasks)} tasks={tasksUsingAgent(a.id, cat.tasks)} />
                ))}
              </div>
              {filtered.length === 0 && <div className="py-6 text-muted">{query ? <>No agents match “{query}”{areaId && ' in this area'}.</> : 'No agents in this area.'}</div>}
            </>
          )
        }}
      </AsyncView>
    </>
  )
}
