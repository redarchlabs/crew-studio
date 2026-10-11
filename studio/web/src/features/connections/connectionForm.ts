import { ApiError, errorMessage } from '../../api/client'
import type { Connection, ConnectionField, ConnectionInput, ConnectionKind, ConnectionKindKey } from '../../api/types.crewstate'

/** What the connection editor holds while someone types. Secrets are only ever typed, never loaded. */
export type ConnectionDraft = {
  name: string
  kind: ConnectionKindKey
  config: Record<string, string>
  area_ids: string[]
  enabled: boolean
  secrets: Record<string, string>
}

export type DraftErrors = Record<string, string>

const URL_INPUTS = new Set(['url', 'https_url'])
const HEADER_NAME = /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,100}$/
const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

export function newDraft(kind: ConnectionKindKey, areaIds: string[]): ConnectionDraft {
  return { name: '', kind, config: {}, area_ids: areaIds, enabled: true, secrets: {} }
}

export function draftFromConnection(c: Connection): ConnectionDraft {
  return { name: c.name, kind: c.kind, config: { ...c.config }, area_ids: [...c.area_ids], enabled: c.enabled, secrets: {} }
}

export function settingFields(kind: ConnectionKind): ConnectionField[] {
  return kind.fields.filter((f) => !f.secret)
}

export function secretFields(kind: ConnectionKind): ConnectionField[] {
  return kind.fields.filter((f) => f.secret)
}

/** A problem with one value, or undefined. Mirrors the server's checks so most mistakes show before saving. */
export function checkValue(field: ConnectionField, raw: string): string | undefined {
  const value = raw.trim()
  if (value === '') return undefined
  if (/[\r\n]/.test(value)) return 'Must be one line'
  if (field.prefix && !value.startsWith(field.prefix)) return `Must start with ${field.prefix}`
  if (URL_INPUTS.has(field.input)) {
    let url: URL
    try {
      url = new URL(value)
    } catch {
      return 'Must be a full URL, such as https://example.com'
    }
    if (field.input === 'https_url' && url.protocol !== 'https:' && !loopbackHttp(url)) return 'Must be an https URL'
    if (url.protocol !== 'https:' && url.protocol !== 'http:') return 'Must be an http or https URL'
    if (url.username || url.password) return 'Must not contain a user name or password'
  }
  if (field.input === 'email' && !EMAIL.test(value)) return 'Must be an email address'
  if (field.input === 'port') {
    const n = Number(value)
    if (!Number.isInteger(n) || n < 1 || n > 65535) return 'Must be a port number (1 to 65535)'
  }
  if (field.input === 'header' && !HEADER_NAME.test(value)) return 'Must be an HTTP header name, such as Authorization'
  if (field.input === 'cloud_id' && !UUID.test(value)) return 'Must be an Atlassian cloud id (a UUID)'
  if (field.input === 'uuid' && !UUID.test(value)) return 'Must be a KM2 folder id (a UUID)'
  if (field.input === 'uuid_list') return checkFolderList(value)
  if (field.input === 'toggle' && value !== 'true' && value !== 'false') return 'Must be on or off'
  if (field.input === 'select' && !(field.options ?? []).includes(value.toLowerCase())) return `Must be one of ${(field.options ?? []).join(', ')}`
  return undefined
}

/** Plain http is accepted for webhooks on this machine only (tests and local relays), like the server does. */
function loopbackHttp(url: URL): boolean {
  const host = url.hostname.replace(/^\[|\]$/g, '')
  return url.protocol === 'http:' && (host === 'localhost' || host === '::1' || /^127\./.test(host))
}

const MAX_FOLDERS = 50

/** A problem with a comma-separated list of KM2 folder ids, or undefined. */
function checkFolderList(value: string): string | undefined {
  const ids = value.split(/[\s,]+/).filter(Boolean)
  const bad = ids.find((id) => !UUID.test(id))
  if (bad) return `"${bad}" is not a KM2 folder id (a UUID)`
  if (new Set(ids.map((id) => id.toLowerCase())).size > MAX_FOLDERS) return `List at most ${MAX_FOLDERS} folders`
  return undefined
}

/** Field problems keyed by "name", "area_ids", "config.<key>" or "secrets.<key>". */
export function validateDraft(draft: ConnectionDraft, kind: ConnectionKind, creating: boolean): DraftErrors {
  const errors: DraftErrors = {}
  if (!draft.name.trim()) errors.name = 'Name is required'
  if (draft.area_ids.length === 0) errors.area_ids = 'Pick at least one functional area'
  for (const f of settingFields(kind)) {
    const value = draft.config[f.key] ?? ''
    const problem = f.required && !value.trim() ? 'Required' : checkValue(f, value)
    if (problem) errors[`config.${f.key}`] = problem
  }
  if (creating) Object.assign(errors, validateSecrets(draft.secrets, kind, false))
  return errors
}

/**
 * Problems with typed secrets. When replacing, the new set replaces every secret, so at least one must be given
 * and required ones cannot be left out.
 */
export function validateSecrets(secrets: Record<string, string>, kind: ConnectionKind, replacing: boolean): DraftErrors {
  const errors: DraftErrors = {}
  for (const f of secretFields(kind)) {
    const value = secrets[f.key] ?? ''
    const problem = replacing && f.required && !value.trim() ? 'Required' : checkValue(f, value)
    if (problem) errors[`secrets.${f.key}`] = problem
  }
  if (replacing && Object.values(secrets).every((v) => !v.trim())) errors.secrets = 'Enter at least one secret'
  return errors
}

function trimmedNonEmpty(values: Record<string, string>, fields: ConnectionField[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const f of fields) {
    const v = (values[f.key] ?? '').trim()
    if (v) out[f.key] = URL_INPUTS.has(f.input) ? v.replace(/\/+$/, '') : v
  }
  return out
}

/** The body to send. Secrets go only with a new connection, and only the ones typed in. */
export function toInput(draft: ConnectionDraft, kind: ConnectionKind, creating: boolean): ConnectionInput {
  const input: ConnectionInput = {
    name: draft.name.trim(),
    kind: draft.kind,
    config: trimmedNonEmpty(draft.config, settingFields(kind)),
    area_ids: draft.area_ids,
    enabled: draft.enabled,
  }
  const secrets = trimmedNonEmpty(draft.secrets, secretFields(kind))
  return creating && Object.keys(secrets).length > 0 ? { ...input, secrets } : input
}

export function secretsToSend(secrets: Record<string, string>, kind: ConnectionKind): Record<string, string> {
  return trimmedNonEmpty(secrets, secretFields(kind))
}

/** Field label for a secret key such as "api_token" ("API token"), or the key itself. */
export function secretLabel(kind: ConnectionKind | undefined, key: string): string {
  return kind?.fields.find((f) => f.key === key)?.label ?? key
}

/**
 * Splits a server error into field messages (details such as "config.site_url: is required" land on that field)
 * and general ones.
 */
export function serverErrors(err: unknown, fieldKeys: readonly string[]): { fields: DraftErrors; general: string[] } {
  const details = err instanceof ApiError && err.details.length > 0 ? err.details : [errorMessage(err)]
  const fields: DraftErrors = {}
  const general: string[] = []
  for (const detail of details) {
    const idx = detail.indexOf(':')
    const field = idx > 0 ? detail.slice(0, idx).trim() : ''
    if (fieldKeys.includes(field)) fields[field] = detail.slice(idx + 1).trim()
    else general.push(detail)
  }
  return { fields, general }
}

/** Every error key the editor can show for a kind. */
export function errorKeys(kind: ConnectionKind): string[] {
  return ['name', 'kind', 'area_ids', 'secrets', ...kind.fields.map((f) => `${f.secret ? 'secrets' : 'config'}.${f.key}`)]
}
