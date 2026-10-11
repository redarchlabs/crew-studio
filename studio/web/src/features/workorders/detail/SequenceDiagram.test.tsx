import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { JournalAgent } from '../../../api/types'
import { makeEntry, makeJournal } from '../testUtils'
import { SequenceDiagram } from './SequenceDiagram'

afterEach(cleanup)

const agent = (role: string): JournalAgent => ({ role, tasks: [], entries: 0, llm_calls: 0, tool_calls: 0, prompt_tokens: 0, completion_tokens: 0, cost_usd: 0, first_at: null, last_at: null })

function journal() {
  return makeJournal({
    agents: [agent('Analyst'), agent('Writer')],
    tasks: [
      { name: 'research', agent_role: 'Analyst', status: 'done', started_at: null, finished_at: null, output: null },
      { name: 'write', agent_role: 'Writer', status: 'running', started_at: null, finished_at: null, output: null },
    ],
    entries: [
      makeEntry({ seq: 1, kind: 'run', title: 'Run started', agent_role: '' }),
      makeEntry({ seq: 2, kind: 'task_started', title: 'Research begins', agent_role: 'Analyst', task_name: 'research' }),
      makeEntry({ seq: 3, kind: 'llm_call', title: 'Thinking hard', agent_role: 'Analyst', task_name: 'research', tokens: 2500, cost_usd: 0.04 }),
      makeEntry({ seq: 4, kind: 'delegation', title: 'Delegated to Writer', agent_role: 'Analyst', task_name: 'research' }),
      makeEntry({ seq: 5, kind: 'tool_call', title: 'search(web)', agent_role: 'Writer', task_name: 'write' }),
    ],
  })
}

describe('SequenceDiagram', () => {
  it('draws a lane header per agent plus the crew, and a chip per entry', () => {
    render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    for (const lane of ['Crew', 'Analyst', 'Writer']) expect(screen.getAllByTitle(lane).length).toBeGreaterThan(0)
    for (const title of ['Run started', 'Research begins', 'Thinking hard', 'Delegated to Writer', 'search(web)']) expect(screen.getByRole('button', { name: new RegExp(title.replace(/[()]/g, '\\$&')) })).toBeTruthy()
    expect(screen.getByText('5/5 entries')).toBeTruthy()
  })

  it('shows token and cost meta on llm calls only', () => {
    render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    expect(screen.getByText(/^[\d.,]+[A-Za-z]*t \$0\.04$/)).toBeTruthy()
  })

  it('draws task bands with their status', () => {
    render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    expect(screen.getByText('research · done')).toBeTruthy()
    expect(screen.getByText('write · running')).toBeTruthy()
  })

  it('draws a delegation arrow between lanes', () => {
    const { container } = render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    expect(container.querySelectorAll('svg line[marker-end]')).toHaveLength(1)
  })

  it('selects an entry on click and marks the selected one', () => {
    const onSelect = vi.fn()
    const j = journal()
    const { rerender } = render(<SequenceDiagram journal={j} selectedSeq={null} onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button', { name: /search\(web\)/ }))
    expect(onSelect).toHaveBeenCalledWith(j.entries[4])
    rerender(<SequenceDiagram journal={j} selectedSeq={5} onSelect={onSelect} />)
    expect(screen.getByRole('button', { name: /search\(web\)/ }).className).toContain('outline-accent')
    expect(screen.getByRole('button', { name: /Run started/ }).className).not.toContain('outline-accent')
  })

  it('filters entry kinds with the toggles and restores them', () => {
    render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    const llm = screen.getByRole('button', { name: 'LLM calls' })
    expect(llm.getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(llm)
    expect(llm.getAttribute('aria-pressed')).toBe('false')
    expect(screen.queryByRole('button', { name: /Thinking hard/ })).toBeNull()
    expect(screen.getByText('4/5 entries')).toBeTruthy()
    fireEvent.click(llm)
    expect(screen.getByRole('button', { name: /Thinking hard/ })).toBeTruthy()
    expect(screen.getByText('5/5 entries')).toBeTruthy()
  })

  it('hides a whole group of kinds at once', () => {
    render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: 'Tools' }))
    expect(screen.queryByRole('button', { name: /search\(web\)/ })).toBeNull()
  })

  it('handles an empty journal', () => {
    render(<SequenceDiagram journal={makeJournal()} selectedSeq={null} onSelect={vi.fn()} />)
    expect(screen.getByText('0/0 entries')).toBeTruthy()
    expect(screen.getAllByText('Crew').length).toBeGreaterThan(0)
  })

  it('pages very long journals with Show more', () => {
    const many = makeJournal({ entries: Array.from({ length: 1600 }, (_, i) => makeEntry({ seq: 10_000 + i, kind: 'run', title: `row ${i}` })) })
    render(<SequenceDiagram journal={many} selectedSeq={null} onSelect={vi.fn()} />)
    expect(screen.getByText('1500/1600 entries')).toBeTruthy()
    expect(screen.queryByText('row 1599')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Show more (100 left)' }))
    expect(screen.getByText('1600/1600 entries')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Show more/ })).toBeNull()
  }, 60_000)

  it('includes a legend', () => {
    render(<SequenceDiagram journal={journal()} selectedSeq={null} onSelect={vi.fn()} />)
    expect(screen.getByText(/Time flows down/)).toBeTruthy()
    expect(screen.getByText('task (done)')).toBeTruthy()
  })
})
