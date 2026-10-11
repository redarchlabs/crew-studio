import { Fragment } from 'react'
import { FileText } from 'lucide-react'
import type { Journal, Run, TaskOutput, WorkOrder } from '../../../api/types'
import { Markdown } from '../../../components/Markdown'
import { Panel } from '../../../components/PageHeader'
import { Chip } from '../../../components/StatusBadge'
import { Collapsible, CopyButton } from '../../../components/misc'
import { FileList } from '../files/FileList'
import { parseWatchInputs, WATCH_INPUT_KEYS, WatchInputsSection } from '../../schedules/watch/WatchInputsView'

/** Task outputs from the run, or from the journal while the run is still going. */
function taskOutputs(run: Run | null, journal: Journal | undefined): TaskOutput[] {
  if (run?.task_outputs && run.task_outputs.length > 0) return [...run.task_outputs].sort((a, b) => a.position - b.position)
  return (journal?.tasks ?? []).flatMap((t, position) => (t.output ? [{ position, task_name: t.name, agent_role: t.agent_role, raw: t.output }] : []))
}

type BriefTabProps = {
  workOrder: WorkOrder
  run: Run | null
  journal: Journal | undefined
}

/** The request (brief, files, inputs) and what came back (final and per-task outputs). */
export function BriefTab({ workOrder, run, journal }: BriefTabProps) {
  // The attempt ran with the brief frozen in its spec; show that, and say when it has been edited since
  const frozen = run?.spec?.work_order?.brief
  const brief = frozen ?? workOrder.brief
  const edited = frozen !== undefined && frozen !== workOrder.brief
  const allInputs = run?.inputs ?? workOrder.inputs
  // A watched schedule's changes and follow-ups are shown readably on their own, not as raw JSON inputs
  const watched = parseWatchInputs(allInputs)
  const hasWatch = Boolean(watched.changes || watched.followups || watched.rollups)
  const inputs = Object.entries(allInputs).filter(([k]) => !(hasWatch && (WATCH_INPUT_KEYS as readonly string[]).includes(k)))
  const outputs = taskOutputs(run, journal)
  const files = workOrder.attachments ?? []

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
      <div className="flex min-w-0 flex-col gap-4">
        <Panel title="Brief" actions={edited ? <Chip tone="warn" title="The work order's brief was edited after this attempt ran">edited since this attempt</Chip> : undefined}>
          {brief.trim() ? <Markdown>{brief}</Markdown> : <p className="text-faint">No brief was written.</p>}
        </Panel>
        {hasWatch && (
          <Panel title="What the watch found">
            <WatchInputsSection inputs={allInputs} />
          </Panel>
        )}
        {run?.error && <div className="rounded-md border border-danger/40 bg-danger-soft px-3 py-2 text-[12.5px] text-danger">{run.error}</div>}
        {run?.output && (
          <Panel title="Final output" actions={<CopyButton text={run.output} />}>
            <div className="max-h-[640px] overflow-auto"><Markdown>{run.output}</Markdown></div>
          </Panel>
        )}
        {outputs.length > 0 && (
          <section className="flex flex-col gap-2">
            <div className="eyebrow">Task outputs</div>
            {outputs.map((o, i) => (
              <Collapsible key={`${o.position}-${o.task_name}`} defaultOpen={outputs.length === 1 || (!run?.output && i === outputs.length - 1)} title={<span className="inline-flex items-center gap-2"><FileText className="size-3.5 text-faint" />{o.position + 1}. {o.task_name}</span>} meta={o.agent_role}>
                <div className="mb-2 flex justify-end"><CopyButton text={o.raw} /></div>
                <Markdown>{o.raw}</Markdown>
              </Collapsible>
            ))}
          </section>
        )}
      </div>
      <aside className="flex min-w-0 flex-col gap-4">
        <Panel title={`Input files (${files.length})`} bodyClassName="p-3">
          {files.length === 0 ? <p className="text-[12px] text-faint">No input files.</p> : <FileList files={files} />}
        </Panel>
        <Panel title={run ? 'Inputs (this attempt)' : 'Inputs'}>
          {inputs.length === 0 ? (
            <p className="text-[12px] text-faint">No inputs.</p>
          ) : (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[12.5px]">
              {inputs.map(([k, v]) => (
                <Fragment key={k}>
                  <dt className="font-mono text-accent">{k}</dt>
                  <dd className="whitespace-pre-wrap break-words">{v || <span className="text-faint">""</span>}</dd>
                </Fragment>
              ))}
            </dl>
          )}
        </Panel>
      </aside>
    </div>
  )
}
