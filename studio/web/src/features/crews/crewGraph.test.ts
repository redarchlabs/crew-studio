import { describe, expect, it } from 'vitest'
import { crewToGraph, DEFAULT_LLM_NODE, type CrewDraft } from './crewGraph'
import { makeAgent, makeLlm, makeTask, makeTool } from '../../lib/testFixtures'

const catalog = {
  llms: [makeLlm({ id: 'l1', name: 'Luna' }), makeLlm({ id: 'mgr', name: 'Manager' })],
  tools: [makeTool({ key: 'google_search', name: 'Google search', available: false, requires_env: ['GEMINI_API_KEY'] })],
  agents: [
    makeAgent({ id: 'a1', name: 'Researcher', llm_id: 'l1', tools: ['google_search'] }),
    makeAgent({ id: 'a2', name: 'Writer', llm_id: null }),
    makeAgent({ id: 'a3', name: 'Spare', llm_id: 'l1', tools: ['google_search'] }),
  ],
  tasks: [
    makeTask({ id: 't1', name: 'research', agent_id: 'a1' }),
    makeTask({ id: 't2', name: 'write', agent_id: 'a2', context_task_ids: ['t1'] }),
    makeTask({ id: 't3', name: 'orphan', agent_id: null, context_task_ids: ['t9'] }),
  ],
}

const base: CrewDraft = {
  name: 'Crew',
  process: 'sequential',
  manager_llm_id: null,
  agent_ids: ['a1', 'a2', 'a3'],
  task_ids: ['t1', 't2'],
}

const ids = (xs: { id: string }[]) => xs.map((x) => x.id).sort()

describe('crewToGraph', () => {
  it('builds crew, task, agent, tool and llm nodes', () => {
    const g = crewToGraph(base, catalog)
    expect(ids(g.nodes)).toEqual(
      ['agent:a1', 'agent:a2', 'agent:a3', 'crew', DEFAULT_LLM_NODE, 'llm:l1', 'task:t1', 'task:t2', 'tool:google_search'].sort(),
    )
  })

  it('chains tasks in order with sequence edges', () => {
    const seq = crewToGraph(base, catalog).edges.filter((e) => e.kind === 'sequence')
    expect(seq.map((e) => [e.source, e.target])).toEqual([
      ['crew', 'task:t1'],
      ['task:t1', 'task:t2'],
    ])
  })

  it('adds assigned, context, tool and llm edges and shares tool/llm nodes', () => {
    const g = crewToGraph(base, catalog)
    const pairs = (kind: string) => g.edges.filter((e) => e.kind === kind).map((e) => `${e.source}>${e.target}`)
    expect(pairs('assigned')).toEqual(['task:t1>agent:a1', 'task:t2>agent:a2'])
    expect(pairs('context')).toEqual(['task:t1>task:t2'])
    expect(pairs('tool')).toEqual(['agent:a1>tool:google_search', 'agent:a3>tool:google_search'])
    expect(pairs('llm')).toEqual(['agent:a1>llm:l1', `agent:a2>${DEFAULT_LLM_NODE}`, 'agent:a3>llm:l1'])
    expect(pairs('member')).toEqual(['crew>agent:a3'])
    expect(g.nodes.find((n) => n.id === 'tool:google_search')?.warnings).toEqual(['Missing GEMINI_API_KEY'])
  })

  it('flags problems on task nodes', () => {
    const g = crewToGraph({ ...base, agent_ids: ['a2'], task_ids: ['t2', 't1', 't3', 'gone'] }, catalog)
    const warn = (id: string) => g.nodes.find((n) => n.id === id)?.warnings
    expect(warn('task:t2')).toEqual(['Context task runs later'])
    expect(warn('task:t1')).toEqual(['Agent is not in this crew'])
    expect(warn('task:t3')).toEqual(['No agent assigned', 'Context task is not in this crew'])
    expect(warn('task:gone')).toEqual(['Task no longer exists'])
    // Task agents outside the crew list still appear so the picture is complete.
    expect(g.nodes.some((n) => n.id === 'agent:a1')).toBe(true)
  })

  it('links the manager LLM for hierarchical crews', () => {
    const g = crewToGraph({ ...base, process: 'hierarchical', manager_llm_id: 'mgr' }, catalog)
    expect(g.edges.find((e) => e.kind === 'manager')).toMatchObject({ source: 'crew', target: 'llm:mgr' })
    const noMgr = crewToGraph({ ...base, process: 'hierarchical' }, catalog)
    expect(noMgr.nodes.find((n) => n.id === 'crew')?.warnings).toHaveLength(1)
  })

  it('handles an empty crew', () => {
    const g = crewToGraph({ ...base, name: '', agent_ids: [], task_ids: [] }, catalog)
    expect(g.nodes).toHaveLength(1)
    expect(g.nodes[0].label).toBe('Untitled crew')
    expect(g.edges).toEqual([])
  })
})
