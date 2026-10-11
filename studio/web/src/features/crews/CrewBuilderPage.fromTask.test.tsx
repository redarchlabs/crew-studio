import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentsApi, crewsApi, llmsApi, tasksApi, toolsApi } from '../../api/endpoints'
import { ApiError } from '../../api/client'
import type { CrewDraft } from '../../api/types'
import { makeAgent, makeCrew, makeTask } from '../../lib/testFixtures'
import CrewBuilderPage from './CrewBuilderPage'
import { renderRoute, setupApi } from './testUtils'

vi.mock('../../api/endpoints')
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }))
vi.mock('../../components/graph/FlowCanvas', async () => ({ FlowCanvas: (await import('./testUtils')).FlowCanvasStub }))

const tasks = [
  makeTask({ id: 't0', name: 'Corporate Profile', agent_id: 'a1' }),
  makeTask({ id: 't1', name: 'Investment Memo', agent_id: 'a1', context_task_ids: ['t0'] }),
]
const draft: CrewDraft = {
  name: 'Investment Memo (task)',
  description: 'Runs Investment Memo',
  process: 'sequential',
  agent_ids: ['a1'],
  task_ids: ['t0', 't1'],
  inputs: [{ key: 'company', label: 'Company', default: 'Acme', required: true }],
  budget_usd: 2.5,
  area_ids: ['area-1'],
}

beforeEach(() => {
  setupApi()
  Element.prototype.scrollIntoView = vi.fn()
  vi.mocked(agentsApi.list).mockResolvedValue([makeAgent({ id: 'a1', name: 'Ann' })])
  vi.mocked(tasksApi.list).mockResolvedValue(tasks)
  vi.mocked(llmsApi.list).mockResolvedValue([])
  vi.mocked(toolsApi.list).mockResolvedValue([])
  vi.mocked(crewsApi.list).mockResolvedValue([])
  vi.mocked(crewsApi.validate).mockResolvedValue({ valid: true, errors: [] })
  vi.mocked(tasksApi.crewDraft).mockResolvedValue(draft)
})
afterEach(cleanup)

const open = (url: string) => renderRoute(<CrewBuilderPage />, { path: '/crews/:id', url })
const nameInput = () => screen.findByLabelText('Name') as Promise<HTMLInputElement>

describe('CrewBuilderPage: new crew from a task', () => {
  it('shows a loading state, then the crew draft as the starting value', async () => {
    let resolve!: (d: CrewDraft) => void
    vi.mocked(tasksApi.crewDraft).mockReturnValue(new Promise((r) => { resolve = r }))
    open('/crews/new?fromTask=t1')
    expect(await screen.findByText('Drafting a crew from the task…')).toBeTruthy()
    resolve(draft)
    expect((await nameInput()).value).toBe('Investment Memo (task)')
    expect(tasksApi.crewDraft).toHaveBeenCalledWith('t1', expect.anything())
    expect((screen.getByLabelText('Budget per run (USD)') as HTMLInputElement).value).toBe('2.5')
    expect(screen.getByText('Corporate Profile')).toBeTruthy()
    expect(screen.getByText('Investment Memo')).toBeTruthy()
    expect(screen.getByText(/Drafted from a task/)).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'New crew' })).toBeTruthy()
  })

  it('creates the crew through the normal create path', async () => {
    vi.mocked(crewsApi.create).mockResolvedValue(makeCrew({ id: 'new-1', name: draft.name }))
    const view = open('/crews/new?fromTask=t1')
    await nameInput()
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(crewsApi.create).toHaveBeenCalledWith({ ...draft, manager_llm_id: null }))
    await waitFor(() => expect(view.router.state.location.pathname).toBe('/crews/new-1'))
  })

  it('shows the API message and problem details when no crew can be made', async () => {
    vi.mocked(tasksApi.crewDraft).mockRejectedValue(new ApiError(422, { code: 'invalid', message: 'This task cannot run as a crew', details: ['task Investment Memo has no agent', 'context task Hidden is outside your areas'] }))
    open('/crews/new?fromTask=t1')
    expect(await screen.findByText('Could not make a crew from this task')).toBeTruthy()
    expect(screen.getByText('This task cannot run as a crew')).toBeTruthy()
    expect(screen.getByText('task Investment Memo has no agent')).toBeTruthy()
    expect(screen.getByText('context task Hidden is outside your areas')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Back to the task' }).getAttribute('href')).toBe('/tasks/t1')
    expect(screen.queryByLabelText('Name')).toBeNull()
  })

  it('shows a 404 and retries on request', async () => {
    vi.mocked(tasksApi.crewDraft).mockRejectedValueOnce(new ApiError(404, { code: 'not_found', message: 'Task not found.' }))
    open('/crews/new?fromTask=gone')
    expect(await screen.findByText('Task not found.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }))
    expect((await nameInput()).value).toBe('Investment Memo (task)')
  })

  it('ignores fromTask for an existing crew and starts blank without it', async () => {
    vi.mocked(crewsApi.list).mockResolvedValue([makeCrew({ id: 'c1', name: 'Saved crew' })])
    open('/crews/c1?fromTask=t1')
    expect((await nameInput()).value).toBe('Saved crew')
    cleanup()
    open('/crews/new')
    expect((await nameInput()).value).toBe('')
    expect(tasksApi.crewDraft).not.toHaveBeenCalled()
    expect(screen.queryByText(/Drafted from a task/)).toBeNull()
  })
})
