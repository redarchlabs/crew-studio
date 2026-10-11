import type { AgentChatSettingsInput } from '../../api/types.chat'
import type { Agent } from '../../api/types'
import { Toggle } from '../../components/Field'

/**
 * The tools that change things and the cards they become in chat (chat.Counterparts on the server): in agent chat an
 * agent never posts or saves itself; it proposes, and the person confirms.
 */
export const CARD_TOOLS: Record<string, string> = {
  tracker_comment: 'Comment on Tracker Issue',
  tracker_suggest_change: 'Suggest Tracker Change',
  tracker_create_issue: 'Create Tracker Issue',
  km_save_document: 'Save to Knowledge (KM2)',
}

/** The tools of an agent that become cards in chat, by name, in the catalog's order. */
export function cardTools(agent: Pick<Agent, 'tools'> | undefined): string[] {
  const tools = agent?.tools ?? []
  return Object.keys(CARD_TOOLS).filter((key) => tools.includes(key)).map((key) => CARD_TOOLS[key])
}

type Props = {
  v: AgentChatSettingsInput
  set: <K extends keyof AgentChatSettingsInput>(key: K, value: AgentChatSettingsInput[K]) => void
  /** The agent being edited, and the agents the editor can see (the specialists among them) */
  agent: Agent
  agents: readonly Agent[]
  canEdit: boolean
}

/**
 * Specialists may propose changes (docs/agent-chat-handoffs-design.md, slice 3d): the agent's own tracker and KM2 writes
 * always become Confirm cards in chat; with this on, its specialists' do too. The note lists which tools that is.
 */
export function ProposeFields({ v, set, agent, agents, canEdit }: Props) {
  const own = cardTools(agent)
  const specialists = v.specialist_ids.map((id) => agents.find((a) => a.id === id)).filter((a): a is Agent => a !== undefined)
    .map((a) => ({ name: a.name, tools: cardTools(a) })).filter((s) => s.tools.length > 0)
  return (
    <section aria-label="Proposals" className="flex flex-col gap-2 border-t border-line pt-3">
      <Toggle label="Specialists may propose changes" checked={v.specialists_may_propose} onChange={(x) => set('specialists_may_propose', x)}
        disabled={!canEdit}
        hint="In chat no agent posts to a tracker or saves to KM2 itself: each such tool becomes a card the person confirms, and only then is the change made, in their name, through the connections above. The agent always gets these cards for its own tools; with this on, its specialists do too, during a turn or a hand-off (at most 5 cards per hand-off)." />
      <ul className="text-[11.5px] text-faint" aria-label="Tools that become cards">
        <li>{own.length > 0 ? `${agent.name}: ${own.join(', ')}.` : `${agent.name} has no tools that post to a tracker or save to KM2.`}</li>
        {specialists.map((s) => (
          <li key={s.name}>{s.name}: {s.tools.join(', ')}{v.specialists_may_propose ? '.' : ' (only with this on).'}</li>
        ))}
        {specialists.length === 0 && v.specialist_ids.length > 0 && <li>None of its specialists you can see has such a tool.</li>}
      </ul>
    </section>
  )
}
