import { MarkerType, type Edge, type Node } from '@xyflow/react'
import type { InteractionEdge, InteractionEdgeKind, InteractionNode, InteractionNodeKind, InteractionStatus, Interactions, RunStatus } from '../../../api/types'
import type { EdgeLook } from '../../../components/graph/edgeStyle'
import { compact, durationMs, usd } from '../../../lib/format'

// The work order's interaction diagram: a left-to-right layered layout
// work order -> crew -> tasks (run order, stacked) -> agents -> tools and models.
// Sequence edges join consecutive tasks vertically, context edges arc on the left of the task column and
// delegations arc on the right of the agent column.

export type WorkflowNodeKind = 'workorder' | InteractionNodeKind

export type WorkflowNodeData = {
  kind: WorkflowNodeKind
  label: string
  sublabel: string
  status?: InteractionStatus
  stats: string[]
  selected: boolean
  dimmed: boolean
}

export type WorkflowFlowNode = Node<WorkflowNodeData, 'workflow'>

export type WorkflowEdgeKind = InteractionEdgeKind | 'submits' | 'sequence'

export const WORKFLOW_LOOKS: Record<WorkflowEdgeKind, EdgeLook> = {
  submits: { color: 'var(--accent)', width: 2 },
  runs: { color: 'var(--accent)', width: 1.75 },
  sequence: { color: 'var(--blue)', width: 2 },
  context: { color: 'var(--blue)', width: 1.5, dash: '6 4' },
  assigned: { color: 'var(--teal)', width: 1.5 },
  tool_call: { color: 'var(--violet)', width: 1.25 },
  llm_call: { color: 'var(--fg-faint)', width: 1.25 },
  delegation: { color: 'var(--danger)', width: 2.5 },
}

export const WORKFLOW_LEGEND: { key: WorkflowEdgeKind; label: string }[] = [
  { key: 'sequence', label: 'run order' },
  { key: 'context', label: 'context' },
  { key: 'assigned', label: 'assigned' },
  { key: 'tool_call', label: 'tool calls' },
  { key: 'llm_call', label: 'LLM calls' },
  { key: 'delegation', label: 'delegation' },
]

export const WO_NODE_ID = 'workorder'

const COLUMN: Record<WorkflowNodeKind, number> = { workorder: 0, crew: 1, task: 2, agent: 3, tool: 4, model: 4 }
const NODE_W = 210
const COL_GAP = 96
const ROW_GAP = 26
const BASE_H = 58
const STATS_H = 18

export function nodeHeight(stats: readonly string[]): number {
  return stats.length > 0 ? BASE_H + STATS_H : BASE_H
}

export function nodeStats(n: Pick<InteractionNode, 'stats'>): string[] {
  const s = n.stats
  const out: string[] = []
  if (s.llm_calls > 0) out.push(`${s.llm_calls} llm`)
  if (s.tool_calls > 0) out.push(`${s.tool_calls} tool`)
  if (s.prompt_tokens + s.completion_tokens > 0) out.push(`${compact(s.prompt_tokens + s.completion_tokens)} tok`)
  if (s.cost_usd > 0) out.push(usd(s.cost_usd))
  if (s.duration_ms > 0) out.push(durationMs(s.duration_ms))
  return out
}

export function runStatusToNode(status: RunStatus | null): InteractionStatus | undefined {
  if (status === null) return undefined
  if (status === 'queued') return 'pending'
  if (status === 'running') return 'running'
  return status === 'succeeded' ? 'done' : 'failed'
}

/** Orders nodes within each column: tasks by run order, others by the earliest neighbour on their left. */
export function columnOrder(data: Interactions, taskOrder: readonly string[]): Map<string, number> {
  const rank = new Map<string, number>()
  const tasks = data.nodes.filter((n) => n.kind === 'task')
  const byOrder = (label: string) => {
    const i = taskOrder.indexOf(label)
    return i < 0 ? taskOrder.length : i
  }
  ;[...tasks].sort((a, b) => byOrder(a.label) - byOrder(b.label)).forEach((t, i) => rank.set(t.id, i))
  // Agents follow the first task they work on; tools and models follow the first agent that uses them
  for (const kinds of [['agent'], ['tool', 'model']] as const) {
    const members = data.nodes.filter((n) => (kinds as readonly string[]).includes(n.kind))
    const score = (n: InteractionNode) => {
      const left = data.edges.filter((e) => e.target === n.id && rank.has(e.source)).map((e) => rank.get(e.source) ?? 0)
      return (left.length ? Math.min(...left) : 1e6) + (n.kind === 'model' ? 0.5 : 0)
    }
    ;[...members].sort((a, b) => score(a) - score(b) || a.label.localeCompare(b.label)).forEach((n, i) => rank.set(n.id, i))
  }
  return rank
}

/** Positions nodes in fixed columns, each column centred vertically on the tallest one. */
export function layoutColumns<N extends { id: string; data: { kind: WorkflowNodeKind; stats: string[] } }>(nodes: readonly N[], rank: ReadonlyMap<string, number>): Map<string, { x: number; y: number }> {
  const columns = new Map<number, N[]>()
  for (const n of nodes) columns.set(COLUMN[n.data.kind], [...(columns.get(COLUMN[n.data.kind]) ?? []), n])
  const heightOf = (col: N[]) => col.reduce((h, n) => h + nodeHeight(n.data.stats), 0) + ROW_GAP * Math.max(0, col.length - 1)
  const tallest = Math.max(0, ...[...columns.values()].map(heightOf))
  const positions = new Map<string, { x: number; y: number }>()
  for (const [col, members] of columns) {
    const sorted = [...members].sort((a, b) => (rank.get(a.id) ?? 0) - (rank.get(b.id) ?? 0))
    let y = (tallest - heightOf(sorted)) / 2
    for (const n of sorted) {
      positions.set(n.id, { x: col * (NODE_W + COL_GAP), y })
      y += nodeHeight(n.data.stats) + ROW_GAP
    }
  }
  return positions
}

function edge(id: string, source: string, target: string, kind: WorkflowEdgeKind, opts: { label?: string; sourceHandle?: string; targetHandle?: string; animated?: boolean } = {}): Edge {
  const look = WORKFLOW_LOOKS[kind]
  return {
    id,
    source,
    target,
    // Nodes have several handles; default to left-in / right-out
    sourceHandle: opts.sourceHandle ?? 'out',
    targetHandle: opts.targetHandle ?? 'in',
    label: opts.label,
    animated: opts.animated,
    data: { kind },
    style: { stroke: look.color, strokeWidth: look.width, strokeDasharray: look.dash },
    markerEnd: { type: MarkerType.ArrowClosed, color: look.color, width: 14, height: 14 },
    labelBgPadding: [4, 2],
    labelBgBorderRadius: 3,
  }
}

function countLabel(e: InteractionEdge): string | undefined {
  if (e.kind !== 'tool_call' && e.kind !== 'llm_call' && e.kind !== 'delegation') return undefined
  return [`${e.count}×`, e.cost_usd > 0 ? usd(e.cost_usd) : ''].filter(Boolean).join(' · ')
}

/** Interaction edges restyled for the layered layout; crew->task "runs" edges become one crew->first task edge. */
function interactionEdges(data: Interactions, orderedTasks: readonly string[], live: boolean, runningTask: string | null): Edge[] {
  const out: Edge[] = []
  for (const e of data.edges) {
    if (e.kind === 'runs') continue
    if (e.kind === 'context') out.push(edge(e.id, e.source, e.target, 'context', { sourceHandle: 'l-out', targetHandle: 'in' }))
    else if (e.kind === 'delegation') out.push(edge(e.id, e.source, e.target, 'delegation', { label: countLabel(e), targetHandle: 'r-in', animated: live }))
    else out.push(edge(e.id, e.source, e.target, e.kind, { label: countLabel(e), animated: live && e.kind === 'assigned' && e.source === runningTask }))
  }
  for (let i = 1; i < orderedTasks.length; i++) {
    const [a, b] = [orderedTasks[i - 1], orderedTasks[i]]
    out.push(edge(`seq:${a}->${b}`, a, b, 'sequence', { sourceHandle: 'bottom', targetHandle: 'top', animated: live && b === runningTask }))
  }
  return out
}

export type WorkflowInput = {
  data: Interactions
  workOrder: { number: number; title: string }
  runStatus: RunStatus | null
  taskOrder: readonly string[]
  live: boolean
  selectedId: string | null
}

/** Builds positioned nodes and styled edges for the work order diagram. */
export function buildWorkflow({ data, workOrder, runStatus, taskOrder, live, selectedId }: WorkflowInput): { nodes: WorkflowFlowNode[]; edges: Edge[] } {
  const linked = new Set<string>(selectedId ? [selectedId] : [])
  if (selectedId) for (const e of data.edges) if (e.source === selectedId || e.target === selectedId) linked.add(e.source).add(e.target)
  const dim = (id: string) => selectedId !== null && !linked.has(id)

  const woNode: WorkflowFlowNode = {
    id: WO_NODE_ID,
    type: 'workflow',
    position: { x: 0, y: 0 },
    data: { kind: 'workorder', label: workOrder.title, sublabel: `WO-${workOrder.number}`, status: runStatusToNode(runStatus), stats: [], selected: selectedId === WO_NODE_ID, dimmed: selectedId !== null && selectedId !== WO_NODE_ID },
  }
  const nodes: WorkflowFlowNode[] = [
    woNode,
    ...data.nodes.map((n) => ({
      id: n.id,
      type: 'workflow' as const,
      position: { x: 0, y: 0 },
      data: { kind: n.kind, label: n.label, sublabel: n.kind, status: n.status, stats: nodeStats(n), selected: n.id === selectedId, dimmed: dim(n.id) },
    })),
  ]
  const rank = columnOrder(data, taskOrder)
  const positions = layoutColumns(nodes, rank)
  const orderedTasks = data.nodes.filter((n) => n.kind === 'task').sort((a, b) => (rank.get(a.id) ?? 0) - (rank.get(b.id) ?? 0)).map((n) => n.id)
  const crew = data.nodes.find((n) => n.kind === 'crew')
  const runningTask = data.nodes.find((n) => n.kind === 'task' && n.status === 'running')?.id ?? null

  const edges: Edge[] = []
  const first = crew?.id ?? orderedTasks[0]
  if (first) edges.push(edge('wo->first', WO_NODE_ID, first, 'submits', { animated: live && !crew }))
  if (crew && orderedTasks[0]) edges.push(edge('crew->first', crew.id, orderedTasks[0], 'runs', { animated: live }))
  edges.push(...interactionEdges(data, orderedTasks, live, runningTask))

  return { nodes: nodes.map((n) => ({ ...n, position: positions.get(n.id) ?? { x: 0, y: 0 } })), edges }
}
