import { ApiError, errorMessage } from '../../api/client'

/** Pages that must never be the destination after signing in (they would loop). */
const AUTH_PATHS = ['/login', '/setup']

/**
 * Only same-site paths are allowed after sign-in: they start with one "/" and are not protocol-relative
 * ("//evil.example" or "/\evil.example", which browsers treat the same). Anything else goes to "/".
 */
export function sanitizeReturnTo(raw: string | null | undefined): string {
  if (!raw) return '/'
  const value = raw.trim()
  if (!value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) return '/'
  // Control characters (including tabs and newlines, which URL parsers strip) could smuggle a host in
  if (/[\u0000-\u001f\u007f]/.test(value)) return '/'
  const path = value.split(/[?#]/)[0]
  if (AUTH_PATHS.some((p) => path === p || path.startsWith(`${p}/`))) return '/'
  return value
}

const SSO_ERRORS: Record<string, string> = {
  not_invited: "Your account hasn't been set up yet. Ask an administrator to invite you.",
  disabled: 'Your account has been disabled. Ask an administrator if you think this is a mistake.',
  unverified_email: 'Your sign-in provider has not verified your email address. Verify it with the provider, then try again.',
  expired: 'The sign-in took too long and expired. Please try again.',
  provider_error: 'The sign-in provider reported a problem. Please try again in a moment.',
}

/** Friendly text for the ?error= code the SSO callback redirects with; null when there is none. */
export function ssoErrorMessage(code: string | null): string | null {
  if (!code) return null
  return SSO_ERRORS[code] ?? 'Sign-in did not complete. Please try again.'
}

/** Friendly text for a failed administrator password sign-in. */
export function passwordLoginError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401 || err.code === 'invalid_credentials') return "That email and password don't match an administrator account."
    if (err.status === 423 || err.code === 'locked') return 'Too many failed attempts. This account is locked for 15 minutes.'
    if (err.status === 429) return 'Too many sign-in attempts from this network. Wait a minute and try again.'
    if (err.status === 403) return 'Password sign-in is only for administrators. Use your organisation sign-in instead.'
  }
  return errorMessage(err)
}

export const PASSWORD_MIN = 12
export const PASSWORD_MAX = 72

export type SetupValues = { token: string; email: string; name: string; password: string; confirm: string }

/** A rough email shape check; the server has the final say. */
export function looksLikeEmail(email: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())
}

/** Problems with a password for local sign-in, or undefined when it is acceptable. */
export function passwordProblem(password: string): string | undefined {
  if (password.length < PASSWORD_MIN) return `Use at least ${PASSWORD_MIN} characters`
  if (password.length > PASSWORD_MAX) return `Use at most ${PASSWORD_MAX} characters`
  return undefined
}

export function validateSetup(v: SetupValues): Partial<Record<keyof SetupValues, string>> {
  const errs: Partial<Record<keyof SetupValues, string>> = {}
  if (!v.token.trim()) errs.token = 'Paste the setup token from the service log'
  if (!looksLikeEmail(v.email)) errs.email = 'Enter a valid email address'
  if (!v.name.trim()) errs.name = 'Enter your name'
  const pw = passwordProblem(v.password)
  if (pw) errs.password = pw
  if (!errs.password && v.password !== v.confirm) errs.confirm = 'Passwords do not match'
  return errs
}
