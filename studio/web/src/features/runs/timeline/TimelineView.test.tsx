import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { RunEvent } from '../../../api/types'
import { makeEvent, makeLlmCall } from '../testUtils'
import { LlmCallsContext, type LlmCallsResource } from '../useLlmCalls'
import { TimelineItemRow } from './TimelineItemRow'
import { FocusChip, TimelineFilters, TimelineList } from './TimelineView'
import { initialTimeline, timelineReducer, type TimelineFilter, type TimelineItem } from './timelineReducer'

vi.mock('../../../components/Markdown', () => ({ Markdown: ({ children }: { children: string }) => <div data-testid="md">{children}</div> }))

afterEach(cleanup)

const NONE: TimelineFilter = { hidden: new Set(), agent: null, focus: null }
const stateOf = (events: RunEvent[]) => timelineReducer(initialTimeline, { type: 'append', events })

function itemOf(event: RunEvent, result: RunEvent | null = null): TimelineItem {
  return { seq: event.seq, event, result, category: 'crew', agents: [], task: null }
}

function withCalls(value: Partial<LlmCallsResource>, ui: React.ReactElement) {
  const ctx: LlmCallsResource = { calls: undefined, loading: false, error: undefined, load: vi.fn(), ...value }
  render(<LlmCallsContext.Provider value={ctx}>{ui}</LlmCallsContext.Provider>)
  return ctx
}

const row = (event: RunEvent, result: RunEvent | null = null) => render(<ol><TimelineItemRow item={itemOf(event, result)} /></ol>)

describe('TimelineList', () => {
  const events = [
    makeEvent(1, 'crew_started', { crew_name: 'Crew' }),
    makeEvent(2, 'task_started', { task_name: 'Research', agent_role: 'Analyst' }),
    makeEvent(3, 'tool_started', { task_name: 'Research', agent_role: 'Analyst', tool_name: 'web' }),
    makeEvent(4, 'task_completed', { task_name: 'Research', agent_role: 'Analyst', output: 'done' }),
    makeEvent(5, 'crew_completed', { cost_usd: 0.5, output: 'final' }),
  ]

  it('shows an empty state without events', () => {
    render(<TimelineList state={initialTimeline} filter={NONE} />)
    expect(screen.getByText('No events yet')).toBeTruthy()
  })

  it('renders groups in order with titles, status and event counts', () => {
    const { container } = render(<TimelineList state={stateOf(events)} filter={NONE} />)
    const titles = [...container.querySelectorAll('section')].map((s) => s.querySelector('header span:nth-of-type(2)')?.textContent)
    expect(titles).toEqual(['Crew start', 'Research', 'Crew outcome'])
    expect(screen.getByText('done')).toBeTruthy()
    expect(screen.getByText('3 ev')).toBeTruthy()
    expect(screen.getAllByText('Analyst').length).toBeGreaterThan(0)
  })

  it('shows running and failed task statuses', () => {
    render(<TimelineList state={stateOf([makeEvent(1, 'task_started', { task_name: 'A' }), makeEvent(2, 'task_started', { task_name: 'B' }), makeEvent(3, 'task_failed', { task_name: 'B', error: 'x' })])} filter={NONE} />)
    expect(screen.getByText('running')).toBeTruthy()
    expect(screen.getByText('failed')).toBeTruthy()
  })

  it('says nothing matches when filters hide everything', () => {
    render(<TimelineList state={stateOf(events)} filter={{ ...NONE, agent: 'Nobody' }} />)
    expect(screen.getByText('Nothing matches these filters')).toBeTruthy()
  })

  it('hides categories', () => {
    render(<TimelineList state={stateOf(events)} filter={{ ...NONE, hidden: new Set(['crew']) }} />)
    expect(screen.queryByText('Crew start')).toBeNull()
    expect(screen.getByText('Research')).toBeTruthy()
  })

  it('applies a focus', () => {
    const { container } = render(<TimelineList state={stateOf(events)} filter={{ ...NONE, focus: { kind: 'tool', value: 'web' } }} />)
    expect(container.querySelectorAll('section')).toHaveLength(1)
  })
})

describe('TimelineFilters', () => {
  it('toggles categories on and off', () => {
    const onChange = vi.fn()
    const { rerender } = render(<TimelineFilters agents={[]} filter={NONE} onChange={onChange} />)
    const chip = screen.getByText('llm')
    expect(chip.getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(chip)
    expect([...onChange.mock.calls[0][0].hidden]).toEqual(['llm'])
    rerender(<TimelineFilters agents={[]} filter={{ ...NONE, hidden: new Set(['llm']) }} onChange={onChange} />)
    expect(screen.getByText('llm').getAttribute('aria-pressed')).toBe('false')
    fireEvent.click(screen.getByText('llm'))
    expect([...onChange.mock.calls[1][0].hidden]).toEqual([])
  })

  it('does not render agents when there are none', () => {
    render(<TimelineFilters agents={[]} filter={NONE} onChange={() => {}} />)
    expect(screen.queryByText('all')).toBeNull()
  })

  it('selects, deselects and clears an agent', () => {
    const onChange = vi.fn()
    const { rerender } = render(<TimelineFilters agents={['A', 'B']} filter={NONE} onChange={onChange} />)
    fireEvent.click(screen.getByText('B'))
    expect(onChange).toHaveBeenLastCalledWith({ ...NONE, agent: 'B' })
    rerender(<TimelineFilters agents={['A', 'B']} filter={{ ...NONE, agent: 'B' }} onChange={onChange} />)
    fireEvent.click(screen.getByText('B'))
    expect(onChange).toHaveBeenLastCalledWith({ ...NONE, agent: null })
    fireEvent.click(screen.getByText('A'))
    expect(onChange).toHaveBeenLastCalledWith({ ...NONE, agent: 'A' })
    fireEvent.click(screen.getByText('all'))
    expect(onChange).toHaveBeenLastCalledWith({ ...NONE, agent: null })
  })

  it('shows and clears the focus', () => {
    const onChange = vi.fn()
    render(<TimelineFilters agents={[]} filter={{ ...NONE, focus: { kind: 'agent', value: 'X' } }} onChange={onChange} />)
    expect(screen.getByText('agent:')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Clear focus'))
    expect(onChange).toHaveBeenCalledWith({ ...NONE, focus: null })
  })
})

describe('FocusChip', () => {
  it('shows kind and value and calls onClear', () => {
    const onClear = vi.fn()
    render(<FocusChip focus={{ kind: 'task', value: 'Research' }} onClear={onClear} />)
    expect(screen.getByText('Research')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Clear focus'))
    expect(onClear).toHaveBeenCalled()
  })
})

describe('TimelineItemRow summaries', () => {
  it('run_status with and without error', () => {
    row(makeEvent(1, 'run_status', { status: 'failed', error: 'oops' }))
    expect(screen.getByText('failed')).toBeTruthy()
    expect(screen.getByText(/oops/)).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'run_status', { status: 'running' }))
    expect(screen.getByText('running')).toBeTruthy()
    expect(screen.queryByText(/—/)).toBeNull()
  })

  it('crew_started, task_started, agent_started', () => {
    row(makeEvent(1, 'crew_started', { crew_name: 'Alpha' }))
    expect(screen.getByText('Alpha')).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'task_started', { agent_role: 'Writer' }))
    expect(screen.getByText('Writer')).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'agent_started', {}))
    expect(screen.getByText('agent')).toBeTruthy()
  })

  it('task_failed, crew_failed with and without a budget stop', () => {
    row(makeEvent(1, 'task_failed', { error: 'bad' }))
    expect(screen.getByText('Task failed: bad')).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'crew_failed', { error: 'bad', budget_stopped: true }))
    expect(screen.getByText('Crew failed (budget stop): bad')).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'crew_failed', { error: 'bad' }))
    expect(screen.getByText('Crew failed: bad')).toBeTruthy()
  })

  it('budget variants', () => {
    row(makeEvent(1, 'budget', { state: 'hard_stopped', spent_usd: 2, budget_usd: 1 }))
    expect(screen.getByText('Budget hard stop — LLM calls halted at $2.00 of $1.00')).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'budget', { state: 'searches_stopped', spent_usd: 0.9, budget_usd: 1 }))
    expect(screen.getByText(/searches stopped at \$0.90 of \$1.00/)).toBeTruthy()
  })

  it('artifacts success (plural, singular, error)', () => {
    row(makeEvent(1, 'artifacts', { files: 3, bytes: 2048 }))
    expect(screen.getByText(/file/).textContent).toContain('files')
    expect(screen.getByText('2.0 KB')).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'artifacts', { files: 1, bytes: 10 }))
    expect(screen.getByText(/file the agents wrote/)).toBeTruthy()
    cleanup()
    row(makeEvent(1, 'artifacts', { error: 'disk full' }))
    expect(screen.getByText('Saving files failed: disk full')).toBeTruthy()
  })

  it('unknown event types fall back to their name with a JSON detail', () => {
    row(makeEvent(1, 'tool_finished', { tool_name: 'x', output: 'o' }))
    expect(screen.getByText('tool_finished')).toBeTruthy()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText(/"tool_name": "x"/)).toBeTruthy()
  })

  it('has no button role when an event has no detail', () => {
    row(makeEvent(1, 'crew_started', { crew_name: 'A' }))
    expect(screen.queryByRole('button')).toBeNull()
  })
})

describe('TimelineItemRow details', () => {
  it('toggles tool_started detail by click and by keyboard, ignoring other keys', () => {
    row(makeEvent(1, 'tool_started', { agent_role: 'A', tool_name: 'web', tool_args: { q: 'cats' } }))
    const btn = screen.getByRole('button')
    expect(btn.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText('Arguments')).toBeNull()
    fireEvent.click(btn)
    expect(screen.getByText('Arguments')).toBeTruthy()
    expect(screen.getByText(/"q": "cats"/)).toBeTruthy()
    fireEvent.keyDown(btn, { key: 'Tab' })
    expect(screen.getByText('Arguments')).toBeTruthy()
    fireEvent.keyDown(btn, { key: 'Enter' })
    expect(screen.queryByText('Arguments')).toBeNull()
    fireEvent.keyDown(btn, { key: ' ' })
    expect(screen.getByText('Arguments')).toBeTruthy()
  })

  it('shows a spinner while a tool is pending, then done with output', () => {
    const start = makeEvent(1, 'tool_started', { agent_role: 'A', tool_name: 'web' })
    const { container } = row(start)
    expect(container.querySelector('.animate-spin')).toBeTruthy()
    cleanup()
    row(start, makeEvent(2, 'tool_finished', { output: 'result text' }))
    expect(screen.getByText('done')).toBeTruthy()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('Output')).toBeTruthy()
    expect(screen.getByText('result text')).toBeTruthy()
  })

  it('shows a failed tool with its error', () => {
    row(makeEvent(1, 'tool_started', { agent_role: 'A', tool_name: 'web' }), makeEvent(2, 'tool_error', { error: 'timeout' }))
    expect(screen.getByText('failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('Error')).toBeTruthy()
    expect(screen.getByText('timeout')).toBeTruthy()
  })

  it('shows empty output when the result has none', () => {
    row(makeEvent(1, 'tool_started', { tool_name: 'web' }), makeEvent(2, 'tool_finished', {}))
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('Output')).toBeTruthy()
  })

  it('search shows query, grounding, cost and a JSON detail', () => {
    row(makeEvent(1, 'search', { agent_role: 'A', query: 'q'.repeat(120), grounded: true, cost_usd: 0.002 }))
    expect(screen.getByText(/grounded · \$0\.0020/)).toBeTruthy()
    expect(screen.getByText(/q{89}…/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText(/"grounded": true/)).toBeTruthy()
  })

  it('search without grounding omits the grounded label', () => {
    row(makeEvent(1, 'search', { query: 'hi', cost_usd: 0.5 }))
    expect(screen.queryByText(/grounded/)).toBeNull()
  })

  it('video shows the file, length, resolution and cost', () => {
    row(makeEvent(1, 'video', { agent_role: 'Video Producer', file: 'videos/scene-01.mp4', made: true, duration_seconds: 8, resolution: '720p', cost_usd: 0.81 }))
    expect(screen.getByText('videos/scene-01.mp4')).toBeTruthy()
    expect(screen.getByText(/made video/)).toBeTruthy()
    expect(screen.getByText(/8s · 720p · \$0\.81/)).toBeTruthy()
  })

  it('a video Google did not return says so', () => {
    row(makeEvent(1, 'video', { agent_role: 'Video Producer', file: 'v.mp4', made: false, duration_seconds: 5, resolution: '360p', cost_usd: 0.17 }))
    expect(screen.getByText(/got no video for/)).toBeTruthy()
  })

  it('delegation names both agents and shows the question and tool', () => {
    row(makeEvent(1, 'delegation', { from_agent: 'Boss', to_agent: 'Worker', question: 'Do it', tool_name: 'Delegate work' }))
    expect(screen.getByText('Boss')).toBeTruthy()
    expect(screen.getByText('Worker')).toBeTruthy()
    expect(screen.getByText(/Do it/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('via Delegate work')).toBeTruthy()
  })

  it('delegation without a question has no dash suffix', () => {
    row(makeEvent(1, 'delegation', { from_agent: 'Boss', to_agent: 'Worker' }))
    expect(screen.queryByText(/—/)).toBeNull()
  })

  it('task_completed renders its output as markdown', () => {
    row(makeEvent(1, 'task_completed', { agent_role: 'A', output: '# Hi' }))
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByTestId('md').textContent).toBe('# Hi')
  })

  it('crew_completed shows cost and the final output', () => {
    row(makeEvent(1, 'crew_completed', { cost_usd: 1.5, output: 'fin' }))
    expect(screen.getByText('$1.50')).toBeTruthy()
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByTestId('md').textContent).toBe('fin')
  })

  it('markdown details tolerate a missing output', () => {
    row(makeEvent(1, 'task_completed', {}))
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByTestId('md').textContent).toBe('')
  })

  it('llm_call summarises tokens, cached tokens and cost, and loads the call on expand', () => {
    const ctx = withCalls({ calls: [makeLlmCall({ seq: 4, response: 'the answer' })] }, <ol><TimelineItemRow item={itemOf(makeEvent(4, 'llm_call', { agent_role: 'A', model: 'gpt-x', cost_usd: 0.02, usage: { prompt_tokens: 1200, completion_tokens: 30, cached_prompt_tokens: 500 } }))} /></ol>)
    expect(screen.getByText(/1,200→30 tok \(500 cached\) · \$0\.02/)).toBeTruthy()
    expect(ctx.load).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button'))
    expect(ctx.load).toHaveBeenCalledWith({ wantSeq: 4 })
    expect(screen.getByText('the answer')).toBeTruthy()
  })

  it('llm_call without usage shows dashes', () => {
    withCalls({}, <ol><TimelineItemRow item={itemOf(makeEvent(4, 'llm_call', { model: 'm' }))} /></ol>)
    expect(screen.getByText(/—→— tok · —/)).toBeTruthy()
  })
})

describe('TimelineItemRow decoration', () => {
  it('highlights delegations and budget events', () => {
    row(makeEvent(1, 'delegation', { from_agent: 'A', to_agent: 'B' }))
    expect(screen.getByRole('listitem').className).toContain('bg-danger-soft')
    cleanup()
    row(makeEvent(1, 'budget', { state: 'x' }))
    expect(screen.getByRole('listitem').className).toContain('bg-warn-soft')
    cleanup()
    row(makeEvent(1, 'crew_started', {}))
    expect(screen.getByRole('listitem').className).not.toMatch(/bg-(danger|warn)-soft/)
  })

  it.each([
    ['run_status', 'text-accent'], ['crew_started', 'text-accent'], ['task_started', 'text-blue'], ['agent_started', 'text-teal'],
    ['llm_call', 'text-muted'], ['search', 'text-blue'], ['delegation', 'text-danger'], ['task_completed', 'text-ok'],
    ['crew_completed', 'text-ok'], ['task_failed', 'text-danger'], ['crew_failed', 'text-danger'], ['tool_error', 'text-danger'],
    ['budget', 'text-warn'], ['artifacts', 'text-teal'], ['tool_started', 'text-violet'], ['log', 'text-faint'],
  ] as const)('%s icon tone is %s', (type, tone) => {
    withCalls({}, <ol><TimelineItemRow item={itemOf(makeEvent(1, type, {}))} /></ol>)
    const icon = screen.getByRole('listitem').querySelector('svg.lucide') as SVGElement
    expect(icon.getAttribute('class')).toContain(tone)
  })

  it('tints failed tools and failed artifact saves red', () => {
    row(makeEvent(1, 'tool_started', { tool_name: 'x' }), makeEvent(2, 'tool_error', { error: 'e' }))
    expect(screen.getByRole('listitem').querySelectorAll('svg.lucide')[0].getAttribute('class')).toContain('text-danger')
    cleanup()
    row(makeEvent(1, 'artifacts', { error: 'x' }))
    expect(screen.getByRole('listitem').querySelectorAll('svg.lucide')[0].getAttribute('class')).toContain('text-danger')
  })
})

describe('LlmCallDetail', () => {
  const llm = (value: Partial<LlmCallsResource>) => withCalls(value, <ol><TimelineItemRow item={itemOf(makeEvent(9, 'llm_call', { model: 'm' }))} /></ol>)
  const open = () => fireEvent.click(screen.getByRole('button'))

  it('shows a loading state before calls are fetched', () => {
    llm({ loading: true })
    open()
    expect(screen.getByText('Loading prompt and response…')).toBeTruthy()
  })

  it('shows an error with a forced retry', () => {
    const ctx = llm({ error: new Error('nope') })
    open()
    expect(screen.getByText('nope')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(ctx.load).toHaveBeenLastCalledWith({ force: true })
  })

  it('explains when the call has not been recorded yet', () => {
    llm({ calls: [makeLlmCall({ seq: 1 })] })
    open()
    expect(screen.getByText(/messages are not available yet/)).toBeTruthy()
  })

  it('shows the call prompt and response when present', () => {
    llm({ calls: [makeLlmCall({ seq: 9, messages: [{ role: 'system', content: 'be nice' }], response: 'ok then' })] })
    open()
    expect(screen.getByText('be nice')).toBeTruthy()
    expect(screen.getByText('ok then')).toBeTruthy()
  })
})
