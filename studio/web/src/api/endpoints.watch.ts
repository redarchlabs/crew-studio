import { request } from './client'
import type { CrewWatch, WatchInput, WatchPreview, WatchView } from './types.watch'

type Signal = AbortSignal | undefined

export const watchesApi = {
  get: (scheduleId: string, signal?: Signal) => request<WatchView>('GET', `/schedules/${scheduleId}/watch`, { signal }),
  save: (scheduleId: string, body: WatchInput) => request<WatchView>('PUT', `/schedules/${scheduleId}/watch`, { body }),
  remove: (scheduleId: string) => request<void>('DELETE', `/schedules/${scheduleId}/watch`),
  /** Fetches the source now; saves and starts nothing */
  preview: (scheduleId: string, body: WatchInput) => request<WatchPreview>('POST', `/schedules/${scheduleId}/watch/preview`, { body }),
  clearSnapshot: (scheduleId: string) => request<void>('DELETE', `/schedules/${scheduleId}/watch/snapshot`),
  ofCrew: (crewId: string, signal?: Signal) =>
    request<CrewWatch[] | null>('GET', `/crews/${crewId}/state/watches`, { signal }).then((d) => d ?? []),
}
