import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentsApi, crewsApi, llmsApi, tasksApi, toolsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeAgent, makeCrew, makeLlm, makeTask, makeTool } from '../../lib/testFixtures'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { AgentsPage } from './AgentsPage'

vi.mock('../../api/endpoints')

beforeEach(() => {
  setupApi()
  vi.mocked(agentsApi.list).mockResolvedValue([
    makeAgent({ id: 'a1', name: 'Zed', role: 'Writer', goal: 'Draft prose', area_ids: ['area-1'], llm_id: 'l1', tools: ['web'] }),
    makeAgent({ id: 'a2', name: 'Abe', role: 'Auditor', goal: 'Check books', area_ids: ['area-2'] }),
  ])
  vi.mocked(llmsApi.list).mockResolvedValue([makeLlm({ id: 'l1', name: 'Fast' })])
  vi.mocked(toolsApi.list).mockResolvedValue([makeTool({ key: 'web', name: 'Web' })])
  vi.mocked(tasksApi.list).mockResolvedValue([makeTask({ id: 't1', agent_id: 'a1' })])
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Crew One', agent_ids: ['a1'] })])
})
afterEach(cleanup)

const cardNames = () => screen.getAllByRole('link').filter((l) => /^\/agents\/a/.test(l.getAttribute('href') ?? '')).map((l) => l.querySelector('.font-semibold')?.textContent)

describe('AgentsPage', () => {
  it('lists agents sorted by name with resolved llm, tools and usage', async () => {
    renderRoute(<AgentsPage />)
    await screen.findByText('Abe')
    expect(cardNames()).toEqual(['Abe', 'Zed'])
    expect(screen.getByText('Fast')).toBeTruthy()
    expect(screen.getByText('Web')).toBeTruthy()
    expect(screen.getByText('1 crew · 1 task — Crew One')).toBeTruthy()
  })

  it('filters by name, role and goal, case-insensitively', async () => {
    renderRoute(<AgentsPage />)
    await screen.findByText('Abe')
    const box = screen.getByPlaceholderText('Filter agents…')
    fireEvent.change(box, { target: { value: 'AUDITOR' } })
    expect(cardNames()).toEqual(['Abe'])
    fireEvent.change(box, { target: { value: 'prose' } })
    expect(cardNames()).toEqual(['Zed'])
    fireEvent.change(box, { target: { value: 'nomatch' } })
    expect(screen.getByText(/No agents match/)).toBeTruthy()
  })

  it('filters by area from the URL and the select', async () => {
    renderRoute(<AgentsPage />, { url: '/?area=area-1' })
    await screen.findByText('Zed')
    expect(cardNames()).toEqual(['Zed'])
    await screen.findByRole('option', { name: 'Ops' })
    fireEvent.change(screen.getByLabelText('Filter by functional area'), { target: { value: 'area-2' } })
    expect(cardNames()).toEqual(['Abe'])
  })

  it('ignores an unknown area in the URL', async () => {
    renderRoute(<AgentsPage />, { url: '/?area=deleted' })
    await screen.findByText('Zed')
    await screen.findByRole('option', { name: 'Ops' })
    expect(cardNames()).toHaveLength(2)
  })

  it('shows area-empty message', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1', name: 'Only', area_ids: ['area-1'] })])
    renderRoute(<AgentsPage />, { url: '/?area=area-2' })
    expect(await screen.findByText('No agents in this area.')).toBeTruthy()
    fireEvent.change(screen.getByPlaceholderText('Filter agents…'), { target: { value: 'q' } })
    expect(screen.getByText(/No agents match “q” in this area/)).toBeTruthy()
  })

  it('shows the empty state with create link', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue([])
    renderRoute(<AgentsPage />)
    expect(await screen.findByText('No agents yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Create an agent' }).getAttribute('href')).toBe('/agents/new')
  })

  it('empty state for a viewer without create offers no action', async () => {
    vi.mocked(agentsApi.list).mockResolvedValue([])
    renderRoute(<AgentsPage />, { perms: without('agents.create') })
    expect(await screen.findByText(/No agents in your functional areas/)).toBeTruthy()
    expect(screen.queryByRole('link', { name: /Create an agent|New agent/ })).toBeNull()
  })

  it('shows an error and retries', async () => {
    vi.mocked(agentsApi.list).mockRejectedValueOnce(new ApiError(503, { code: 'down', message: 'unavailable now' }))
    renderRoute(<AgentsPage />)
    expect(await screen.findByText('unavailable now')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('Abe')).toBeTruthy()
  })

  it('gates header actions by permission', async () => {
    const { unmount } = renderRoute(<AgentsPage />)
    await screen.findByText('Abe')
    expect(screen.getByRole('link', { name: /New agent/ })).toBeTruthy()
    expect(screen.getByText('History')).toBeTruthy()
    unmount()
    renderRoute(<AgentsPage />, { perms: without('agents.create', 'history.view') })
    await screen.findByText('Abe')
    expect(screen.queryByRole('link', { name: /New agent/ })).toBeNull()
    expect(screen.queryByText('History')).toBeNull()
  })
})
