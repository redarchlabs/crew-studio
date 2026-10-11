import { useState, type FormEvent } from 'react'
import { CircleCheck, CircleSlash, Save } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import type { AccessSettings, Role } from '../../api/types'
import { Button } from '../../components/Button'
import { FormErrors, SelectField, Toggle } from '../../components/Field'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { NO_ERRORS, parseFieldErrors, type FieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { HostListEditor } from './HostListEditor'

const FIELDS = ['sso_auto_provision', 'auto_provision_role_id', 'callback_allowed_hosts'] as const

function roleOptions(roles: readonly Role[], current: string | null, rolesVisible: boolean) {
  const options = [{ value: '', label: 'No role (they see nothing until given one)' }, ...roles.map((r) => ({ value: r.id, label: r.name }))]
  // Keep the saved choice selectable when the role list cannot be read
  if (current && !roles.some((r) => r.id === current)) options.push({ value: current, label: rolesVisible ? 'Deleted role' : 'Current role (you cannot list roles)' })
  return options
}

function AccessForm({ initial, roles, rolesVisible, onSaved }: { initial: AccessSettings; roles: Role[]; rolesVisible: boolean; onSaved: (s: AccessSettings) => void }) {
  const [value, setValue] = useState<AccessSettings>(initial)
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [saving, setSaving] = useState(false)
  const dirty = JSON.stringify(value) !== JSON.stringify(initial)
  useUnsavedGuard(dirty)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setSaving(true)
    try {
      const saved = await adminApi.access.update(value)
      setErrors(NO_ERRORS)
      notifySuccess('Sign-in settings saved')
      onSaved(saved)
    } catch (err) {
      setErrors(parseFieldErrors(err, FIELDS))
      notifyError('Could not save sign-in settings', err)
    } finally {
      setSaving(false)
    }
  }

  const f = errors.fields
  return (
    <form onSubmit={submit} noValidate className="flex min-w-0 flex-col gap-4">
      <Panel title="New single sign-on users">
        <div className="flex flex-col gap-3">
          <Toggle
            label="Create accounts automatically"
            checked={value.sso_auto_provision}
            onChange={(on) => setValue((prev) => ({ ...prev, sso_auto_provision: on }))}
            hint="When on, anyone who signs in through a configured provider gets an account (with no functional areas). When off, people must be invited under People first."
          />
          <SelectField
            label="Role for automatically created accounts"
            value={value.auto_provision_role_id ?? ''}
            onChange={(id) => setValue((prev) => ({ ...prev, auto_provision_role_id: id || null }))}
            options={roleOptions(roles, value.auto_provision_role_id, rolesVisible)}
            disabled={!value.sso_auto_provision}
            error={f.auto_provision_role_id}
            hint="Give it as little as possible; an administrator can grant more and add areas later."
            className="max-w-md"
          />
          {f.sso_auto_provision && <div className="text-[12px] text-danger">{f.sso_auto_provision}</div>}
        </div>
      </Panel>
      <Panel title="Callback allowed hosts">
        <p className="mb-3 text-[12px] text-muted">
          Run callbacks normally go only to public <code className="font-mono">https</code> addresses. Hosts listed here may also be reached over plain{' '}
          <code className="font-mono">http</code> and on private networks, for internal systems. Enter a bare host name or IP address, without scheme, port or path.
        </p>
        <HostListEditor hosts={value.callback_allowed_hosts} onChange={(hosts) => setValue((prev) => ({ ...prev, callback_allowed_hosts: hosts }))} />
        {f.callback_allowed_hosts && <div className="mt-1 text-[12px] text-danger">{f.callback_allowed_hosts}</div>}
      </Panel>
      <FormErrors messages={errors.general} />
      <div className="flex items-center gap-2">
        <Button type="submit" variant="primary" icon={<Save className="size-4" />} busy={saving} disabled={!dirty}>Save sign-in settings</Button>
        {dirty && <Button variant="ghost" onClick={() => { setValue(initial); setErrors(NO_ERRORS) }}>Discard changes</Button>}
      </div>
    </form>
  )
}

function ProvidersPanel() {
  const { status } = useAuth()
  const providers = status?.providers ?? []
  return (
    <Panel title="Sign-in methods">
      <ul className="flex flex-col gap-2 text-[12.5px]">
        {providers.map((p) => (
          <li key={p.key} className="flex items-center gap-2">
            <CircleCheck className="size-4 text-ok" />
            <span className="flex-1">{p.label}</span>
            <Chip tone="blue">{p.key}</Chip>
          </li>
        ))}
        {providers.length === 0 && (
          <li className="flex items-center gap-2 text-muted"><CircleSlash className="size-4 text-faint" /> No single sign-on provider configured</li>
        )}
        <li className="flex items-center gap-2 border-t border-line pt-2">
          {status?.local_login ? <CircleCheck className="size-4 text-ok" /> : <CircleSlash className="size-4 text-faint" />}
          <span className="flex-1">Administrator password sign-in</span>
          <Chip tone={status?.local_login ? 'ok' : 'neutral'}>{status?.local_login ? 'enabled' : 'disabled'}</Chip>
        </li>
      </ul>
      <p className="mt-3 text-[11.5px] text-faint">
        Providers are configured on the server in <code className="font-mono">studio/.env</code> (<code className="font-mono">STUDIO_MICROSOFT_*</code>,{' '}
        <code className="font-mono">STUDIO_CLERK_*</code>) and take effect after a restart. They cannot be changed here.
      </p>
    </Panel>
  )
}

export function AccessPage() {
  const { can, canAny } = useAuth()
  const rolesVisible = canAny('admin.roles', 'admin.users')
  const res = useAsync(async (signal) => {
    const [settings, roles] = await Promise.all([adminApi.access.get(signal), rolesVisible ? adminApi.roles.list(signal) : Promise.resolve<Role[]>([])])
    return { settings, roles: [...roles].sort((a, b) => a.name.localeCompare(b.name)) }
  }, [rolesVisible])

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Sign-in settings"
        description="How new people get accounts and which internal hosts run callbacks may reach."
        actions={can('admin.audit') ? <HistoryLink type="access_settings" /> : undefined}
      />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        <AsyncView {...res}>
          {({ settings, roles }) => (
            <AccessForm
              key={JSON.stringify(settings)}
              initial={{ ...settings, callback_allowed_hosts: settings.callback_allowed_hosts ?? [] }}
              roles={roles}
              rolesVisible={rolesVisible}
              onSaved={(s) => res.setData((prev) => prev && { ...prev, settings: s })}
            />
          )}
        </AsyncView>
        <aside className="min-w-0"><ProvidersPanel /></aside>
      </div>
    </>
  )
}
