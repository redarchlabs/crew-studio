import { cleanup, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { makeAgent, makeCrew, makeLlm, makeTask, makeTool } from '../../lib/testFixtures'
import { AgentCard } from './AgentCard'
import { renderRoute, setupApi, without } from '../crews/testUtils'

vi.mock('../../api/endpoints')

beforeEach(() => setupApi())
afterEach(cleanup)

const tools = new Map([
  ['web', makeTool({ key: 'web', name: 'Web', description: 'Searches' })],
  ['mail', makeTool({ key: 'mail', name: 'Mail', available: false, requires_env: ['SMTP'] })],
])

function card(over: Partial<Parameters<typeof AgentCard>[0]> = {}, perms?: Parameters<typeof renderRoute>[1]) {
  return renderRoute(<AgentCard agent={makeAgent({ id: 'a1', name: 'Ann', role: 'Analyst', goal: 'Win' })} llm={undefined} tools={tools} crews={[]} tasks={[]} {...over} />, perms)
}

describe('AgentCard', () => {
  it('links to the editor and shows name, role, goal, and default model', async () => {
    card()
    expect((await screen.findByRole('link')).getAttribute('href')).toBe('/agents/a1')
    expect(screen.getByText('Ann')).toBeTruthy()
    expect(screen.getByText('Analyst')).toBeTruthy()
    expect(screen.getByText('Win')).toBeTruthy()
    expect(screen.getByText('CrewAI default')).toBeTruthy()
    expect(screen.getByText('No tools')).toBeTruthy()
    expect(screen.getByText('Not used yet')).toBeTruthy()
  })

  it('shows placeholders for empty role and goal', async () => {
    card({ agent: makeAgent({ id: 'a1', role: '', goal: '' }) })
    expect(await screen.findByText('No role')).toBeTruthy()
    expect(screen.getByText('No goal set')).toBeTruthy()
  })

  it('shows the llm name and a delegates chip', async () => {
    card({ agent: makeAgent({ id: 'a1', llm_id: 'l1', allow_delegation: true }), llm: makeLlm({ id: 'l1', name: 'Fast' }) })
    expect(await screen.findByText('Fast')).toBeTruthy()
    expect(screen.getByText('delegates')).toBeTruthy()
  })

  it('distinguishes a missing LLM by permission', async () => {
    const agent = makeAgent({ id: 'a1', llm_id: 'gone' })
    const { unmount } = card({ agent })
    expect(await screen.findByText('Missing LLM')).toBeTruthy()
    unmount()
    card({ agent }, { perms: without('llms.view') })
    expect(await screen.findByText('LLM assigned')).toBeTruthy()
  })

  it('renders tool chips with availability titles', async () => {
    card({ agent: makeAgent({ id: 'a1', tools: ['web', 'mail', 'ghost'] }) })
    expect((await screen.findByText('Web')).getAttribute('title')).toBe('Searches')
    expect(screen.getByText('Mail').getAttribute('title')).toBe('Unavailable: missing SMTP')
    expect(screen.getByText('ghost').getAttribute('title')).toBe('Unknown tool')
  })

  it('summarises usage with singular/plural and crew names', async () => {
    card({ crews: [makeCrew({ id: 'c1', name: 'Alpha' })], tasks: [makeTask({ id: 't1' }), makeTask({ id: 't2' })] })
    expect(await screen.findByText('1 crew · 2 tasks — Alpha')).toBeTruthy()
  })

  it('pluralises crews and singular task', async () => {
    card({ crews: [makeCrew({ id: 'c1', name: 'A' }), makeCrew({ id: 'c2', name: 'B' })], tasks: [makeTask({ id: 't1' })] })
    expect(await screen.findByText('2 crews · 1 task — A, B')).toBeTruthy()
  })

  it('shows area chips', async () => {
    card()
    expect(await screen.findByText('Finance')).toBeTruthy()
  })
})
