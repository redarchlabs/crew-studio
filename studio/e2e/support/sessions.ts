import crypto from 'node:crypto'
import fs from 'node:fs'
import { psql } from '../harness/db.ts'
import { readState } from '../harness/state.ts'

/**
 * Signs a user in by inserting a session row directly and returns the cookie value. Password sign-in is
 * administrators-only and SSO cannot be driven offline, so this is how non-admin browser sessions exist.
 */
export function insertSession(userId: string): string {
  const token = crypto.randomBytes(32).toString('base64url')
  const hash = crypto.createHash('sha256').update(token).digest('hex')
  psql(readState().dbName, `INSERT INTO sessions (token_hash, user_id, provider, ip, user_agent, expires_at) VALUES ('${hash}', '${userId}', 'local', '127.0.0.1', 'e2e', now() + interval '7 days')`)
  return token
}

export function cookieState(token: string): { cookies: Record<string, unknown>[]; origins: [] } {
  return {
    cookies: [{ name: 'crewstudio_session', value: token, domain: '127.0.0.1', path: '/', httpOnly: true, secure: false, sameSite: 'Lax', expires: Math.floor(Date.now() / 1000) + 6 * 86400 }],
    origins: [],
  }
}

export function writeSessionState(file: string, userId: string): void {
  fs.writeFileSync(file, JSON.stringify(cookieState(insertSession(userId))))
}
