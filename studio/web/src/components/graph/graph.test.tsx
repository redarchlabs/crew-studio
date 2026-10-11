import { ReactFlow, ReactFlowProvider, type Edge } from '@xyflow/react'
import { render, screen } from '@testing-library/react'
import { beforeAll, describe, expect, it } from 'vitest'
import { styledEdge } from './edgeStyle'
import { EntityNode, nodeTypes, type EntityFlowNode, type EntityNodeData } from './EntityNode'
import { FlowCanvas } from './FlowCanvas'
import { Legend } from './Legend'
import { NODE_HEIGHT, NODE_WIDTH, layoutLeftToRight } from './layout'
import { autoCleanup } from '../../lib/testUtils'

autoCleanup()

beforeAll(() => {
  // React Flow measures its container; jsdom has no ResizeObserver or layout.
  class RO {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  ;(globalThis as unknown as { ResizeObserver: typeof RO }).ResizeObserver = RO
  ;(globalThis as unknown as { DOMMatrixReadOnly: unknown }).DOMMatrixReadOnly ??= class {
    m22 = 1
    constructor(_t?: string) {}
  }
})

const node = (id: string, data: Partial<EntityNodeData> = {}): EntityFlowNode => ({
  id,
  type: 'entity',
  position: { x: 0, y: 0 },
  data: { kind: 'agent', label: id, ...data },
})

function renderNode(data: Partial<EntityNodeData>, selected = false) {
  const props = { id: 'n', data: { kind: 'agent', label: 'L', ...data }, selected } as unknown as Parameters<typeof EntityNode>[0]
  return render(
    <ReactFlowProvider>
      <EntityNode {...props} />
    </ReactFlowProvider>,
  )
}

describe('edgeStyle.styledEdge', () => {
  it('builds an edge with a coloured stroke and a matching arrow marker', () => {
    const e = styledEdge('e1', 'a', 'b', { color: '#f00', width: 2, dash: '4 2', animated: true }, 'uses')
    expect(e).toMatchObject({ id: 'e1', source: 'a', target: 'b', label: 'uses', animated: true })
    expect(e.style).toEqual({ stroke: '#f00', strokeWidth: 2, strokeDasharray: '4 2' })
    expect(e.markerEnd).toMatchObject({ color: '#f00', width: 14, height: 14 })
  })
  it('leaves label, dash and animated undefined when not given', () => {
    const e = styledEdge('e', 'a', 'b', { color: '#0f0', width: 1 })
    expect(e.label).toBeUndefined()
    expect(e.animated).toBeUndefined()
    expect(e.style?.strokeDasharray).toBeUndefined()
  })
})

describe('layoutLeftToRight', () => {
  const nodes = [node('a'), node('b'), node('c')]
  const edges: Edge[] = [{ id: 'ab', source: 'a', target: 'b' }, { id: 'bc', source: 'b', target: 'c' }]

  it('places a chain left to right on increasing x without mutating inputs', () => {
    const out = layoutLeftToRight(nodes, edges)
    expect(out).toHaveLength(3)
    const [a, b, c] = out
    expect(a.position.x).toBeLessThan(b.position.x)
    expect(b.position.x).toBeLessThan(c.position.x)
    expect(nodes[0].position).toEqual({ x: 0, y: 0 })
    expect(out[0]).not.toBe(nodes[0])
    expect(out.map((n) => n.id)).toEqual(['a', 'b', 'c'])
  })

  it('positions are top-left corners (centre minus half size)', () => {
    const [only] = layoutLeftToRight([node('solo')], [])
    // margins are 10, so the centre is at 10 + w/2 and the top-left lands at 10
    expect(only.position.x).toBeCloseTo(10)
    expect(only.position.y).toBeCloseTo(10)
    expect(NODE_WIDTH).toBe(210)
    expect(NODE_HEIGHT).toBe(64)
  })

  it('honours custom width/height/spacing options', () => {
    const [a1, b1] = layoutLeftToRight(nodes.slice(0, 2), edges.slice(0, 1))
    const [a2, b2] = layoutLeftToRight(nodes.slice(0, 2), edges.slice(0, 1), { ranksep: 200, width: 100 })
    expect(b1.position.x - a1.position.x).toBe(NODE_WIDTH + 48) // defaults
    expect(b2.position.x - a2.position.x).toBe(300) // width 100 + ranksep 200
  })

  it('heightOf overrides the per-node height', () => {
    const [a, b] = layoutLeftToRight([node('a'), node('b')], [], { heightOf: (n) => (n.id === 'a' ? 100 : 20), nodesep: 10 })
    // same rank, stacked vertically: b starts below a's 100px height plus the 10px gap
    expect(Math.abs(b.position.y - a.position.y)).toBe(110)
  })

  it('ignores edges to unknown nodes and self loops, and handles no nodes', () => {
    const out = layoutLeftToRight([node('a'), node('b')], [
      { id: 'x', source: 'a', target: 'ghost' },
      { id: 'y', source: 'ghost', target: 'b' },
      { id: 'z', source: 'a', target: 'a' },
    ])
    expect(out).toHaveLength(2)
    expect(out.every((n) => Number.isFinite(n.position.x) && Number.isFinite(n.position.y))).toBe(true)
    expect(layoutLeftToRight([], [])).toEqual([])
  })
})

describe('Legend', () => {
  it('renders one labelled swatch per item', () => {
    const { container } = render(
      <Legend items={[
        { key: 'a', label: 'Depends on', look: { color: '#111', width: 1 } },
        { key: 'b', label: 'Delegates', look: { color: '#222', width: 3, dash: '3 3' } },
      ]} />,
    )
    expect(screen.getByText('Depends on')).toBeTruthy()
    expect(screen.getByText('Delegates')).toBeTruthy()
    const lines = container.querySelectorAll('line')
    expect(lines).toHaveLength(2)
    expect(lines[0].getAttribute('stroke')).toBe('#111')
    expect(lines[0].getAttribute('stroke-width')).toBe('1.5') // thin lines are floored for visibility
    expect(lines[1].getAttribute('stroke-width')).toBe('3')
    expect(lines[1].getAttribute('stroke-dasharray')).toBe('3 3')
    expect(container.querySelector('svg')!.getAttribute('aria-hidden')).toBe('true')
  })
  it('renders nothing visible for no items', () => {
    const { container } = render(<Legend items={[]} />)
    expect(container.querySelectorAll('svg')).toHaveLength(0)
  })
})

describe('EntityNode', () => {
  it('shows label and falls back to the kind name as sublabel', () => {
    renderNode({ kind: 'crew', label: 'Research' })
    expect(screen.getByText('Research')).toBeTruthy()
    expect(screen.getByText('crew')).toBeTruthy()
  })
  it('uses the explicit sublabel when given', () => {
    renderNode({ label: 'Writer', sublabel: 'gpt-x' })
    expect(screen.getByText('gpt-x')).toBeTruthy()
    expect(screen.queryByText('agent')).toBeNull()
  })
  it.each(['crew', 'task', 'agent', 'tool', 'llm', 'model'] as const)('renders kind %s', (kind) => {
    const { container } = renderNode({ kind, label: `L-${kind}` })
    expect(screen.getByText(`L-${kind}`)).toBeTruthy()
    expect(container.querySelector('svg')).not.toBeNull()
  })
  it.each(['pending', 'running', 'done', 'failed'] as const)('status %s gets a titled dot', (status) => {
    renderNode({ status })
    expect(screen.getByTitle(status)).toBeTruthy()
  })
  it('no status means no status dot', () => {
    const { container } = renderNode({})
    expect(container.querySelector('.rounded-full')).toBeNull()
  })
  it('warnings show an icon and a tooltip listing them', () => {
    const { container } = renderNode({ warnings: ['No LLM', 'No tools'] })
    expect(container.firstElementChild!.getAttribute('title')).toBe('No LLM\nNo tools')
    expect(container.querySelector('.text-warn')).not.toBeNull()
  })
  it('an empty warnings list is not a warning', () => {
    const { container } = renderNode({ warnings: [] })
    expect(container.querySelector('.text-warn')).toBeNull()
  })
  it('lists stats when present, nothing when empty', () => {
    const { container, unmount } = renderNode({ stats: ['3 calls', '$0.10'] })
    expect(screen.getByText('3 calls')).toBeTruthy()
    expect(screen.getByText('$0.10')).toBeTruthy()
    unmount()
    const again = renderNode({ stats: [] })
    expect(again.container.querySelector('.border-t')).toBeNull()
    expect(container).toBeTruthy()
  })
  it('selected or focused nodes get an outline; dimmed nodes fade', () => {
    const sel = renderNode({}, true)
    expect(sel.container.firstElementChild!.className).toContain('outline-accent')
    sel.unmount()
    const foc = renderNode({ focused: true })
    expect(foc.container.firstElementChild!.className).toContain('outline-accent')
    foc.unmount()
    const dim = renderNode({ dimmed: true })
    expect(dim.container.firstElementChild!.className).toContain('opacity-35')
    dim.unmount()
    const plain = renderNode({})
    expect(plain.container.firstElementChild!.className).not.toContain('outline-accent')
    expect(plain.container.firstElementChild!.className).not.toContain('opacity-35')
  })
  it('exposes itself under the "entity" node type', () => {
    expect(nodeTypes.entity).toBe(EntityNode)
  })
  it('renders inside a real ReactFlow', () => {
    render(
      <div style={{ width: 400, height: 300 }}>
        <ReactFlow nodes={[node('flow-node', { label: 'In flow' })]} edges={[]} nodeTypes={nodeTypes} />
      </div>,
    )
    expect(screen.getByText('In flow')).toBeTruthy()
  })
})

describe('FlowCanvas', () => {
  const nodes = [node('a', { label: 'Alpha' }), node('b', { label: 'Beta' })]
  const edges: Edge[] = [{ id: 'ab', source: 'a', target: 'b' }]

  it('renders the nodes inside a bordered, sized container', () => {
    const { container } = render(<FlowCanvas nodes={nodes} edges={edges} />)
    expect(container.firstElementChild!.className).toContain('h-[460px]')
    expect(screen.getByText('Alpha')).toBeTruthy()
    expect(screen.getByText('Beta')).toBeTruthy()
  })

  it('accepts a className override and renders a minimap only when asked', () => {
    const a = render(<FlowCanvas nodes={nodes} edges={edges} className="h-40" />)
    expect(a.container.firstElementChild!.className).toContain('h-40')
    expect(a.container.querySelector('.react-flow__minimap')).toBeNull()
    a.unmount()
    const b = render(<FlowCanvas nodes={nodes} edges={edges} minimap />)
    expect(b.container.querySelector('.react-flow__minimap')).not.toBeNull()
  })

  it('renders without nodes', () => {
    const { container } = render(<FlowCanvas nodes={[]} edges={[]} />)
    expect(container.querySelector('.react-flow')).not.toBeNull()
  })
})
