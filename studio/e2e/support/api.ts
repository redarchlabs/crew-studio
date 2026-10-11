import { expect, request as pwRequest, type APIRequestContext, type APIResponse } from '@playwright/test'
import { BASE_URL } from '../harness/paths.ts'
import { statePath, type RoleKey } from './accounts.ts'

export type Envelope<T = unknown> = { success: boolean; data: T; error: { code: string; message: string; details?: string[] } | null; meta?: { total: number } }

/** An API client carrying the saved browser session of a role (cookie auth, like the UI). */
export async function apiAs(role: RoleKey): Promise<APIRequestContext> {
  return pwRequest.newContext({ baseURL: BASE_URL, storageState: statePath(role) })
}

/** A bare client with no credentials (optionally with a bearer token). */
export async function apiAnon(token?: string): Promise<APIRequestContext> {
  // The test runner applies project defaults (the admin storageState) to new contexts, so empty it explicitly
  return pwRequest.newContext({ baseURL: BASE_URL, storageState: { cookies: [], origins: [] }, extraHTTPHeaders: token ? { Authorization: `Bearer ${token}` } : {} })
}

export async function unwrap<T>(res: APIResponse, expectedStatus?: number): Promise<T> {
  const body = (await res.json()) as Envelope<T>
  if (expectedStatus !== undefined) expect(res.status(), JSON.stringify(body.error)).toBe(expectedStatus)
  else expect(res.ok(), `${res.url()} -> ${res.status()} ${JSON.stringify(body.error)}`).toBe(true)
  return body.data
}

export type Named = { id: string; name: string }

export async function findByName<T extends Named>(api: APIRequestContext, path: string, name: string): Promise<T> {
  const res = await api.get(path)
  const data = await unwrap<T[] | { items: T[] }>(res)
  const list = Array.isArray(data) ? data : data.items
  const hit = list.find((x) => x.name === name)
  if (!hit) throw new Error(`${name} not found at ${path}`)
  return hit
}
