import { memo, useEffect } from 'react'
import { Background, Controls, Handle, MiniMap, Position, ReactFlow, useNodesInitialized, useReactFlow, type Edge, type NodeProps } from '@xyflow/react'
import { Bot, ClipboardList, Cpu, ListChecks, Workflow, Wrench, type LucideIcon } from 'lucide-react'
import type { InteractionStatus } from '../../../api/types'
import { useTheme } from '../../../lib/theme'
import type { WorkflowFlowNode, WorkflowNodeKind } from './workflowGraph'

const KIND: Record<WorkflowNodeKind, { icon: LucideIcon; cls: string }> = {
  workorder: { icon: ClipboardList, cls: 'text-accent-fg bg-accent' },
  crew: { icon: Workflow, cls: 'text-accent bg-accent-soft' },
  task: { icon: ListChecks, cls: 'text-blue bg-blue-soft' },
  agent: { icon: Bot, cls: 'text-teal bg-teal-soft' },
  tool: { icon: Wrench, cls: 'text-violet bg-violet-soft' },
  model: { icon: Cpu, cls: 'text-muted bg-panel-2' },
}

const BORDER: Record<InteractionStatus, string> = {
  pending: 'border-line border-dashed',
  running: 'border-accent shadow-[0_0_0_4px_var(--accent-soft)]',
  done: 'border-ok/70',
  failed: 'border-danger',
}

const DOT: Record<InteractionStatus, string> = {
  pending: 'bg-faint',
  running: 'bg-accent pulse-dot',
  done: 'bg-ok',
  failed: 'bg-danger',
}

const hidden = '!size-1.5 !border-0 !bg-line-strong'
const invisible = '!size-1 !border-0 !bg-transparent'

function WorkflowNodeView({ data }: NodeProps<WorkflowFlowNode>) {
  const kind = KIND[data.kind]
  const Icon = kind.icon
  const border = data.status ? BORDER[data.status] : 'border-line'
  const ring = data.selected ? 'outline outline-2 outline-offset-2 outline-accent' : ''
  return (
    <div className={`w-[210px] cursor-pointer rounded-lg border bg-panel px-2.5 py-2 text-left transition-opacity ${border} ${ring} ${data.dimmed ? 'opacity-35' : ''}`}>
      <Handle type="target" position={Position.Left} id="in" className={hidden} />
      <Handle type="source" position={Position.Left} id="l-out" className={invisible} />
      <Handle type="source" position={Position.Right} id="out" className={hidden} />
      <Handle type="target" position={Position.Right} id="r-in" className={invisible} />
      <Handle type="target" position={Position.Top} id="top" className={invisible} />
      <Handle type="source" position={Position.Bottom} id="bottom" className={invisible} />
      <div className="flex items-center gap-2">
        <span className={`flex size-6 shrink-0 items-center justify-center rounded ${kind.cls}`}>
          <Icon className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] font-medium leading-tight text-fg" title={data.label}>{data.label}</div>
          <div className="truncate font-mono text-[10.5px] leading-tight text-faint">{data.sublabel}</div>
        </div>
        {data.status && <span className={`size-2 shrink-0 rounded-full ${DOT[data.status]}`} title={data.status} />}
      </div>
      {data.stats.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-x-2 border-t border-line pt-1 font-mono text-[10.5px] text-muted">
          {data.stats.map((s) => <span key={s}>{s}</span>)}
        </div>
      )}
    </div>
  )
}

const nodeTypes = { workflow: memo(WorkflowNodeView) }

function AutoFit({ shapeKey }: { shapeKey: string }) {
  const { fitView } = useReactFlow()
  const initialized = useNodesInitialized()
  useEffect(() => {
    if (!initialized) return
    const t = setTimeout(() => void fitView({ padding: 0.12, maxZoom: 1.1, duration: 200 }), 20)
    return () => clearTimeout(t)
  }, [initialized, shapeKey, fitView])
  return null
}

type WorkflowCanvasProps = {
  nodes: WorkflowFlowNode[]
  edges: Edge[]
  onSelect: (id: string | null) => void
}

/** Read-only React Flow canvas for the work order diagram (custom node with handles on every side). */
export function WorkflowCanvas({ nodes, edges, onSelect }: WorkflowCanvasProps) {
  const { theme } = useTheme()
  return (
    <div className="h-[440px] overflow-hidden rounded-lg border border-line md:h-[580px]">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        colorMode={theme}
        onNodeClick={(_, n) => onSelect(n.id)}
        onPaneClick={() => onSelect(null)}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        fitView
        fitViewOptions={{ padding: 0.12, maxZoom: 1.1 }}
        minZoom={0.15}
      >
        <Background gap={18} size={1} />
        <Controls showInteractive={false} />
        <MiniMap pannable zoomable nodeColor="var(--line-strong)" nodeStrokeColor="var(--fg-faint)" maskColor="rgb(0 0 0 / 0.25)" className="!hidden md:!block" />
        <AutoFit shapeKey={nodes.map((n) => n.id).join('|')} />
      </ReactFlow>
    </div>
  )
}
