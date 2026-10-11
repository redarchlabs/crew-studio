import { Graph, layout } from '@dagrejs/dagre'
import type { Edge, Node } from '@xyflow/react'

export const NODE_WIDTH = 210
export const NODE_HEIGHT = 64

type LayoutOptions = { width?: number; height?: number; ranksep?: number; nodesep?: number; heightOf?: (node: Node) => number }

/** Left-to-right dagre layout; returns new node objects with positions set. */
export function layoutLeftToRight<N extends Node>(nodes: readonly N[], edges: readonly Edge[], opts: LayoutOptions = {}): N[] {
  const width = opts.width ?? NODE_WIDTH
  const height = (n: Node) => opts.heightOf?.(n) ?? opts.height ?? NODE_HEIGHT
  const g = new Graph()
  g.setGraph({ rankdir: 'LR', ranksep: opts.ranksep ?? 48, nodesep: opts.nodesep ?? 22, marginx: 10, marginy: 10 })
  g.setDefaultEdgeLabel(() => ({}))
  for (const n of nodes) g.setNode(n.id, { width, height: height(n) })
  for (const e of edges) {
    if (g.hasNode(e.source) && g.hasNode(e.target) && e.source !== e.target) g.setEdge(e.source, e.target)
  }
  layout(g)
  return nodes.map((n) => {
    const pos = g.node(n.id) as { x: number; y: number } | undefined
    const h = height(n)
    return { ...n, position: pos ? { x: pos.x - width / 2, y: pos.y - h / 2 } : { x: 0, y: 0 } }
  })
}
