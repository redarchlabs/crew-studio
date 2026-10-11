import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen } from '@testing-library/react'
import type { Interactions, Journal } from '../../../api/types'
import type { AsyncState } from '../../../lib/useAsync'
import { makeAttachment, makeEdge, makeEntry, makeInteractions, makeJournal, makeNode, makeRun, makeWorkOrder, renderRoute } from '../testUtils'
import { WorkflowTab } from './WorkflowTab'
import type { WorkflowFlowNode } from './workflowGraph'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())

// The canvas has its own tests; here it is a list of buttons so selection can be driven.
const canvasProps = vi.hoisted(() => ({ last: null as null | { nodes: { id: string; data: { label: string; selected: boolean; dimmed: boolean } }[]; edges: { id: string }[] } }))
vi.mock('./WorkflowCanvas', () => ({
  WorkflowCanvas: (props: { nodes: WorkflowFlowNode[]; edges: { id: string }[]; onSelect: (id: string | null) => void }) => {
    canvasProps.last = props
    return (
      <div data-testid="canvas">
        {props.nodes.map((n) => <button key={n.id} onClick={() => props.onSelect(n.id)}>node:{n.data.label}</button>)}
        <button onClick={() => props.onSelect(null)}>pane</button>
        <button onClick={() => props.onSelect('ghost')}>ghost</button>
      </div>
    )
  },
}))

afterEach(cleanup)
beforeEach(() => {
  canvasProps.last = null
})

function state<T>(over: Partial<AsyncState<T | undefined>> = {}): AsyncState<T | undefined> {
  return { data: undefined, error: undefined, loading: false, reload: vi.fn(), setData: vi.fn(), ...over }
}

const interactions: Interactions = makeInteractions(
  [makeNode({ id: 't1', label: 'research', status: 'done' }), makeNode({ id: 'ag', kind: 'agent', label: 'Analyst' })],
  [makeEdge({ id: 'e', source: 't1', target: 'ag' })],
)

const journal: Journal = makeJournal({
  tasks: [{ name: 'research', agent_role: 'Analyst', status: 'done', started_at: null, finished_at: null, output: null }],
  entries: [makeEntry({ seq: 1, agent_role: 'Analyst', task_name: 'research', kind: 'tool_call', title: 'search(web)', body: 'q=1' })],
})

type Props = Partial<Parameters<typeof WorkflowTab>[0]>
function renderTab(props: Props = {}, url = '/wo') {
  return renderRoute(
    <WorkflowTab
      workOrder={makeWorkOrder({ brief: 'The **brief**', inputs: { topic: 'AI', blank: '' }, attachments: [makeAttachment()], budget_usd: 5 })}
      run={makeRun({ budget_usd: 3 })}
      status="running"
      live
      interactions={state({ data: interactions })}
      journal={state({ data: journal })}
      canViewRuns
      {...props}
    />,
    { path: '/wo', url },
  )
}

describe('view switching', () => {
  it('defaults to the graph with its legend for people who can view runs', () => {
    renderTab()
    expect(screen.getByRole('tab', { name: /Graph/ }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByTestId('canvas')).toBeTruthy()
    expect(screen.getByText('run order')).toBeTruthy()
    expect(screen.getByText('delegation')).toBeTruthy()
    expect(screen.getByText('pending')).toBeTruthy()
  })

  it('offers only the sequence view without run permission', () => {
    renderTab({ canViewRuns: false }, '/wo?view=graph')
    expect(screen.queryByRole('tab', { name: /Graph/ })).toBeNull()
    expect(screen.getByRole('tab', { name: /Sequence/ }).getAttribute('aria-selected')).toBe('true')
    expect(screen.queryByTestId('canvas')).toBeNull()
    expect(screen.queryByText('run order')).toBeNull()
  })

  it('honours ?view=sequence and writes the choice to the URL', () => {
    const view = renderTab({}, '/wo?view=sequence')
    expect(screen.getByRole('tab', { name: /Sequence/ }).getAttribute('aria-selected')).toBe('true')
    expect(screen.getByText(/Time flows down/)).toBeTruthy()
    fireEvent.click(screen.getByRole('tab', { name: /Graph/ }))
    expect(view.location().search).toBe('?view=graph')
    expect(screen.getByTestId('canvas')).toBeTruthy()
  })
})

describe('graph view states', () => {
  it('shows loading', () => {
    renderTab({ interactions: state({ loading: true }) })
    expect(screen.getByText('Loading interaction graph…')).toBeTruthy()
  })

  it('shows an error with retry when nothing loaded', () => {
    const reload = vi.fn()
    renderTab({ interactions: state({ error: new Error('graph failed'), reload }) })
    expect(screen.getByText('graph failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(reload).toHaveBeenCalled()
  })

  it('keeps showing the graph if a refresh fails', () => {
    renderTab({ interactions: state({ data: interactions, error: new Error('refresh failed') }) })
    expect(screen.getByTestId('canvas')).toBeTruthy()
    expect(screen.queryByText('refresh failed')).toBeNull()
  })

  it('shows an empty state when only the work order node exists', () => {
    renderTab({ interactions: state({ data: makeInteractions([]) }) })
    expect(screen.getByText('No interactions yet')).toBeTruthy()
    expect(screen.queryByTestId('canvas')).toBeNull()
  })

  it('renders nothing before data when not loading', () => {
    renderTab({ interactions: state() })
    expect(screen.queryByTestId('canvas')).toBeNull()
    expect(screen.queryByText('No interactions yet')).toBeNull()
  })

  it('builds the flow from interactions, with the work order first', () => {
    renderTab()
    expect(canvasProps.last!.nodes.map((n) => n.id)).toEqual(['workorder', 't1', 'ag'])
    expect(canvasProps.last!.nodes[0].data.label).toBe('Quarterly review')
  })
})

describe('selection panel', () => {
  it('shows node details when a node is selected and closes again', () => {
    renderTab()
    expect(screen.queryByRole('complementary')).toBeNull()
    fireEvent.click(screen.getByText('node:Analyst'))
    expect(screen.getByRole('complementary')).toBeTruthy()
    expect(screen.getByText('agent')).toBeTruthy()
    expect(screen.getByText('Journal (1)')).toBeTruthy()
    expect(canvasProps.last!.nodes.find((n) => n.id === 'ag')!.data.selected).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Close details' }))
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('dims other nodes while one is selected', () => {
    renderTab()
    fireEvent.click(screen.getByText('node:research'))
    expect(canvasProps.last!.nodes.find((n) => n.id === 'workorder')!.data.dimmed).toBe(true)
  })

  it('selecting the pane clears the selection', () => {
    renderTab()
    fireEvent.click(screen.getByText('node:research'))
    fireEvent.click(screen.getByText('pane'))
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('shows the work order summary for the work order node, using the attempt budget and inputs', () => {
    renderTab({ run: makeRun({ budget_usd: 3, inputs: { topic: 'attempt value' } }) })
    fireEvent.click(screen.getByText('node:Quarterly review'))
    expect(screen.getByText('WO-7')).toBeTruthy()
    expect(screen.getByText('brief').tagName).toBe('STRONG')
    expect(screen.getByText('1 input files · budget $3.00')).toBeTruthy()
    expect(screen.getByText('attempt value')).toBeTruthy()
  })

  it('falls back to the work order budget and inputs without a run', () => {
    renderTab({ run: null })
    fireEvent.click(screen.getByText('node:Quarterly review'))
    expect(screen.getByText('1 input files · budget $5.00')).toBeTruthy()
    expect(screen.getByText('AI')).toBeTruthy()
    expect(screen.getByText('""')).toBeTruthy()
  })

  it('handles a work order without brief, inputs or files', () => {
    renderTab({ run: null, workOrder: makeWorkOrder({ brief: ' ', inputs: {}, attachments: undefined, budget_usd: null }) })
    fireEvent.click(screen.getByText('node:Quarterly review'))
    expect(screen.getByText('No brief.')).toBeTruthy()
    expect(screen.getByText('0 input files · budget —')).toBeTruthy()
    expect(screen.queryByRole('term')).toBeNull()
  })

  it('renders nothing for a node that no longer exists', () => {
    renderTab()
    fireEvent.click(screen.getByText('ghost'))
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('works with no journal loaded yet', () => {
    renderTab({ journal: state() })
    fireEvent.click(screen.getByText('node:Analyst'))
    expect(screen.getByText('Journal (0)')).toBeTruthy()
  })
})

describe('sequence view', () => {
  const seqUrl = '/wo?view=sequence'

  it('shows loading and error states', () => {
    const { unmount } = renderTab({ journal: state({ loading: true }) }, seqUrl)
    expect(screen.getByText('Loading journal…')).toBeTruthy()
    unmount()
    const reload = vi.fn()
    renderTab({ journal: state({ error: new Error('journal failed'), reload }) }, seqUrl)
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(reload).toHaveBeenCalled()
    expect(screen.getByText('journal failed')).toBeTruthy()
  })

  it('shows nothing before the journal arrives when not loading', () => {
    renderTab({ journal: state() }, seqUrl)
    expect(screen.queryByText(/Time flows down/)).toBeNull()
  })

  it('explains an empty journal, with a live hint only while live', () => {
    const { unmount } = renderTab({ journal: state({ data: makeJournal() }) }, seqUrl)
    expect(screen.getByText('Nothing has happened yet')).toBeTruthy()
    expect(screen.getByText('Entries appear as agents start working.')).toBeTruthy()
    unmount()
    renderTab({ journal: state({ data: makeJournal() }), live: false }, seqUrl)
    expect(screen.queryByText('Entries appear as agents start working.')).toBeNull()
  })

  it('shows entry details when a chip is clicked', () => {
    renderTab({}, seqUrl)
    fireEvent.click(screen.getByRole('button', { name: /search\(web\)/ }))
    expect(screen.getByRole('complementary')).toBeTruthy()
    expect(screen.getByText('q=1')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Close details' }))
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('works for people without run permission', () => {
    renderTab({ canViewRuns: false, interactions: state() })
    expect(screen.getByText(/Time flows down/)).toBeTruthy()
  })
})

describe('task order', () => {
  it('takes the run order from the journal tasks', () => {
    const j = makeJournal({ tasks: ['b', 'a'].map((name) => ({ name, agent_role: '', status: 'done' as const, started_at: null, finished_at: null, output: null })) })
    const data = makeInteractions([makeNode({ id: 'ta', label: 'a' }), makeNode({ id: 'tb', label: 'b' })])
    renderTab({ journal: state({ data: j }), interactions: state({ data }) })
    const y = (id: string) => (canvasProps.last!.nodes as unknown as { id: string; position: { y: number } }[]).find((n) => n.id === id)!.position.y
    expect(y('tb')).toBeLessThan(y('ta'))
  })

  it('falls back to the spec tasks of the run', () => {
    const run = makeRun({ spec: { tasks: [{ name: 'b' }, { name: 'a' }] } as never })
    const data = makeInteractions([makeNode({ id: 'ta', label: 'a' }), makeNode({ id: 'tb', label: 'b' })])
    renderTab({ journal: state(), interactions: state({ data }), run })
    const y = (id: string) => (canvasProps.last!.nodes as unknown as { id: string; position: { y: number } }[]).find((n) => n.id === id)!.position.y
    expect(y('tb')).toBeLessThan(y('ta'))
  })
})
