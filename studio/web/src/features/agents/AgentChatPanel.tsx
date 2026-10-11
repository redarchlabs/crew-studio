import { useState } from 'react'
import { Link } from 'react-router'
import { chatApi } from '../../api/endpoints.chat'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { AgentChatSettings, AgentChatSettingsInput } from '../../api/types.chat'
import type { Agent, Crew, LLM } from '../../api/types'
import type { Connection, ConnectionKindKey } from '../../api/types.crewstate'
import { Button } from '../../components/Button'
import { NumberField, SelectField, Toggle } from '../../components/Field'
import { MultiSelect } from '../../components/MultiSelect'
import { Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { sortByName } from '../../lib/catalog'
import { dateTime } from '../../lib/format'
import { parseFieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { ProposeFields } from './AgentChatProposeFields'
import { QUESTION_DEFAULTS, QuestionFields } from './AgentChatQuestionFields'

/** Most specialists, crews and connections an agent may use in chat (model.MaxChatSpecialists, MaxChatCrews, MaxChatConnections) */
export const MAX_SPECIALISTS = 8
export const MAX_CREWS = 10
export const MAX_CONNECTIONS = 10
/** The connection kinds an agent may read through in chat (model.ChatNeedOf), with their labels: tracker and KM2 reads,
 * and MCP servers, whose enabled tools marked Reads it gets */
const READ_KINDS: Partial<Record<ConnectionKindKey, string>> = { jira: 'Jira', linear: 'Linear', km2_tracker: 'KM2 issues', km2: 'KM2', mcp: 'MCP' }

type Props = {
  agent: Agent
  /** Agents, crews and LLMs the editor can see */
  agents: readonly Agent[]
  crews: readonly Crew[]
  llms: readonly LLM[]
  canEdit: boolean
}

const FIELDS = ['enabled', 'llm_id', 'specialist_ids', 'crew_ids', 'connection_ids', 'daily_budget_usd', 'person_daily_budget_usd',
  'handoffs_enabled', 'handoff_budget_usd', 'handoff_timeout_minutes', 'max_active_handoffs', 'questions_enabled', 'question_rules',
  'question_timeout_minutes', 'question_on_timeout', 'max_questions', 'specialists_may_propose'] as const

/** Hand-off limits when the server sends none (model.DefaultHandoff*) and their ranges */
export const HANDOFF_DEFAULTS = { budget: 0.5, timeout: 20, active: 2 }
export const MAX_HANDOFF_TIMEOUT = 180
export const MAX_ACTIVE_HANDOFFS = 5

const toInput = (s: AgentChatSettings): AgentChatSettingsInput => ({
  enabled: s.enabled, llm_id: s.llm_id, specialist_ids: s.specialist_ids, crew_ids: s.crew_ids, connection_ids: s.connection_ids,
  daily_budget_usd: s.daily_budget_usd, person_daily_budget_usd: s.person_daily_budget_usd,
  handoffs_enabled: s.handoffs_enabled ?? false, handoff_budget_usd: s.handoff_budget_usd ?? HANDOFF_DEFAULTS.budget,
  handoff_timeout_minutes: s.handoff_timeout_minutes ?? HANDOFF_DEFAULTS.timeout, max_active_handoffs: s.max_active_handoffs ?? HANDOFF_DEFAULTS.active,
  questions_enabled: s.questions_enabled ?? false, question_rules: s.question_rules ?? '',
  question_timeout_minutes: s.question_timeout_minutes ?? QUESTION_DEFAULTS.timeout, question_on_timeout: s.question_on_timeout ?? 'proceed',
  max_questions: s.max_questions ?? QUESTION_DEFAULTS.max, specialists_may_propose: s.specialists_may_propose ?? false,
})

type Loaded = { settings: AgentChatSettings; connections: Connection[] | null }

/**
 * Whether people can chat with the agent directly, which agents it may ask, which crews it may start, which
 * connections it may read through, and what chat may spend.
 */
export function AgentChatPanel(props: Props) {
  const { can } = useAuth()
  const seesConnections = can('connections.view')
  const res = useAsync(async (signal): Promise<Loaded> => {
    const [settings, connections] = await Promise.all([chatApi.agentSettings(props.agent.id, signal), seesConnections ? connectionsApi.list(signal) : null])
    return { settings, connections }
  }, [props.agent.id, seesConnections])
  return (
    <Panel title="Chat">
      <AsyncView {...res}>
        {({ settings, connections }) => (
          <AgentChatForm key={settings.updated_at ?? 'new'} {...props} saved={settings} connections={connections}
            onSaved={(next) => res.setData(() => ({ settings: next, connections }))} />
        )}
      </AsyncView>
    </Panel>
  )
}

type FormProps = Props & { saved: AgentChatSettings; connections: Connection[] | null; onSaved: (s: AgentChatSettings) => void }

function AgentChatForm({ agent, agents, crews, llms, connections, canEdit, saved, onSaved }: FormProps) {
  const [v, setV] = useState<AgentChatSettingsInput>(toInput(saved))
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = <K extends keyof AgentChatSettingsInput>(key: K, value: AgentChatSettingsInput[K]) => setV((prev) => ({ ...prev, [key]: value }))
  const dirty = JSON.stringify(v) !== JSON.stringify(toInput(saved))

  const hiddenLlm = v.llm_id && !llms.some((l) => l.id === v.llm_id) ? [{ value: v.llm_id, label: 'Chosen model (not visible to you)' }] : []
  const llmOptions = [{ value: '', label: 'The agent’s own model' }, ...sortByName(llms).map((l) => ({ value: l.id, label: `${l.name} — ${l.model}` })), ...hiddenLlm]
  // The server leaves out specialists and crews outside the editor's areas, counts them, and keeps them on save
  const hiddenAgents = saved.hidden_specialists
  const hiddenCrews = saved.hidden_crews
  const specialistOptions = sortByName(agents.filter((a) => a.id !== agent.id)).map((a) => ({
    value: a.id, label: `${a.name} — ${a.role}`, disabled: a.llm_id === null || v.specialist_ids.length + hiddenAgents >= MAX_SPECIALISTS,
    disabledReason: a.llm_id === null ? 'Give it an LLM first.' : `At most ${MAX_SPECIALISTS} specialists.`,
  }))
  const crewOptions = sortByName(crews).map((c) => ({
    value: c.id, label: c.name, hint: c.description || undefined, disabled: v.crew_ids.length + hiddenCrews >= MAX_CREWS, disabledReason: `At most ${MAX_CREWS} crews.`,
  }))
  const hiddenConnections = saved.hidden_connections
  const connectionOptions = sortByName((connections ?? []).filter((c) => READ_KINDS[c.kind])).map((c) => ({
    value: c.id, label: c.name, hint: `${READ_KINDS[c.kind]}${c.enabled ? '' : ' · disabled: not used until enabled'}`,
    disabled: v.connection_ids.length + hiddenConnections >= MAX_CONNECTIONS, disabledReason: `At most ${MAX_CONNECTIONS} connections.`,
  }))

  const save = async () => {
    setSaving(true)
    setErrors({})
    try {
      onSaved(await chatApi.saveAgentSettings(agent.id, v))
      notifySuccess('Saved the chat settings')
    } catch (err) {
      setErrors(parseFieldErrors(err, FIELDS).fields)
      notifyError('Could not save the chat settings', err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <fieldset disabled={!canEdit} className="flex flex-col gap-3">
      <Toggle label="People can chat with this agent" checked={v.enabled} onChange={(x) => set('enabled', x)} disabled={!canEdit}
        hint={<>On the <Link className="text-accent" to="/chat">Chat</Link> page, under Agents, for people with Chat with crews and agents in its areas.</>} />
      <SelectField label="Chat model" value={v.llm_id ?? ''} onChange={(x) => set('llm_id', x || null)} options={llmOptions} error={errors.llm_id}
        hint="A cheaper model is usually fine for chat. Specialists keep their own models." />
      <MultiSelect label="Specialists it may ask" options={specialistOptions} value={v.specialist_ids} onChange={(x) => set('specialist_ids', x)}
        error={errors.specialist_ids} emptyText="No other agents to choose from." />
      <p className="-mt-2 text-[11.5px] text-faint">
        It asks them in the same chat turn; they use their own model and read-only tools, and their cost counts towards the chat budget.
        Each needs a role of its own.{hiddenAgents > 0 && ` ${hiddenAgents} more outside your areas ${hiddenAgents === 1 ? 'is' : 'are'} kept.`}
      </p>
      <MultiSelect label="Crews it may start" options={crewOptions} value={v.crew_ids} onChange={(x) => set('crew_ids', x)} error={errors.crew_ids}
        emptyText="No crews to choose from." />
      <p className="-mt-2 text-[11.5px] text-faint">
        It proposes a work order on one of these when a request fits; nothing runs until the person confirms.
        {hiddenCrews > 0 && ` ${hiddenCrews} more outside your areas ${hiddenCrews === 1 ? 'is' : 'are'} kept.`}
      </p>
      {connections ? (
        <MultiSelect label="Connections it may read" options={connectionOptions} value={v.connection_ids} onChange={(x) => set('connection_ids', x)}
          error={errors.connection_ids} emptyText="No Jira, Linear, KM2 or MCP connections in your areas." />
      ) : (
        <p className="text-[12.5px] text-muted">
          {v.connection_ids.length + hiddenConnections} connections are listed. You need View connections to see or change them.
        </p>
      )}
      <p className="-mt-2 text-[11.5px] text-faint">
        Its tracker and KM2 read tools, and its specialists’, use these in chat; anything written through them is a card the person
        confirms (below). An MCP connection gives the agent itself the server’s tools marked Reads on the Connections page. A chat uses only the
        ones the person chatting can see, and at most one may be a KM2 connection. In a chat that reads them, no agent gets Scrape Website.{hiddenConnections > 0 && ` ${hiddenConnections} more outside your areas ${hiddenConnections === 1 ? 'is' : 'are'} kept.`}
      </p>
      <NumberField label="Daily budget per conversation (USD)" value={v.daily_budget_usd} onChange={(x) => set('daily_budget_usd', x ?? Number.NaN)}
        min={0} step="any" error={errors.daily_budget_usd} hint="What one conversation may spend in any 24 hours, specialists included." />
      <NumberField label="Daily budget per person (USD)" value={v.person_daily_budget_usd} onChange={(x) => set('person_daily_budget_usd', x ?? Number.NaN)}
        min={0} step="any" error={errors.person_daily_budget_usd} hint="What one person may spend chatting with this agent in any 24 hours, over all their conversations." />
      <ProposeFields v={v} set={set} agent={agent} agents={agents} canEdit={canEdit} />
      <HandoffFields v={v} set={set} errors={errors} canEdit={canEdit} />
      {v.handoffs_enabled && <QuestionFields v={v} set={set} errors={errors} canEdit={canEdit} />}
      {saved.updated_at && <p className="text-[11.5px] text-faint">Last changed by {saved.updated_by_name || 'someone'} · {dateTime(saved.updated_at)}</p>}
      {canEdit && (
        <div className="flex justify-end">
          <Button size="sm" variant="primary" busy={saving} disabled={!dirty} onClick={() => void save()}>Save chat settings</Button>
        </div>
      )}
    </fieldset>
  )
}

type HandoffFieldsProps = {
  v: AgentChatSettingsInput
  set: <K extends keyof AgentChatSettingsInput>(key: K, value: AgentChatSettingsInput[K]) => void
  errors: Record<string, string>
  canEdit: boolean
}

/** Hand-offs: background runs for work too long for one reply, each started by the person's Confirm. */
function HandoffFields({ v, set, errors, canEdit }: HandoffFieldsProps) {
  return (
    <section aria-label="Hand-offs" className="flex flex-col gap-3 border-t border-line pt-3">
      <Toggle label="Hand off longer work" checked={v.handoffs_enabled} onChange={(x) => set('handoffs_enabled', x)} disabled={!canEdit}
        hint="The agent may propose handing work too long for one reply to a background run, worked by itself or a specialist. Each starts only when the person confirms its card; the result comes back to the conversation while they keep chatting." />
      {v.handoffs_enabled && (
        <div className="grid gap-3 sm:grid-cols-3">
          <NumberField label="Budget per hand-off (USD)" value={v.handoff_budget_usd} onChange={(x) => set('handoff_budget_usd', x ?? Number.NaN)}
            min={0} step="any" error={errors.handoff_budget_usd} hint="The daily budgets above still apply." />
          <NumberField label="Time limit (minutes)" value={v.handoff_timeout_minutes} onChange={(x) => set('handoff_timeout_minutes', x ?? Number.NaN)}
            min={1} max={MAX_HANDOFF_TIMEOUT} step={1} error={errors.handoff_timeout_minutes} hint={`1 to ${MAX_HANDOFF_TIMEOUT}; it is stopped after that.`} />
          <NumberField label="At once per conversation" value={v.max_active_handoffs} onChange={(x) => set('max_active_handoffs', x ?? Number.NaN)}
            min={1} max={MAX_ACTIVE_HANDOFFS} step={1} error={errors.max_active_handoffs} hint={`1 to ${MAX_ACTIVE_HANDOFFS}.`} />
        </div>
      )}
    </section>
  )
}
