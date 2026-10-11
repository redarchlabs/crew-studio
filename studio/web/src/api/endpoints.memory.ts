import { request, requestPage } from './client'
import type { Page } from './types'
import type {
  MemoryDeleteRequest,
  MemoryDeleteResult,
  MemoryInput,
  MemoryPatch,
  MemoryPromoteRequest,
  MemoryPromoteResult,
  MemoryQuery,
  MemoryRecord,
  MemoryScopeCount,
  MemorySettingsInput,
  MemorySettingsView,
} from './types.memory'
import type { KM2FolderList } from './types.crewstate'

type Signal = AbortSignal | undefined

function listQuery({ pinned, ...rest }: MemoryQuery) {
  return { ...rest, pinned: pinned ? 'true' : undefined }
}

export const memoryApi = {
  settings: (crewId: string, signal?: Signal) => request<MemorySettingsView>('GET', `/crews/${crewId}/memory/settings`, { signal }),
  saveSettings: (crewId: string, body: MemorySettingsInput) => request<MemorySettingsView>('PUT', `/crews/${crewId}/memory/settings`, { body }),
  list: (crewId: string, query: MemoryQuery, signal?: Signal): Promise<Page<MemoryRecord>> =>
    requestPage<MemoryRecord>(`/crews/${crewId}/memory`, { query: listQuery(query), signal }),
  scopes: (crewId: string, signal?: Signal) =>
    request<MemoryScopeCount[] | null>('GET', `/crews/${crewId}/memory/scopes`, { signal }).then((d) => d ?? []),
  create: (crewId: string, body: MemoryInput) => request<MemoryRecord>('POST', `/crews/${crewId}/memory`, { body }),
  update: (crewId: string, memoryId: string, body: MemoryPatch) => request<MemoryRecord>('PATCH', `/crews/${crewId}/memory/${memoryId}`, { body }),
  /** Approves a pending memory so runs can recall it */
  approve: (crewId: string, memoryId: string) => request<MemoryRecord>('PATCH', `/crews/${crewId}/memory/${memoryId}`, { body: { status: 'active' } }),
  remove: (crewId: string, memoryId: string) => request<void>('DELETE', `/crews/${crewId}/memory/${memoryId}`),
  /** With dry_run only counts what would be deleted */
  bulkDelete: (crewId: string, body: MemoryDeleteRequest) => request<MemoryDeleteResult>('POST', `/crews/${crewId}/memory/delete`, { body }),
  promote: (crewId: string, body: MemoryPromoteRequest) => request<MemoryPromoteResult>('POST', `/crews/${crewId}/memory/promote`, { body }),
  /** The folders of the KM2 connection promotions save through */
  promoteFolders: (crewId: string) => request<KM2FolderList>('GET', `/crews/${crewId}/memory/promote/folders`),
}
