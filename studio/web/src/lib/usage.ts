import type { Agent, Crew, Task } from '../api/types'

/** Crews that involve an agent directly or through one of their tasks. */
export function crewsUsingAgent(agentId: string, crews: readonly Crew[], tasks: readonly Task[]): Crew[] {
  const taskAgent = new Map(tasks.map((t) => [t.id, t.agent_id]))
  return crews.filter((c) => c.agent_ids.includes(agentId) || c.task_ids.some((tid) => taskAgent.get(tid) === agentId))
}

export function tasksUsingAgent(agentId: string, tasks: readonly Task[]): Task[] {
  return tasks.filter((t) => t.agent_id === agentId)
}

export function crewsUsingTask(taskId: string, crews: readonly Crew[]): Crew[] {
  return crews.filter((c) => c.task_ids.includes(taskId))
}

/** Input keys defined by any of the given crews. */
export function inputKeysOf(crews: readonly Crew[]): string[] {
  return [...new Set(crews.flatMap((c) => c.inputs.map((i) => i.key)))]
}

export function agentLabel(agent: Agent | undefined): string {
  return agent ? agent.name : 'Unassigned'
}
