import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { agentsApi } from '../../api/endpoints'
import type { Agent, AgentInput } from '../../api/types'
import { AreaPicker, defaultAreaIds } from '../../components/AreaChips'
import { EditorLayout, UsageList } from '../../components/EditorLayout'
import { FormErrors, NumberField, SelectField, TextField, Toggle } from '../../components/Field'
import { ItemEditorHeader } from '../../components/ItemEditorHeader'
import { Panel } from '../../components/PageHeader'
import { LockableFieldset, ReadOnlyNote } from '../../components/ReadOnlyNote'
import { PlaceholderTextarea } from '../../components/PlaceholderTextarea'
import { AsyncView, EmptyState } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { sortByName, type Catalog } from '../../lib/catalog'
import { editAccess } from '../../lib/editAccess'
import { notifyError, notifySuccess } from '../../lib/notify'
import { crewsUsingAgent, inputKeysOf, tasksUsingAgent } from '../../lib/usage'
import { useAsync } from '../../lib/useAsync'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { PriceMatchBadge } from '../llms/PriceMatchBadge'
import { loadVisibleCatalog } from '../../lib/visibleCatalog'
import { AGENT_FIELDS, newAgentInput, toAgentInput, validateAgent } from './agentForm'
import { AgentChatPanel } from './AgentChatPanel'
import { ToolPicker } from './ToolPicker'

function normalize(v: AgentInput): AgentInput {
  return { ...v, name: v.name.trim(), role: v.role.trim(), goal: v.goal.trim() }
}

function AgentEditor({ agent, catalog }: { agent: Agent | null; catalog: Catalog }) {
  const navigate = useNavigate()
  const { me, can } = useAuth()
  const access = editAccess('agents', !agent, can)
  const [deleting, setDeleting] = useState(false)
  const form = useEntityForm<AgentInput, Agent>({
    entityId: agent?.id ?? null,
    initial: agent ? toAgentInput(agent) : newAgentInput(defaultAreaIds(me)),
    fields: AGENT_FIELDS,
    label: 'agent',
    validate: validateAgent,
    normalize,
    create: agentsApi.create,
    update: agentsApi.update,
  })
  useUnsavedGuard(form.dirty)
  const { value: v, set, errors } = form
  const f = errors.fields

  const crews = agent ? crewsUsingAgent(agent.id, catalog.crews, catalog.tasks) : []
  const tasks = agent ? tasksUsingAgent(agent.id, catalog.tasks) : []
  const knownKeys = crews.length > 0 ? inputKeysOf(crews) : undefined
  const llm = catalog.llms.find((l) => l.id === v.llm_id)

  const onSave = async () => {
    const saved = await form.save()
    if (saved && !agent) navigate(`/agents/${saved.id}`, { replace: true, state: SKIP_GUARD })
  }

  const onDelete = async () => {
    if (!agent) return
    setDeleting(true)
    try {
      await agentsApi.remove(agent.id)
      notifySuccess(`Deleted ${agent.name}`)
      navigate('/agents', { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${agent.name}`, err)
    } finally {
      setDeleting(false)
    }
  }

  // Without llms.view the list is empty; keep the agent's current LLM selectable so it is not silently cleared
  const unlistedLlm = v.llm_id && !llm ? [{ value: v.llm_id, label: can('llms.view') ? 'Missing LLM' : 'Assigned LLM (you cannot view LLMs)' }] : []
  const llmOptions = [{ value: '', label: 'CrewAI default model' }, ...sortByName(catalog.llms).map((l) => ({ value: l.id, label: `${l.name} — ${l.model}` })), ...unlistedLlm]

  return (
    <>
      <ItemEditorHeader backTo="/agents" backLabel="Agents" title={agent ? agent.name : 'New agent'} isNew={!agent} dirty={form.dirty} saving={form.saving} entityType="agent" entityId={agent?.id} onSave={() => void onSave()} onDelete={() => void onDelete()} deleting={deleting} canSave={access.canSave} canDelete={access.canDelete} />
      {access.missing && <ReadOnlyNote perm={access.missing} />}
      <EditorLayout
        main={
          <div className="flex flex-col gap-4">
          <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (access.canSave) void onSave() }} noValidate>
            <LockableFieldset locked={!access.canSave} className="flex flex-col gap-4">
            <Panel title="Identity">
              <div className="flex flex-col gap-3">
                <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder="Researcher" hint="Short unique label shown across the studio." />
                <AreaPicker value={v.area_ids} onChange={(x) => set('area_ids', x)} error={f.area_ids} />
                <PlaceholderTextarea label="Role" rows={2} value={v.role} onChange={(x) => set('role', x)} error={f.role} placeholder="Senior {industry} research analyst" knownKeys={knownKeys} />
                <PlaceholderTextarea label="Goal" rows={3} value={v.goal} onChange={(x) => set('goal', x)} error={f.goal} placeholder="Uncover the latest developments about {company}" knownKeys={knownKeys} />
                <PlaceholderTextarea label="Backstory" rows={9} value={v.backstory} onChange={(x) => set('backstory', x)} error={f.backstory} placeholder="You have spent a decade…" knownKeys={knownKeys} hint="{placeholders} are filled from the crew's run inputs." />
              </div>
            </Panel>
            <Panel title="Model & tools">
              <div className="flex flex-col gap-3">
                <SelectField label="LLM" value={v.llm_id ?? ''} onChange={(x) => set('llm_id', x || null)} options={llmOptions} error={f.llm_id} aside={can('llms.view') ? <Link to="/llms" className="text-[11.5px] text-faint hover:text-accent">Manage LLMs</Link> : undefined} />
                {llm && <PriceMatchBadge model={llm.model} prices={catalog.pricing} verbose />}
                <ToolPicker tools={catalog.tools} value={v.tools} onChange={(x) => set('tools', x)} error={f.tools} />
              </div>
            </Panel>
            <Panel title="Behaviour">
              <div className="grid gap-4 sm:grid-cols-[160px_1fr_1fr] sm:items-start">
                <NumberField label="Max iterations" value={v.max_iter} onChange={(x) => set('max_iter', x ?? Number.NaN)} min={1} max={100} step={1} error={f.max_iter} hint="1 – 100" />
                <Toggle label="Allow delegation" checked={v.allow_delegation} onChange={(x) => set('allow_delegation', x)} hint="Lets this agent hand work to other agents in the crew." />
                <Toggle label="Verbose" checked={v.verbose} onChange={(x) => set('verbose', x)} hint="More detailed CrewAI console logs." />
              </div>
            </Panel>
            <FormErrors messages={errors.general} />
            </LockableFieldset>
          </form>
          {/* Its own form and Save: chat settings apply to the saved agent */}
          {agent && <AgentChatPanel agent={agent} agents={catalog.agents} crews={catalog.crews} llms={catalog.llms} canEdit={access.canSave} />}
          </div>
        }
        side={
          <Panel title="Used by">
            <div className="flex flex-col gap-4">
              <UsageList title="Crews" empty="Not in any crew." items={crews.map((c) => ({ key: c.id, node: <Link className="hover:text-accent" to={`/crews/${c.id}`}>{c.name}</Link> }))} />
              <UsageList title="Tasks" empty="Not assigned to any task." items={tasks.map((t) => ({ key: t.id, node: <Link className="hover:text-accent" to={`/tasks/${t.id}`}>{t.name}</Link> }))} />
              {knownKeys && (
                <div>
                  <div className="eyebrow mb-1.5">Inputs available</div>
                  <div className="flex flex-wrap gap-1">
                    {knownKeys.length === 0 ? <span className="text-[12px] text-faint">Those crews define no inputs.</span> : knownKeys.map((k) => <span key={k} className="rounded bg-accent-soft px-1.5 py-px font-mono text-[11px] text-accent">{`{${k}}`}</span>)}
                  </div>
                </div>
              )}
            </div>
          </Panel>
        }
      />
    </>
  )
}

export function AgentEditorPage() {
  const { id = 'new' } = useParams()
  // Reload when the id changes so a just-created agent is present in the catalog.
  const { can } = useAuth()
  const res = useAsync((signal) => loadVisibleCatalog(can, signal), [id])
  return (
    <AsyncView {...res}>
      {(catalog) => {
        const agent = id === 'new' ? null : catalog.agents.find((a) => a.id === id)
        if (agent === undefined) return <EmptyState title="Agent not found" hint="It may have been deleted." action={<Link className="text-accent" to="/agents">Back to agents</Link>} />
        return <AgentEditor key={id} agent={agent} catalog={catalog} />
      }}
    </AsyncView>
  )
}
