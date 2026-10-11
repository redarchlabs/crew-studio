import { request } from './client'
import type {
  AgentChatSettings, AgentChatSettingsInput, ChatAgent, ChatCrew, ChatMessage, ChatSettings, ChatSettingsInput, Conversation, ConversationView,
  Handoff,
} from './types.chat'
import type { HumanQuestion } from './types.human'

type Signal = AbortSignal | undefined

/** A message as JSON, or as multipart/form-data when files go with it (images and PDFs). */
export function messageBody(content: string, files: readonly File[], extra: Record<string, string> = {}): FormData | Record<string, string> {
  if (files.length === 0) return { ...extra, content }
  const form = new FormData()
  for (const [k, v] of Object.entries(extra)) form.append(k, v)
  form.append('content', content)
  for (const file of files) form.append('file', file, file.name)
  return form
}

export const chatApi = {
  crews: (signal?: Signal) => request<ChatCrew[] | null>('GET', '/chat/crews', { signal }).then((d) => d ?? []),
  /** Agents you can chat with directly */
  agents: (signal?: Signal) => request<ChatAgent[] | null>('GET', '/chat/agents', { signal }).then((d) => d ?? []),
  conversations: (crewId?: string, signal?: Signal, agentId?: string) =>
    request<Conversation[] | null>('GET', '/chat/conversations', { query: { crew_id: crewId || undefined, agent_id: agentId || undefined }, signal })
      .then((d) => d ?? []),
  conversation: (id: string, signal?: Signal) => request<ConversationView>('GET', `/chat/conversations/${id}`, { signal }),
  /** Starts a conversation with its first message */
  start: (crewId: string, content: string, files: readonly File[] = []) =>
    request<ConversationView>('POST', '/chat/conversations', { body: messageBody(content, files, { crew_id: crewId }) }),
  /** Starts a conversation with an agent (agent chat) with its first message */
  startWithAgent: (agentId: string, content: string, files: readonly File[] = []) =>
    request<ConversationView>('POST', '/chat/conversations', { body: messageBody(content, files, { agent_id: agentId }) }),
  send: (id: string, content: string, files: readonly File[] = []) =>
    request<ConversationView>('POST', `/chat/conversations/${id}/messages`, { body: messageBody(content, files) }),
  /** Summarises the conversation so far; later messages are given the summary instead of the history */
  compact: (id: string) => request<ConversationView>('POST', `/chat/conversations/${id}/compact`, { body: {} }),
  remove: (id: string) => request<void>('DELETE', `/chat/conversations/${id}`),
  confirm: (messageId: string) => request<ChatMessage>('POST', `/chat/messages/${messageId}/confirm`, { body: {} }),
  decline: (messageId: string) => request<ChatMessage>('POST', `/chat/messages/${messageId}/decline`, { body: {} }),
  undo: (messageId: string) => request<ChatMessage>('POST', `/chat/messages/${messageId}/undo`, { body: {} }),
  /** One hand-off of your own conversation */
  handoff: (id: string, signal?: Signal) => request<Handoff>('GET', `/chat/handoffs/${id}`, { signal }),
  /** The questions your conversation's hand-offs asked you, open ones first; answer them with humanApi.answer */
  questions: (id: string, signal?: Signal) =>
    request<HumanQuestion[] | null>('GET', `/chat/conversations/${id}/questions`, { signal }).then((d) => d ?? []),
  /** Cancels a hand-off's run (you, signed in) */
  cancelHandoff: (id: string) => request<Handoff>('POST', `/chat/handoffs/${id}/cancel`, { body: {} }),
  settings: (crewId: string, signal?: Signal) => request<ChatSettings>('GET', `/crews/${crewId}/chat`, { signal }),
  saveSettings: (crewId: string, body: ChatSettingsInput) => request<ChatSettings>('PUT', `/crews/${crewId}/chat`, { body }),
  agentSettings: (agentId: string, signal?: Signal) => request<AgentChatSettings>('GET', `/agents/${agentId}/chat`, { signal }),
  saveAgentSettings: (agentId: string, body: AgentChatSettingsInput) =>
    request<AgentChatSettings>('PUT', `/agents/${agentId}/chat`, { body }),
}
