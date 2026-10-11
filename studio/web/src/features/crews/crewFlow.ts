import type { Edge } from '@xyflow/react'
import type { EntityFlowNode } from '../../components/graph/EntityNode'
import { styledEdge, type EdgeLook } from '../../components/graph/edgeStyle'
import { layoutLeftToRight } from '../../components/graph/layout'
import type { CrewGraph, CrewGraphEdgeKind } from './crewGraph'

const LOOKS: Record<CrewGraphEdgeKind, EdgeLook> = {
  sequence: { color: 'var(--accent)', width: 2 },
  assigned: { color: 'var(--teal)', width: 1.5 },
  context: { color: 'var(--blue)', width: 1.5, dash: '6 4' },
  tool: { color: 'var(--violet)', width: 1 },
  llm: { color: 'var(--fg-faint)', width: 1 },
  manager: { color: 'var(--accent)', width: 1.5, dash: '3 3' },
  member: { color: 'var(--fg-faint)', width: 1, dash: '2 4' },
}

/** Converts the framework-agnostic crew graph into laid-out React Flow nodes and edges. */
export function crewGraphToFlow(graph: CrewGraph): { nodes: EntityFlowNode[]; edges: Edge[] } {
  const nodes: EntityFlowNode[] = graph.nodes.map((n) => ({
    id: n.id,
    type: 'entity',
    position: { x: 0, y: 0 },
    data: { kind: n.kind, label: n.label, sublabel: n.sublabel, warnings: n.warnings },
  }))
  const edges = graph.edges.map((e) => styledEdge(e.id, e.source, e.target, LOOKS[e.kind], e.label))
  return { nodes: layoutLeftToRight(nodes, edges), edges }
}

export const CREW_LEGEND: { kind: CrewGraphEdgeKind; label: string }[] = [
  { kind: 'sequence', label: 'order' },
  { kind: 'assigned', label: 'assigned agent' },
  { kind: 'context', label: 'context' },
  { kind: 'tool', label: 'tool' },
  { kind: 'llm', label: 'llm' },
]

export function legendLook(kind: CrewGraphEdgeKind): EdgeLook {
  return LOOKS[kind]
}
