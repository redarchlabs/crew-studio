import { request } from './client'
import type { EscalationSettings, EscalationSettingsInput, HumanQuestion } from './types.human'

type Signal = AbortSignal | undefined

export const humanApi = {
  escalation: (crewId: string, signal?: Signal) => request<EscalationSettings>('GET', `/crews/${crewId}/escalation`, { signal }),
  saveEscalation: (crewId: string, body: EscalationSettingsInput) => request<EscalationSettings>('PUT', `/crews/${crewId}/escalation`, { body }),
  /** Open questions waiting on the signed-in person */
  mine: (signal?: Signal) => request<HumanQuestion[] | null>('GET', '/questions', { signal }).then((d) => d ?? []),
  /** Pushes the signed-in person's open questions ("questions" events) whenever they change */
  streamUrl: '/api/questions/stream',
  workOrderQuestions: (workOrderId: string, signal?: Signal) =>
    request<HumanQuestion[] | null>('GET', `/work-orders/${workOrderId}/questions`, { signal }).then((d) => d ?? []),
  answer: (id: string, answer: string) => request<HumanQuestion>('POST', `/questions/${id}/answer`, { body: { answer } }),
}
