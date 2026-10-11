import { useMemo } from 'react'
import { useNavigate } from 'react-router'
import { toast } from 'sonner'
import { FlowCanvas } from '../../components/graph/FlowCanvas'
import { Legend } from '../../components/graph/Legend'
import type { Catalog } from '../../lib/catalog'
import { CREW_LEGEND, crewGraphToFlow, legendLook } from './crewFlow'
import type { CrewGraph } from './crewGraph'

type CrewCanvasProps = { graph: CrewGraph; catalog: Catalog; onCrewClick: () => void }

/** Live picture of the crew; clicking a node opens that entity's editor. */
export function CrewCanvas({ graph, catalog, onCrewClick }: CrewCanvasProps) {
  const navigate = useNavigate()
  const flow = useMemo(() => crewGraphToFlow(graph), [graph])
  const nodeById = useMemo(() => new Map(graph.nodes.map((n) => [n.id, n])), [graph])

  const open = (id: string) => {
    const node = nodeById.get(id)
    if (!node) return
    switch (node.kind) {
      case 'crew':
        return onCrewClick()
      case 'task':
        return node.entityId && navigate(`/tasks/${node.entityId}`)
      case 'agent':
        return node.entityId && navigate(`/agents/${node.entityId}`)
      case 'llm':
        return navigate(node.entityId ? `/llms?edit=${node.entityId}` : '/llms')
      case 'tool': {
        const tool = catalog.tools.find((t) => t.key === node.entityId)
        toast.info(tool?.name ?? node.label, {
          description: tool ? `${tool.description}${tool.available ? '' : ` — unavailable, needs ${tool.requires_env.join(', ')}`}` : 'This tool key is not in the server catalog.',
        })
      }
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <FlowCanvas nodes={flow.nodes} edges={flow.edges} onNodeClick={(_, n) => open(n.id)} className="h-[420px] xl:h-[520px]" />
      <Legend items={CREW_LEGEND.map((l) => ({ key: l.kind, label: l.label, look: legendLook(l.kind) }))} />
    </div>
  )
}
