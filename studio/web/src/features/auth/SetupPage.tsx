import { useState, type FormEvent } from 'react'
import { Navigate, useSearchParams } from 'react-router'
import { RefreshCw, Rocket } from 'lucide-react'
import { ApiError, errorMessage } from '../../api/client'
import { authApi } from '../../api/endpoints'
import { Button } from '../../components/Button'
import { TextField } from '../../components/Field'
import { Loading } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { AuthCard, AuthNotice } from './AuthLayout'
import { PASSWORD_MAX, PASSWORD_MIN, validateSetup, type SetupValues } from './authLogic'

function setupError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401 || err.status === 403) return 'That setup token is not valid. Copy it again from the service log.'
    if (err.status === 409) return 'Setup has already been completed. Sign in instead.'
    if (err.status === 429) return 'Too many attempts. Wait a minute and try again.'
    if (err.details.length > 0) return err.details.join(' · ')
  }
  return errorMessage(err)
}

function SetupForm({ initialToken }: { initialToken: string }) {
  const { setMe, reload } = useAuth()
  const [values, setValues] = useState<SetupValues>({ token: initialToken, email: '', name: '', password: '', confirm: '' })
  const [errors, setErrors] = useState<Partial<Record<keyof SetupValues, string>>>({})
  const [serverError, setServerError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  // The link's token is used as-is; the field appears when there is none or the server rejected it
  const [tokenRejected, setTokenRejected] = useState(false)
  const set = (key: keyof SetupValues) => (v: string) => setValues((prev) => ({ ...prev, [key]: v }))

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const local = validateSetup(values)
    setErrors(local)
    if (Object.keys(local).length > 0) return
    setBusy(true)
    setServerError(null)
    try {
      const me = await authApi.setup({ token: values.token.trim(), email: values.email.trim(), name: values.name.trim(), password: values.password })
      setMe(me)
      // Refresh the sign-in status so setup_required clears before entering the app
      reload()
    } catch (err) {
      setServerError(setupError(err))
      if (err instanceof ApiError && (err.status === 401 || err.status === 403)) setTokenRejected(true)
      setBusy(false)
    }
  }

  return (
    <form className="flex flex-col gap-3" onSubmit={submit} noValidate>
      {serverError && <AuthNotice>{serverError}</AuthNotice>}
      {initialToken && !tokenRejected ? (
        <AuthNotice tone="info">Setup token found in the link.</AuthNotice>
      ) : (
        <TextField
          label="Setup token"
          value={values.token}
          onChange={set('token')}
          error={errors.token}
          autoComplete="off"
          className="font-mono"
          hint="Printed in the service log at start-up, as a /setup?token=… link."
        />
      )}
      <TextField label="Your name" value={values.name} onChange={set('name')} error={errors.name} autoComplete="name" />
      <TextField label="Email" type="email" value={values.email} onChange={set('email')} error={errors.email} autoComplete="email" hint="Use the address you sign in to Microsoft or Clerk with, so single sign-on finds you." />
      <TextField label="Password" type="password" value={values.password} onChange={set('password')} error={errors.password} autoComplete="new-password" hint={`${PASSWORD_MIN}–${PASSWORD_MAX} characters. Password sign-in is for administrators only.`} />
      <TextField label="Confirm password" type="password" value={values.confirm} onChange={set('confirm')} error={errors.confirm} autoComplete="new-password" />
      <Button type="submit" variant="primary" className="h-9 w-full" icon={<Rocket className="size-4" />} busy={busy}>
        Create administrator
      </Button>
    </form>
  )
}

export function SetupPage() {
  const { me, status, loading, error, reload } = useAuth()
  const [params] = useSearchParams()
  const token = params.get('token') ?? ''

  if (!status || (loading && me)) {
    if (error && !status) {
      return (
        <AuthCard title="Cannot reach Crew Studio">
          <AuthNotice>{errorMessage(error)}</AuthNotice>
          <Button icon={<RefreshCw className="size-3.5" />} onClick={reload}>Retry</Button>
        </AuthCard>
      )
    }
    return <AuthCard title="Set up Crew Studio"><Loading label={me ? 'Signing you in…' : 'Loading…'} /></AuthCard>
  }
  if (!status.setup_required) return <Navigate to={me ? '/' : '/login'} replace />

  return (
    <AuthCard
      title="Set up Crew Studio"
      subtitle="Create the first administrator. They can then invite people, set up roles and functional areas, and configure sign-in."
      footer="This screen is only available until the first account exists."
    >
      <SetupForm initialToken={token} />
    </AuthCard>
  )
}
