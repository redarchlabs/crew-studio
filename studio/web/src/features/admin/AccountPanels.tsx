import { useState, type FormEvent } from 'react'
import { KeyRound, LogOut, Trash2 } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import type { LinkedIdentity, Permission, PermissionDef } from '../../api/types'
import { Button } from '../../components/Button'
import { TextField } from '../../components/Field'
import { Panel } from '../../components/PageHeader'
import { Chip } from '../../components/StatusBadge'
import { ConfirmButton } from '../../components/misc'
import { dateTime, relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { PASSWORD_MAX, PASSWORD_MIN, passwordProblem } from '../auth/authLogic'
import { summarizeByFeature } from './permissions'

/** Read-only summary of what the account ends up holding (roles + direct grants). */
export function EffectivePermissionsPanel({ perms, defs, note }: { perms: readonly Permission[]; defs: readonly PermissionDef[]; note?: string }) {
  const groups = summarizeByFeature(perms, defs)
  return (
    <Panel title={`Effective permissions · ${perms.length}`}>
      {groups.length === 0 ? (
        <p className="text-[12px] text-faint">None yet. Give them a role or grant permissions directly.</p>
      ) : (
        <dl className="flex flex-col gap-1.5 text-[12px]">
          {groups.map((g) => (
            <div key={g.feature} className="flex gap-2">
              <dt className="w-24 shrink-0 text-muted">{g.feature}</dt>
              <dd className="min-w-0 flex-1">{g.all && g.labels.length > 1 ? <span className="text-ok">All</span> : g.labels.join(', ')}</dd>
            </div>
          ))}
        </dl>
      )}
      {note && <p className="mt-3 text-[11.5px] text-faint">{note}</p>}
    </Panel>
  )
}

export function IdentitiesPanel({ identities }: { identities: readonly LinkedIdentity[] }) {
  return (
    <Panel title="Linked sign-ins">
      {identities.length === 0 ? (
        <p className="text-[12px] text-faint">Not linked yet. Their first single sign-on with this email links the account.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {identities.map((i) => (
            <li key={`${i.provider}:${i.email}`} className="flex items-start gap-2 text-[12.5px]">
              <Chip tone="blue">{i.provider}</Chip>
              <div className="min-w-0 flex-1">
                <div className="truncate" title={i.email}>{i.email}</div>
                <div className="text-[11px] text-faint" title={dateTime(i.last_login_at)}>last used {relative(i.last_login_at)}</div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

export function SessionsPanel({ userId, isSelf }: { userId: string; isSelf: boolean }) {
  const [busy, setBusy] = useState(false)
  const revoke = async () => {
    setBusy(true)
    try {
      const { ended } = await adminApi.users.revokeSessions(userId)
      notifySuccess(`Signed out of ${ended} session${ended === 1 ? '' : 's'}`)
    } catch (err) {
      notifyError('Could not sign them out', err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Panel title="Sessions">
      <p className="mb-3 text-[12px] text-muted">
        {isSelf ? 'This ends every session of yours, including this one.' : 'Ends every browser session at once, e.g. after a lost laptop. API keys are not affected.'}
      </p>
      <ConfirmButton icon={<LogOut className="size-3.5" />} busy={busy} onConfirm={() => void revoke()}>Sign out everywhere</ConfirmButton>
    </Panel>
  )
}

type PasswordPanelProps = { userId: string; hasPassword: boolean; onChanged: (hasPassword: boolean) => void }

/** Local password for administrators (people holding admin.users); everyone else uses single sign-on. */
export function PasswordPanel({ userId, hasPassword, onChanged }: PasswordPanelProps) {
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [errors, setErrors] = useState<{ password?: string; confirm?: string }>({})
  const [busy, setBusy] = useState<'set' | 'clear' | null>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const problem = passwordProblem(password)
    const next = { password: problem, confirm: !problem && password !== confirm ? 'Passwords do not match' : undefined }
    setErrors(next)
    if (next.password || next.confirm) return
    setBusy('set')
    try {
      await adminApi.users.setPassword(userId, password)
      setPassword('')
      setConfirm('')
      notifySuccess(hasPassword ? 'Password changed' : 'Password set')
      onChanged(true)
    } catch (err) {
      notifyError('Could not set the password', err)
    } finally {
      setBusy(null)
    }
  }

  const clear = async () => {
    setBusy('clear')
    try {
      await adminApi.users.clearPassword(userId)
      notifySuccess('Password removed')
      onChanged(false)
    } catch (err) {
      notifyError('Could not remove the password', err)
    } finally {
      setBusy(null)
    }
  }

  return (
    <Panel title="Administrator password">
      <p className="mb-3 text-[12px] text-muted">
        Email and password sign-in is only for administrators, as a fallback when single sign-on is unavailable.{' '}
        {hasPassword ? 'A password is set.' : 'No password is set.'}
      </p>
      <form className="flex flex-col gap-2.5" onSubmit={submit} noValidate>
        <TextField label={hasPassword ? 'New password' : 'Password'} type="password" autoComplete="new-password" value={password} onChange={setPassword} error={errors.password} hint={`${PASSWORD_MIN}–${PASSWORD_MAX} characters`} />
        <TextField label="Confirm" type="password" autoComplete="new-password" value={confirm} onChange={setConfirm} error={errors.confirm} />
        <div className="flex flex-wrap gap-2">
          <Button type="submit" size="sm" icon={<KeyRound className="size-3.5" />} busy={busy === 'set'}>{hasPassword ? 'Change password' : 'Set password'}</Button>
          {hasPassword && <ConfirmButton icon={<Trash2 className="size-3.5" />} busy={busy === 'clear'} onConfirm={() => void clear()}>Remove</ConfirmButton>}
        </div>
      </form>
    </Panel>
  )
}
