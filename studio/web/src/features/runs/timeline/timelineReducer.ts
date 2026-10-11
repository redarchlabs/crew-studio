import type { RunEvent, RunStatus } from '../../../api/types'
import { eventAgents, eventCategory, isTerminal, num, runStatusOf, str, type EventCategory } from './payload'
import { emptyTally, tallyEvent, toolKey, type SpendTally } from './spendTally'

export const CREW_START = 'crew:start'
export const CREW_END = 'crew:end'

export type TimelineItem = {
  seq: number
  event: RunEvent
  /** For tool_started: the matching tool_finished / tool_error, once it arrives. */
  result: RunEvent | null
  category: EventCategory
  agents: string[]
  task: string | null
}

export type TaskStatus = 'running' | 'done' | 'failed'

export type TimelineGroup = {
  key: string
  title: string
  task: string | null
  firstSeq: number
  status: TaskStatus | null
  agents: string[]
  items: TimelineItem[]
}

export type LogLine = { seq: number; line: string; at: string }

export type TimelineState = {
  lastSeq: number
  eventCount: number
  groups: TimelineGroup[]
  logs: LogLine[]
  agents: string[]
  runStatus: RunStatus | null
  runError: string | null
  spentUsd: number | null
  budgetUsd: number | null
  budgetState: string | null
  activeTask: string | null
  /** Spend and tokens by source so far, from llm_call / search / video events. */
  tally: SpendTally
  /** How many "Ask a human" journal lines (questions asked, answers given) the run has, to refresh question lists */
  humanEvents: number
}

export type TimelineAction = { type: 'append'; events: readonly RunEvent[] } | { type: 'reset' }

export const initialTimeline: TimelineState = {
  lastSeq: 0,
  eventCount: 0,
  groups: [],
  logs: [],
  agents: [],
  runStatus: null,
  runError: null,
  spentUsd: null,
  budgetUsd: null,
  budgetState: null,
  activeTask: null,
  tally: emptyTally,
  humanEvents: 0,
}

function groupKeyFor(event: RunEvent, state: TimelineState, hasTasks: boolean): { key: string; task: string | null } {
  const task = str(event.payload, 'task_name')
  if (task) return { key: `task:${task}`, task }
  switch (event.type) {
    case 'crew_started':
      return { key: CREW_START, task: null }
    case 'crew_completed':
    case 'crew_failed':
      return { key: CREW_END, task: null }
    case 'run_status':
      return { key: isTerminal(runStatusOf(event.payload)) ? CREW_END : CREW_START, task: null }
    default:
      if (state.activeTask) return { key: `task:${state.activeTask}`, task: state.activeTask }
      return { key: hasTasks ? CREW_END : CREW_START, task: null }
  }
}

function nextTaskStatus(current: TaskStatus | null, event: RunEvent): TaskStatus | null {
  if (event.type === 'task_started' || event.type === 'agent_started') return current ?? 'running'
  if (event.type === 'task_completed') return 'done'
  if (event.type === 'task_failed') return 'failed'
  return current
}

function titleFor(key: string, task: string | null): string {
  if (task) return task
  return key === CREW_START ? 'Crew start' : 'Crew outcome'
}

/** Attaches a tool result to the latest open tool_started item with the same agent and tool. */
function attachToolResult(items: TimelineItem[], event: RunEvent): TimelineItem[] | null {
  const tool = str(event.payload, 'tool_name')
  const agent = str(event.payload, 'agent_role')
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i]
    if (it.event.type !== 'tool_started' || it.result) continue
    if (str(it.event.payload, 'tool_name') === tool && str(it.event.payload, 'agent_role') === agent) {
      return items.map((x, idx) => (idx === i ? { ...x, result: event } : x))
    }
  }
  return null
}

function updateGroup(group: TimelineGroup, event: RunEvent, item: TimelineItem | null): TimelineGroup {
  const agents = [...new Set([...group.agents, ...eventAgents(event)])]
  const status = group.task ? nextTaskStatus(group.status, event) : group.status
  if (!item) return { ...group, agents, status }
  const merged = event.type === 'tool_finished' || event.type === 'tool_error' ? attachToolResult(group.items, event) : null
  return { ...group, agents, status, items: merged ?? [...group.items, item] }
}

/** Applies one event to the state, returning a new state object. */
function applyEvent(state: TimelineState, event: RunEvent): TimelineState {
  const p = event.payload
  const base: TimelineState = { ...state, lastSeq: Math.max(state.lastSeq, event.seq), eventCount: state.eventCount + 1 }

  if (event.type === 'log') {
    const line = str(p, 'line') ?? ''
    return { ...base, logs: [...state.logs, { seq: event.seq, line, at: event.created_at }] }
  }

  const spent = num(p, 'spent_usd')
  const withMeta: TimelineState = {
    ...base,
    agents: [...new Set([...state.agents, ...eventAgents(event)])],
    spentUsd: spent ?? state.spentUsd,
    tally: tallyEvent(state.tally, event),
    humanEvents: event.type === 'state' && str(p, 'tool') === 'ask_human' ? state.humanEvents + 1 : state.humanEvents,
    budgetUsd: event.type === 'budget' ? (num(p, 'budget_usd') ?? state.budgetUsd) : state.budgetUsd,
    budgetState: event.type === 'budget' ? (str(p, 'state') ?? state.budgetState) : state.budgetState,
    runStatus: event.type === 'run_status' ? (runStatusOf(p) ?? state.runStatus) : state.runStatus,
    runError:
      event.type === 'run_status' || event.type === 'crew_failed' ? (str(p, 'error') ?? state.runError) : state.runError,
    activeTask:
      event.type === 'task_started'
        ? str(p, 'task_name')
        : (event.type === 'task_completed' || event.type === 'task_failed') && str(p, 'task_name') === state.activeTask
          ? null
          : state.activeTask,
  }

  const category = eventCategory(event.type)
  if (!category) return withMeta
  const hasTasks = state.groups.some((g) => g.task !== null)
  const { key, task } = groupKeyFor(event, state, hasTasks)
  const item: TimelineItem = { seq: event.seq, event, result: null, category, agents: eventAgents(event), task }
  const existing = state.groups.find((g) => g.key === key)
  const group: TimelineGroup = existing ?? {
    key,
    title: titleFor(key, task),
    task,
    firstSeq: event.seq,
    status: null,
    agents: [],
    items: [],
  }
  const updated = updateGroup(group, event, item)
  const groups = existing ? state.groups.map((g) => (g.key === key ? updated : g)) : [...state.groups, updated]
  return { ...withMeta, groups }
}

export function timelineReducer(state: TimelineState, action: TimelineAction): TimelineState {
  if (action.type === 'reset') return initialTimeline
  const fresh = [...action.events].filter((e) => e.seq > state.lastSeq).sort((a, b) => a.seq - b.seq)
  // Duplicate seqs inside one batch are dropped by the running lastSeq check.
  return fresh.reduce((acc, e) => (e.seq > acc.lastSeq ? applyEvent(acc, e) : acc), state)
}

/** Crew start first, tasks by first appearance, crew outcome last. */
export function orderedGroups(groups: readonly TimelineGroup[]): TimelineGroup[] {
  const rank = (g: TimelineGroup) => (g.key === CREW_START ? 0 : g.key === CREW_END ? 2 : 1)
  return [...groups].sort((a, b) => rank(a) - rank(b) || a.firstSeq - b.firstSeq)
}

export type TimelineFocus = { kind: 'task' | 'agent' | 'tool' | 'model'; value: string }

export type TimelineFilter = {
  hidden: ReadonlySet<EventCategory>
  agent: string | null
  focus: TimelineFocus | null
}

function matchesFocus(item: TimelineItem, focus: TimelineFocus): boolean {
  const p = item.event.payload
  switch (focus.kind) {
    case 'task':
      return item.task === focus.value
    case 'agent':
      return item.agents.includes(focus.value)
    case 'tool':
      return toolKey(str(p, 'tool_name') ?? '') === toolKey(focus.value)
    case 'model':
      return str(p, 'model') === focus.value
  }
}

/** Applies filter chips and graph focus; drops groups left empty. */
export function filterGroups(groups: readonly TimelineGroup[], filter: TimelineFilter): TimelineGroup[] {
  return orderedGroups(groups)
    .map((g) => ({
      ...g,
      items: g.items.filter(
        (it) =>
          !filter.hidden.has(it.category) &&
          (filter.agent === null || it.agents.includes(filter.agent)) &&
          (filter.focus === null || matchesFocus(it, filter.focus)),
      ),
    }))
    .filter((g) => g.items.length > 0)
}
