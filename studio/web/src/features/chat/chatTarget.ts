// Who a chat is with: a crew (its front desk answers) or, in agent chat, an agent (docs/agent-chat-design.md).

import type { ChatAgent, ChatCrew, Conversation } from '../../api/types.chat'

export type ChatKind = 'crew' | 'agent'
export type ChatTarget = { kind: ChatKind; id: string }

/** The crew or agent a conversation is with. */
export function targetOf(conv: Conversation): ChatTarget {
  return conv.agent_id ? { kind: 'agent', id: conv.agent_id } : { kind: 'crew', id: conv.crew_id ?? '' }
}

/** What the page talks to: the open conversation's target, else ?agent= or ?crew=, else the first crew, else the first agent. */
export function pickTarget(conv: Conversation | null | undefined, params: URLSearchParams, crews: readonly ChatCrew[], agents: readonly ChatAgent[]): ChatTarget {
  if (conv) return targetOf(conv)
  const agent = params.get('agent')
  if (agent) return { kind: 'agent', id: agent }
  const crew = params.get('crew')
  if (crew) return { kind: 'crew', id: crew }
  return crews.length > 0 || agents.length === 0 ? { kind: 'crew', id: crews[0]?.id ?? '' } : { kind: 'agent', id: agents[0].id }
}

export function belongsTo(conv: Conversation, target: ChatTarget): boolean {
  return target.kind === 'agent' ? conv.agent_id === target.id : !conv.agent_id && conv.crew_id === target.id
}

/** The page for a new conversation with the target. */
export function targetUrl(target: ChatTarget): string {
  return `/chat?${target.kind}=${encodeURIComponent(target.id)}`
}

/** What the message box says for the target: a crew's front desk proposes changes, an agent proposes runs. */
export function composerText(kind: ChatKind, name: string | undefined): { placeholder: string; confirmHint: string } {
  if (kind === 'crew') return { placeholder: 'Message the crew…', confirmHint: 'Changes the crew proposes wait for your Confirm.' }
  return { placeholder: `Message ${name || 'the agent'}…`, confirmHint: 'Runs the agent proposes wait for your Confirm.' }
}
