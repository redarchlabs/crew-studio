import { Link } from 'react-router'
import { Lock, Plus, UserCog } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { ReadOnlyNote } from '../../components/ReadOnlyNote'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { int } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'

export function RolesPage() {
  const { can } = useAuth()
  const canEdit = can('admin.roles')
  const res = useAsync(async (signal) => {
    const roles = await adminApi.roles.list(signal)
    return [...roles].sort((a, b) => Number(b.builtin) - Number(a.builtin) || a.name.localeCompare(b.name))
  }, [])

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Roles"
        description="Named sets of permissions. A person holds every permission of every role they have, plus any granted to them directly."
        actions={
          <>
            {can('admin.audit') && <HistoryLink type="role" />}
            {canEdit && <LinkButton to="/admin/roles/new" variant="primary" icon={<Plus className="size-4" />}>New role</LinkButton>}
          </>
        }
      />
      {!canEdit && <ReadOnlyNote perm="admin.roles" />}
      <AsyncView {...res}>
        {(roles) =>
          roles.length === 0 ? (
            <EmptyState icon={<UserCog className="size-6" />} title="No roles yet" action={canEdit ? <LinkButton to="/admin/roles/new" variant="primary">Create a role</LinkButton> : undefined} />
          ) : (
            <div className="overflow-x-auto rounded-lg border border-line bg-panel">
              <table className="w-full min-w-[640px] text-left">
                <thead className="border-b border-line">
                  <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                    <th>Role</th><th>Description</th><th className="text-right">Permissions</th><th className="text-right">People</th>
                  </tr>
                </thead>
                <tbody>
                  {roles.map((r) => (
                    <tr key={r.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                      <td className="whitespace-nowrap font-medium">
                        <Link to={`/admin/roles/${r.id}`} className="hover:text-accent">{r.name}</Link>
                        {r.builtin && <Chip tone="accent" className="ml-2"><Lock className="size-3" /> built-in</Chip>}
                      </td>
                      <td className="max-w-md text-[12.5px] text-muted">{r.description || <span className="text-faint">—</span>}</td>
                      <td className="text-right font-mono tabular">{r.builtin ? 'all' : int(r.permissions.length)}</td>
                      <td className="text-right font-mono tabular">{int(r.user_count)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </AsyncView>
    </>
  )
}
