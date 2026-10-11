import { describe, expect, it } from 'vitest'
import type { InteractionEdge, InteractionEdgeKind, InteractionNode, InteractionStats, Interactions } from '../../api/types'
import { INTERACTION_LOOKS, focusForNode, interactionsToFlow } from './interactionsFlow'

const zero: InteractionStats = { llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, duration_ms: 0 }
const node = (id: string, over: Partial<InteractionNode> = {}): InteractionNode => ({
  id, kind: (id.split(':')[0] as InteractionNode['kind']) ?? 'crew', label: id, stats: zero, ...over,
})
const edge = (id: string, source: string, target: string, kind: InteractionEdgeKind = 'runs', over: Partial<InteractionEdge> = {}): InteractionEdge => ({
  id, source, target, kind, count: 1, cost_usd: 0, ...over,
})

const graph: Interactions = {
  nodes: [node('crew:c'), node('task:a'), node('agent:x'), node('tool:t'), node('model:m')],
  edges: [edge('e1', 'crew:c', 'task:a'), edge('e2', 'task:a', 'agent:x', 'assigned'), edge('e3', 'agent:x', 'tool:t', 'tool_call'), edge('e4', 'agent:x', 'model:m', 'llm_call')],
}

describe('focusForNode', () => {
  it.each(['task', 'agent', 'tool', 'model'] as const)('maps a %s node id to a focus', (kind) => {
    expect(focusForNode(`${kind}:name`)).toEqual({ kind, value: 'name' })
  })
  it('keeps colons inside the value', () => {
    expect(focusForNode('tool:web:search')).toEqual({ kind: 'tool', value: 'web:search' })
  })
  it('returns null for the crew node, unknown prefixes and ids without a colon', () => {
    expect(focusForNode('crew:c')).toBeNull()
    expect(focusForNode('other:x')).toBeNull()
    expect(focusForNode('plain')).toBeNull()
    expect(focusForNode('')).toBeNull()
  })
  it('allows an empty value', () => {
    expect(focusForNode('task:')).toEqual({ kind: 'task', value: '' })
  })
})

describe('INTERACTION_LOOKS', () => {
  it('has a look for each edge kind and animates delegations only', () => {
    expect(Object.keys(INTERACTION_LOOKS).sort()).toEqual(['assigned', 'context', 'delegation', 'llm_call', 'runs', 'tool_call'])
    expect(INTERACTION_LOOKS.delegation.animated).toBe(true)
    expect(INTERACTION_LOOKS.runs.animated).toBeUndefined()
  })
})

describe('interactionsToFlow', () => {
  it('returns empty flow for empty data', () => {
    expect(interactionsToFlow({ nodes: [], edges: [] }, null)).toEqual({ nodes: [], edges: [] })
  })

  it('creates entity nodes with sublabel, status and laid-out positions left to right', () => {
    const { nodes } = interactionsToFlow({ ...graph, nodes: graph.nodes.map((n) => (n.id === 'task:a' ? { ...n, status: 'running' as const } : n)) }, null)
    expect(nodes).toHaveLength(5)
    const task = nodes.find((n) => n.id === 'task:a')!
    expect(task.type).toBe('entity')
    expect(task.data).toMatchObject({ kind: 'task', label: 'task:a', sublabel: 'task', status: 'running', focused: false, dimmed: false })
    const crew = nodes.find((n) => n.id === 'crew:c')!
    const agent = nodes.find((n) => n.id === 'agent:x')!
    expect(crew.position.x).toBeLessThan(task.position.x)
    expect(task.position.x).toBeLessThan(agent.position.x)
  })

  it('formats node stats and omits zero values', () => {
    const rich = node('agent:x', { stats: { llm_calls: 3, tool_calls: 2, prompt_tokens: 6000, completion_tokens: 6000, cost_usd: 0.5, duration_ms: 2500 } })
    const { nodes } = interactionsToFlow({ nodes: [rich, node('task:a')], edges: [] }, null)
    expect((nodes[0].data as { stats: string[] }).stats).toEqual(['3 llm', '2 tool', '12k tok', '$0.50', '2.5s'])
    expect((nodes[1].data as { stats: string[] }).stats).toEqual([])
  })

  it('compacts token counts under 10k and sub-cent costs', () => {
    const n = node('agent:x', { stats: { ...zero, prompt_tokens: 1000, completion_tokens: 500, cost_usd: 0.0042 } })
    const { nodes } = interactionsToFlow({ nodes: [n], edges: [] }, null)
    expect((nodes[0].data as { stats: string[] }).stats).toEqual(['1.5k tok', '$0.0042'])
  })

  it('gives nodes with stats more vertical room than those without', () => {
    const withStats = node('agent:x', { stats: { ...zero, llm_calls: 1 } })
    const a = node('task:a')
    const b = node('task:b')
    const { nodes } = interactionsToFlow({ nodes: [withStats, a, b], edges: [edge('e', 'task:a', 'agent:x'), edge('e2', 'task:b', 'agent:x')] }, null)
    expect(nodes.every((n) => Number.isFinite(n.position.x) && Number.isFinite(n.position.y))).toBe(true)
  })

  it('styles edges and labels them by kind', () => {
    const data: Interactions = {
      nodes: [node('crew:c'), node('task:a'), node('agent:x'), node('agent:y'), node('tool:t'), node('model:m')],
      edges: [
        edge('runs', 'crew:c', 'task:a', 'runs'),
        edge('assigned', 'task:a', 'agent:x', 'assigned'),
        edge('context', 'task:a', 'agent:y', 'context'),
        edge('tool', 'agent:x', 'tool:t', 'tool_call', { count: 4, cost_usd: 0.1 }),
        edge('llm', 'agent:x', 'model:m', 'llm_call', { count: 2 }),
        edge('deleg', 'agent:x', 'agent:y', 'delegation', { count: 3, cost_usd: 0.25 }),
        edge('deleg0', 'agent:y', 'agent:x', 'delegation', { count: 1 }),
      ],
    }
    const { edges } = interactionsToFlow(data, null)
    const by = (id: string) => edges.find((e) => e.id === id)!
    expect(by('runs').label).toBeUndefined()
    expect(by('assigned').label).toBeUndefined()
    expect(by('context').label).toBe('context')
    expect(by('tool').label).toBe('4× · $0.10')
    expect(by('llm').label).toBe('2×')
    expect(by('deleg').label).toBe('delegated 3× · $0.25')
    expect(by('deleg0').label).toBe('delegated 1×')
    expect(by('deleg').animated).toBe(true)
    expect(by('runs').style).toMatchObject({ stroke: 'var(--accent)', strokeWidth: 2 })
    expect(by('context').style).toMatchObject({ strokeDasharray: '6 4' })
    expect(by('deleg').source).toBe('agent:x')
    expect(by('deleg').target).toBe('agent:y')
  })

  it('ignores edges to unknown nodes in layout but still returns them', () => {
    const { nodes, edges } = interactionsToFlow({ nodes: [node('task:a')], edges: [edge('e', 'task:a', 'task:ghost'), edge('self', 'task:a', 'task:a')] }, null)
    expect(nodes).toHaveLength(1)
    expect(edges).toHaveLength(2)
  })

  describe('focus', () => {
    it('marks the focused node and dims nodes not linked to it', () => {
      const { nodes } = interactionsToFlow(graph, { kind: 'agent', value: 'x' })
      const d = (id: string) => nodes.find((n) => n.id === id)!.data as { focused: boolean; dimmed: boolean }
      expect(d('agent:x')).toMatchObject({ focused: true, dimmed: false })
      expect(d('task:a').dimmed).toBe(false)
      expect(d('tool:t').dimmed).toBe(false)
      expect(d('model:m').dimmed).toBe(false)
      expect(d('crew:c')).toMatchObject({ focused: false, dimmed: true })
    })

    it('dims everything else when the focus has no edges', () => {
      const { nodes } = interactionsToFlow(graph, { kind: 'task', value: 'missing' })
      expect(nodes.every((n) => (n.data as { dimmed: boolean }).dimmed)).toBe(true)
    })

    it('does not dim anything without focus', () => {
      const { nodes } = interactionsToFlow(graph, null)
      expect(nodes.some((n) => (n.data as { dimmed: boolean }).dimmed)).toBe(false)
    })

    it('keeps node positions identical regardless of focus', () => {
      const a = interactionsToFlow(graph, null).nodes.map((n) => n.position)
      const b = interactionsToFlow(graph, { kind: 'tool', value: 't' }).nodes.map((n) => n.position)
      expect(b).toEqual(a)
    })
  })
})
