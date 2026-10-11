import type { ApiErrorBody, Envelope, Page, PageMeta } from './types'

export const API_BASE = '/api'

/** Fired on window whenever the API answers 401, so the app can send the person to the sign-in page. */
export const UNAUTHENTICATED_EVENT = 'crewstudio:unauthenticated'

/** Error thrown by every API call. `status` is 0 for network failures. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details: string[]

  constructor(status: number, body: ApiErrorBody) {
    super(body.message)
    this.name = 'ApiError'
    this.status = status
    this.code = body.code
    this.details = body.details ?? []
  }
}

export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export type RequestOptions = {
  body?: unknown
  query?: Record<string, string | number | undefined | null>
  signal?: AbortSignal
}

type Unwrapped<T> = { data: T; meta: PageMeta | undefined }

export function buildUrl(path: string, query?: RequestOptions['query']): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== '') params.set(key, String(value))
  }
  const qs = params.toString()
  return `${API_BASE}${path}${qs ? `?${qs}` : ''}`
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isEnvelope(value: unknown): value is Envelope<unknown> {
  return isRecord(value) && typeof value.success === 'boolean' && 'data' in value
}

function toErrorBody(value: unknown, fallback: ApiErrorBody): ApiErrorBody {
  if (!isRecord(value)) return fallback
  const code = typeof value.code === 'string' ? value.code : fallback.code
  const message = typeof value.message === 'string' && value.message ? value.message : fallback.message
  const details = Array.isArray(value.details)
    ? value.details.filter((d): d is string => typeof d === 'string')
    : []
  return { code, message, details }
}

function statusFallback(status: number): ApiErrorBody {
  const messages: Record<number, string> = {
    400: 'The request was malformed.',
    401: 'Your session has ended. Sign in again.',
    403: 'You do not have permission to do that.',
    404: 'Not found.',
    413: 'That file is too large.',
    409: 'Conflict: the name is taken or the item is still referenced.',
    415: 'The server rejected the content type.',
    422: 'Validation failed.',
    500: 'The server hit an internal error.',
    502: 'The Crew Studio API is not reachable through the dev proxy. Is the backend running?',
    503: 'The Crew Studio API is unavailable.',
    504: 'The Crew Studio API timed out.',
  }
  return { code: `http_${status}`, message: messages[status] ?? `Request failed with status ${status}.` }
}

async function readJson(res: Response): Promise<unknown> {
  const text = await res.text()
  if (!text) return undefined
  try {
    return JSON.parse(text) as unknown
  } catch {
    return undefined
  }
}

/** Performs a request and unwraps the envelope, returning data plus pagination meta. */
export async function requestEnvelope<T>(
  method: HttpMethod,
  path: string,
  opts: RequestOptions = {},
): Promise<Unwrapped<T>> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const init: RequestInit = { method, headers, signal: opts.signal, credentials: 'same-origin' }
  if (opts.body instanceof FormData) {
    // The browser sets the multipart boundary; the custom header makes cross-site form posts impossible
    headers['X-Requested-With'] = 'crewstudio'
    init.body = opts.body
  } else {
    if (method !== 'GET') headers['Content-Type'] = 'application/json'
    if (opts.body !== undefined) init.body = JSON.stringify(opts.body)
  }

  let res: Response
  try {
    res = await fetch(buildUrl(path, opts.query), init)
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') throw err
    throw new ApiError(0, {
      code: 'network_error',
      message: 'Cannot reach the Crew Studio API. Is the backend running on 127.0.0.1:8088?',
    })
  }

  if (res.status === 401 && !path.startsWith('/auth/')) {
    window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT))
  }
  if (res.status === 204) return { data: undefined as T, meta: undefined }

  const json = await readJson(res)
  if (!isEnvelope(json)) {
    if (!res.ok) throw new ApiError(res.status, statusFallback(res.status))
    throw new ApiError(res.status, {
      code: 'bad_response',
      message: 'The server returned a response that is not a Crew Studio envelope.',
    })
  }
  if (!res.ok || !json.success) {
    const fallback = res.ok ? { code: 'unknown_error', message: 'The request failed.' } : statusFallback(res.status)
    throw new ApiError(res.ok ? 500 : res.status, toErrorBody(json.error, fallback))
  }
  return { data: json.data as T, meta: json.meta }
}

export async function request<T>(method: HttpMethod, path: string, opts?: RequestOptions): Promise<T> {
  const { data } = await requestEnvelope<T>(method, path, opts)
  return data
}

export async function requestPage<T>(path: string, opts?: RequestOptions): Promise<Page<T>> {
  const { data, meta } = await requestEnvelope<T[] | null>('GET', path, opts)
  const items = data ?? []
  return { items, meta: meta ?? { total: items.length, limit: items.length, offset: 0 } }
}

/** Best-effort human message for any thrown value. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'Something went wrong.'
}

export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}
