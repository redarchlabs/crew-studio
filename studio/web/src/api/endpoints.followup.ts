import { request } from './client'
import { messageBody } from './endpoints.chat'
import type { ConversationView } from './types.chat'
import type { ReviseInput, ReviseResult, WorkOrderFeedback } from './types.followup'

type Signal = AbortSignal | undefined

export const followUpApi = {
  /** Your conversation about the work order; null before you start one */
  conversation: (workOrderId: string, signal?: Signal) =>
    request<ConversationView | null>('GET', `/work-orders/${workOrderId}/conversation`, { signal }),
  /** Sends a message, starting the conversation if need be */
  send: (workOrderId: string, content: string, files: readonly File[] = []) =>
    request<ConversationView>('POST', `/work-orders/${workOrderId}/conversation/messages`, { body: messageBody(content, files) }),
  compact: (workOrderId: string) =>
    request<ConversationView>('POST', `/work-orders/${workOrderId}/conversation/compact`, { body: {} }),
  feedback: (workOrderId: string, signal?: Signal) =>
    request<WorkOrderFeedback[] | null>('GET', `/work-orders/${workOrderId}/feedback`, { signal }).then((d) => d ?? []),
  /** Starts a new attempt that revises the latest one with the feedback */
  revise: (workOrderId: string, body: ReviseInput) => request<ReviseResult>('POST', `/work-orders/${workOrderId}/revise`, { body }),
}
