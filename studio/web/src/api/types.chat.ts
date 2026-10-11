// Crew chat: talk to a crew's front desk agent and confirm the changes it proposes. Mirror studio/docs/api.md.

import type { RunStatus } from './types'
import type { EscalationOnTimeout, HumanQuestion } from './types.human'

export type ChatSettings = {
  crew_id: string
  enabled: boolean
  front_desk_agent_id: string | null
  /** A cheaper model for chat instead of the front desk agent's own; null keeps the agent's */
  llm_id: string | null
  daily_budget_usd: number
  /** What one person may spend on the crew's chat in any 24 hours, over all their conversations */
  person_daily_budget_usd: number
  updated_by_name: string
  updated_at: string | null
}

export type ChatSettingsInput = Pick<ChatSettings, 'enabled' | 'front_desk_agent_id' | 'llm_id' | 'daily_budget_usd' | 'person_daily_budget_usd'>

export type ChatCrew = { id: string; name: string; description: string; front_desk_name: string; front_desk_role: string }

/** An agent or crew named in agent chat; role is set for agents */
export type ChatNameRef = { id: string; name: string; role?: string }

/** An agent people can chat with directly (docs/agent-chat-design.md), with the specialists and crews it may use that you can see */
export type ChatAgent = { id: string; name: string; role: string; goal: string; specialists: ChatNameRef[]; crews: ChatNameRef[] }

/** An agent's chat settings: the specialists it may ask, the crews it may start and the connections it may read */
export type AgentChatSettings = {
  agent_id: string
  enabled: boolean
  /** A cheaper model for the agent in chat; specialists keep their own */
  llm_id: string | null
  /** Only the agents, crews and connections in the reader's areas; saving keeps the others where they are */
  specialist_ids: string[]
  crew_ids: string[]
  /** Jira, Linear, KM2 issues and KM2 connections its tracker and KM2 read tools may use in chat */
  connection_ids: string[]
  daily_budget_usd: number
  person_daily_budget_usd: number
  /** The agent may propose hand-offs: background runs for work too long for one turn, each started by the person's Confirm */
  handoffs_enabled: boolean
  /** What one hand-off may spend (the daily budgets also apply), how long it may run, and how many run at once per conversation */
  handoff_budget_usd: number
  handoff_timeout_minutes: number
  max_active_handoffs: number
  /** During a hand-off the lead may bring a specialist's question to the person when these rules say so (required then) */
  questions_enabled: boolean
  question_rules: string
  /** How long a question waits (never past a minute before the hand-off's deadline), what happens then, and how many per hand-off */
  question_timeout_minutes: number
  question_on_timeout: EscalationOnTimeout
  max_questions: number
  /** The specialists get the proposing counterparts of their own tracker and KM2 writes, as the lead always does (cards only) */
  specialists_may_propose: boolean
  updated_by_name: string
  updated_at: string | null
  /** How many specialists, crews and connections are outside the reader's areas (left out of the lists above) */
  hidden_specialists: number
  hidden_crews: number
  hidden_connections: number
}

export type AgentChatSettingsInput = Pick<AgentChatSettings, 'enabled' | 'llm_id' | 'specialist_ids' | 'crew_ids' | 'connection_ids' | 'daily_budget_usd' |
  'person_daily_budget_usd' | 'handoffs_enabled' | 'handoff_budget_usd' | 'handoff_timeout_minutes' | 'max_active_handoffs' |
  'questions_enabled' | 'question_rules' | 'question_timeout_minutes' | 'question_on_timeout' | 'max_questions' | 'specialists_may_propose'>

export type Conversation = {
  id: string
  /** Set for a conversation with a crew; null for one with an agent */
  crew_id: string | null
  crew_name: string
  /** Set for a conversation with an agent (agent chat) */
  agent_id?: string | null
  agent_name?: string
  user_id: string
  /** Set for a conversation about a work order (it lives on the work order, not the Chat page) */
  work_order_id?: string | null
  title: string
  created_at: string
  updated_at: string
}

export type ProposalType = 'start_work_order' | 'notes_put' | 'tracking_add' | 'tracking_remove' | 'start_handoff' |
  'tracker_comment' | 'tracker_suggestion' | 'tracker_issue' | 'km2_save'
export type ProposalState = 'pending' | 'confirmed' | 'declined' | 'expired'

export type Proposal = { type: ProposalType | string; summary: string; payload: Record<string, unknown> }

export type ChatMessage = {
  id: string
  conversation_id: string
  /** summary: a compaction of the conversation before it; later turns get it instead of those messages */
  role: 'user' | 'assistant' | 'summary'
  /** Empty for a reply that has not arrived (or failed) */
  content: string
  proposal: Proposal | null
  proposal_state: ProposalState | null
  proposed_at?: string | null
  proposal_result: Record<string, unknown> | null
  decided_by_name: string
  decided_at: string | null
  undone_at: string | null
  run_id: string | null
  /** chat for a reply, handoff for a hand-off's result (left out without a run) */
  run_kind?: 'chat' | 'handoff'
  /** Set on a hand-off's messages: its result, and the cards it proposed */
  handoff?: HandoffRef | null
  /** A card a hand-off proposed while it worked (its content is the card's summary), not its result */
  handoff_proposal?: boolean
  run_status: RunStatus | null
  run_error?: string
  cost_usd: number | null
  created_at: string
  /** Files sent with a person's message (older servers leave it out) */
  attachments?: ChatAttachment[]
}

/** What a hand-off's result message says about the hand-off */
export type HandoffRef = {
  id: string; title: string; worker_name: string; worker_role: string; deadline_at: string; budget_usd: number
  /** Its time limit, counted from when its run started (older servers leave these out) */
  timeout_minutes?: number
  started_at?: string | null
  /** The question its run is waiting on you to answer, if any */
  open_question_id?: string | null
}

/** A hand-off from an agent chat, worked in the background (docs/agent-chat-handoffs-design.md) */
export type Handoff = {
  id: string
  conversation_id: string
  request_message_id: string | null
  result_message_id: string
  /** Null once the run was deleted (after a day); the hand-off and its result are kept */
  run_id: string | null
  worker_agent_id: string | null
  worker_name: string
  worker_role: string
  title: string
  brief: string
  budget_usd: number
  timeout_minutes: number
  /** timeout_minutes after the run started (after it was made, while it waits for a slot) */
  deadline_at: string
  created_at: string
  run_status: RunStatus | null
  run_error?: string
  cost_usd: number | null
  started_at: string | null
  finished_at: string | null
  /** The question its run is waiting on you to answer, if any */
  open_question_id?: string | null
}

export type ChatAttachment = { id: string; filename: string; content_type: string; size_bytes: number }

export type ChatBudget = {
  daily_budget_usd: number
  spent_last_24h_usd: number
  /** The person's own cap over all their conversations with the crew */
  person_daily_budget_usd?: number
  person_spent_last_24h_usd?: number
  remaining_usd: number
}

export type ConversationView = {
  conversation: Conversation
  messages: ChatMessage[]
  budget: ChatBudget
  /** False once the crew's (or agent's) chat was turned off: the conversation can be read, not continued */
  chat_enabled: boolean
  /** The conversation's latest hand-offs, newest first (older servers leave it out) */
  handoffs?: Handoff[]
  /** The open questions its hand-offs are waiting on you to answer, soonest due first (older servers leave it out) */
  questions?: HumanQuestion[]
}
