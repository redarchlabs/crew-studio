import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { adminApi } from '../../api/endpoints'
import type { Role, RoleInput } from '../../api/types'
import { EditorLayout } from '../../components/EditorLayout'
import { FormErrors, TextAreaField, TextField } from '../../components/Field'
import { ItemEditorHeader } from '../../components/ItemEditorHeader'
import { Panel } from '../../components/PageHeader'
import { LockableFieldset, ReadOnlyNote } from '../../components/ReadOnlyNote'
import { AsyncView, EmptyState } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { int } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useEntityForm } from '../../lib/useEntityForm'
import { SKIP_GUARD, useUnsavedGuard } from '../../lib/useUnsavedGuard'
import { EffectivePermissionsPanel } from './AccountPanels'
import { PermissionMatrix } from './PermissionMatrix'
import { EMPTY_ROLE, normalizeRole, ROLE_FIELDS, roleLockReason, toRoleInput, validateRole } from './roleForm'
import { useAdminRefs, type AdminRefs } from './useAdminRefs'

function RoleEditor({ role, refs }: { role: Role | null; refs: AdminRefs }) {
  const navigate = useNavigate()
  const { can, reload: reloadMe } = useAuth()
  const [deleting, setDeleting] = useState(false)
  const lock = roleLockReason(role, can('admin.roles'))
  const form = useEntityForm<RoleInput, Role>({
    entityId: role?.id ?? null,
    initial: role ? toRoleInput(role) : EMPTY_ROLE,
    fields: ROLE_FIELDS,
    label: 'role',
    validate: validateRole,
    normalize: normalizeRole,
    create: adminApi.roles.create,
    update: adminApi.roles.update,
  })
  useUnsavedGuard(form.dirty)
  const { value: v, set, errors } = form
  const f = errors.fields
  // The built-in role holds everything regardless of its stored list
  const shown = role?.builtin ? refs.defs.map((d) => d.key) : v.permissions

  const onSave = async () => {
    const saved = await form.save()
    if (!saved) return
    // The signed-in person may hold this role; refresh their permissions
    reloadMe()
    if (!role) navigate(`/admin/roles/${saved.id}`, { replace: true, state: SKIP_GUARD })
  }

  const onDelete = async () => {
    if (!role) return
    setDeleting(true)
    try {
      await adminApi.roles.remove(role.id)
      notifySuccess(`Deleted ${role.name}`)
      navigate('/admin/roles', { state: SKIP_GUARD })
    } catch (err) {
      notifyError(`Could not delete ${role.name}`, err)
      setDeleting(false)
    }
  }

  return (
    <>
      <ItemEditorHeader
        backTo="/admin/roles"
        backLabel="Roles"
        title={role ? role.name : 'New role'}
        isNew={!role}
        dirty={form.dirty}
        saving={form.saving}
        entityType="role"
        entityId={role?.id}
        onSave={() => void onSave()}
        onDelete={() => void onDelete()}
        deleting={deleting}
        canSave={lock === null}
        canDelete={lock === null}
      />
      {lock === 'builtin' && (
        <ReadOnlyNote>
          The built-in <strong>{role?.name}</strong> role always holds every permission, including ones added in later versions. It cannot be changed or deleted, so there is always a way back in.
        </ReadOnlyNote>
      )}
      {lock === 'permission' && <ReadOnlyNote perm="admin.roles" />}
      <EditorLayout
        main={
          <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); if (lock === null) void onSave() }} noValidate>
            <LockableFieldset locked={lock !== null} className="flex flex-col gap-4">
              <Panel title="Role">
                <div className="flex flex-col gap-3">
                  <TextField label="Name" value={v.name} onChange={(x) => set('name', x)} error={f.name} placeholder="Finance analyst" />
                  <TextAreaField label="Description" rows={2} value={v.description} onChange={(x) => set('description', x)} error={f.description} placeholder="Runs finance crews and reads their history" />
                </div>
              </Panel>
              <Panel title="Permissions">
                <p className="mb-3 text-[12px] text-muted">Hover a permission for what it allows. What people can see is still limited to their functional areas.</p>
                <PermissionMatrix defs={refs.defs} value={shown} onChange={(x) => set('permissions', x)} />
                {f.permissions && <div className="mt-1 text-[12px] text-danger">{f.permissions}</div>}
              </Panel>
              <FormErrors messages={errors.general} />
            </LockableFieldset>
          </form>
        }
        side={
          <>
            <EffectivePermissionsPanel perms={shown} defs={refs.defs} />
            {role && (
              <Panel title="Holders">
                <p className="text-[12.5px]">
                  <span className="font-mono tabular">{int(role.user_count)}</span> {role.user_count === 1 ? 'account holds' : 'accounts hold'} this role.
                </p>
                {role.user_count > 0 && lock === null && <p className="mt-2 text-[11.5px] text-faint">Changes apply to them at once. Deleting the role removes its permissions from them.</p>}
                {can('admin.users') && <Link to="/admin/users" className="mt-2 inline-block text-[12px] text-muted hover:text-accent">Manage people →</Link>}
              </Panel>
            )}
          </>
        }
      />
    </>
  )
}

export function RoleEditorPage() {
  const { id = 'new' } = useParams()
  const refs = useAdminRefs(false)
  // GET /admin/roles/{id} needs admin.roles, but admin.users may read the list: find the role there
  const res = useAsync(async (signal) => {
    if (id === 'new') return { role: null, missing: false }
    const role = (await adminApi.roles.list(signal)).find((r) => r.id === id) ?? null
    return { role, missing: role === null }
  }, [id])
  return (
    <AsyncView data={refs.data} error={refs.error} loading={refs.loading} reload={refs.reload}>
      {(r) => (
        <AsyncView {...res}>
          {({ role, missing }) =>
            missing ? (
              <EmptyState title="Role not found" hint="It may have been deleted." action={<Link className="text-accent" to="/admin/roles">Back to roles</Link>} />
            ) : (
              <RoleEditor key={id} role={role} refs={r} />
            )
          }
        </AsyncView>
      )}
    </AsyncView>
  )
}
