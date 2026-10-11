import { describe, expect, it } from 'vitest'
import { makeEdge, makeInteractions, makeNode } from '../testUtils'
import {
  buildWorkflow,
  columnOrder,
  layoutColumns,
  nodeHeight,
  nodeStats,
  runStatusToNode,
  WO_NODE_ID,
  WORKFLOW_LEGEND,
  WORKFLOW_LOOKS,
  type WorkflowInput,
} from './workflowGraph'

const WO = { number: 12, title: 'Vendor review' }

function input(over: Partial<WorkflowInput> & Pick<WorkflowInput, 'data'>): WorkflowInput {
  return { workOrder: WO, runStatus: 'running', taskOrder: [], live: false, selectedId: null, ...over }
}

describe('nodeStats / nodeHeight', () => {
  it('lists only non-zero stats in a fixed order', () => {
    const n = makeNode({ id: 'a', stats: { llm_calls: 3, tool_calls: 2, prompt_tokens: 1000, completion_tokens: 500, cost_usd: 0.25, duration_ms: 2500 } })
    const stats = nodeStats(n)
    expect(stats[0]).toBe('3 llm')
    expect(stats[1]).toBe('2 tool')
    expect(stats[2]).toMatch(/tok$/)
    expect(stats[3]).toBe('$0.25')
    expect(stats).toHaveLength(5)
  })

  it('returns nothing for an idle node', () => {
    expect(nodeStats(makeNode({ id: 'a' }))).toEqual([])
  })

  it('counts tokens even when only completion tokens exist', () => {
    const n = makeNode({ id: 'a', stats: { llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 10, cost_usd: 0, duration_ms: 0 } })
    expect(nodeStats(n)).toEqual([expect.stringMatching(/tok$/)])
  })

  it('makes nodes with stats taller', () => {
    expect(nodeHeight(['1 llm'])).toBeGreaterThan(nodeHeight([]))
  })
})

describe('runStatusToNode', () => {
  it.each([
    [null, undefined],
    ['queued', 'pending'],
    ['running', 'running'],
    ['succeeded', 'done'],
    ['failed', 'failed'],
    ['cancelled', 'failed'],
  ] as const)('%s -> %s', (status, expected) => {
    expect(runStatusToNode(status)).toBe(expected)
  })
})

describe('look tables', () => {
  it('has a look for every legend entry', () => {
    for (const l of WORKFLOW_LEGEND) expect(WORKFLOW_LOOKS[l.key]).toBeDefined()
  })
})

describe('columnOrder', () => {
  it('ranks tasks by the given run order, unknown tasks last', () => {
    const data = makeInteractions([makeNode({ id: 't-c', label: 'c' }), makeNode({ id: 't-a', label: 'a' }), makeNode({ id: 't-b', label: 'b' })])
    const rank = columnOrder(data, ['a', 'b'])
    expect(rank.get('t-a')).toBe(0)
    expect(rank.get('t-b')).toBe(1)
    expect(rank.get('t-c')).toBe(2)
  })

  it('orders agents after the first task they work on, then by label', () => {
    const data = makeInteractions(
      [
        makeNode({ id: 't1', label: 't1' }),
        makeNode({ id: 't2', label: 't2' }),
        makeNode({ id: 'ag-z', kind: 'agent', label: 'Zed' }),
        makeNode({ id: 'ag-a', kind: 'agent', label: 'Amy' }),
        makeNode({ id: 'ag-orphan', kind: 'agent', label: 'Aaron' }),
      ],
      [makeEdge({ id: 'e1', source: 't1', target: 'ag-z' }), makeEdge({ id: 'e2', source: 't2', target: 'ag-a' })],
    )
    const rank = columnOrder(data, ['t1', 't2'])
    expect(rank.get('ag-z')).toBe(0)
    expect(rank.get('ag-a')).toBe(1)
    // no incoming edge: sorted last
    expect(rank.get('ag-orphan')).toBe(2)
  })

  it('places models just after tools that share the same parent rank', () => {
    const data = makeInteractions(
      [
        makeNode({ id: 't1', label: 't1' }),
        makeNode({ id: 'ag', kind: 'agent', label: 'A' }),
        makeNode({ id: 'tool', kind: 'tool', label: 'search' }),
        makeNode({ id: 'model', kind: 'model', label: 'gpt' }),
      ],
      [
        makeEdge({ id: 'e1', source: 't1', target: 'ag' }),
        makeEdge({ id: 'e2', source: 'ag', target: 'model', kind: 'llm_call' }),
        makeEdge({ id: 'e3', source: 'ag', target: 'tool', kind: 'tool_call' }),
      ],
    )
    const rank = columnOrder(data, ['t1'])
    expect(rank.get('tool')).toBe(0)
    expect(rank.get('model')).toBe(1)
  })
})

describe('layoutColumns', () => {
  const mk = (id: string, kind: 'workorder' | 'task' | 'agent', stats: string[] = []) => ({ id, data: { kind, stats } })

  it('puts each kind in its own column, left to right', () => {
    const pos = layoutColumns([mk('w', 'workorder'), mk('t', 'task'), mk('a', 'agent')], new Map())
    expect(pos.get('w')!.x).toBeLessThan(pos.get('t')!.x)
    expect(pos.get('t')!.x).toBeLessThan(pos.get('a')!.x)
  })

  it('stacks column members by rank and centres shorter columns vertically', () => {
    const nodes = [mk('w', 'workorder'), mk('t1', 'task'), mk('t2', 'task'), mk('t3', 'task')]
    const pos = layoutColumns(nodes, new Map([['t1', 2], ['t2', 0], ['t3', 1]]))
    expect(pos.get('t2')!.y).toBeLessThan(pos.get('t3')!.y)
    expect(pos.get('t3')!.y).toBeLessThan(pos.get('t1')!.y)
    // lone workorder sits vertically in the middle of the tall task column
    expect(pos.get('w')!.y).toBeGreaterThan(0)
    expect(pos.get('t2')!.y).toBe(0)
  })

  it('handles no nodes', () => {
    expect(layoutColumns([], new Map()).size).toBe(0)
  })
})

describe('buildWorkflow', () => {
  const crewData = () =>
    makeInteractions(
      [
        makeNode({ id: 'crew', kind: 'crew', label: 'Crew' }),
        makeNode({ id: 't-write', label: 'write', status: 'pending' }),
        makeNode({ id: 't-research', label: 'research', status: 'running', stats: { llm_calls: 2, tool_calls: 0, prompt_tokens: 5, completion_tokens: 5, cost_usd: 0.1, duration_ms: 1000 } }),
        makeNode({ id: 'ag', kind: 'agent', label: 'Analyst', status: 'done' }),
        makeNode({ id: 'tool', kind: 'tool', label: 'search' }),
        makeNode({ id: 'model', kind: 'model', label: 'gpt-x' }),
      ],
      [
        makeEdge({ id: 'r1', source: 'crew', target: 't-research', kind: 'runs' }),
        makeEdge({ id: 'r2', source: 'crew', target: 't-write', kind: 'runs' }),
        makeEdge({ id: 'c1', source: 't-research', target: 't-write', kind: 'context' }),
        makeEdge({ id: 'a1', source: 't-research', target: 'ag', kind: 'assigned' }),
        makeEdge({ id: 'tc', source: 'ag', target: 'tool', kind: 'tool_call', count: 4, cost_usd: 0 }),
        makeEdge({ id: 'lc', source: 'ag', target: 'model', kind: 'llm_call', count: 3, cost_usd: 0.3 }),
      ],
    )

  it('always adds the work order node with WO number, run status and no stats', () => {
    const { nodes } = buildWorkflow(input({ data: makeInteractions([]), runStatus: 'succeeded' }))
    expect(nodes).toHaveLength(1)
    expect(nodes[0].id).toBe(WO_NODE_ID)
    expect(nodes[0].data).toMatchObject({ kind: 'workorder', label: 'Vendor review', sublabel: 'WO-12', status: 'done', stats: [] })
  })

  it('builds no edges for an empty diagram', () => {
    expect(buildWorkflow(input({ data: makeInteractions([]) })).edges).toEqual([])
  })

  it('maps interaction nodes with kind sublabels, statuses and stats', () => {
    const { nodes } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
    const research = nodes.find((n) => n.id === 't-research')!
    expect(research.data).toMatchObject({ kind: 'task', label: 'research', sublabel: 'task', status: 'running', selected: false, dimmed: false })
    expect(research.data.stats).toContain('2 llm')
    expect(nodes.find((n) => n.id === 'ag')!.data.status).toBe('done')
    expect(nodes.every((n) => n.type === 'workflow')).toBe(true)
  })

  it('lays out tasks in run order down the task column', () => {
    const { nodes } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
    const y = (id: string) => nodes.find((n) => n.id === id)!.position.y
    expect(y('t-research')).toBeLessThan(y('t-write'))
    const x = (id: string) => nodes.find((n) => n.id === id)!.position.x
    expect(x(WO_NODE_ID)).toBeLessThan(x('crew'))
    expect(x('crew')).toBeLessThan(x('t-research'))
    expect(x('t-research')).toBeLessThan(x('ag'))
    expect(x('ag')).toBeLessThan(x('tool'))
    expect(x('tool')).toBe(x('model'))
  })

  it('connects the work order to the crew and the crew to the first task, dropping per-task runs edges', () => {
    const { edges } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
    const ids = edges.map((e) => e.id)
    expect(ids).toContain('wo->first')
    expect(edges.find((e) => e.id === 'wo->first')).toMatchObject({ source: WO_NODE_ID, target: 'crew', data: { kind: 'submits' } })
    expect(edges.find((e) => e.id === 'crew->first')).toMatchObject({ source: 'crew', target: 't-research', data: { kind: 'runs' } })
    expect(ids).not.toContain('r1')
    expect(ids).not.toContain('r2')
  })

  it('links the work order straight to the first task when there is no crew node', () => {
    const data = makeInteractions([makeNode({ id: 't2', label: 'b' }), makeNode({ id: 't1', label: 'a' })])
    const { edges } = buildWorkflow(input({ data, taskOrder: ['a', 'b'], live: true }))
    const first = edges.find((e) => e.id === 'wo->first')!
    expect(first.target).toBe('t1')
    expect(first.animated).toBe(true)
    expect(edges.some((e) => e.id === 'crew->first')).toBe(false)
  })

  it('does not animate the submit edge when a crew exists', () => {
    const { edges } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'], live: true }))
    expect(edges.find((e) => e.id === 'wo->first')!.animated).toBe(false)
    expect(edges.find((e) => e.id === 'crew->first')!.animated).toBe(true)
  })

  it('adds a vertical sequence edge between consecutive tasks', () => {
    const { edges } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'], live: true }))
    const seq = edges.find((e) => e.id === 'seq:t-research->t-write')!
    expect(seq).toMatchObject({ source: 't-research', target: 't-write', sourceHandle: 'bottom', targetHandle: 'top', data: { kind: 'sequence' } })
    // the second task is not running, so no animation
    expect(seq.animated).toBe(false)
  })

  it('animates the sequence edge into the running task only when live', () => {
    const data = makeInteractions([makeNode({ id: 'a', label: 'a', status: 'done' }), makeNode({ id: 'b', label: 'b', status: 'running' })])
    const live = buildWorkflow(input({ data, taskOrder: ['a', 'b'], live: true })).edges.find((e) => e.id === 'seq:a->b')!
    const idle = buildWorkflow(input({ data, taskOrder: ['a', 'b'], live: false })).edges.find((e) => e.id === 'seq:a->b')!
    expect(live.animated).toBe(true)
    expect(idle.animated).toBe(false)
  })

  it('draws context edges from the left handle with a dashed blue look', () => {
    const { edges } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
    const ctx = edges.find((e) => e.id === 'c1')!
    expect(ctx).toMatchObject({ sourceHandle: 'l-out', targetHandle: 'in', data: { kind: 'context' } })
    expect(ctx.style?.strokeDasharray).toBe(WORKFLOW_LOOKS.context.dash)
    expect(ctx.style?.stroke).toBe(WORKFLOW_LOOKS.context.color)
    expect(ctx.label).toBeUndefined()
  })

  it('draws delegation edges into the right handle with a count label and animation when live', () => {
    const data = makeInteractions(
      [makeNode({ id: 'm', kind: 'agent', label: 'Manager' }), makeNode({ id: 'w', kind: 'agent', label: 'Worker' })],
      [makeEdge({ id: 'd', source: 'm', target: 'w', kind: 'delegation', count: 2, cost_usd: 0.5 })],
    )
    const live = buildWorkflow(input({ data, live: true })).edges.find((e) => e.id === 'd')!
    expect(live).toMatchObject({ targetHandle: 'r-in', sourceHandle: 'out', label: '2× · $0.50', animated: true, data: { kind: 'delegation' } })
    expect(live.style?.strokeWidth).toBe(WORKFLOW_LOOKS.delegation.width)
    expect(buildWorkflow(input({ data, live: false })).edges.find((e) => e.id === 'd')!.animated).toBe(false)
  })

  it('labels tool and llm edges with count and cost only when there is cost', () => {
    const { edges } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
    expect(edges.find((e) => e.id === 'tc')!.label).toBe('4×')
    expect(edges.find((e) => e.id === 'lc')!.label).toBe('3× · $0.30')
  })

  it('animates assigned edges only from the running task while live', () => {
    const live = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'], live: true }))
    expect(live.edges.find((e) => e.id === 'a1')!.animated).toBe(true)
    const idle = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'], live: false }))
    expect(idle.edges.find((e) => e.id === 'a1')!.animated).toBe(false)
  })

  it('does not animate assigned edges from other tasks', () => {
    const data = makeInteractions(
      [makeNode({ id: 't1', label: 'a', status: 'done' }), makeNode({ id: 't2', label: 'b', status: 'running' }), makeNode({ id: 'ag', kind: 'agent', label: 'X' })],
      [makeEdge({ id: 'a1', source: 't1', target: 'ag', kind: 'assigned' })],
    )
    const { edges } = buildWorkflow(input({ data, taskOrder: ['a', 'b'], live: true }))
    expect(edges.find((e) => e.id === 'a1')!.animated).toBe(false)
  })

  it('gives every edge an arrow marker in its look colour', () => {
    const { edges } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
    for (const e of edges) {
      const kind = (e.data as { kind: keyof typeof WORKFLOW_LOOKS }).kind
      expect(e.markerEnd).toMatchObject({ color: WORKFLOW_LOOKS[kind].color })
    }
  })

  describe('selection', () => {
    it('marks the selected node and dims nodes not linked to it', () => {
      const { nodes } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'], selectedId: 'ag' }))
      const by = (id: string) => nodes.find((n) => n.id === id)!.data
      expect(by('ag')).toMatchObject({ selected: true, dimmed: false })
      // neighbours: t-research (assigned), tool and model
      expect(by('t-research').dimmed).toBe(false)
      expect(by('tool').dimmed).toBe(false)
      expect(by('model').dimmed).toBe(false)
      expect(by('t-write')).toMatchObject({ selected: false, dimmed: true })
      expect(by('crew').dimmed).toBe(true)
      expect(by(WO_NODE_ID).dimmed).toBe(true)
    })

    it('selecting the work order node dims everything else', () => {
      const { nodes } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'], selectedId: WO_NODE_ID }))
      expect(nodes.find((n) => n.id === WO_NODE_ID)!.data).toMatchObject({ selected: true, dimmed: false })
      expect(nodes.filter((n) => n.id !== WO_NODE_ID).every((n) => n.data.dimmed)).toBe(true)
    })

    it('dims nothing without a selection', () => {
      const { nodes } = buildWorkflow(input({ data: crewData(), taskOrder: ['research', 'write'] }))
      expect(nodes.some((n) => n.data.dimmed || n.data.selected)).toBe(false)
    })
  })
})
