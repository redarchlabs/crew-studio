import { useMemo, useState } from 'react'
import { useNavigate } from 'react-router'
import { CircleAlert } from 'lucide-react'
import { ApiError } from '../../api/client'
import { adminApi } from '../../api/endpoints'
import type { Permission, User, UserInput, UserKind } from '../../api/types'
import { EditorLayout } from '../../components/EditorLayout'
import { FormErrors, SelectField, TextField, Toggle } from '../../components/Field'
import { ItemEditorHeader } from '../../components/ItemEditorHeader'
import { MultiSelect } from '../../components/MultiSelect'
import { Panel } from '../../components/PageHeader'
import { LockableFieldset } from '../../components/ReadOnlyNote'
import { useAuth } from '../../lib/auth'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAreas } from '../../lib/useAreas'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { ACCOUNT_FIELDS, areaWarning, conflictText, emptyAccount, normalizeAccount, toAccountInput, validateAccount } from './accountForm'
import { EffectivePermissionsPanel, IdentitiesPanel, PasswordPanel, SessionsPanel } from './AccountPanels'
import { accountKeySource, ApiKeysPanel } from './ApiKeysPanel'
import { PermissionMatrix } from './PermissionMatrix'
import { effectivePermissions } from './permissions'
import { SigningSecretPanel } from './SigningSecretPanel'
import type { AdminRefs } from './useAdminRefs'

export const ACCOUNT_BASE: Record<UserKind, string> = { human: '/admin/users', service: '/admin/service-accounts' }
const NOUN: Record<UserKind, string> = { human: 'person', service: 'service account' }

function ConflictBanner({ text, onDismiss }: { text: { title: string; body: string }; onDismiss: () => void }) {
  return (
    <div className="mb-4 flex items-start gap-3 rounded-lg border border-danger/40 bg-danger-soft px-4 py-3" role="alert">
      <CircleAlert className="mt-0.5 size-4 shrink-0 text-danger" />
      <div className="min-w-0 flex-1">
        <div className="font-medium">{text.title}</div>
        <div className="text-muted">{text.body}</div>
      </div>
      <button type="button" className="text-[12px] text-muted hover:text-fg" onClick={onDismiss}>Dismiss</button>
    </div>
  )
}

type AccountEditorProps = { kind: UserKind; user: User | null; refs: AdminRefs }

/** Create/edit form shared by people and service accounts. */
export function AccountEditor({ kind, user: loaded, refs }: AccountEditorProps) {
  const navigate = useNavigate()
  const { me, reload: reloadMe } = useAuth()
  const { areas, loading: areasLoading } = useAreas()
  const [user, setUser] = useState<User | null>(loaded)
  const [conflict, setConflict] = useState<{ title: string; body: string } | null>(null)
  const [deleting, setDeleting] = useState(false)
  const base = ACCOUNT_BASE[kind]
  const noun = NOUN[kind]
  const isSelf = !!user && user.id === me?.id
  const ssoLinked = (user?.identities?.length ?? 0) > 0

  // A 409 (last administrator, email taken) deserves more than a toast: keep it on screen
  const trapConflict = async <T,>(call: Promise<T>): Promise<T> => {
    try {
      return await call
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) setConflict(conflictText(err.message, err.code))
      throw err
    }
  }

  const form = useEntityForm<UserInput, User>({
    entityId: user?.id ?? null,
    initial: user ? toAccountInput(user) : emptyAccount(kind),
    fields: ACCOUNT_FIELDS,
    label: noun,
    validate: validateAccount,
    normalize: normalizeAccount,
    create: (body) => trapConflict(adminApi.users.create(body)),
    update: (id, body) => trapConflict(adminApi.users.update(id, body)),
  })
  useUnsavedGuard(form.dirty)
  const { value: v, set, errors } = form
  const f = errors.fields

  const catalog = useMemo(() => refs.defs.map((d) => d.key), [refs.defs])
  const rolePerms = useMemo(() => new Set<Permission>(effectivePermissions(refs.roles, v.role_ids, [], catalog)), [refs.roles, v.role_ids, catalog])
  // Without the role list the server's figure (as last saved) is the best available
  const effective = refs.rolesVisible ? effectivePermissions(refs.roles, v.role_ids, v.permissions, catalog) : (user?.effective_permissions ?? v.permissions)

  const onSave = async () => {
    const saved = await form.save()
    if (!saved) return
    setConflict(null)
    if (saved.id === me?.id) reloadMe()
    if (!user) navigate(`${base}/${saved.id}`, { replace: true, state: SKIP_GUARD })
    else setUser(saved)
  }

  const onDelete = async () => {
    if (!user) return
    setDeleting(true)
    try {
      await trapConflict(adminApi.users.remove(user.id))
      notifySuccess(`Deleted ${user.name}`)
      navigate(base, { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${user.name}`, err)
      setDeleting(false)
    }
  }

  const roleOptions = refs.roles.map((r) => ({ value: r.id, label: r.name, hint: r.builtin ? 'Built-in · every permission' : r.description || `${r.permissions.length} permissions` }))
  const areaOptions = areas.map((a) => ({ value: a.id, label: a.name, hint: a.description || undefined }))
  const noAreas = areaWarning(v)
  const showPassword = kind === 'human' && !!user && user.effective_permissions.includes('admin.users')

  return (
    <>
      <ItemEditorHeader
        backTo={base}
        backLabel={kind === 'human' ? 'People' : 'Service accounts'}
        title={user ? user.name : kind === 'human' ? 'Invite a person' : 'New service account'}
        isNew={!user}
        dirty={form.dirty}
        saving={form.saving}
        entityType="user"
        entityId={user?.id}
        onSave={() => void onSave()}
        onDelete={() => void onDelete()}
        deleting={deleting}
        canSave
        canDelete={!isSelf}
      />
      {conflict && <ConflictBanner text={conflict} onDismiss={() => setConflict(null)} />}
      <EditorLayout
        main={
          <div className="flex flex-col gap-4">
            <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void onSave() }} noValidate>
              <Panel title={kind === 'human' ? 'Person' : 'Service account'}>
                <div className="grid gap-3 sm:grid-cols-2">
                  <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder={kind === 'human' ? 'Ada Lovelace' : 'ERP integration'} className={kind === 'service' ? 'sm:col-span-2' : ''} />
                  {kind === 'human' && (
                    <TextField
                      label="Email"
                      type="email"
                      value={v.email}
                      onChange={(x) => set('email', x)}
                      error={f.email}
                      placeholder="ada@example.com"
                      // The server refuses to move the email of a linked account: SSO matched them on it
                      disabled={ssoLinked}
                      hint={ssoLinked ? 'Locked: this person signs in with single sign-on.' : 'Single sign-on matches people by this address the first time.'}
                    />
                  )}
                  <SelectField
                    label="Status"
                    value={v.status}
                    onChange={(x) => set('status', x === 'disabled' ? 'disabled' : 'active')}
                    options={[{ value: 'active', label: 'Active' }, { value: 'disabled', label: 'Disabled' }]}
                    error={f.status}
                    hint={kind === 'human' ? 'Disabling signs them out everywhere and blocks sign-in.' : 'Disabling stops every API key of this account.'}
                  />
                </div>
                {isSelf && v.status === 'disabled' && <p className="mt-2 text-[12px] text-warn">You are about to disable your own account.</p>}
              </Panel>
              <Panel title="Roles and areas">
                <div className="flex flex-col gap-4">
                  {refs.rolesVisible ? (
                    <MultiSelect label="Roles" options={roleOptions} value={v.role_ids} onChange={(x) => set('role_ids', x)} error={f.role_ids} emptyText="No roles yet. Create them under Admin › Roles." />
                  ) : (
                    <p className="text-[12px] text-muted">Roles are hidden: you need <code className="font-mono">admin.users</code> or <code className="font-mono">admin.roles</code> to see and assign them. Existing roles are kept.</p>
                  )}
                  <div className="flex flex-col gap-2">
                    <Toggle label="All areas" checked={v.all_areas} onChange={(x) => set('all_areas', x)} hint="Sees every crew, agent, task and work order, including areas added later." />
                    <LockableFieldset locked={v.all_areas} className={v.all_areas ? 'opacity-50' : ''}>
                      <MultiSelect label="Functional areas" options={areaOptions} value={v.area_ids} onChange={(x) => set('area_ids', x)} error={f.area_ids} emptyText={areasLoading ? 'Loading areas…' : 'No functional areas yet.'} />
                    </LockableFieldset>
                    {noAreas && <p className="text-[12px] text-warn">{noAreas}</p>}
                  </div>
                </div>
              </Panel>
              <Panel title="Direct permissions">
                <p className="mb-3 text-[12px] text-muted">Granted on top of their roles. Prefer roles; use these for one-off exceptions.</p>
                <PermissionMatrix defs={refs.defs} value={v.permissions} onChange={(x) => set('permissions', x)} inherited={rolePerms} />
              </Panel>
              <FormErrors messages={errors.general} />
            </form>
            {kind === 'service' && user && (
              <ApiKeysPanel
                key={user.id}
                source={accountKeySource(user.id, true)}
                intro="Keys let another system call the API as this account. Revoking a key stops it immediately."
                emptyText="No API keys yet. Create one for the system that will call Crew Studio."
              />
            )}
            {kind === 'human' && user && (
              <ApiKeysPanel
                key={user.id}
                title="Personal API keys"
                source={accountKeySource(user.id, false)}
                intro="Keys this person created under My API keys to call the API as themselves. Revoke them if a key may be exposed or the person leaves; only they can create keys."
                emptyText="This person has no API keys."
              />
            )}
            {kind === 'service' && !user && <p className="text-[12px] text-faint">Create the account first, then add API keys for it.</p>}
          </div>
        }
        side={
          <>
            <EffectivePermissionsPanel perms={effective} defs={refs.defs} note={form.dirty ? 'Includes unsaved changes.' : undefined} />
            {kind === 'service' && user && <SigningSecretPanel userId={user.id} />}
            {kind === 'human' && user && <IdentitiesPanel identities={user.identities ?? []} />}
            {showPassword && user && <PasswordPanel userId={user.id} hasPassword={user.has_password} onChanged={(has) => setUser((u) => u && { ...u, has_password: has })} />}
            {kind === 'human' && user && <SessionsPanel userId={user.id} isSelf={isSelf} />}
          </>
        }
      />
    </>
  )
}
