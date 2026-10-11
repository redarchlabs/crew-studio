import { useEffect } from 'react'
import { Background, Controls, MiniMap, ReactFlow, useNodesInitialized, useReactFlow, type Edge, type NodeMouseHandler } from '@xyflow/react'
import { useTheme } from '../../lib/theme'
import { nodeTypes, type EntityFlowNode } from './EntityNode'

type FlowCanvasProps = {
  nodes: EntityFlowNode[]
  edges: Edge[]
  onNodeClick?: NodeMouseHandler<EntityFlowNode>
  onPaneClick?: () => void
  className?: string
  minimap?: boolean
}

/** Re-fits the viewport when the graph's shape changes (nodes added or removed). */
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

/** Read-only React Flow canvas with our node type and theme. */
export function FlowCanvas({ nodes, edges, onNodeClick, onPaneClick, className = 'h-[460px]', minimap = false }: FlowCanvasProps) {
  const { theme } = useTheme()
  return (
    <div className={`overflow-hidden rounded-lg border border-line ${className}`}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        colorMode={theme}
        onNodeClick={onNodeClick}
        onPaneClick={onPaneClick}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        fitView
        fitViewOptions={{ padding: 0.15, maxZoom: 1.1 }}
        minZoom={0.2}
      >
        <Background gap={18} size={1} />
        <Controls showInteractive={false} />
        {minimap && <MiniMap pannable zoomable nodeColor="var(--line-strong)" nodeStrokeColor="var(--fg-faint)" maskColor="rgb(0 0 0 / 0.25)" className="hidden md:block" />}
        <AutoFit shapeKey={nodes.map((n) => n.id).join('|')} />
      </ReactFlow>
    </div>
  )
}
