import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { watchesApi } from '../../api/endpoints.watch'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { crewsApi } from '../../api/endpoints'
import { crewStateApi } from '../../api/endpoints.crewstate'
import { trackingApi } from '../../api/endpoints.trackers'
import { makeCrew } from '../../lib/testFixtures'
import CrewBuilderPage from './CrewBuilderPage'
import { renderRoute, setupApi, without } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('../../api/endpoints.crewstate')
vi.mock('../../api/endpoints.watch')
vi.mock('../../api/endpoints.trackers')
vi.mock('../../components/graph/FlowCanvas', async () => ({ FlowCanvas: (await import('./testUtils')).FlowCanvasStub }))

beforeEach(() => {
  setupApi()
  vi.mocked(watchesApi.ofCrew).mockResolvedValue([])
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Research', area_ids: ['area-1'] })])
  vi.mocked(crewsApi.validate).mockResolvedValue({ valid: true, errors: [] })
  vi.mocked(crewStateApi.notes).mockResolvedValue([])
  vi.mocked(crewStateApi.log).mockResolvedValue({ items: [], meta: { total: 0, limit: 25, offset: 0 } })
  vi.mocked(crewStateApi.connections).mockResolvedValue([])
  vi.mocked(trackingApi.list).mockResolvedValue({ items: [], connections: [] })
})
afterEach(cleanup)

const open = (url: string, perms?: Parameters<typeof without>) =>
  renderRoute(<CrewBuilderPage />, { path: '/crews/:id', url, ...(perms ? { perms: without(...perms) } : {}) })

describe('CrewBuilderPage: State tab', () => {
  it('switches between the design and the crew state', async () => {
    const { router } = open('/crews/c1')
    fireEvent.click(await screen.findByRole('tab', { name: 'State' }))
    expect(await screen.findByText('No notes yet')).toBeTruthy()
    expect(router.state.location.search).toBe('?tab=state')
    expect(screen.queryByLabelText('Name')).toBeNull()
    fireEvent.click(screen.getByRole('tab', { name: 'Design' }))
    await waitFor(() => expect(router.state.location.search).toBe(''))
    expect(await screen.findByLabelText('Name')).toBeTruthy()
  })

  it('opens on the state from the link', async () => {
    open('/crews/c1?tab=state')
    expect(await screen.findByText('Nothing logged yet')).toBeTruthy()
  })

  it('has no tabs for a new crew or without state.view and connections.view', async () => {
    open('/crews/c1?tab=state', ['state.view', 'connections.view'])
    expect(await screen.findByLabelText('Name')).toBeTruthy()
    expect(screen.queryByRole('tab')).toBeNull()
    cleanup()
    open('/crews/new')
    expect(await screen.findByLabelText('Name')).toBeTruthy()
    expect(screen.queryByRole('tab')).toBeNull()
  })
})
