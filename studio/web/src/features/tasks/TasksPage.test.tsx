import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentsApi, crewsApi, tasksApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import { makeAgent, makeCrew, makeTask } from '../../lib/testFixtures'
import { renderRoute, setupApi, without } from '../crews/testUtils'
import { TasksPage } from './TasksPage'

vi.mock('../../api/endpoints')

beforeEach(() => {
  setupApi()
  vi.mocked(tasksApi.list).mockResolvedValue([
    makeTask({ id: 't1', name: 'Zip', description: 'Research {company} deeply', expected_output: 'Report on {region}', agent_id: 'a1', area_ids: ['area-1'], context_task_ids: ['t2', 'gone'] }),
    makeTask({ id: 't2', name: 'Abc', description: '', agent_id: null, area_ids: ['area-2'] }),
    makeTask({ id: 't3', name: 'Mid', description: 'x'.repeat(300), agent_id: 'hidden', area_ids: ['area-1'] }),
  ])
  vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1', name: 'Ann' })])
  vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Crew One', task_ids: ['t1'] })])
})
afterEach(cleanup)

const rowNames = () => screen.getAllByRole('link').filter((l) => /^\/tasks\/t/.test(l.getAttribute('href') ?? '')).map((l) => l.textContent)
const row = (name: string) => screen.getByRole('link', { name }).closest('tr') as HTMLElement

describe('TasksPage', () => {
  it('lists tasks sorted by name', async () => {
    renderRoute(<TasksPage />)
    await screen.findByText('Abc')
    expect(rowNames()).toEqual(['Abc', 'Mid', 'Zip'])
  })

  it('shows agent link, unassigned, and not-visible agents', async () => {
    renderRoute(<TasksPage />)
    await screen.findByText('Abc')
    expect(within(row('Zip')).getByRole('link', { name: 'Ann' }).getAttribute('href')).toBe('/agents/a1')
    expect(within(row('Abc')).getByText('Unassigned')).toBeTruthy()
    expect(within(row('Mid')).getByText('Not visible')).toBeTruthy()
  })

  it('shows context task names, missing context, placeholder keys, crews and truncated descriptions', async () => {
    renderRoute(<TasksPage />)
    await screen.findByText('Abc')
    const zip = row('Zip')
    expect(within(zip).getByText('Abc, missing')).toBeTruthy()
    expect(within(zip).getByText('company')).toBeTruthy()
    expect(within(zip).getByText('region')).toBeTruthy()
    expect(within(zip).getByText('Crew One')).toBeTruthy()
    expect(within(row('Abc')).getByText('No description')).toBeTruthy()
    const mid = within(row('Mid')).getByText(/^x+/)
    expect(mid.textContent!.length).toBeLessThan(300)
  })

  it('filters by search over name and description', async () => {
    renderRoute(<TasksPage />)
    await screen.findByText('Abc')
    const box = screen.getByPlaceholderText('Filter tasks…')
    fireEvent.change(box, { target: { value: 'DEEPLY' } })
    expect(rowNames()).toEqual(['Zip'])
    fireEvent.change(box, { target: { value: 'abc' } })
    expect(rowNames()).toEqual(['Abc'])
    fireEvent.change(box, { target: { value: 'zzz' } })
    expect(screen.getByText(/No tasks match “zzz”/)).toBeTruthy()
  })

  it('filters by area', async () => {
    renderRoute(<TasksPage />, { url: '/?area=area-2' })
    await screen.findByText('Abc')
    expect(rowNames()).toEqual(['Abc'])
    await screen.findByRole('option', { name: 'Finance' })
    fireEvent.change(screen.getByLabelText('Filter by functional area'), { target: { value: 'area-1' } })
    expect(rowNames()).toEqual(['Mid', 'Zip'])
    fireEvent.change(screen.getByLabelText('Filter by functional area'), { target: { value: '' } })
    expect(rowNames()).toHaveLength(3)
  })

  it('says when the area has no tasks, with and without a search', async () => {
    vi.mocked(tasksApi.list).mockResolvedValue([makeTask({ id: 't1', name: 'Only', area_ids: ['area-1'] })])
    renderRoute(<TasksPage />, { url: '/?area=area-2' })
    expect(await screen.findByText('No tasks in this area.')).toBeTruthy()
    fireEvent.change(screen.getByPlaceholderText('Filter tasks…'), { target: { value: 'q' } })
    expect(screen.getByText(/No tasks match “q” in this area/)).toBeTruthy()
  })

  it('empty state offers creation to creators only', async () => {
    vi.mocked(tasksApi.list).mockResolvedValue([])
    const { unmount } = renderRoute(<TasksPage />)
    expect(await screen.findByText('No tasks yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Create a task' }).getAttribute('href')).toBe('/tasks/new')
    unmount()
    renderRoute(<TasksPage />, { perms: without('tasks.create') })
    expect(await screen.findByText(/No tasks in your functional areas/)).toBeTruthy()
    expect(screen.queryByRole('link', { name: /Create a task|New task/ })).toBeNull()
  })

  it('shows an error and retries', async () => {
    vi.mocked(tasksApi.list).mockRejectedValueOnce(new ApiError(500, { code: 'x', message: 'tasks failed' }))
    renderRoute(<TasksPage />)
    expect(await screen.findByText('tasks failed')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect(await screen.findByText('Abc')).toBeTruthy()
  })

  it('gates the history link and new-task action', async () => {
    renderRoute(<TasksPage />, { perms: without('history.view', 'tasks.create') })
    await screen.findByText('Abc')
    expect(screen.queryByText('History')).toBeNull()
    expect(screen.queryByRole('link', { name: /New task/ })).toBeNull()
  })
})
