import type { AccountApiKey, ApiKey, ApiKeyStatus, UserKind } from '../../api/types'

export type KeyStatus = ApiKeyStatus

export function keyStatus(key: Pick<ApiKey, 'expires_at' | 'revoked_at'>, now: number = Date.now()): KeyStatus {
  if (key.revoked_at) return 'revoked'
  if (key.expires_at && Date.parse(key.expires_at) <= now) return 'expired'
  return 'active'
}

/** How a key is recognised without its secret: csk_<prefix>_… */
export function maskedKey(prefix: string): string {
  return `csk_${prefix}_…`
}

/** Expiry for a key picked as a calendar date: the end of that day in the browser's time zone. */
export function expiresAtFromDate(date: string): string | null {
  if (!date) return null
  const at = new Date(`${date}T23:59:59`)
  return Number.isNaN(at.getTime()) ? null : at.toISOString()
}

export function newKeyProblems(name: string, date: string, now: number = Date.now()): { name?: string; expires?: string } {
  const errs: { name?: string; expires?: string } = {}
  if (!name.trim()) errs.name = 'Give the key a name, e.g. the system that will use it'
  if (date) {
    const at = expiresAtFromDate(date)
    if (!at) errs.expires = 'Pick a valid date'
    else if (Date.parse(at) <= now) errs.expires = 'Pick a date in the future'
  }
  return errs
}

/** Active keys first, then newest first. */
export function sortKeys<T extends ApiKey>(keys: readonly T[], now: number = Date.now()): T[] {
  const rank = (k: ApiKey) => (keyStatus(k, now) === 'active' ? 0 : 1)
  return [...keys].sort((a, b) => rank(a) - rank(b) || b.created_at.localeCompare(a.created_at))
}

export type KeyFilter = { query: string; kind: '' | UserKind; status: '' | KeyStatus }

/** Where the key's account is edited. */
export function accountPath(k: Pick<AccountApiKey, 'user_id' | 'user_kind'>): string {
  return k.user_kind === 'service' ? `/admin/service-accounts/${k.user_id}` : `/admin/users/${k.user_id}`
}
