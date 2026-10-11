import { Download, FileText } from 'lucide-react'
import type { Run, TaskOutput } from '../../../api/types'
import { Button } from '../../../components/Button'
import { Markdown } from '../../../components/Markdown'
import { EmptyState } from '../../../components/States'
import { CopyButton } from '../../../components/misc'
import { downloadText, safeFilename } from '../../../lib/download'
import { str } from '../timeline/payload'
import type { TimelineState } from '../timeline/timelineReducer'

function OutputCard({ title, meta, text, filename }: { title: string; meta?: string; text: string; filename: string }) {
  return (
    <section className="rounded-lg border border-line bg-panel">
      <header className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-2">
        <FileText className="size-4 text-faint" />
        <span className="font-medium">{title}</span>
        {meta && <span className="text-[12px] text-faint">{meta}</span>}
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

/** Task outputs from the run, or (while it is running) from task_completed events. */
function taskOutputs(run: Run, timeline: TimelineState): TaskOutput[] {
  if (run.task_outputs && run.task_outputs.length > 0) return [...run.task_outputs].sort((a, b) => a.position - b.position)
  return timeline.groups
    .flatMap((g) => g.items)
    .filter((i) => i.event.type === 'task_completed')
    .map((i, idx) => ({ position: idx, task_name: str(i.event.payload, 'task_name') ?? 'task', agent_role: str(i.event.payload, 'agent_role') ?? '', raw: str(i.event.payload, 'output') ?? '' }))
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
        <OutputCard key={`${o.position}-${o.task_name}`} title={`${o.position + 1}. ${o.task_name}`} meta={o.agent_role} text={o.raw} filename={`${run.crew_name}-${o.task_name}`} />
      ))}
    </div>
  )
}
