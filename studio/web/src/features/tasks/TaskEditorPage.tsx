import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { Workflow } from 'lucide-react'
import { tasksApi } from '../../api/endpoints'
import type { Task, TaskInput } from '../../api/types'
import { AreaPicker, defaultAreaIds } from '../../components/AreaChips'
import { Button } from '../../components/Button'
import { ItemEditorHeader } from '../../components/ItemEditorHeader'
import { EditorLayout, UsageList } from '../../components/EditorLayout'
import { FormErrors, SelectField, TextField, Toggle } from '../../components/Field'
import { MultiSelect } from '../../components/MultiSelect'
import { Panel } from '../../components/PageHeader'
import { LockableFieldset, ReadOnlyNote } from '../../components/ReadOnlyNote'
import { PlaceholderTextarea } from '../../components/PlaceholderTextarea'
import { AsyncView, EmptyState } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { sortByName, type Catalog } from '../../lib/catalog'
import { editAccess } from '../../lib/editAccess'
import { notifyError, notifySuccess } from '../../lib/notify'
import { crewsUsingTask, inputKeysOf } from '../../lib/usage'
import { useAsync } from '../../lib/useAsync'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { loadVisibleCatalog } from '../../lib/visibleCatalog'
import { TASK_FIELDS, newTaskInput, toTaskInput, validateTask } from './taskForm'

function TaskEditor({ task, catalog }: { task: Task | null; catalog: Catalog }) {
  const navigate = useNavigate()
  const { me, can } = useAuth()
  const access = editAccess('tasks', !task, can)
  const [deleting, setDeleting] = useState(false)
  const form = useEntityForm<TaskInput, Task>({
    entityId: task?.id ?? null,
    initial: task ? toTaskInput(task) : newTaskInput(defaultAreaIds(me)),
    fields: TASK_FIELDS,
    label: 'task',
    validate: validateTask,
    normalize: (v) => ({ ...v, name: v.name.trim() }),
    create: tasksApi.create,
    update: tasksApi.update,
  })
  useUnsavedGuard(form.dirty)
  const { value: v, set, errors } = form
  const f = errors.fields
  const crews = task ? crewsUsingTask(task.id, catalog.crews) : []
  // The crew draft endpoint needs both permissions
  const canMakeCrew = Boolean(task) && can('tasks.view') && can('crews.create')
  const knownKeys = crews.length > 0 ? inputKeysOf(crews) : undefined

  const onSave = async () => {
    const saved = await form.save()
    if (saved && !task) navigate(`/tasks/${saved.id}`, { replace: true, state: SKIP_GUARD })
  }

  const onDelete = async () => {
    if (!task) return
    setDeleting(true)
    try {
      await tasksApi.remove(task.id)
      notifySuccess(`Deleted ${task.name}`)
      navigate('/tasks', { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${task.name}`, err)
    } finally {
      setDeleting(false)
    }
  }

  // Keep an agent the person cannot see (outside their areas, or no agents.view) selectable so saving keeps it
  const hiddenAgent = v.agent_id && !catalog.agents.some((a) => a.id === v.agent_id) ? [{ value: v.agent_id, label: 'Assigned agent (not visible to you)' }] : []
  const agentOptions = [{ value: '', label: 'Unassigned (manager decides in hierarchical crews)' }, ...sortByName(catalog.agents).map((a) => ({ value: a.id, label: `${a.name} — ${a.role}` })), ...hiddenAgent]
  const contextOptions = sortByName(catalog.tasks.filter((t) => t.id !== task?.id)).map((t) => ({ value: t.id, label: t.name, hint: t.expected_output.slice(0, 90) }))

  return (
    <>
      <ItemEditorHeader
        backTo="/tasks"
        backLabel="Tasks"
        title={task ? task.name : 'New task'}
        isNew={!task}
        dirty={form.dirty}
        saving={form.saving}
        entityType="task"
        entityId={task?.id}
        onSave={() => void onSave()}
        onDelete={() => void onDelete()}
        deleting={deleting}
        canSave={access.canSave}
        canDelete={access.canDelete}
        extra={
          canMakeCrew && task && (
            <Button icon={<Workflow className="size-4" />} disabled={form.dirty} title={form.dirty ? 'Save the task first' : 'Start a new crew that runs this task and the tasks it takes context from'} onClick={() => navigate(`/crews/new?fromTask=${encodeURIComponent(task.id)}`)}>
              Make a crew
            </Button>
          )
        }
      />
      {access.missing && <ReadOnlyNote perm={access.missing} />}
      <EditorLayout
        main={
          <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (access.canSave) void onSave() }} noValidate>
            <LockableFieldset locked={!access.canSave} className="flex flex-col gap-4">
            <Panel title="Task">
              <div className="flex flex-col gap-3">
                <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder="research_company" />
                <AreaPicker value={v.area_ids} onChange={(x) => set('area_ids', x)} error={f.area_ids} />
                <PlaceholderTextarea label="Description" rows={8} value={v.description} onChange={(x) => set('description', x)} error={f.description} placeholder="Research {company} and summarise its last quarter…" knownKeys={knownKeys} />
                <PlaceholderTextarea label="Expected output" rows={5} value={v.expected_output} onChange={(x) => set('expected_output', x)} error={f.expected_output} placeholder="A markdown report with sections…" knownKeys={knownKeys} />
              </div>
            </Panel>
            <Panel title="Execution">
              <div className="flex flex-col gap-3">
                <SelectField label="Agent" value={v.agent_id ?? ''} onChange={(x) => set('agent_id', x || null)} options={agentOptions} error={f.agent_id} hint="Required for sequential crews." aside={v.agent_id ? <Link to={`/agents/${v.agent_id}`} className="text-[11.5px] text-faint hover:text-accent">Open agent</Link> : undefined} />
                <MultiSelect label="Context tasks (their outputs are passed to this task)" options={contextOptions} value={v.context_task_ids} onChange={(x) => set('context_task_ids', x)} error={f.context_task_ids} emptyText="No other tasks exist yet." />
                <Toggle label="Markdown output" checked={v.markdown} onChange={(x) => set('markdown', x)} hint="Ask the agent to format its answer as markdown." />
              </div>
            </Panel>
            <FormErrors messages={errors.general} />
            </LockableFieldset>
          </form>
        }
        side={
          <Panel title="Used by">
            <UsageList title="Crews" empty="Not in any crew yet." items={crews.map((c) => ({ key: c.id, node: <Link className="hover:text-accent" to={`/crews/${c.id}`}>{c.name}</Link> }))} />
            <p className="mt-3 text-[11.5px] text-faint">Context tasks must run earlier in the same crew; the crew builder flags ordering problems.</p>
            {canMakeCrew && <p className="mt-2 text-[11.5px] text-faint">Work orders, schedules and triggers run crews. To run just this task, use Make a crew.</p>}
          </Panel>
        }
      />
    </>
  )
}

export function TaskEditorPage() {
  const { id = 'new' } = useParams()
  const { can } = useAuth()
  const res = useAsync((signal) => loadVisibleCatalog(can, signal), [id])
  return (
    <AsyncView {...res}>
      {(catalog) => {
        const task = id === 'new' ? null : catalog.tasks.find((t) => t.id === id)
        if (task === undefined) return <EmptyState title="Task not found" hint="It may have been deleted." action={<Link className="text-accent" to="/tasks">Back to tasks</Link>} />
        return <TaskEditor key={id} task={task} catalog={catalog} />
      }}
    </AsyncView>
  )
}
