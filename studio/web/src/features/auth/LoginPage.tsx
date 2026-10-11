import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router'
import { ChevronDown, ChevronRight, KeyRound, LogIn, RefreshCw } from 'lucide-react'
import { authApi } from '../../api/endpoints'
import type { SsoProvider } from '../../api/types'
import { errorMessage } from '../../api/client'
import { Button } from '../../components/Button'
import { TextField } from '../../components/Field'
import { Loading } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { AuthCard, AuthNotice } from './AuthLayout'
import { passwordLoginError, sanitizeReturnTo, ssoErrorMessage } from './authLogic'

const PROVIDER_NAMES: Record<SsoProvider['key'], string> = { microsoft: 'Microsoft', clerk: 'Clerk' }

function SsoButtons({ providers, returnTo }: { providers: readonly SsoProvider[]; returnTo: string }) {
  const [leaving, setLeaving] = useState<string | null>(null)
  return (
    <div className="flex flex-col gap-2">
      {providers.map((p) => (
        <Button
          key={p.key}
          variant="primary"
          className="h-9 w-full"
          icon={<LogIn className="size-4" />}
          busy={leaving === p.key}
          disabled={leaving !== null}
          onClick={() => {
            setLeaving(p.key)
            // Full-page navigation: the API redirects to the provider and back to return_to
            window.location.assign(authApi.ssoLoginUrl(p.key, returnTo))
          }}
        >
          Continue with {PROVIDER_NAMES[p.key] ?? p.label}
        </Button>
      ))}
    </div>
  )
}

function PasswordForm({ returnTo }: { returnTo: string }) {
  const { setMe } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email.trim() || !password) return setError('Enter your email and password.')
    setBusy(true)
    setError(null)
    try {
      const me = await authApi.login(email.trim(), password)
      setMe(me)
      navigate(returnTo, { replace: true })
    } catch (err) {
      setError(passwordLoginError(err))
      setBusy(false)
    }
  }

  return (
    <form className="flex flex-col gap-3" onSubmit={submit} noValidate>
      {error && <AuthNotice>{error}</AuthNotice>}
      <TextField label="Email" type="email" autoComplete="username" value={email} onChange={setEmail} required />
      <TextField label="Password" type="password" autoComplete="current-password" value={password} onChange={setPassword} required />
      <Button type="submit" variant="secondary" className="h-9 w-full" icon={<KeyRound className="size-4" />} busy={busy}>
        Sign in
      </Button>
    </form>
  )
}

/** Administrator sign-in: open on its own, folded away under the SSO buttons otherwise. */
function AdminSignIn({ returnTo, collapsed }: { returnTo: string; collapsed: boolean }) {
  const [open, setOpen] = useState(!collapsed)
  if (!collapsed) return <PasswordForm returnTo={returnTo} />
  return (
    <div className="mt-5 border-t border-line pt-3">
      <button type="button" aria-expanded={open} className="flex w-full items-center gap-1.5 text-[12.5px] text-muted hover:text-fg" onClick={() => setOpen((o) => !o)}>
        {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
        Administrator sign-in
      </button>
      {open && (
        <div className="mt-3">
          <p className="mb-3 text-[11.5px] text-faint">Email and password sign-in is only for administrators. Everyone else uses the buttons above.</p>
          <PasswordForm returnTo={returnTo} />
        </div>
      )}
    </div>
  )
}

export function LoginPage() {
  const { me, status, loading, error, reload } = useAuth()
  const [params] = useSearchParams()
  const returnTo = sanitizeReturnTo(params.get('return_to'))
  const ssoError = ssoErrorMessage(params.get('error'))

  if (!status) {
    if (error) {
      return (
        <AuthCard title="Cannot reach Crew Studio">
          <AuthNotice>{errorMessage(error)}</AuthNotice>
          <Button icon={<RefreshCw className="size-3.5" />} onClick={reload}>Retry</Button>
        </AuthCard>
      )
    }
    return <AuthCard title="Sign in"><Loading label={loading ? 'Checking sign-in options…' : 'Loading…'} /></AuthCard>
  }
  if (status.setup_required) return <Navigate to="/setup" replace />
  if (me) return <Navigate to={returnTo} replace />

  const providers = status.providers ?? []
  const nothingConfigured = providers.length === 0 && !status.local_login

  return (
    <AuthCard
      title="Sign in"
      subtitle={nothingConfigured ? undefined : providers.length > 0 ? 'Use your organisation account to continue.' : 'Administrator sign-in with email and password.'}
      footer="Sessions last up to 7 days and end after 12 hours without use."
    >
      {ssoError && <AuthNotice>{ssoError}</AuthNotice>}
      {nothingConfigured ? (
        <AuthNotice tone="info">
          No sign-in method is configured yet. An administrator must set up single sign-on by adding the{' '}
          <code className="font-mono text-[11.5px]">STUDIO_CLERK_*</code> or <code className="font-mono text-[11.5px]">STUDIO_MICROSOFT_*</code>{' '}
          settings in <code className="font-mono text-[11.5px]">studio/.env</code> and restarting the service.
        </AuthNotice>
      ) : (
        <>
          {providers.length > 0 && <SsoButtons providers={providers} returnTo={returnTo} />}
          {status.local_login && <AdminSignIn returnTo={returnTo} collapsed={providers.length > 0} />}
        </>
      )}
    </AuthCard>
  )
}
