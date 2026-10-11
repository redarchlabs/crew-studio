import { request } from './client'
import type { PeopleMapEntry, PeopleSync, PeopleView, TrackingCheck, TrackingItem, TrackingItemInput, TrackingView } from './types.trackers'

type Signal = AbortSignal | undefined

export const trackingApi = {
  list: (crewId: string, includeRemoved = false, signal?: Signal) =>
    request<TrackingView>('GET', `/crews/${crewId}/state/tracking`, { query: includeRemoved ? { include_removed: 'true' } : {}, signal }),
  /** Looks the item up in the tracker; saves nothing */
  check: (crewId: string, body: TrackingItemInput) => request<TrackingCheck>('POST', `/crews/${crewId}/state/tracking/check`, { body }),
  add: (crewId: string, body: TrackingItemInput) => request<TrackingItem>('POST', `/crews/${crewId}/state/tracking`, { body }),
  remove: (crewId: string, itemId: string) => request<TrackingItem>('DELETE', `/crews/${crewId}/state/tracking/${itemId}`),
}

export const peopleApi = {
  list: (connectionId: string, signal?: Signal) => request<PeopleView>('GET', `/connections/${connectionId}/people`, { signal }),
  sync: (connectionId: string) => request<PeopleSync>('POST', `/connections/${connectionId}/people/sync`, { body: {} }),
  link: (connectionId: string, accountId: string, userId: string | null) =>
    request<PeopleMapEntry>('PUT', `/connections/${connectionId}/people/${encodeURIComponent(accountId)}`, { body: { user_id: userId } }),
}
