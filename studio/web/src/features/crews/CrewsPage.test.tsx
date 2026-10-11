import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentsApi, crewsApi, tasksApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeAgent, makeCrew, makeTask } from '../../lib/testFixtures'
import { CrewsPage } from './CrewsPage'
import { renderRoute, setupApi, without } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))

beforeEach(() => {
  setupApi()
  vi.mocked(crewsApi.list).mockResolvedValue([
    makeCrew({ id: 'c1', name: 'Zeta crew', description: 'Does research', task_ids: ['t1', 't2'], agent_ids: ['a1'], inputs: [{ key: 'topic', label: 'Topic', default: '', required: true }], area_ids: ['area-1'], process: 'hierarchical' }),
    makeCrew({ id: 'c2', name: 'Alpha crew', description: '', task_ids: [], area_ids: ['area-2'] }),
  ])
  vi.mocked(tasksApi.list).mockResolvedValue([makeTask({ id: 't1', name: 'Gather' })])
  vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1' })])
})
afterEach(cleanup)

const names = () => screen.getAllByRole('link').filter((l) => l.getAttribute('href')?.startsWith('/crews/c')).map((l) => l.textContent)

describe('CrewsPage', () => {
  it('shows a loading state then crews sorted by name', async () => {
    renderRoute(<CrewsPage />)
    expect(await screen.findByText('Alpha crew')).toBeTruthy()
    expect(names()).toEqual(['Alpha crew', 'Zeta crew'])
  })

  it('shows the pipeline with task names, unknown tasks as ?, and inputs', async () => {
    renderRoute(<CrewsPage />)
    expect(await screen.findByText('Gather → ?')).toBeTruthy()
    expect(screen.getByText('no tasks')).toBeTruthy()
    expect(screen.getByText('topic')).toBeTruthy()
    expect(screen.getByText('hierarchical')).toBeTruthy()
    expect(screen.getByText('sequential')).toBeTruthy()
  })

  it('filters by search text over name and description', async () => {
    renderRoute(<CrewsPage />)
    await screen.findByText('Alpha crew')
    fireEvent.change(screen.getByPlaceholderText('Filter crews…'), { target: { value: 'RESEARCH' } })
    expect(names()).toEqual(['Zeta crew'])
    fireEvent.change(screen.getByPlaceholderText('Filter crews…'), { target: { value: 'nomatch' } })
    expect(screen.getByText(/No crews match/)).toBeTruthy()
  })

  it('filters by area via the select and the URL', async () => {
    renderRoute(<CrewsPage />, { url: '/?area=area-2' })
    await screen.findByText('Alpha crew')
    expect(names()).toEqual(['Alpha crew'])
    const select = screen.getByLabelText('Filter by functional area') as HTMLSelectElement
    await screen.findByRole('option', { name: 'Finance' })
    fireEvent.change(select, { target: { value: 'area-1' } })
    expect(names()).toEqual(['Zeta crew'])
    fireEvent.change(select, { target: { value: '' } })
    expect(names()).toHaveLength(2)
  })

  it('says so when an area has no crews, and mentions the area for a search miss', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Only', area_ids: ['area-1'] })])
    renderRoute(<CrewsPage />, { url: '/?area=area-2' })
    expect(await screen.findByText('No crews in this area.')).toBeTruthy()
    fireEvent.change(screen.getByPlaceholderText('Filter crews…'), { target: { value: 'zzz' } })
    expect(screen.getByText(/in this area/)).toBeTruthy()
  })

  it('shows the empty state with a build link for creators', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([])
    renderRoute(<CrewsPage />)
    expect(await screen.findByText('No crews yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Build a crew' }).getAttribute('href')).toBe('/crews/new')
  })

  it('shows a plain empty state without crews.create', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([])
    renderRoute(<CrewsPage />, { perms: without('crews.create') })
    expect(await screen.findByText(/No crews in your functional areas yet/)).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'Build a crew' })).toBeNull()
    expect(screen.queryByRole('link', { name: /New crew/ })).toBeNull()
  })

  it('shows an error with retry', async () => {
    vi.mocked(crewsApi.list).mockRejectedValueOnce(new ApiError(500, { code: 'boom', message: 'Server exploded' }))
    renderRoute(<CrewsPage />)
    expect(await screen.findByText('Server exploded')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('Alpha crew')).toBeTruthy()
  })

  it('opens the run modal for a crew and closes it', async () => {
    renderRoute(<CrewsPage />)
    await screen.findByText('Alpha crew')
    const row = screen.getByText('Zeta crew').closest('tr') as HTMLElement
    fireEvent.click(within(row).getByRole('button', { name: 'Run' }))
    expect(await screen.findByText('Run Zeta crew')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('hides run buttons and column without crews.run', async () => {
    renderRoute(<CrewsPage />, { perms: without('crews.run') })
    await screen.findByText('Alpha crew')
    expect(screen.queryByRole('button', { name: 'Run' })).toBeNull()
  })

  it('hides the history link without history.view', async () => {
    renderRoute(<CrewsPage />, { perms: without('history.view') })
    await screen.findByText('Alpha crew')
    expect(screen.queryByText('History')).toBeNull()
  })

  it('shows history link and new crew action with permissions', async () => {
    renderRoute(<CrewsPage />)
    await screen.findByText('Alpha crew')
    expect(screen.getByText('History')).toBeTruthy()
    expect(screen.getByRole('link', { name: /New crew/ }).getAttribute('href')).toBe('/crews/new')
  })

  it('does not request collections the person cannot view', async () => {
    renderRoute(<CrewsPage />, { perms: ['crews.view'] })
    await screen.findByText('Alpha crew')
    expect(agentsApi.list).not.toHaveBeenCalled()
    expect(tasksApi.list).not.toHaveBeenCalled()
  })
})
