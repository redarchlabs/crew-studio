import { useMemo } from 'react'
import { Network } from 'lucide-react'
import type { Interactions } from '../../../api/types'
import { FlowCanvas } from '../../../components/graph/FlowCanvas'
import { Legend } from '../../../components/graph/Legend'
import { EmptyState, ErrorState, Loading } from '../../../components/States'
import { INTERACTION_LOOKS, focusForNode, interactionsToFlow } from '../interactionsFlow'
import type { TimelineFocus } from '../timeline/timelineReducer'

type InteractionsTabProps = {
  data: Interactions | undefined
  error: unknown
  loading: boolean
  reload: () => void
  focus: TimelineFocus | null
  onFocus: (focus: TimelineFocus | null) => void
}

const LEGEND = (['runs', 'assigned', 'context', 'tool_call', 'llm_call', 'delegation'] as const).map((k) => ({ key: k, label: k.replace('_', ' '), look: INTERACTION_LOOKS[k] }))

export function InteractionsTab({ data, error, loading, reload, focus, onFocus }: InteractionsTabProps) {
  // Layout depends only on graph data; focus restyles the same positions.
  const flow = useMemo(() => (data ? interactionsToFlow(data, focus) : null), [data, focus])
  if (error && !data) return <ErrorState error={error} onRetry={reload} title="Could not load interactions" />
  if (!flow) return loading ? <Loading label="Loading interaction graph…" /> : null
  if (flow.nodes.length === 0) return <EmptyState icon={<Network className="size-6" />} title="No interactions yet" hint="The graph fills in as agents start working." />
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2 text-[12px] text-muted">
        <span>Click a node to filter the timeline to it. Delegations between agents are drawn in red.</span>
        <Legend items={LEGEND} />
      </div>
      <FlowCanvas nodes={flow.nodes} edges={flow.edges} minimap className="h-[560px]" onNodeClick={(_, n) => onFocus(focusForNode(n.id))} onPaneClick={() => focus && onFocus(null)} />
    </div>
  )
}
