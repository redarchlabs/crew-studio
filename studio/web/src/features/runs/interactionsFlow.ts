import type { Edge } from '@xyflow/react'
import type { InteractionEdgeKind, InteractionNode, Interactions } from '../../api/types'
import type { EntityFlowNode } from '../../components/graph/EntityNode'
import { styledEdge, type EdgeLook } from '../../components/graph/edgeStyle'
import { layoutLeftToRight, NODE_HEIGHT } from '../../components/graph/layout'
import { compact, durationMs, usd } from '../../lib/format'
import type { TimelineFocus } from './timeline/timelineReducer'

export const INTERACTION_LOOKS: Record<InteractionEdgeKind, EdgeLook> = {
  runs: { color: 'var(--accent)', width: 2 },
  assigned: { color: 'var(--teal)', width: 1.5 },
  context: { color: 'var(--blue)', width: 1.5, dash: '6 4' },
  tool_call: { color: 'var(--violet)', width: 1.25 },
  llm_call: { color: 'var(--fg-faint)', width: 1.25 },
  delegation: { color: 'var(--danger)', width: 3, animated: true },
}

function nodeStats(n: InteractionNode): string[] {
  const s = n.stats
  const out: string[] = []
  if (s.llm_calls > 0) out.push(`${s.llm_calls} llm`)
  if (s.tool_calls > 0) out.push(`${s.tool_calls} tool`)
  if (s.prompt_tokens + s.completion_tokens > 0) out.push(`${compact(s.prompt_tokens + s.completion_tokens)} tok`)
  if (s.cost_usd > 0) out.push(usd(s.cost_usd))
  if (s.duration_ms > 0) out.push(durationMs(s.duration_ms))
  return out
}

function edgeLabel(kind: InteractionEdgeKind, count: number, cost: number): string | undefined {
  if (kind === 'runs' || kind === 'assigned' || kind === 'context') return kind === 'context' ? 'context' : undefined
  const parts = [`${count}×`]
  if (cost > 0) parts.push(usd(cost))
  return kind === 'delegation' ? `delegated ${parts.join(' · ')}` : parts.join(' · ')
}

/** "task:research" -> { kind: 'task', value: 'research' }; the crew node clears focus. */
export function focusForNode(id: string): TimelineFocus | null {
  const idx = id.indexOf(':')
  if (idx < 0) return null
  const prefix = id.slice(0, idx)
  const value = id.slice(idx + 1)
  if (prefix === 'task' || prefix === 'agent' || prefix === 'tool' || prefix === 'model') return { kind: prefix, value }
  return null
}

export function interactionsToFlow(data: Interactions, focus: TimelineFocus | null): { nodes: EntityFlowNode[]; edges: Edge[] } {
  const focusId = focus ? `${focus.kind}:${focus.value}` : null
  const linked = new Set<string>(focusId ? [focusId] : [])
  if (focusId) for (const e of data.edges) if (e.source === focusId || e.target === focusId) linked.add(e.source).add(e.target)

  const nodes: EntityFlowNode[] = data.nodes.map((n) => {
    const stats = nodeStats(n)
    return {
      id: n.id,
      type: 'entity',
      position: { x: 0, y: 0 },
      data: {
        kind: n.kind,
        label: n.label,
        sublabel: n.kind,
        status: n.status,
        stats,
        focused: n.id === focusId,
        dimmed: focusId !== null && !linked.has(n.id),
      },
    }
  })
  const edges = data.edges.map((e) => styledEdge(e.id, e.source, e.target, INTERACTION_LOOKS[e.kind], edgeLabel(e.kind, e.count, e.cost_usd)))
  const laid = layoutLeftToRight(nodes, edges, { heightOf: (n) => ((n.data as { stats?: string[] }).stats?.length ? NODE_HEIGHT + 16 : NODE_HEIGHT), ranksep: 90 })
  return { nodes: laid, edges }
}
