import { Link } from 'react-router'
import { Bot, Cpu, Users } from 'lucide-react'
import type { Agent, Crew, LLM, Task, Tool } from '../../api/types'
import { AreaChips } from '../../components/AreaChips'
import { Chip } from '../../components/StatusBadge'
import { useAuth } from '../../lib/auth'

type AgentCardProps = { agent: Agent; llm: LLM | undefined; tools: ReadonlyMap<string, Tool>; crews: Crew[]; tasks: Task[] }

export function AgentCard({ agent, llm, tools, crews, tasks }: AgentCardProps) {
  const { can } = useAuth()
  const llmLabel = llm ? llm.name : !agent.llm_id ? 'CrewAI default' : can('llms.view') ? 'Missing LLM' : 'LLM assigned'
  return (
    <Link to={`/agents/${agent.id}`} className="group flex min-w-0 flex-col gap-2.5 rounded-lg border border-line bg-panel p-4 transition-colors hover:border-accent/60">
      <div className="flex items-start gap-2.5">
        <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-teal-soft text-teal">
          <Bot className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate font-semibold group-hover:text-accent">{agent.name}</div>
          <div className="truncate text-[12px] text-muted">{agent.role || <span className="text-faint">No role</span>}</div>
        </div>
        {agent.allow_delegation && <Chip tone="violet" title="Can delegate to other agents">delegates</Chip>}
      </div>
      <p className="line-clamp-2 min-h-[2.6em] text-[12.5px] text-muted">{agent.goal || <span className="text-faint">No goal set</span>}</p>
      <div className="flex items-center gap-1.5 text-[12px] text-muted">
        <Cpu className="size-3.5 text-faint" />
        <span className="truncate font-mono text-[11.5px]">{llmLabel}</span>
      </div>
      <div className="flex min-h-5 flex-wrap gap-1">
        {agent.tools.length === 0 && <span className="text-[11.5px] text-faint">No tools</span>}
        {agent.tools.map((key) => {
          const tool = tools.get(key)
          const tone = !tool || !tool.available ? 'warn' : 'blue'
          return (
            <Chip key={key} tone={tone} title={tool ? (tool.available ? tool.description : `Unavailable: missing ${tool.requires_env.join(', ')}`) : 'Unknown tool'}>
              {tool?.name ?? key}
            </Chip>
          )
        })}
      </div>
      <AreaChips areaIds={agent.area_ids ?? []} />
      <div className="mt-auto flex items-center gap-1.5 border-t border-line pt-2 text-[11.5px] text-faint">
        <Users className="size-3.5" />
        <span className="truncate">
          {crews.length === 0 && tasks.length === 0
            ? 'Not used yet'
            : `${crews.length} crew${crews.length === 1 ? '' : 's'} · ${tasks.length} task${tasks.length === 1 ? '' : 's'}`}
          {crews.length > 0 && ` — ${crews.map((c) => c.name).join(', ')}`}
        </span>
      </div>
    </Link>
  )
}
