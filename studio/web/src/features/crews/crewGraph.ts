import type { Agent, CrewBody, LLM, Task, Tool } from '../../api/types'

export type CrewGraphNodeKind = 'crew' | 'task' | 'agent' | 'tool' | 'llm'
export type CrewGraphEdgeKind = 'sequence' | 'assigned' | 'context' | 'tool' | 'llm' | 'manager' | 'member'

export type CrewGraphNode = {
  id: string
  kind: CrewGraphNodeKind
  label: string
  sublabel?: string
  /** Id of the backing entity (task/agent/llm id, tool key). */
  entityId?: string
  warnings: string[]
}

export type CrewGraphEdge = {
  id: string
  source: string
  target: string
  kind: CrewGraphEdgeKind
  label?: string
}

export type CrewGraph = { nodes: CrewGraphNode[]; edges: CrewGraphEdge[] }

export type CrewGraphCatalog = {
  agents: readonly Agent[]
  tasks: readonly Task[]
  llms: readonly LLM[]
  tools: readonly Tool[]
}

export type CrewDraft = Pick<CrewBody, 'name' | 'process' | 'manager_llm_id' | 'agent_ids' | 'task_ids'>

export const DEFAULT_LLM_NODE = 'llm:default'

export const nodeIds = {
  crew: 'crew',
  task: (id: string) => `task:${id}`,
  agent: (id: string) => `agent:${id}`,
  tool: (key: string) => `tool:${key}`,
  llm: (id: string | null) => (id ? `llm:${id}` : DEFAULT_LLM_NODE),
}

function edge(kind: CrewGraphEdgeKind, source: string, target: string, label?: string): CrewGraphEdge {
  return { id: `${kind}:${source}->${target}`, source, target, kind, label }
}

/**
 * Converts a (possibly unsaved) crew plus the entity catalog into a graph:
 * crew -> tasks in order (sequence), task -> agent (assigned), context edges
 * between tasks, agent -> tools, agent -> LLM, crew -> manager LLM.
 */
export function crewToGraph(crew: CrewDraft, catalog: CrewGraphCatalog): CrewGraph {
  const taskById = new Map(catalog.tasks.map((t) => [t.id, t]))
  const agentById = new Map(catalog.agents.map((a) => [a.id, a]))
  const llmById = new Map(catalog.llms.map((l) => [l.id, l]))
  const toolByKey = new Map(catalog.tools.map((t) => [t.key, t]))

  const nodes = new Map<string, CrewGraphNode>()
  const edges = new Map<string, CrewGraphEdge>()
  const addEdge = (e: CrewGraphEdge) => {
    if (!edges.has(e.id)) edges.set(e.id, e)
  }
  const addLlm = (llmId: string | null) => {
    const id = nodeIds.llm(llmId)
    if (nodes.has(id)) return id
    const llm = llmId ? llmById.get(llmId) : undefined
    nodes.set(id, {
      id,
      kind: 'llm',
      label: llmId ? (llm?.name ?? 'Missing LLM') : 'CrewAI default',
      sublabel: llm?.model,
      entityId: llmId ?? undefined,
      warnings: llmId && !llm ? ['LLM no longer exists'] : [],
    })
    return id
  }

  nodes.set(nodeIds.crew, {
    id: nodeIds.crew,
    kind: 'crew',
    label: crew.name.trim() || 'Untitled crew',
    sublabel: crew.process,
    warnings: crew.process === 'hierarchical' && !crew.manager_llm_id ? ['Hierarchical crews need a manager LLM'] : [],
  })
  if (crew.process === 'hierarchical' && crew.manager_llm_id) {
    addEdge(edge('manager', nodeIds.crew, addLlm(crew.manager_llm_id), 'manager'))
  }

  const crewAgentIds = new Set(crew.agent_ids)
  const crewTaskIds = new Set(crew.task_ids)
  const assignedAgentIds = new Set<string>()
  let previous = nodeIds.crew

  crew.task_ids.forEach((taskId, index) => {
    const task = taskById.get(taskId)
    const id = nodeIds.task(taskId)
    const warnings: string[] = []
    if (!task) warnings.push('Task no longer exists')
    if (task && !task.agent_id && crew.process === 'sequential') warnings.push('No agent assigned')
    if (task?.agent_id && !crewAgentIds.has(task.agent_id)) warnings.push('Agent is not in this crew')
    const earlier = new Set(crew.task_ids.slice(0, index))
    for (const ctx of task?.context_task_ids ?? []) {
      if (!crewTaskIds.has(ctx)) warnings.push('Context task is not in this crew')
      else if (!earlier.has(ctx)) warnings.push('Context task runs later')
    }
    nodes.set(id, {
      id,
      kind: 'task',
      label: task?.name ?? 'Missing task',
      sublabel: `#${index + 1}`,
      entityId: taskId,
      warnings: [...new Set(warnings)],
    })
    addEdge(edge('sequence', previous, id))
    previous = id
    if (task?.agent_id) assignedAgentIds.add(task.agent_id)
  })

  // Context edges once every task node exists.
  for (const taskId of crew.task_ids) {
    for (const ctx of taskById.get(taskId)?.context_task_ids ?? []) {
      if (crewTaskIds.has(ctx)) addEdge(edge('context', nodeIds.task(ctx), nodeIds.task(taskId), 'context'))
    }
  }

  const allAgentIds = [...new Set([...crew.agent_ids, ...assignedAgentIds])]
  for (const agentId of allAgentIds) {
    const agent = agentById.get(agentId)
    const id = nodeIds.agent(agentId)
    nodes.set(id, {
      id,
      kind: 'agent',
      label: agent?.name ?? 'Missing agent',
      sublabel: agent?.role,
      entityId: agentId,
      warnings: agent ? [] : ['Agent no longer exists'],
    })
    if (!assignedAgentIds.has(agentId)) addEdge(edge('member', nodeIds.crew, id))
    if (!agent) continue
    for (const key of agent.tools) {
      const toolId = nodeIds.tool(key)
      if (!nodes.has(toolId)) {
        const tool = toolByKey.get(key)
        nodes.set(toolId, {
          id: toolId,
          kind: 'tool',
          label: tool?.name ?? key,
          sublabel: key,
          entityId: key,
          warnings: !tool ? ['Unknown tool'] : tool.available ? [] : [`Missing ${tool.requires_env.join(', ')}`],
        })
      }
      addEdge(edge('tool', id, toolId))
    }
    addEdge(edge('llm', id, addLlm(agent.llm_id)))
  }

  for (const taskId of crew.task_ids) {
    const agentId = taskById.get(taskId)?.agent_id
    if (agentId) addEdge(edge('assigned', nodeIds.task(taskId), nodeIds.agent(agentId)))
  }

  return { nodes: [...nodes.values()], edges: [...edges.values()] }
}
