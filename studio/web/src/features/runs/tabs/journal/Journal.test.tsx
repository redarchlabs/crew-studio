import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import type { Journal } from '../../../../api/types'
import { makeEntry, makeJournal } from '../../testUtils'
import { AgentAvatar, KIND_LOOK, KindIcon } from './journalKinds'
import { EntryBody, EntryMeta, FullPromptToggle, JournalEntryRow } from './JournalEntryRow'
import { JournalRail } from './JournalRail'
import { JournalTab } from './JournalTab'
import { delegationTarget, groupByTask, initials, SLOT_CLASSES, taskLabel, avatarClass, colorSlot } from './journalModel'

vi.mock('../../../../components/Markdown', () => ({ Markdown: ({ children }: { children: string }) => <div data-testid="md">{children}</div> }))
vi.mock('../../timeline/LlmCallDetail', () => ({ LlmCallDetail: ({ seq }: { seq: number }) => <div data-testid="llm-detail">call {seq}</div> }))

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

const journal = (): Journal => makeJournal({
  agents: [
    { role: 'Analyst', tasks: ['Research'], entries: 3, llm_calls: 2, tool_calls: 1, prompt_tokens: 8000, completion_tokens: 4000, cost_usd: 0.5, first_at: null, last_at: null },
    { role: 'Writer', tasks: ['Write'], entries: 1, llm_calls: 1, tool_calls: 0, prompt_tokens: 10, completion_tokens: 5, cost_usd: 0.001, first_at: null, last_at: null },
  ],
  tasks: [
    { name: 'Research', agent_role: 'Analyst', status: 'done', started_at: '2026-01-01T00:00:00Z', finished_at: '2026-01-01T00:00:30Z', output: null },
    { name: 'Write', agent_role: 'Writer', status: 'running', started_at: '2026-01-01T00:01:00Z', finished_at: null, output: null },
    { name: 'Publish', agent_role: 'Writer', status: 'pending', started_at: null, finished_at: null, output: null },
    { name: 'Broken', agent_role: 'Writer', status: 'failed', started_at: null, finished_at: null, output: null },
  ],
  entries: [
    makeEntry({ seq: 1, kind: 'run', task_name: '', agent_role: '', title: 'Run started', body: 'kickoff' }),
    makeEntry({ seq: 2, kind: 'llm_call', task_name: 'Research', title: 'Asked the model', body: 'thinking about cats', tokens: 1500, cost_usd: 0.2, model: 'gpt-x' }),
    makeEntry({ seq: 3, kind: 'tool_call', task_name: 'Research', title: 'Used web search', body: '' }),
    makeEntry({ seq: 4, kind: 'task_completed', task_name: 'Research', title: 'Research done', body: '## Findings' }),
    makeEntry({ seq: 5, kind: 'delegation', task_name: 'Write', agent_role: 'Writer', title: 'Delegated to Analyst', body: 'help' }),
  ],
})

describe('JournalTab', () => {
  const props = { error: undefined, loading: false, reload: vi.fn(), live: false }

  it('shows loading, error, and nothing when idle without data', () => {
    const { rerender, container } = render(<JournalTab {...props} journal={undefined} loading />)
    expect(screen.getByText('Loading journal…')).toBeTruthy()
    rerender(<JournalTab {...props} journal={undefined} error={new Error('nope')} />)
    expect(screen.getByText('nope')).toBeTruthy()
    fireEvent.click(screen.getByText('Retry'))
    expect(props.reload).toHaveBeenCalled()
    rerender(<JournalTab {...props} journal={undefined} />)
    expect(container.textContent).toBe('')
  })

  it('has different empty hints for live and finished runs', () => {
    const { rerender } = render(<JournalTab {...props} journal={makeJournal()} live />)
    expect(screen.getByText('Entries appear as agents start working.')).toBeTruthy()
    rerender(<JournalTab {...props} journal={makeJournal()} live={false} />)
    expect(screen.getByText('This attempt recorded no activity.')).toBeTruthy()
  })

  it('renders sections per task with totals and an entry counter', () => {
    render(<JournalTab {...props} journal={journal()} />)
    expect(screen.getByText('5/5')).toBeTruthy()
    expect(screen.getByText('Crew')).toBeTruthy()
    expect(screen.getByText('3 entries · 1.5k tok · $0.20', { exact: false })).toBeTruthy()
    expect(screen.getByText('Run started')).toBeTruthy()
  })

  it('filters by search text and shows nothing-matches', () => {
    render(<JournalTab {...props} journal={journal()} />)
    const search = screen.getByLabelText('Search journal')
    fireEvent.change(search, { target: { value: 'CATS' } })
    expect(screen.getByText('1/5')).toBeTruthy()
    fireEvent.change(search, { target: { value: 'zzz' } })
    expect(screen.getByText('Nothing matches these filters')).toBeTruthy()
  })

  it('toggles kind groups off and back on', () => {
    render(<JournalTab {...props} journal={journal()} />)
    const tools = screen.getByText('Tools')
    expect(tools.getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(tools)
    expect(tools.getAttribute('aria-pressed')).toBe('false')
    expect(screen.getByText('4/5')).toBeTruthy()
    fireEvent.click(tools)
    expect(screen.getByText('5/5')).toBeTruthy()
    // hiding a group where only some kinds are hidden hides the rest too
    fireEvent.click(screen.getByText('Run'))
    expect(screen.getByText('4/5')).toBeTruthy()
  })

  it('filters by agent and task through the rail and clears them', () => {
    render(<JournalTab {...props} journal={journal()} />)
    const rail = screen.getByText('Agents').closest('section')!
    fireEvent.click(within(rail).getByText('Writer'))
    expect(screen.getByText('1/5')).toBeTruthy()
    fireEvent.click(within(rail).getByText('clear'))
    expect(screen.getByText('5/5')).toBeTruthy()
    const tasks = screen.getByText('Tasks', { selector: '.eyebrow' }).closest('section')!
    fireEvent.click(within(tasks).getByText('1. Research'))
    expect(screen.getByText('3/5')).toBeTruthy()
    fireEvent.click(within(tasks).getByText('clear'))
    expect(screen.getByText('5/5')).toBeTruthy()
  })

  it('warns when the journal is truncated', () => {
    render(<JournalTab {...props} journal={{ ...journal(), truncated: true }} />)
    expect(screen.getByText(/truncated to its first 5 entries/)).toBeTruthy()
  })

  it('refreshes via the button', () => {
    const reload = vi.fn()
    render(<JournalTab {...props} reload={reload} journal={journal()} />)
    fireEvent.click(screen.getByText('Refresh'))
    expect(reload).toHaveBeenCalled()
  })

  it('paginates large journals by 800 entries', () => {
    const entries = Array.from({ length: 1000 }, (_, i) => makeEntry({ seq: i + 1, title: `E${i + 1}` }))
    render(<JournalTab {...props} journal={makeJournal({ entries })} />)
    expect(screen.queryByText('E801')).toBeNull()
    expect(screen.getByText('E800')).toBeTruthy()
    fireEvent.click(screen.getByText('Show 200 more'))
    expect(screen.getByText('E1000')).toBeTruthy()
    expect(screen.queryByText(/Show \d+ more/)).toBeNull()
  }, 60_000) // renders 1,000 rows; slow under coverage on a busy machine
})

describe('JournalEntryRow', () => {
  const row = (e: Parameters<typeof makeEntry>[0]) => render(<ol><JournalEntryRow entry={makeEntry(e)} /></ol>)

  it('expands to show agent, task and body', () => {
    row({ seq: 1, kind: 'tool_call', title: 'Used tool', body: 'raw body' })
    const btn = screen.getByRole('button')
    expect(btn.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(btn)
    expect(screen.getByText('raw body')).toBeTruthy()
    expect(screen.getByText('Analyst · Research')).toBeTruthy()
    fireEvent.click(btn)
    expect(screen.queryByText('raw body')).toBeNull()
  })

  it('is not expandable without a body unless it is an llm_call', () => {
    row({ seq: 1, kind: 'tool_call', body: '' })
    const btn = screen.getByRole('button') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    expect(btn.getAttribute('aria-expanded')).toBeNull()
    cleanup()
    row({ seq: 1, kind: 'llm_call', body: '' })
    expect((screen.getByRole('button') as HTMLButtonElement).disabled).toBe(false)
  })

  it('omits the agent line for crew-level entries and task suffix when none', () => {
    row({ seq: 1, kind: 'run', agent_role: '', task_name: '', body: 'b' })
    fireEvent.click(screen.getByRole('button'))
    expect(screen.queryByText(/·/)).toBeNull()
    cleanup()
    row({ seq: 1, kind: 'budget', agent_role: 'Ann', task_name: '', body: 'b' })
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByText('Ann')).toBeTruthy()
  })

  it('renders markdown for task_completed and run, preformatted otherwise', () => {
    row({ seq: 1, kind: 'task_completed', body: '# md' })
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByTestId('md')).toBeTruthy()
    cleanup()
    row({ seq: 1, kind: 'search', body: 'plain' })
    fireEvent.click(screen.getByRole('button'))
    expect(screen.queryByTestId('md')).toBeNull()
    expect(screen.getByText('plain').tagName).toBe('PRE')
  })

  it('shows the full prompt toggle for llm calls', () => {
    row({ seq: 12, kind: 'llm_call', body: 'summary' })
    fireEvent.click(screen.getByRole('button', { name: /llm call|Asked|Entry/i }))
    expect(screen.queryByTestId('llm-detail')).toBeNull()
    fireEvent.click(screen.getByText('Show full prompt/response'))
    expect(screen.getByTestId('llm-detail').textContent).toBe('call 12')
    fireEvent.click(screen.getByText('Hide full prompt/response'))
    expect(screen.queryByTestId('llm-detail')).toBeNull()
  })

  it.each([
    ['delegation', 'bg-danger-soft'], ['budget', 'bg-warn-soft'], ['tool_error', 'bg-warn-soft'], ['task_failed', 'bg-warn-soft'],
  ] as const)('tints %s rows', (kind, cls) => {
    row({ seq: 1, kind })
    expect(screen.getByRole('listitem').className).toContain(cls)
  })

  it('does not tint ordinary rows', () => {
    row({ seq: 1, kind: 'llm_call' })
    expect(screen.getByRole('listitem').className).not.toMatch(/-soft/)
  })
})

describe('EntryBody / FullPromptToggle / EntryMeta', () => {
  it('shows a placeholder when there is no body', () => {
    render(<EntryBody entry={makeEntry({ seq: 1, body: '' })} />)
    expect(screen.getByText('No details recorded.')).toBeTruthy()
  })

  it('renders nothing for an entry without cost, tokens or model', () => {
    const { container } = render(<EntryMeta entry={makeEntry({ seq: 1 })} />)
    expect(container.textContent).toBe('')
  })

  it.each([
    [{ tokens: 2500, cost_usd: 0.5, model: 'm' }, 'm · 2.5k tok · $0.50'],
    [{ tokens: 20, cost_usd: 0 }, '20 tok'],
    [{ tokens: 0, cost_usd: 0.1 }, '$0.10'],
    [{ model: 'only-model' }, 'only-model'],
  ])('formats meta %j', (over, text) => {
    const { container } = render(<EntryMeta entry={makeEntry({ seq: 1, ...over })} />)
    expect(container.textContent).toBe(text)
  })

  it('FullPromptToggle is closed initially', () => {
    render(<FullPromptToggle seq={3} />)
    expect(screen.queryByTestId('llm-detail')).toBeNull()
  })
})

const agentsSection = () => screen.getByText('Agents').closest('section') as HTMLElement

describe('JournalRail', () => {
  const setup = (over: Partial<Parameters<typeof JournalRail>[0]> = {}) => {
    const onAgent = vi.fn()
    const onTask = vi.fn()
    render(<JournalRail journal={journal()} agent={null} task={null} onAgent={onAgent} onTask={onTask} now={Date.parse('2026-01-01T00:01:20Z')} {...over} />)
    return { onAgent, onTask }
  }

  it('lists agents with their stats', () => {
    setup()
    expect(screen.getByText('2 llm · 1 tool · 12k tok · $0.50')).toBeTruthy()
    expect(screen.getByText('1 llm · 0 tool · 15 tok · $0.0010')).toBeTruthy()
  })

  it('toggles agent selection', () => {
    const { onAgent } = setup()
    fireEvent.click(within(agentsSection()).getByText('Analyst'))
    expect(onAgent).toHaveBeenCalledWith('Analyst')
  })

  it('deselects the active agent or task on second click and shows clear links', () => {
    const { onAgent, onTask } = setup({ agent: 'Analyst', task: 'Research' })
    fireEvent.click(within(agentsSection()).getByText('Analyst'))
    expect(onAgent).toHaveBeenCalledWith(null)
    fireEvent.click(screen.getByText('1. Research'))
    expect(onTask).toHaveBeenCalledWith(null)
    const clears = screen.getAllByText('clear')
    expect(clears).toHaveLength(2)
    fireEvent.click(clears[0])
    expect(onAgent).toHaveBeenLastCalledWith(null)
    fireEvent.click(clears[1])
    expect(onTask).toHaveBeenLastCalledWith(null)
  })

  it('selects a task', () => {
    const { onTask } = setup()
    fireEvent.click(screen.getByText('2. Write'))
    expect(onTask).toHaveBeenCalledWith('Write')
  })

  it('shows durations for started tasks (live ones against now) and status for others', () => {
    setup()
    expect(screen.getByText(/· 30s$/)).toBeTruthy()
    expect(screen.getByText(/· 20s$/)).toBeTruthy()
    expect(screen.getByText('pending')).toBeTruthy()
    expect(screen.getByText('failed')).toBeTruthy()
  })

  it('shows a note when no agent has acted', () => {
    setup({ journal: makeJournal() })
    expect(screen.getByText('No agents have acted yet.')).toBeTruthy()
  })

  it('uses a coloured dot per task status', () => {
    setup()
    const dot = (status: string) => (screen.getByTitle(status)).className
    expect(dot('done')).toContain('bg-ok')
    expect(dot('running')).toContain('bg-accent')
    expect(dot('pending')).toContain('bg-faint')
    expect(dot('failed')).toContain('bg-danger')
  })
})

describe('journalKinds', () => {
  it('KindIcon labels each kind and falls back to run for unknown kinds', () => {
    for (const kind of Object.keys(KIND_LOOK) as (keyof typeof KIND_LOOK)[]) {
      const { unmount } = render(<KindIcon kind={kind} />)
      expect(screen.getByLabelText(KIND_LOOK[kind].label)).toBeTruthy()
      unmount()
    }
    render(<KindIcon kind={'mystery' as never} className="size-8" />)
    expect(screen.getByLabelText('run').getAttribute('class')).toContain('size-8')
  })

  it('AgentAvatar shows initials and a Crew title for the crew lane', () => {
    render(<AgentAvatar role="Market Research Analyst" />)
    expect(screen.getByText('MR').getAttribute('title')).toBe('Market Research Analyst')
    cleanup()
    render(<AgentAvatar role="" size="size-9" />)
    const crew = screen.getByText('C')
    expect(crew.getAttribute('title')).toBe('Crew')
    expect(crew.className).toContain('size-9')
    expect(crew.className).toContain('bg-panel-2')
  })
})

describe('journalModel extras', () => {
  it('groupByTask starts new sections when the task changes back and totals cost and tokens', () => {
    const e = (seq: number, task: string, cost = 0, tokens = 0) => makeEntry({ seq, task_name: task, cost_usd: cost, tokens })
    const sections = groupByTask([e(1, 'A', 0.1, 10), e(2, 'A', 0.2, 20), e(3, 'B'), e(4, 'A', 1, 1)])
    expect(sections.map((s) => [s.key, s.entries.length])).toEqual([['0:A', 2], ['1:B', 1], ['2:A', 1]])
    expect(sections[0].cost).toBeCloseTo(0.3)
    expect(sections[0].tokens).toBe(30)
    expect(groupByTask([])).toEqual([])
  })

  it('initials handles whitespace, one word and empty', () => {
    expect(initials('  ')).toBe('C')
    expect(initials('analyst')).toBe('A')
    expect(initials('a b c')).toBe('AB')
  })

  it('colorSlot is stable and in range; avatarClass uses the slot table', () => {
    for (const role of ['a', 'Analyst', 'Writer', '日本語']) {
      const slot = colorSlot(role)
      expect(slot).toBeGreaterThanOrEqual(0)
      expect(slot).toBeLessThan(6)
      expect(colorSlot(role)).toBe(slot)
      expect(avatarClass(role)).toBe(SLOT_CLASSES[slot])
    }
    expect(avatarClass('')).toBe('bg-panel-2 text-muted')
  })

  it('delegationTarget picks the longest other role mentioned', () => {
    const entry = { title: 'Delegated to Senior Analyst', body: '', agent_role: 'Boss' }
    expect(delegationTarget(entry, ['Analyst', 'Senior Analyst', 'Boss'])).toBe('Senior Analyst')
    expect(delegationTarget(entry, ['Writer'])).toBeNull()
    expect(delegationTarget({ ...entry, agent_role: 'Analyst', title: 'Delegated to Analyst' }, ['Analyst'])).toBeNull()
    expect(delegationTarget(entry, ['', 'Senior Analyst'])).toBe('Senior Analyst')
  })

  it('taskLabel names the crew lane', () => {
    expect(taskLabel('')).toBe('Crew')
    expect(taskLabel('X')).toBe('X')
  })
})
