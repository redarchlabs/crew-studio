import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowDown, ArrowUp, Plus, TriangleAlert, X } from 'lucide-react'
import type { Agent, Task } from '../../api/types'
import { Button, IconButton } from '../../components/Button'
import { sortByName } from '../../lib/catalog'
import { moveItem } from './crewForm'

type TaskOrderListProps = {
  taskIds: readonly string[]
  agentIds: readonly string[]
  tasks: readonly Task[]
  agents: readonly Agent[]
  onChange: (taskIds: string[]) => void
  onAddAgent: (agentId: string) => void
  error?: string
}

/** Ordered task list with reorder, remove and add. */
export function TaskOrderList({ taskIds, agentIds, tasks, agents, onChange, onAddAgent, error }: TaskOrderListProps) {
  const [adding, setAdding] = useState('')
  const taskById = new Map(tasks.map((t) => [t.id, t]))
  const agentById = new Map(agents.map((a) => [a.id, a]))
  const available = sortByName(tasks.filter((t) => !taskIds.includes(t.id)))

  const add = () => {
    if (!adding) return
    onChange([...taskIds, adding])
    setAdding('')
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between">
        <span className="text-[12px] font-medium text-muted">Tasks (execution order)</span>
        <span className="font-mono text-[11px] text-faint">{taskIds.length}</span>
      </div>
      <ol className="flex flex-col gap-1">
        {taskIds.length === 0 && <li className="rounded-md border border-dashed border-line px-3 py-3 text-[12px] text-faint">No tasks yet. Add one below.</li>}
        {taskIds.map((id, idx) => {
          const task = taskById.get(id)
          const agent = task?.agent_id ? agentById.get(task.agent_id) : undefined
          const agentMissing = task?.agent_id ? !agentIds.includes(task.agent_id) : false
          return (
            <li key={id} className="flex items-center gap-2 rounded-md border border-line bg-raised px-2 py-1.5">
              <span className="w-5 text-center font-mono text-[11px] text-faint">{idx + 1}</span>
              <div className="min-w-0 flex-1">
                {task ? <Link to={`/tasks/${id}`} className="block truncate text-[12.5px] font-medium hover:text-accent">{task.name}</Link> : <span className="text-danger">Missing task</span>}
                <div className="flex items-center gap-1 truncate text-[11.5px] text-faint">
                  {agent ? agent.name : task?.agent_id ? 'missing agent' : 'no agent'}
                  {agentMissing && task?.agent_id && (
                    <button type="button" className="inline-flex items-center gap-1 text-warn hover:underline" onClick={() => task.agent_id && onAddAgent(task.agent_id)}>
                      <TriangleAlert className="size-3" /> not in crew — add
                    </button>
                  )}
                </div>
              </div>
              <IconButton label="Move up" icon={<ArrowUp className="size-3.5" />} disabled={idx === 0} onClick={() => onChange(moveItem(taskIds, idx, -1))} />
              <IconButton label="Move down" icon={<ArrowDown className="size-3.5" />} disabled={idx === taskIds.length - 1} onClick={() => onChange(moveItem(taskIds, idx, 1))} />
              <IconButton label="Remove task" tone="danger" icon={<X className="size-3.5" />} onClick={() => onChange(taskIds.filter((t) => t !== id))} />
            </li>
          )
        })}
      </ol>
      <div className="flex gap-2">
        <select aria-label="Task to add" className="field-input" value={adding} onChange={(e) => setAdding(e.target.value)}>
          <option value="">{available.length === 0 ? 'All tasks are in this crew' : 'Choose a task to add…'}</option>
          {available.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <Button icon={<Plus className="size-3.5" />} disabled={!adding} onClick={add}>Add</Button>
      </div>
      {error && <div className="text-[12px] text-danger">{error}</div>}
    </div>
  )
}
