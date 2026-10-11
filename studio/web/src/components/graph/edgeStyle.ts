import { MarkerType, type Edge } from '@xyflow/react'

export type EdgeLook = { color: string; width: number; dash?: string; animated?: boolean }

/** Builds a styled React Flow edge with an arrow marker in the same colour. */
export function styledEdge(id: string, source: string, target: string, look: EdgeLook, label?: string): Edge {
  return {
    id,
    source,
    target,
    label,
    animated: look.animated,
    style: { stroke: look.color, strokeWidth: look.width, strokeDasharray: look.dash },
    markerEnd: { type: MarkerType.ArrowClosed, color: look.color, width: 14, height: 14 },
    labelBgPadding: [4, 2],
    labelBgBorderRadius: 3,
  }
}
