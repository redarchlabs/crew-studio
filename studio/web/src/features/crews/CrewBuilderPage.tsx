import { useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { Play } from 'lucide-react'
import { toast } from 'sonner'
import { ApiError } from '../../api/client'
import { crewsApi, tasksApi } from '../../api/endpoints'
import type { Crew, CrewBody } from '../../api/types'
import { Button } from '../../components/Button'
import { AreaPicker, defaultAreaIds } from '../../components/AreaChips'
import { ItemEditorHeader } from '../../components/ItemEditorHeader'
import { FormErrors, NumberField, SelectField, TextAreaField, TextField } from '../../components/Field'
import { MultiSelect } from '../../components/MultiSelect'
import { Panel } from '../../components/PageHeader'
import { LockableFieldset, ReadOnlyNote } from '../../components/ReadOnlyNote'
import { AsyncView, EmptyState, ErrorState, Loading } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { sortByName, type Catalog } from '../../lib/catalog'
import { editAccess } from '../../lib/editAccess'
import { notifyError, notifySuccess } from '../../lib/notify'
import { agentTexts, detectCrewInputs, placeholdersIn, taskTexts } from '../../lib/placeholders'
import { useAsync } from '../../lib/useAsync'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { loadVisibleCatalog } from '../../lib/visibleCatalog'
import { Tabs } from '../../components/Tabs'
import { CrewCanvas } from './CrewCanvas'
import { canSeeCrewState, CrewStatePanel } from './state/CrewStatePanel'
import { MemoryPanel } from './memory/MemoryPanel'
import { ChatSettingsPanel } from './ChatSettingsPanel'
import { EscalationPanel } from './EscalationPanel'
import { CREW_FIELDS, crewBodyFromDraft, newCrewBody, normalizeCrew, toCrewBody, validateCrew } from './crewForm'
import { crewToGraph } from './crewGraph'
import { InputsEditor } from './InputsEditor'
import { RunCrewModal } from './RunCrewModal'
import { TaskOrderList } from './TaskOrderList'
import { ValidationPanel } from './ValidationPanel'

type CrewBuilderProps = {
  crew: Crew | null
  catalog: Catalog
  /** Starting value for a new crew drafted from a task (?fromTask=) */
  draft?: CrewBody | null
}

function CrewBuilder({ crew, catalog, draft = null }: CrewBuilderProps) {
  const navigate = useNavigate()
  const { me, can } = useAuth()
  const access = editAccess('crews', !crew, can)
  const canRun = can('crews.run')
  const formTop = useRef<HTMLDivElement>(null)
  const [deleting, setDeleting] = useState(false)
  const [running, setRunning] = useState(false)
  const [saved, setSaved] = useState<Crew | null>(crew)
  // ?tab=state shows the crew's state (notes, action log, connections) instead of its design; ?tab=memory its memories
  const [params, setParams] = useSearchParams()
  const stateTab = !!saved && canSeeCrewState(can)
  const memoryTab = !!saved && can('state.view')
  const requested = params.get('tab')
  const tab = stateTab && requested === 'state' ? 'state' : memoryTab && requested === 'memory' ? 'memory' : 'design'
  const form = useEntityForm<CrewBody, Crew>({
    entityId: crew?.id ?? null,
    initial: crew ? toCrewBody(crew) : (draft ?? newCrewBody(defaultAreaIds(me))),
    fields: CREW_FIELDS,
    label: 'crew',
    validate: validateCrew,
    normalize: normalizeCrew,
    create: crewsApi.create,
    update: crewsApi.update,
  })
  useUnsavedGuard(form.dirty)
  const validation = useAsync((signal) => (saved ? crewsApi.validate(saved.id, signal) : Promise.resolve(undefined)), [saved])
  const { value: v, set, errors } = form
  const f = errors.fields

  const graph = useMemo(() => crewToGraph(v, catalog), [v, catalog])
  const draftWarnings = useMemo(() => [...new Set(graph.nodes.flatMap((n) => n.warnings.map((w) => `${n.label}: ${w}`)))], [graph])

  const usedKeys = useMemo(() => {
    const taskById = new Map(catalog.tasks.map((t) => [t.id, t]))
    const agentById = new Map(catalog.agents.map((a) => [a.id, a]))
    const tasks = v.task_ids.flatMap((id) => taskById.get(id) ?? [])
    const agentIds = new Set([...v.agent_ids, ...tasks.flatMap((t) => (t.agent_id ? [t.agent_id] : []))])
    const agents = [...agentIds].flatMap((id) => agentById.get(id) ?? [])
    return new Set(placeholdersIn([...tasks.flatMap(taskTexts), ...agents.flatMap(agentTexts)]))
  }, [v.task_ids, v.agent_ids, catalog])

  const detect = () => {
    const { inputs, added } = detectCrewInputs(v.inputs, v.task_ids, v.agent_ids, catalog.tasks, catalog.agents)
    if (added.length === 0) toast.info('No new placeholders found', { description: 'Every {key} in this crew already has an input.' })
    else {
      set('inputs', inputs)
      toast.success(`Added ${added.length} input${added.length === 1 ? '' : 's'}`, { description: added.join(', ') })
    }
  }

  // Adding a task also adds its agent to the crew, since the agent must be a crew member.
  const onTasksChange = (taskIds: string[]) => {
    const added = taskIds.filter((id) => !v.task_ids.includes(id))
    const newAgents = added.flatMap((id) => catalog.tasks.find((t) => t.id === id)?.agent_id ?? []).filter((a) => !v.agent_ids.includes(a))
    form.update((prev) => ({ ...prev, task_ids: taskIds, agent_ids: [...new Set([...prev.agent_ids, ...newAgents])] }))
  }

  const onSave = async () => {
    const result = await form.save()
    if (!result) return
    setSaved(result)
    if (!crew) navigate(`/crews/${result.id}`, { replace: true, state: SKIP_GUARD })
  }

  const onDelete = async () => {
    if (!crew) return
    setDeleting(true)
    try {
      await crewsApi.remove(crew.id)
      notifySuccess(`Deleted ${crew.name}`)
      navigate('/crews', { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${crew.name}`, err)
    } finally {
      setDeleting(false)
    }
  }

  // Keep a manager LLM the person cannot list (no llms.view) selectable so saving keeps it
  const hiddenLlm = v.manager_llm_id && !catalog.llms.some((l) => l.id === v.manager_llm_id) ? [{ value: v.manager_llm_id, label: 'Assigned LLM (not visible to you)' }] : []
  const llmOptions = [{ value: '', label: 'Choose a manager LLM…' }, ...sortByName(catalog.llms).map((l) => ({ value: l.id, label: `${l.name} — ${l.model}` })), ...hiddenLlm]
  const agentOptions = sortByName(catalog.agents).map((a) => ({ value: a.id, label: a.name, hint: a.role }))
  const runBlocked = !saved || form.dirty

  return (
    <>
      <ItemEditorHeader
        canSave={access.canSave}
        canDelete={access.canDelete}
        backTo="/crews"
        backLabel="Crews"
        title={crew ? crew.name : 'New crew'}
        isNew={!crew}
        dirty={form.dirty}
        saving={form.saving}
        entityType="crew"
        entityId={crew?.id}
        onSave={() => void onSave()}
        onDelete={() => void onDelete()}
        deleting={deleting}
        extra={
          canRun && (
            <Button icon={<Play className="size-4" />} disabled={runBlocked} title={runBlocked ? 'Save the crew before running it' : 'Start a run'} onClick={() => setRunning(true)}>
              Run
            </Button>
          )
        }
      />
      {access.missing && <ReadOnlyNote perm={access.missing} />}
      {draft && !crew && (
        <p className="mb-4 rounded-md border border-accent/40 bg-accent-soft px-3 py-2 text-[12.5px] text-muted">
          Drafted from a task: it runs that task and the tasks it takes context from, with their agents and inputs. Review it, then Create.
        </p>
      )}
      {stateTab && (
        <div className="mb-4">
          <Tabs tabs={[{ key: 'design', label: 'Design' }, { key: 'state', label: 'State' }, ...(memoryTab ? [{ key: 'memory' as const, label: 'Memory' }] : [])]} active={tab}
            onChange={(k) => setParams(k === 'design' ? {} : { tab: k }, { replace: true })} />
        </div>
      )}
      {tab === 'state' && saved ? <CrewStatePanel crewId={saved.id} /> : tab === 'memory' && saved ? <MemoryPanel crewId={saved.id} llms={catalog.llms} /> : (
      <div className="grid gap-5 xl:grid-cols-[minmax(360px,440px)_minmax(0,1fr)]">
        <div className="flex min-w-0 flex-col gap-4">
        <LockableFieldset locked={!access.canSave}>
        <div ref={formTop} className="flex min-w-0 scroll-mt-24 flex-col gap-4">
          <Panel title="Crew">
            <div className="flex flex-col gap-3">
              <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder="Market research" />
              <TextAreaField label="Description" rows={2} value={v.description} onChange={(x) => set('description', x)} error={f.description} />
              <AreaPicker value={v.area_ids} onChange={(x) => set('area_ids', x)} error={f.area_ids} />
              <div className="grid gap-3 sm:grid-cols-2">
                <SelectField label="Process" value={v.process} onChange={(x) => set('process', x === 'hierarchical' ? 'hierarchical' : 'sequential')} options={[{ value: 'sequential', label: 'Sequential' }, { value: 'hierarchical', label: 'Hierarchical' }]} error={f.process} />
                <NumberField label="Budget per run (USD)" value={v.budget_usd} onChange={(x) => set('budget_usd', x ?? Number.NaN)} min={0} step="any" error={f.budget_usd} />
              </div>
              {v.process === 'hierarchical' && (
                <SelectField label="Manager LLM" value={v.manager_llm_id ?? ''} onChange={(x) => set('manager_llm_id', x || null)} options={llmOptions} error={f.manager_llm_id} hint="The manager plans and delegates tasks to the crew's agents." />
              )}
            </div>
          </Panel>
          <Panel title="Work">
            <div className="flex flex-col gap-4">
              <TaskOrderList taskIds={v.task_ids} agentIds={v.agent_ids} tasks={catalog.tasks} agents={catalog.agents} onChange={onTasksChange} onAddAgent={(id) => set('agent_ids', [...v.agent_ids, id])} error={f.task_ids} />
              <MultiSelect label="Agents" options={agentOptions} value={v.agent_ids} onChange={(x) => set('agent_ids', x)} error={f.agent_ids} emptyText="Create agents first." />
            </div>
          </Panel>
          <Panel title="Run inputs">
            <InputsEditor inputs={v.inputs} onChange={(x) => set('inputs', x)} onDetect={detect} usedKeys={usedKeys} error={f.inputs} />
          </Panel>
          <FormErrors messages={errors.general} />
        </div>
        </LockableFieldset>
        {saved && (
          <>
            <EscalationPanel crewId={saved.id} canEdit={access.canSave} />
            <ChatSettingsPanel crewId={saved.id} agentIds={saved.agent_ids} agents={catalog.agents} llms={catalog.llms} canEdit={access.canSave} />
          </>
        )}
        </div>
        <div className="flex min-w-0 flex-col gap-4 xl:sticky xl:top-20 xl:self-start">
          <Panel title="Crew graph" bodyClassName="p-3">
            <CrewCanvas graph={graph} catalog={catalog} onCrewClick={() => formTop.current?.scrollIntoView({ behavior: 'smooth' })} />
          </Panel>
          <ValidationPanel validation={validation.data} loading={validation.loading} error={validation.error} onRefresh={validation.reload} isNew={!saved} dirty={form.dirty} draftWarnings={draftWarnings} />
        </div>
      </div>
      )}
      {running && saved && canRun && <RunCrewModal crew={saved} onClose={() => setRunning(false)} />}
    </>
  )
}

/** The draft could not be made: the API message, its problems, and a way back to the task. */
function DraftError({ taskId, error, onRetry }: { taskId: string; error: unknown; onRetry: () => void }) {
  const details = error instanceof ApiError ? error.details : []
  return (
    <div className="flex flex-col gap-3">
      <ErrorState error={error} onRetry={onRetry} title="Could not make a crew from this task" />
      <FormErrors messages={details} />
      <Link className="text-accent" to={`/tasks/${encodeURIComponent(taskId)}`}>Back to the task</Link>
    </div>
  )
}

/** /crews/:id; /crews/new?fromTask=<taskId> starts from the crew the server drafts for that task. */
export default function CrewBuilderPage() {
  const { id = 'new' } = useParams()
  const [params] = useSearchParams()
  const fromTask = id === 'new' ? params.get('fromTask') : null
  const { can } = useAuth()
  const res = useAsync((signal) => loadVisibleCatalog(can, signal), [id])
  const draft = useAsync((signal) => (fromTask ? tasksApi.crewDraft(fromTask, signal).then(crewBodyFromDraft) : Promise.resolve(null)), [fromTask])
  if (fromTask && draft.error) return <DraftError taskId={fromTask} error={draft.error} onRetry={draft.reload} />
  return (
    <AsyncView {...res}>
      {(catalog) => {
        if (fromTask && !draft.data) return <Loading label="Drafting a crew from the task…" />
        const crew = id === 'new' ? null : catalog.crews.find((c) => c.id === id)
        if (crew === undefined) return <EmptyState title="Crew not found" hint="It may have been deleted." action={<Link className="text-accent" to="/crews">Back to crews</Link>} />
        return <CrewBuilder key={`${id}:${fromTask ?? ''}`} crew={crew} catalog={catalog} draft={draft.data} />
      }}
    </AsyncView>
  )
}
