import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Download, FileText } from 'lucide-react'
import type { Run, TaskOutput } from '../../../api/types'
import { Button } from '../../../components/Button'
import { Markdown } from '../../../components/Markdown'
import { EmptyState } from '../../../components/States'
import { CopyButton } from '../../../components/misc'
import { downloadText, safeFilename } from '../../../lib/download'
import { shortId, usd } from '../../../lib/format'
import { num, str } from '../timeline/payload'
import type { TimelineState } from '../timeline/timelineReducer'

function OutputCard({ title, meta, text, filename }: { title: string; meta?: ReactNode; text: string; filename: string }) {
  return (
    <section className="rounded-lg border border-line bg-panel">
      <header className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-2">
        <FileText className="size-4 text-faint" />
        <span className="font-medium">{title}</span>
        {meta && <span className="flex flex-wrap items-center gap-x-2 text-[12px] text-faint">{meta}</span>}
        <span className="ml-auto flex gap-1">
          <CopyButton text={text} />
          <Button size="sm" variant="ghost" icon={<Download className="size-3.5" />} onClick={() => downloadText(`${safeFilename(filename)}.md`, text)}>.md</Button>
        </span>
      </header>
      <div className="max-h-[640px] overflow-auto px-4 py-3">
        <Markdown>{text}</Markdown>
      </div>
    </section>
  )
}

/**
 * The run's stored task outputs plus, while it is running, tasks that finished since (task_completed events say
 * where each task sits). A resumed run already has the outputs it reused, so live ones are added by position.
 */
function taskOutputs(run: Run, timeline: TimelineState): TaskOutput[] {
  const completed = timeline.groups.flatMap((g) => g.items).filter((i) => i.event.type === 'task_completed')
  const fromEvent = (payload: Record<string, unknown>, position: number): TaskOutput => ({
    position, task_name: str(payload, 'task_name') ?? 'task', agent_role: str(payload, 'agent_role') ?? '', raw: str(payload, 'output') ?? '',
  })
  const byPosition = new Map<number, TaskOutput>((run.task_outputs ?? []).map((o) => [o.position, o]))
  for (const { event } of completed) {
    const position = num(event.payload, 'position')
    // Events without a position are older runners' (or another crew's, such as compaction's): see below
    if (position !== null && !byPosition.has(position)) byPosition.set(position, fromEvent(event.payload, position))
  }
  if (byPosition.size === 0) return completed.map((i, idx) => fromEvent(i.event.payload, idx))
  return [...byPosition.values()].sort((a, b) => a.position - b.position)
}

/** Who wrote a task's output, what it cost, and (for a resumed run) which run it was reused from. */
function taskMeta(o: TaskOutput): ReactNode {
  return (
    <>
      {o.agent_role && <span>{o.agent_role}</span>}
      {o.reused_from_run_id ? (
        <span className="rounded border border-line px-1.5 text-muted">
          {o.reused_from_unavailable ? (
            'reused from an earlier run that is no longer available'
          ) : (
            <>reused from run <Link className="font-mono text-accent hover:underline" to={`/runs/${o.reused_from_run_id}`}>{shortId(o.reused_from_run_id)}</Link></>
          )}
          {' · '}$0 here{o.cost_usd !== undefined && `, ${usd(o.cost_usd)} when it ran`}
        </span>
      ) : (
        o.cost_usd !== undefined && <span>{usd(o.cost_usd)}</span>
      )}
    </>
  )
}

export function OutputsTab({ run, timeline }: { run: Run; timeline: TimelineState }) {
  const outputs = taskOutputs(run, timeline)
  if (!run.output && outputs.length === 0) {
    return <EmptyState icon={<FileText className="size-6" />} title="No outputs yet" hint={run.error ? `The run ended with an error: ${run.error}` : 'Outputs appear as tasks complete.'} />
  }
  return (
    <div className="flex flex-col gap-4">
      {run.output && <OutputCard title="Final output" meta={run.crew_name} text={run.output} filename={`${run.crew_name}-${run.id.slice(0, 8)}`} />}
      {outputs.map((o) => (
        <OutputCard key={`${o.position}-${o.task_name}`} title={`${o.position + 1}. ${o.task_name}`} meta={taskMeta(o)} text={o.raw} filename={`${run.crew_name}-${o.task_name}`} />
      ))}
    </div>
  )
}
