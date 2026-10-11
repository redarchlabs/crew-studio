import type { ChatAgent, ChatCrew, ChatMessage, Conversation, ConversationView } from '../../api/types.chat'

/** A chat message; an assistant reply that succeeded unless overridden. */
export function msg(over: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'm1', conversation_id: 'c1', role: 'assistant', content: '', proposal: null, proposal_state: null, proposal_result: null,
    decided_by_name: '', decided_at: null, undone_at: null, run_id: 'r1', run_status: 'succeeded', cost_usd: null,
    created_at: '2026-10-05T07:00:00Z', ...over,
  }
}

export const TS = '2026-10-05T07:00:00Z'

export function chatCrew(over: Partial<ChatCrew> = {}): ChatCrew {
  return { id: 'crew-1', name: 'PMO', description: '', front_desk_name: 'Coordinator', front_desk_role: 'PMO Coordinator', ...over }
}

export function chatAgent(over: Partial<ChatAgent> = {}): ChatAgent {
  return {
    id: 'agent-1', name: 'Deal Partner', role: 'Private Equity Deal Partner', goal: 'Decide', specialists: [{ id: 'agent-2', name: 'Ann', role: 'Analyst' }],
    crews: [{ id: 'crew-1', name: 'PMO' }], ...over,
  }
}

export function conversation(over: Partial<Conversation> = {}): Conversation {
  return { id: 'conv-1', crew_id: 'crew-1', crew_name: 'PMO', user_id: 'me-1', title: 'What is slipping?', created_at: TS, updated_at: TS, ...over }
}

export function view(over: Partial<ConversationView> = {}): ConversationView {
  return {
    conversation: conversation(),
    messages: [msg({ id: 'u1', role: 'user', content: 'What is slipping?', run_id: null, run_status: null }), msg({ id: 'a1', content: 'Atlas is.' })],
    budget: { daily_budget_usd: 1, spent_last_24h_usd: 0.25, remaining_usd: 0.75 },
    chat_enabled: true,
    ...over,
  }
}
