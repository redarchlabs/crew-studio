import { useState } from 'react'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeAgent, makeTask } from '../../lib/testFixtures'
import { TaskOrderList } from './TaskOrderList'

afterEach(cleanup)

const tasks = [
  makeTask({ id: 't1', name: 'Alpha', agent_id: 'a1' }),
  makeTask({ id: 't2', name: 'Beta', agent_id: 'a2' }),
  makeTask({ id: 't3', name: 'Gamma', agent_id: null }),
  makeTask({ id: 't4', name: 'Delta', agent_id: 'ghost' }),
]
const agents = [makeAgent({ id: 'a1', name: 'Ann' }), makeAgent({ id: 'a2', name: 'Bob' })]

function Harness({ initial, agentIds = ['a1', 'a2'], onAddAgent = () => {}, error }: { initial: string[]; agentIds?: string[]; onAddAgent?: (id: string) => void; error?: string }) {
  const [ids, setIds] = useState(initial)
  return (
    <MemoryRouter>
      <TaskOrderList taskIds={ids} agentIds={agentIds} tasks={tasks} agents={agents} onChange={setIds} onAddAgent={onAddAgent} error={error} />
      <pre data-testid="ids">{ids.join(',')}</pre>
    </MemoryRouter>
  )
}
const ids = () => screen.getByTestId('ids').textContent
const rows = () => screen.getAllByRole('listitem')

describe('TaskOrderList', () => {
  it('shows an empty hint and the count', () => {
    render(<Harness initial={[]} />)
    expect(screen.getByText(/No tasks yet/)).toBeTruthy()
    expect(screen.getByText('0')).toBeTruthy()
  })

  it('renders tasks in order with agent names and links', () => {
    render(<Harness initial={['t2', 't1']} />)
    const [first, second] = rows()
    expect(within(first).getByText('Beta').getAttribute('href')).toBe('/tasks/t2')
    expect(within(first).getByText('Bob')).toBeTruthy()
    expect(within(second).getByText('Alpha')).toBeTruthy()
  })

  it('disables move up on first and move down on last', () => {
    render(<Harness initial={['t1', 't2']} />)
    expect((screen.getAllByRole('button', { name: 'Move up' })[0] as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getAllByRole('button', { name: 'Move up' })[1] as HTMLButtonElement).disabled).toBe(false)
    expect((screen.getAllByRole('button', { name: 'Move down' })[1] as HTMLButtonElement).disabled).toBe(true)
  })

  it('reorders with move up / move down', () => {
    render(<Harness initial={['t1', 't2', 't3']} />)
    fireEvent.click(screen.getAllByRole('button', { name: 'Move down' })[0])
    expect(ids()).toBe('t2,t1,t3')
    fireEvent.click(screen.getAllByRole('button', { name: 'Move up' })[2])
    expect(ids()).toBe('t2,t3,t1')
  })

  it('removes a task', () => {
    render(<Harness initial={['t1', 't2']} />)
    fireEvent.click(screen.getAllByRole('button', { name: 'Remove task' })[0])
    expect(ids()).toBe('t2')
  })

  it('adds a chosen task, excluding ones already present, then resets the picker', () => {
    render(<Harness initial={['t1']} />)
    const select = screen.getByLabelText('Task to add') as HTMLSelectElement
    expect(Array.from(select.options).map((o) => o.textContent)).toEqual(['Choose a task to add…', 'Beta', 'Delta', 'Gamma'])
    const addBtn = screen.getByRole('button', { name: 'Add' }) as HTMLButtonElement
    expect(addBtn.disabled).toBe(true)
    fireEvent.change(select, { target: { value: 't3' } })
    expect(addBtn.disabled).toBe(false)
    fireEvent.click(addBtn)
    expect(ids()).toBe('t1,t3')
    expect(select.value).toBe('')
  })

  it('says when every task is already in the crew', () => {
    render(<Harness initial={['t1', 't2', 't3', 't4']} />)
    expect(screen.getByText('All tasks are in this crew')).toBeTruthy()
  })

  it('marks missing task, no agent and missing agent', () => {
    render(<Harness initial={['nope', 't3', 't4']} />)
    expect(screen.getByText('Missing task')).toBeTruthy()
    expect(screen.getAllByText('no agent')).toHaveLength(2)
    expect(screen.getByText(/missing agent/)).toBeTruthy()
  })

  it('offers to add an assigned agent that is not in the crew', () => {
    const onAddAgent = vi.fn()
    render(<Harness initial={['t2']} agentIds={['a1']} onAddAgent={onAddAgent} />)
    fireEvent.click(screen.getByRole('button', { name: /not in crew/ }))
    expect(onAddAgent).toHaveBeenCalledWith('a2')
  })

  it('does not warn when the agent is a crew member', () => {
    render(<Harness initial={['t1']} />)
    expect(screen.queryByRole('button', { name: /not in crew/ })).toBeNull()
  })

  it('shows an error', () => {
    render(<Harness initial={[]} error="task_ids bad" />)
    expect(screen.getByText('task_ids bad')).toBeTruthy()
  })
})
