import type { Journal, JournalTask } from '../../../../api/types'
import { between, compact, durationMs, timeOfDay, usd } from '../../../../lib/format'
import { AgentAvatar } from './journalKinds'

const TASK_DOT: Record<JournalTask['status'], string> = {
  pending: 'bg-faint',
  running: 'bg-accent pulse-dot',
  done: 'bg-ok',
  failed: 'bg-danger',
}

type JournalRailProps = {
  journal: Journal
  agent: string | null
  task: string | null
  onAgent: (role: string | null) => void
  onTask: (task: string | null) => void
  now: number
}

/** Agents and tasks of the run, acting as filters. Horizontal scroller on phones, a column on wide screens. */
export function JournalRail({ journal, agent, task, onAgent, onTask, now }: JournalRailProps) {
  const item = (on: boolean) =>
    `flex min-w-[200px] shrink-0 items-start gap-2 rounded-md border px-2.5 py-1.5 text-left transition-colors lg:w-full lg:min-w-0 ${on ? 'border-accent/60 bg-accent-soft' : 'border-line bg-panel hover:border-line-strong'}`
  return (
    <div className="flex min-w-0 flex-col gap-3 lg:sticky lg:top-4 lg:self-start">
      <section className="min-w-0">
        <div className="eyebrow mb-1.5 flex items-center justify-between">
          Agents {agent && <button type="button" className="normal-case tracking-normal text-accent" onClick={() => onAgent(null)}>clear</button>}
        </div>
        <ul className="flex gap-1.5 overflow-x-auto pb-1 lg:flex-col lg:overflow-visible lg:pb-0">
          {journal.agents.length === 0 && <li className="text-[12px] text-faint">No agents have acted yet.</li>}
          {journal.agents.map((a) => (
            <li key={a.role}>
              <button type="button" aria-pressed={agent === a.role} className={item(agent === a.role)} onClick={() => onAgent(agent === a.role ? null : a.role)}>
                <AgentAvatar role={a.role} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[12.5px] font-medium">{a.role}</span>
                  <span className="block font-mono text-[10.5px] text-faint">
                    {a.llm_calls} llm · {a.tool_calls} tool · {compact(a.prompt_tokens + a.completion_tokens)} tok · {usd(a.cost_usd)}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      </section>
      <section className="min-w-0">
        <div className="eyebrow mb-1.5 flex items-center justify-between">
          Tasks {task !== null && <button type="button" className="normal-case tracking-normal text-accent" onClick={() => onTask(null)}>clear</button>}
        </div>
        <ul className="flex gap-1.5 overflow-x-auto pb-1 lg:flex-col lg:overflow-visible lg:pb-0">
          {journal.tasks.map((t, i) => (
            <li key={t.name}>
              <button type="button" aria-pressed={task === t.name} className={item(task === t.name)} onClick={() => onTask(task === t.name ? null : t.name)}>
                <span className={`mt-1.5 size-2 shrink-0 rounded-full ${TASK_DOT[t.status]}`} title={t.status} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[12.5px] font-medium">{i + 1}. {t.name}</span>
                  <span className="block truncate text-[11px] text-muted">{t.agent_role}</span>
                  <span className="block font-mono text-[10.5px] text-faint">
                    {t.started_at ? `${timeOfDay(t.started_at)} · ${durationMs(between(t.started_at, t.finished_at, now))}` : t.status}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
