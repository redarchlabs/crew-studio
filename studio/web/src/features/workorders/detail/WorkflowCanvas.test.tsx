import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { ThemeProvider } from '../../../lib/theme'
import { installReactFlowStubs } from '../reactFlowStubs'
import { makeEdge, makeInteractions, makeNode } from '../testUtils'
import { WorkflowCanvas } from './WorkflowCanvas'
import { buildWorkflow } from './workflowGraph'

beforeAll(() => installReactFlowStubs())
afterEach(cleanup)

function flow(selectedId: string | null = null) {
  const data = makeInteractions(
    [
      makeNode({ id: 'crew', kind: 'crew', label: 'Crew A', status: 'running' }),
      makeNode({ id: 't1', label: 'research', status: 'done', stats: { llm_calls: 2, tool_calls: 1, prompt_tokens: 100, completion_tokens: 50, cost_usd: 0.1, duration_ms: 1000 } }),
      makeNode({ id: 't2', label: 'write', status: 'pending' }),
      makeNode({ id: 'ag', kind: 'agent', label: 'Analyst', status: 'failed' }),
      makeNode({ id: 'tool', kind: 'tool', label: 'search' }),
      makeNode({ id: 'model', kind: 'model', label: 'gpt-x' }),
    ],
    [
      makeEdge({ id: 'a', source: 't1', target: 'ag', kind: 'assigned' }),
      makeEdge({ id: 'b', source: 'ag', target: 'tool', kind: 'tool_call', count: 2 }),
      makeEdge({ id: 'c', source: 'ag', target: 'model', kind: 'llm_call', count: 3 }),
    ],
  )
  return buildWorkflow({ data, workOrder: { number: 3, title: 'Review' }, runStatus: 'running', taskOrder: ['research', 'write'], live: true, selectedId })
}

const renderCanvas = (onSelect = vi.fn(), selectedId: string | null = null) => {
  const { nodes, edges } = flow(selectedId)
  const view = render(<ThemeProvider><WorkflowCanvas nodes={nodes} edges={edges} onSelect={onSelect} /></ThemeProvider>)
  return { ...view, onSelect }
}

describe('WorkflowCanvas', () => {
  it('renders a node card for each graph node with label, kind and stats', () => {
    renderCanvas()
    for (const label of ['Review', 'Crew A', 'research', 'write', 'Analyst', 'search', 'gpt-x']) expect(screen.getByText(label)).toBeTruthy()
    expect(screen.getByText('WO-3')).toBeTruthy()
    expect(screen.getByText('2 llm')).toBeTruthy()
    expect(screen.getByText('1 tool')).toBeTruthy()
  })

  it('shows a status dot with the status as its tooltip', () => {
    renderCanvas()
    expect(screen.getAllByTitle('running').length).toBeGreaterThan(0)
    expect(screen.getAllByTitle('done')).toHaveLength(1)
    expect(screen.getAllByTitle('failed')).toHaveLength(1)
    expect(screen.getAllByTitle('pending')).toHaveLength(1)
  })

  it('selects a node when clicked', () => {
    const { onSelect } = renderCanvas()
    fireEvent.click(screen.getByText('Analyst'))
    expect(onSelect).toHaveBeenCalledWith('ag')
  })

  it('clears the selection when the background is clicked', () => {
    const { onSelect, container } = renderCanvas()
    fireEvent.click(container.querySelector('.react-flow__pane')!)
    expect(onSelect).toHaveBeenCalledWith(null)
  })

  it('outlines the selected node and fades the rest', () => {
    renderCanvas(vi.fn(), 'ag')
    const card = (label: string) => screen.getByText(label).closest('div.w-\\[210px\\]') as HTMLElement
    expect(card('Analyst').className).toContain('outline-accent')
    expect(card('write').className).toContain('opacity-35')
    expect(card('research').className).not.toContain('opacity-35')
  })

  it('draws the controls', () => {
    const { container } = renderCanvas()
    expect(container.querySelector('.react-flow__controls')).toBeTruthy()
  })
})
