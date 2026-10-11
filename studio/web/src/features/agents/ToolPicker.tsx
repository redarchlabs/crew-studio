import type { Tool } from '../../api/types'
import { MultiSelect, type MultiOption } from '../../components/MultiSelect'
import { Chip } from '../../components/StatusBadge'
import { CARD_TOOLS } from './AgentChatProposeFields'

type ToolPickerProps = { tools: readonly Tool[]; value: readonly string[]; onChange: (v: string[]) => void; error?: string }

const EFFECT_LABELS = { reads: 'Reads', proposes: 'Proposes', changes: 'Changes' } as const

/** What happens to a tool in agent chat, for the chip's hover text (docs/agent-chat-handoffs-design.md, section 7). */
export function chatUse(tool: Tool): string {
  if (tool.effect === 'changes' && CARD_TOOLS[tool.key]) {
    return 'Changes things outside the run. In agent chat the agent never uses it itself: it proposes the change as a card the person confirms (for specialists, when their agent allows it).'
  }
  if (tool.effect === 'changes') return 'Changes things outside the run. It is never given to an agent in chat.'
  if (tool.effect === 'proposes') return 'Proposes a change that the person confirms before anything happens.'
  if (tool.needs.includes('crew') || tool.needs.includes('schedule')) return 'Only reads, but needs a crew, so agents do not get it in agent chat.'
  if (tool.needs.includes('tracker')) return "Only reads. In agent chat it works through a Jira, Linear or KM2 issues connection listed in the agent's chat settings."
  if (tool.needs.includes('km2')) return "Only reads. In agent chat it works through a KM2 connection listed in the agent's chat settings."
  return 'Only reads. Agents keep it in agent chat.'
}

function EffectChip({ tool }: { tool: Tool }) {
  return (
    <Chip tone={tool.effect === 'reads' ? 'ok' : 'warn'} title={chatUse(tool)} className="mr-1 align-middle">
      {EFFECT_LABELS[tool.effect]}
    </Chip>
  )
}

/** Tools multi-select; tools missing env vars are shown disabled with the variable they need. */
export function ToolPicker({ tools, value, onChange, error }: ToolPickerProps) {
  const known = new Set(tools.map((t) => t.key))
  const options: MultiOption[] = [
    ...tools.map((t) => ({
      value: t.key,
      label: t.name,
      hint: (
        <>
          <EffectChip tool={t} />
          <span className="font-mono">{t.key}</span>
          {t.description && <> · {t.description}</>}
        </>
      ),
      disabled: !t.available,
      disabledReason: t.available ? undefined : `Unavailable — missing ${t.requires_env.join(', ') || 'configuration'} on the server`,
    })),
    ...value.filter((k) => !known.has(k)).map((k) => ({ value: k, label: k, hint: 'Unknown tool — not in the server catalog' })),
  ]
  return <MultiSelect label="Tools" options={options} value={value} onChange={onChange} error={error} emptyText="The server reports no tools." maxHeight="max-h-72" />
}
