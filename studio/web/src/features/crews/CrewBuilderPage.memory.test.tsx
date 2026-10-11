import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { crewsApi } from '../../api/endpoints'
import { crewStateApi } from '../../api/endpoints.crewstate'
import { memoryApi } from '../../api/endpoints.memory'
import { trackingApi } from '../../api/endpoints.trackers'
import { watchesApi } from '../../api/endpoints.watch'
import { makeCrew } from '../../lib/testFixtures'
import CrewBuilderPage from './CrewBuilderPage'
import { makeSettingsView } from './memory/testUtils'
import { renderRoute, setupApi, without } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')
vi.mock('../../api/endpoints.memory')
vi.mock('../../api/endpoints.watch')
vi.mock('../../api/endpoints.trackers')
vi.mock('../../components/graph/FlowCanvas', async () => ({ FlowCanvas: (await import('./testUtils')).FlowCanvasStub }))

beforeEach(() => {
  setupApi()
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Research', area_ids: ['area-1'] })])
  vi.mocked(crewsApi.validate).mockResolvedValue({ valid: true, errors: [] })
  vi.mocked(memoryApi.settings).mockResolvedValue(makeSettingsView())
  vi.mocked(memoryApi.list).mockResolvedValue({ items: [], meta: { total: 0, limit: 25, offset: 0 } })
  vi.mocked(memoryApi.scopes).mockResolvedValue([])
  vi.mocked(watchesApi.ofCrew).mockResolvedValue([])
  vi.mocked(crewStateApi.notes).mockResolvedValue([])
  vi.mocked(crewStateApi.log).mockResolvedValue({ items: [], meta: { total: 0, limit: 25, offset: 0 } })
  vi.mocked(crewStateApi.connections).mockResolvedValue([])
  vi.mocked(trackingApi.list).mockResolvedValue({ items: [], connections: [] })
})
afterEach(cleanup)

const open = (url: string, perms?: Parameters<typeof without>) =>
  renderRoute(<CrewBuilderPage />, { path: '/crews/:id', url, ...(perms ? { perms: without(...perms) } : {}) })

describe('CrewBuilderPage: Memory tab', () => {
  it('switches between the design, the state and the memories', async () => {
    const { router } = open('/crews/c1')
    fireEvent.click(await screen.findByRole('tab', { name: 'Memory' }))
    expect(await screen.findByText('No memories yet')).toBeTruthy()
    expect(router.state.location.search).toBe('?tab=memory')
    expect(screen.queryByLabelText('Name')).toBeNull()
    fireEvent.click(screen.getByRole('tab', { name: 'State' }))
    await waitFor(() => expect(router.state.location.search).toBe('?tab=state'))
    fireEvent.click(screen.getByRole('tab', { name: 'Design' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(await screen.findByLabelText('Name')).toBeTruthy()
  })

  it('opens on the memories from the link', async () => {
    open('/crews/c1?tab=memory')
    expect(await screen.findByRole('button', { name: 'Save memory settings' })).toBeTruthy()
  })

  it('hides the tab without state.view, even with connections.view', async () => {
    open('/crews/c1?tab=memory', ['state.view'])
    expect(await screen.findByLabelText('Name')).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'State' })).toBeTruthy()
    expect(screen.queryByRole('tab', { name: 'Memory' })).toBeNull()
    expect(memoryApi.settings).not.toHaveBeenCalled()
  })
})
