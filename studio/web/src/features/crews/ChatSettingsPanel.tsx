import { useState } from 'react'
import { Link } from 'react-router'
import { chatApi } from '../../api/endpoints.chat'
import type { ChatSettings, ChatSettingsInput } from '../../api/types.chat'
import type { Agent, LLM } from '../../api/types'
import { Button } from '../../components/Button'
import { NumberField, SelectField, Toggle } from '../../components/Field'
import { Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { sortByName } from '../../lib/catalog'
import { dateTime } from '../../lib/format'
import { parseFieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'

type Props = {
  crewId: string
  /** The saved crew's agents; the front desk must be one of them */
  agentIds: readonly string[]
  agents: readonly Agent[]
  llms: readonly LLM[]
  canEdit: boolean
}

const FIELDS = ['enabled', 'front_desk_agent_id', 'llm_id', 'daily_budget_usd', 'person_daily_budget_usd'] as const

const toInput = (s: ChatSettings): ChatSettingsInput => ({
  enabled: s.enabled, front_desk_agent_id: s.front_desk_agent_id, llm_id: s.llm_id, daily_budget_usd: s.daily_budget_usd,
  person_daily_budget_usd: s.person_daily_budget_usd,
})

/** Whether people can chat with the crew, which agent answers, the chat model and the daily budget. */
export function ChatSettingsPanel({ crewId, agentIds, agents, llms, canEdit }: Props) {
  const res = useAsync((signal) => chatApi.settings(crewId, signal), [crewId])
  return (
    <Panel title="Chat">
      <AsyncView {...res}>
        {(s) => <ChatSettingsForm key={s.updated_at ?? 'new'} crewId={crewId} saved={s} agentIds={agentIds} agents={agents} llms={llms} canEdit={canEdit} onSaved={(next) => res.setData(() => next)} />}
      </AsyncView>
    </Panel>
  )
}

type FormProps = Omit<Props, 'crewId'> & { crewId: string; saved: ChatSettings; onSaved: (s: ChatSettings) => void }

function ChatSettingsForm({ crewId, saved, agentIds, agents, llms, canEdit, onSaved }: FormProps) {
  const [v, setV] = useState<ChatSettingsInput>(toInput(saved))
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const set = <K extends keyof ChatSettingsInput>(key: K, value: ChatSettingsInput[K]) => setV((prev) => ({ ...prev, [key]: value }))
  const dirty = JSON.stringify(v) !== JSON.stringify(toInput(saved))

  const members = sortByName(agents.filter((a) => agentIds.includes(a.id)))
  const deskOptions = [{ value: '', label: 'Choose the front desk agent…' }, ...members.map((a) => ({ value: a.id, label: `${a.name} — ${a.role}` }))]
  const hiddenLlm = v.llm_id && !llms.some((l) => l.id === v.llm_id) ? [{ value: v.llm_id, label: 'Chosen model (not visible to you)' }] : []
  const llmOptions = [{ value: '', label: 'The agent’s own model' }, ...sortByName(llms).map((l) => ({ value: l.id, label: `${l.name} — ${l.model}` })), ...hiddenLlm]

  const save = async () => {
    setSaving(true)
    setErrors({})
    try {
      onSaved(await chatApi.saveSettings(crewId, v))
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
      <Toggle label="People can chat with this crew" checked={v.enabled} onChange={(x) => set('enabled', x)} disabled={!canEdit}
        hint={<>On the <Link className="text-accent" to="/chat">Chat</Link> page, for people with Chat with crews and agents in its areas.</>} />
      <SelectField label="Front desk agent" value={v.front_desk_agent_id ?? ''} onChange={(x) => set('front_desk_agent_id', x || null)} options={deskOptions}
        error={errors.front_desk_agent_id} hint="It answers every message, with chat tools only: reading notes and the action log, and proposing a run or a note change." />
      <SelectField label="Chat model" value={v.llm_id ?? ''} onChange={(x) => set('llm_id', x || null)} options={llmOptions} error={errors.llm_id}
        hint="A cheaper model is usually fine for chat." />
      <NumberField label="Daily budget per conversation (USD)" value={v.daily_budget_usd} onChange={(x) => set('daily_budget_usd', x ?? Number.NaN)}
        min={0} step="any" error={errors.daily_budget_usd} hint="What one conversation may spend in any 24 hours." />
      <NumberField label="Daily budget per person (USD)" value={v.person_daily_budget_usd} onChange={(x) => set('person_daily_budget_usd', x ?? Number.NaN)}
        min={0} step="any" error={errors.person_daily_budget_usd} hint="What one person may spend on this crew's chat in any 24 hours, over all their conversations." />
      {saved.updated_at && <p className="text-[11.5px] text-faint">Last changed by {saved.updated_by_name || 'someone'} · {dateTime(saved.updated_at)}</p>}
      {canEdit && (
        <div className="flex justify-end">
          <Button size="sm" variant="primary" busy={saving} disabled={!dirty} onClick={() => void save()}>Save chat settings</Button>
        </div>
      )}
    </fieldset>
  )
}
