import type { ChatAgent, ChatCrew } from '../../api/types.chat'
import type { ChatKind, ChatTarget } from './chatTarget'

type Props = {
  crews: readonly ChatCrew[]
  agents: readonly ChatAgent[]
  target: ChatTarget
  /** Shown when the target is not in the lists (its chat was turned off since) */
  fallbackName?: string
  onPick: (target: ChatTarget) => void
}

const KINDS: { kind: ChatKind; label: string }[] = [{ kind: 'crew', label: 'Crews' }, { kind: 'agent', label: 'Agents' }]

/** Picks whom to chat with: a crew's front desk, or an agent directly. */
export function ChatTargetPicker({ crews, agents, target, fallbackName, onPick }: Props) {
  const switchTo = (kind: ChatKind) => {
    const first = kind === 'crew' ? crews[0]?.id : agents[0]?.id
    if (kind !== target.kind && first) onPick({ kind, id: first })
  }
  const options = target.kind === 'crew' ? crews : agents
  const known = options.some((o) => o.id === target.id)
  const agent = target.kind === 'agent' ? agents.find((a) => a.id === target.id) : undefined
  const crew = target.kind === 'crew' ? crews.find((c) => c.id === target.id) : undefined
  const label = target.kind === 'crew' ? 'Crew' : 'Agent'
  return (
    <div className="flex flex-col gap-3">
      {crews.length > 0 && agents.length > 0 && (
        <div role="tablist" aria-label="Chat with" className="grid grid-cols-2 rounded-md border border-line p-0.5 text-[12.5px]">
          {KINDS.map(({ kind, label: text }) => (
            <button key={kind} type="button" role="tab" aria-selected={target.kind === kind} onClick={() => switchTo(kind)}
              className={`rounded px-2 py-1 ${target.kind === kind ? 'bg-accent-soft text-accent' : 'text-muted hover:text-fg'}`}>
              {text}
            </button>
          ))}
        </div>
      )}
      <select aria-label={label} className="field-input" value={target.id} onChange={(e) => onPick({ kind: target.kind, id: e.target.value })}>
        {!known && <option value={target.id}>{fallbackName || `Chat is off for this ${target.kind}`}</option>}
        {options.map((o) => <option key={o.id} value={o.id}>{o.name}</option>)}
      </select>
      {crew && <p className="text-[11.5px] text-muted">Answered by {crew.front_desk_name} ({crew.front_desk_role}).</p>}
      {agent && <AgentAbout agent={agent} />}
    </div>
  )
}

function AgentAbout({ agent }: { agent: ChatAgent }) {
  return (
    <div className="flex flex-col gap-1 text-[11.5px] text-muted">
      <p>{agent.role}</p>
      <p>{agent.specialists.length > 0 ? `Can ask: ${agent.specialists.map((s) => s.name).join(', ')}.` : 'Works alone.'}</p>
      {agent.crews.length > 0 && <p>Can start: {agent.crews.map((c) => c.name).join(', ')}.</p>}
    </div>
  )
}
