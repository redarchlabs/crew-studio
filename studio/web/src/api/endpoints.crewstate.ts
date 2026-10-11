import { request, requestPage } from './client'
import type { Page } from './types'
import type {
  ActionEntry,
  Connection,
  ConnectionInput,
  ConnectionKinds,
  ConnectionTestResult,
  ConnectionTool,
  ConnectionToolChange,
  DiscoverToolsResult,
  KM2FolderList,
  NoteInfo,
  NoteKey,
  NoteVersion,
} from './types.crewstate'

type Signal = AbortSignal | undefined

export const connectionsApi = {
  kinds: (signal?: Signal) => request<ConnectionKinds>('GET', '/connections/kinds', { signal }),
  list: (signal?: Signal) => request<Connection[] | null>('GET', '/connections', { signal }).then((d) => d ?? []),
  get: (id: string, signal?: Signal) => request<Connection>('GET', `/connections/${id}`, { signal }),
  create: (body: ConnectionInput) => request<Connection>('POST', '/connections', { body }),
  /** Secrets cannot be sent here; use replaceSecret */
  update: (id: string, body: Omit<ConnectionInput, 'secrets'>) => request<Connection>('PUT', `/connections/${id}`, { body }),
  replaceSecret: (id: string, secrets: Record<string, string>) =>
    request<Connection>('PUT', `/connections/${id}/secret`, { body: { secrets } }),
  test: (id: string) => request<ConnectionTestResult>('POST', `/connections/${id}/test`, { body: {} }),
  /** KM2 connections: the folders the key can see and the ones the connection allows */
  km2Folders: (id: string, signal?: Signal) => request<KM2FolderList>('GET', `/connections/${id}/km2/folders`, { signal }),
  remove: (id: string) => request<void>('DELETE', `/connections/${id}`),
  /** MCP connections: the tools Discover tools last found, with their effects */
  tools: (id: string, signal?: Signal) => request<ConnectionTool[] | null>('GET', `/connections/${id}/tools`, { signal }).then((d) => d ?? []),
  discoverTools: (id: string) => request<DiscoverToolsResult>('POST', `/connections/${id}/tools/discover`, { body: {} }),
  setTools: (id: string, tools: ConnectionToolChange[]) =>
    request<ConnectionTool[] | null>('PUT', `/connections/${id}/tools`, { body: { tools } }).then((d) => d ?? []),
}

/** schedule_id in a list filter: '' is every schedule, 'none' is crew-wide state only. */
export type StateScheduleFilter = string

export type ActionLogQuery = { schedule_id?: StateScheduleFilter; subject?: string; action?: string; limit: number; offset: number }

function keyQuery(key: NoteKey) {
  return { schedule_id: key.schedule_id ?? 'none', subject: key.subject, name: key.name }
}

export const crewStateApi = {
  notes: (crewId: string, scheduleId: StateScheduleFilter = '', signal?: Signal) =>
    request<NoteInfo[] | null>('GET', `/crews/${crewId}/state/notes`, { query: { schedule_id: scheduleId }, signal }).then((d) => d ?? []),
  versions: (crewId: string, key: NoteKey, signal?: Signal) =>
    request<NoteVersion[] | null>('GET', `/crews/${crewId}/state/notes/versions`, { query: keyQuery(key), signal }).then((d) => d ?? []),
  putNote: (crewId: string, key: NoteKey, content: string, expectedVersion: number) =>
    request<NoteVersion>('PUT', `/crews/${crewId}/state/notes`, { body: { ...key, content, expected_version: expectedVersion } }),
  rollback: (crewId: string, key: NoteKey, version: number, expectedVersion: number) =>
    request<NoteVersion>('POST', `/crews/${crewId}/state/notes/rollback`, { body: { ...key, version, expected_version: expectedVersion } }),
  deleteNote: (crewId: string, key: NoteKey) => request<void>('DELETE', `/crews/${crewId}/state/notes`, { query: keyQuery(key) }),
  log: (crewId: string, query: ActionLogQuery, signal?: Signal): Promise<Page<ActionEntry>> =>
    requestPage<ActionEntry>(`/crews/${crewId}/state/log`, { query, signal }),
  deleteLogEntry: (crewId: string, entryId: string) => request<void>('DELETE', `/crews/${crewId}/state/log/${entryId}`),
  connections: (crewId: string, signal?: Signal) =>
    request<Connection[] | null>('GET', `/crews/${crewId}/connections`, { signal }).then((d) => d ?? []),
  setConnections: (crewId: string, connectionIds: string[]) =>
    request<Connection[] | null>('PUT', `/crews/${crewId}/connections`, { body: { connection_ids: connectionIds } }).then((d) => d ?? []),
}
