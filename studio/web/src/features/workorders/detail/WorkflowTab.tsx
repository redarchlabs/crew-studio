import { Fragment, useMemo, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router'
import { GitBranch, Network, Rows3 } from 'lucide-react'
import type { Interactions, Journal, JournalEntry, Run, RunStatus, WorkOrder } from '../../../api/types'
import { Legend } from '../../../components/graph/Legend'
import { Markdown } from '../../../components/Markdown'
import { EmptyState, ErrorState, Loading } from '../../../components/States'
import { truncate, usd } from '../../../lib/format'
import type { AsyncState } from '../../../lib/useAsync'
import { WoBadge } from '../workOrderLabels'
import { EntryDetails, EntryTitle, NodeDetails, PanelShell } from './DetailPanel'
import { SequenceDiagram } from './SequenceDiagram'
import { WorkflowCanvas } from './WorkflowCanvas'
import { buildWorkflow, WO_NODE_ID, WORKFLOW_LEGEND, WORKFLOW_LOOKS } from './workflowGraph'

type View = 'graph' | 'sequence'
type Selection = { type: 'node'; id: string } | { type: 'entry'; entry: JournalEntry } | null

type WorkflowTabProps = {
  workOrder: WorkOrder
  run: Run | null
  status: RunStatus | null
  live: boolean
  interactions: AsyncState<Interactions | undefined>
  journal: AsyncState<Journal | undefined>
  canViewRuns: boolean
}

const LEGEND = WORKFLOW_LEGEND.map((l) => ({ key: l.key, label: l.label, look: WORKFLOW_LOOKS[l.key] }))

const STATUS_LEGEND = [
  { label: 'pending', cls: 'bg-faint' },
  { label: 'running', cls: 'bg-accent pulse-dot' },
  { label: 'done', cls: 'bg-ok' },
  { label: 'failed', cls: 'bg-danger' },
]

/** Interaction diagram for the selected attempt: a layered graph, or a swimlane sequence of the journal. */
export function WorkflowTab({ workOrder, run, status, live, interactions, journal, canViewRuns }: WorkflowTabProps) {
  const [params, setParams] = useSearchParams()
  const view: View = params.get('view') === 'sequence' || !canViewRuns ? 'sequence' : 'graph'
  const setView = (v: View) => setParams((p) => { const next = new URLSearchParams(p); next.set('view', v); return next }, { replace: true })
  const [selection, setSelection] = useState<Selection>(null)
  const selectedNodeId = selection?.type === 'node' ? selection.id : null

  const taskOrder = useMemo(() => journal.data?.tasks.map((t) => t.name) ?? run?.spec?.tasks.map((t) => t.name) ?? [], [journal.data, run])
  const flow = useMemo(
    () => (interactions.data ? buildWorkflow({ data: interactions.data, workOrder, runStatus: status, taskOrder, live, selectedId: selectedNodeId }) : null),
    [interactions.data, workOrder, status, taskOrder, live, selectedNodeId],
  )

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <div role="tablist" aria-label="Diagram view" className="inline-flex rounded-md border border-line bg-raised p-0.5">
          {canViewRuns && <ViewButton on={view === 'graph'} onClick={() => setView('graph')} icon={<Network className="size-3.5" />}>Graph</ViewButton>}
          <ViewButton on={view === 'sequence'} onClick={() => setView('sequence')} icon={<Rows3 className="size-3.5" />}>Sequence</ViewButton>
        </div>
        {view === 'graph' && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <Legend items={LEGEND} />
            <span className="flex items-center gap-2 text-[11px] text-faint">
              {STATUS_LEGEND.map((s) => <span key={s.label} className="inline-flex items-center gap-1"><span className={`size-2 rounded-full ${s.cls}`} />{s.label}</span>)}
            </span>
          </div>
        )}
      </div>
      <div className={selection ? 'grid gap-3 lg:grid-cols-[minmax(0,1fr)_420px]' : ''}>
        <div className="min-w-0">
          {view === 'graph' ? (
            <GraphView flow={flow} state={interactions} onSelect={(id) => setSelection(id ? { type: 'node', id } : null)} />
          ) : (
            <SequenceView state={journal} live={live} selectedSeq={selection?.type === 'entry' ? selection.entry.seq : null} onSelect={(entry) => setSelection({ type: 'entry', entry })} />
          )}
        </div>
        {selection && <SelectionPanel selection={selection} workOrder={workOrder} run={run} interactions={interactions.data} journal={journal.data} onClose={() => setSelection(null)} />}
      </div>
    </div>
  )
}

function ViewButton({ on, onClick, icon, children }: { on: boolean; onClick: () => void; icon: ReactNode; children: ReactNode }) {
  return (
    <button type="button" role="tab" aria-selected={on} onClick={onClick} className={`inline-flex items-center gap-1.5 rounded px-2.5 py-0.5 text-[12px] ${on ? 'bg-panel-2 text-fg' : 'text-muted hover:text-fg'}`}>
      {icon}{children}
    </button>
  )
}

function GraphView({ flow, state, onSelect }: { flow: ReturnType<typeof buildWorkflow> | null; state: AsyncState<Interactions | undefined>; onSelect: (id: string | null) => void }) {
  if (state.error && !state.data) return <ErrorState error={state.error} onRetry={state.reload} title="Could not load the interaction graph" />
  if (!flow) return state.loading ? <Loading label="Loading interaction graph…" /> : null
  if (flow.nodes.length <= 1) return <EmptyState icon={<GitBranch className="size-6" />} title="No interactions yet" hint="The diagram fills in as agents start working." />
  return <WorkflowCanvas nodes={flow.nodes} edges={flow.edges} onSelect={onSelect} />
}

function SequenceView({ state, live, selectedSeq, onSelect }: { state: AsyncState<Journal | undefined>; live: boolean; selectedSeq: number | null; onSelect: (e: JournalEntry) => void }) {
  if (state.error && !state.data) return <ErrorState error={state.error} onRetry={state.reload} title="Could not load the journal" />
  if (!state.data) return state.loading ? <Loading label="Loading journal…" /> : null
  if (state.data.entries.length === 0) return <EmptyState icon={<Rows3 className="size-6" />} title="Nothing has happened yet" hint={live ? 'Entries appear as agents start working.' : undefined} />
  return <SequenceDiagram journal={state.data} selectedSeq={selectedSeq} onSelect={onSelect} />
}

type SelectionPanelProps = {
  selection: NonNullable<Selection>
  workOrder: WorkOrder
  run: Run | null
  interactions: Interactions | undefined
  journal: Journal | undefined
  onClose: () => void
}

function SelectionPanel({ selection, workOrder, run, interactions, journal, onClose }: SelectionPanelProps) {
  if (selection.type === 'entry') {
    return <PanelShell title={<EntryTitle entry={selection.entry} />} onClose={onClose}><EntryDetails entry={selection.entry} /></PanelShell>
  }
  if (selection.id === WO_NODE_ID) {
    const inputs = Object.entries(run?.inputs ?? workOrder.inputs)
    return (
      <PanelShell title={<span className="flex items-center gap-2"><WoBadge number={workOrder.number} /><span className="truncate font-medium">{workOrder.title}</span></span>} onClose={onClose}>
        <div className="flex flex-col gap-3 text-[12.5px]">
          <div className="max-h-72 overflow-auto rounded-md border border-line bg-raised px-3 py-2">
            {workOrder.brief.trim() ? <Markdown>{truncate(workOrder.brief, 4000)}</Markdown> : <span className="text-faint">No brief.</span>}
          </div>
          <div className="text-muted">{workOrder.attachments?.length ?? 0} input files · budget {usd(run?.budget_usd ?? workOrder.budget_usd)}</div>
          {inputs.length > 0 && (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1">
              {inputs.map(([k, v]) => <Fragment key={k}><dt className="font-mono text-accent">{k}</dt><dd className="truncate" title={v}>{v || '""'}</dd></Fragment>)}
            </dl>
          )}
        </div>
      </PanelShell>
    )
  }
  const node = interactions?.nodes.find((n) => n.id === selection.id)
  if (!node) return null
  return (
    <PanelShell title={<span className="flex min-w-0 items-center gap-2"><span className="eyebrow">{node.kind}</span><span className="truncate font-medium">{node.label}</span></span>} onClose={onClose}>
      <NodeDetails node={node} entries={journal?.entries ?? []} />
    </PanelShell>
  )
}
