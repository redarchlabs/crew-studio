import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { LlmCall } from '../../../api/types'
import { LlmCallsContext, type LlmCallsResource } from '../../runs/useLlmCalls'
import { makeEntry, makeNode, ZERO_STATS } from '../testUtils'
import { EntryDetails, EntryTitle, entriesForNode, NodeDetails, PanelShell } from './DetailPanel'

afterEach(cleanup)

const entries = [
  makeEntry({ seq: 1, agent_role: 'Analyst', task_name: 'research', kind: 'llm_call', model: 'gpt-x', title: 'Analyst thinks' }),
  makeEntry({ seq: 2, agent_role: 'Analyst', task_name: 'research', kind: 'tool_call', title: 'search(web)', body: 'query=cats' }),
  makeEntry({ seq: 3, agent_role: 'Writer', task_name: 'write', kind: 'tool_result', title: 'done', body: 'used web search' }),
  makeEntry({ seq: 4, agent_role: 'Manager', task_name: 'write', kind: 'delegation', title: 'Delegated to Analyst' }),
  makeEntry({ seq: 5, agent_role: '', task_name: '', kind: 'run', title: 'Run started' }),
  makeEntry({ seq: 6, agent_role: 'Writer', task_name: 'write', kind: 'search', title: 'lookup', body: 'tool: calc' }),
  makeEntry({ seq: 7, agent_role: 'Writer', task_name: 'write', kind: 'llm_call', model: 'gpt-y', title: 'Writer thinks' }),
]
const seqs = (list: { seq: number }[]) => list.map((e) => e.seq)

describe('entriesForNode', () => {
  it('agent: its own entries plus delegations naming it', () => {
    expect(seqs(entriesForNode({ kind: 'agent', label: 'Analyst' }, entries))).toEqual([1, 2, 4])
  })

  it('task: entries of that task', () => {
    expect(seqs(entriesForNode({ kind: 'task', label: 'research' }, entries))).toEqual([1, 2])
  })

  it('tool: tool-ish entries mentioning the tool in title or body', () => {
    expect(seqs(entriesForNode({ kind: 'tool', label: 'web' }, entries))).toEqual([2, 3])
    expect(seqs(entriesForNode({ kind: 'tool', label: 'calc' }, entries))).toEqual([6])
    // llm calls and delegations never count as tool entries
    expect(seqs(entriesForNode({ kind: 'tool', label: 'Analyst' }, entries))).toEqual([])
  })

  it('tool: matches the display name against the sanitized one CrewAI reports', () => {
    const mixed = [
      makeEntry({ seq: 1, kind: 'tool_call', title: 'Analyst used google_search', body: '' }),
      makeEntry({ seq: 2, kind: 'search', title: 'Google Search', body: 'query' }),
    ]
    expect(seqs(entriesForNode({ kind: 'tool', label: 'Google Search' }, mixed))).toEqual([1, 2])
  })

  it('model: entries recorded with that model', () => {
    expect(seqs(entriesForNode({ kind: 'model', label: 'gpt-y' }, entries))).toEqual([7])
  })

  it('crew: entries not owned by an agent', () => {
    expect(seqs(entriesForNode({ kind: 'crew', label: 'x' }, entries))).toEqual([5])
  })

  it('returns nothing for no entries', () => {
    expect(entriesForNode({ kind: 'agent', label: 'A' }, [])).toEqual([])
  })
})

describe('NodeDetails', () => {
  const node = makeNode({
    id: 'ag',
    kind: 'agent',
    label: 'Analyst',
    status: 'running',
    stats: { llm_calls: 3, tool_calls: 2, prompt_tokens: 1500, completion_tokens: 500, cost_usd: 0.25, duration_ms: 65_000 },
  })

  it('shows stats and the related journal entries', () => {
    render(<NodeDetails node={node} entries={entries} />)
    expect(screen.getByText('LLM calls').nextSibling?.textContent).toBe('3')
    expect(screen.getByText('Tool calls').nextSibling?.textContent).toBe('2')
    expect(screen.getByText('$0.25')).toBeTruthy()
    expect(screen.getByText('1m 5s')).toBeTruthy()
    expect(screen.getByText('1,500 in · 500 out')).toBeTruthy()
    expect(screen.getByText('running')).toBeTruthy()
    expect(screen.getByText('Journal (3)')).toBeTruthy()
    expect(screen.getByText('Analyst thinks')).toBeTruthy()
    expect(screen.getByText('Delegated to Analyst')).toBeTruthy()
  })

  it('omits the status stat when unknown and says when there are no entries', () => {
    render(<NodeDetails node={makeNode({ id: 't', kind: 'task', label: 'nothing yet', stats: ZERO_STATS })} entries={entries} />)
    expect(screen.queryByText('Status')).toBeNull()
    expect(screen.getByText('Journal (0)')).toBeTruthy()
    expect(screen.getByText('No journal entries for this task yet.')).toBeTruthy()
  })

  it('caps the list at 200 entries and points to the Journal tab', () => {
    const many = Array.from({ length: 205 }, (_, i) => makeEntry({ seq: 1000 + i, agent_role: 'Analyst', title: `e${i}` }))
    render(<NodeDetails node={node} entries={many} />)
    expect(screen.getByText('Journal (205)')).toBeTruthy()
    expect(screen.getByText('e199')).toBeTruthy()
    expect(screen.queryByText('e200')).toBeNull()
    expect(screen.getByText(/Showing the first 200/)).toBeTruthy()
  })
})

describe('EntryDetails', () => {
  it('shows who, what and the body', () => {
    render(<EntryDetails entry={makeEntry({ agent_role: 'Analyst', task_name: 'research', kind: 'tool_call', body: 'query=cats', tokens: 1200, cost_usd: 0.5, model: 'gpt-x' })} />)
    expect(screen.getByText('Analyst')).toBeTruthy()
    expect(screen.getByText('· research')).toBeTruthy()
    expect(screen.getByText('tool call')).toBeTruthy()
    expect(screen.getByText('gpt-x')).toBeTruthy()
    expect(screen.getByText('1,200')).toBeTruthy()
    expect(screen.getByText('$0.50')).toBeTruthy()
    expect(screen.getByText('query=cats')).toBeTruthy()
  })

  it('labels crew-level entries and hides absent figures', () => {
    render(<EntryDetails entry={makeEntry({ agent_role: '', kind: 'run', body: '' })} />)
    expect(screen.getByText('Crew')).toBeTruthy()
    expect(screen.queryByText('Model')).toBeNull()
    expect(screen.queryByText('Tokens')).toBeNull()
    expect(screen.queryByText('Cost')).toBeNull()
    expect(screen.queryByText('Full prompt and response')).toBeNull()
  })

  it('falls back to the raw kind for unknown kinds', () => {
    render(<EntryDetails entry={makeEntry({ kind: 'mystery' as never, body: 'b' })} />)
    expect(screen.getByText('mystery')).toBeTruthy()
  })

  it('loads the full prompt of an llm_call entry', () => {
    const call: LlmCall = { seq: 42, agent_role: 'A', task_name: 't', model: 'm', usage: null, cost_usd: 0, messages: [{ role: 'user', content: 'Hello model' }], response: 'Hello human', created_at: '2026-10-03T10:00:00Z' }
    const load = vi.fn()
    const ctx: LlmCallsResource = { calls: [call], loading: false, error: undefined, load }
    render(
      <LlmCallsContext.Provider value={ctx}>
        <EntryDetails entry={makeEntry({ seq: 42, kind: 'llm_call', agent_role: 'A' })} />
      </LlmCallsContext.Provider>,
    )
    expect(screen.getByText('Full prompt and response')).toBeTruthy()
    expect(load).toHaveBeenCalledWith({ wantSeq: 42 })
    expect(screen.getByText('Hello human')).toBeTruthy()
  })
})

describe('EntryTitle / PanelShell', () => {
  it('shows the title with a tooltip', () => {
    render(<EntryTitle entry={makeEntry({ kind: 'tool_call', title: 'search(web)' })} />)
    expect(screen.getByTitle('search(web)').textContent).toBe('search(web)')
  })

  it('renders a closable shell', () => {
    const onClose = vi.fn()
    render(<PanelShell title="Details" onClose={onClose}><p>Body</p></PanelShell>)
    expect(screen.getByText('Details')).toBeTruthy()
    expect(screen.getByText('Body')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Close details' }))
    expect(onClose).toHaveBeenCalled()
  })
})
