import type { Agent, CrewInput, Task } from '../api/types'

/** Input keys follow api.md: [a-z][a-z0-9_]* wrapped in single braces. */
const PLACEHOLDER_RE = /\{([a-z][a-z0-9_]*)\}/g

export type TextSegment = { text: string; placeholder: string | null }

/** Unique placeholder keys in order of first appearance. */
export function findPlaceholders(text: string): string[] {
  const seen = new Set<string>()
  for (const match of text.matchAll(PLACEHOLDER_RE)) seen.add(match[1])
  return [...seen]
}

/** Splits text into plain and placeholder segments, for highlighting. */
export function splitPlaceholders(text: string): TextSegment[] {
  const segments: TextSegment[] = []
  let last = 0
  for (const match of text.matchAll(PLACEHOLDER_RE)) {
    const start = match.index ?? 0
    if (start > last) segments.push({ text: text.slice(last, start), placeholder: null })
    segments.push({ text: match[0], placeholder: match[1] })
    last = start + match[0].length
  }
  if (last < text.length) segments.push({ text: text.slice(last), placeholder: null })
  return segments
}

/** Unique placeholders across several texts, in order of first appearance. */
export function placeholdersIn(texts: readonly string[]): string[] {
  return [...new Set(texts.flatMap(findPlaceholders))]
}

export function agentTexts(agent: Pick<Agent, 'role' | 'goal' | 'backstory'>): string[] {
  return [agent.role, agent.goal, agent.backstory]
}

export function taskTexts(task: Pick<Task, 'description' | 'expected_output'>): string[] {
  return [task.description, task.expected_output]
}

/** "target_company" -> "Target company" */
export function labelFromKey(key: string): string {
  const words = key.split('_').filter(Boolean).join(' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/**
 * Scans a crew's tasks and agents (the crew's own agents plus each task's agent)
 * and returns the inputs list with any missing placeholder keys appended.
 */
export function detectCrewInputs(
  existing: readonly CrewInput[],
  taskIds: readonly string[],
  agentIds: readonly string[],
  tasks: readonly Task[],
  agents: readonly Agent[],
): { inputs: CrewInput[]; added: string[] } {
  const taskById = new Map(tasks.map((t) => [t.id, t]))
  const agentById = new Map(agents.map((a) => [a.id, a]))
  const crewTasks = taskIds.map((id) => taskById.get(id)).filter((t): t is Task => t !== undefined)
  const involvedAgentIds = [...new Set([...agentIds, ...crewTasks.flatMap((t) => (t.agent_id ? [t.agent_id] : []))])]
  const crewAgents = involvedAgentIds.map((id) => agentById.get(id)).filter((a): a is Agent => a !== undefined)

  const keys = placeholdersIn([...crewTasks.flatMap(taskTexts), ...crewAgents.flatMap(agentTexts)])
  const have = new Set(existing.map((i) => i.key))
  const added = keys.filter((k) => !have.has(k))
  const inputs = [
    ...existing,
    ...added.map((key) => ({ key, label: labelFromKey(key), default: '', required: true })),
  ]
  return { inputs, added }
}
