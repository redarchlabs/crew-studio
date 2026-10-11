import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { KeyRound, Plus, Users } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { byId } from '../../lib/catalog'
import { dateTime, relative } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { AccountAreas, RoleChips, StatusChip } from './AccountBits'
import { AccountFilters } from './AccountFilters'
import { filterAccounts, providersOf, type StatusFilter } from './accountList'

export function UsersPage() {
  const { can, me } = useAuth()
  const res = useAsync(async (signal) => {
    const [users, roles] = await Promise.all([adminApi.users.list('human', signal), adminApi.roles.list(signal)])
    return { users, roles: byId(roles) }
  }, [])
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('')
  const visible = useMemo(() => filterAccounts(res.data?.users ?? [], query, status), [res.data, query, status])

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="People"
        description="Everyone who can sign in. People sign in with single sign-on; invite them here first (unless auto-provisioning is on), then give them roles and functional areas."
        actions={
          <>
            {can('admin.audit') && <HistoryLink type="user" />}
            <LinkButton to="/admin/users/new" variant="primary" icon={<Plus className="size-4" />}>Invite person</LinkButton>
          </>
        }
      />
      <AsyncView {...res}>
        {({ users, roles }) =>
          users.length === 0 ? (
            <EmptyState icon={<Users className="size-6" />} title="No people yet" action={<LinkButton to="/admin/users/new" variant="primary">Invite someone</LinkButton>} />
          ) : (
            <>
              <AccountFilters query={query} onQuery={setQuery} status={status} onStatus={setStatus} placeholder="Filter by name or email…" />
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[960px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Name</th><th>Email</th><th>Roles</th><th>Areas</th><th>Status</th><th>Last sign-in</th><th>Sign-in methods</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visible.map((u) => (
                      <tr key={u.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                        <td className="font-medium">
                          <Link to={`/admin/users/${u.id}`} className="hover:text-accent">{u.name}</Link>
                          {u.id === me?.id && <span className="ml-1.5 text-[11px] text-faint">(you)</span>}
                        </td>
                        <td className="max-w-56 truncate text-[12.5px] text-muted" title={u.email ?? undefined}>{u.email || <span className="text-faint">—</span>}</td>
                        <td><RoleChips roleIds={u.role_ids ?? []} roles={roles} /></td>
                        <td><AccountAreas user={u} /></td>
                        <td><StatusChip status={u.status} /></td>
                        <td className="whitespace-nowrap text-[12px] text-muted" title={dateTime(u.last_login_at)}>{u.last_login_at ? relative(u.last_login_at) : <span className="text-faint">never</span>}</td>
                        <td>
                          <span className="inline-flex flex-wrap gap-1">
                            {providersOf(u).map((p) => <Chip key={p} tone="blue">{p}</Chip>)}
                            {u.has_password && <Chip title="Has an administrator password"><KeyRound className="size-3" /> password</Chip>}
                            {providersOf(u).length === 0 && !u.has_password && <span className="text-[11.5px] text-faint">not linked yet</span>}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {visible.length === 0 && <div className="py-6 text-muted">No people match these filters.</div>}
            </>
          )
        }
      </AsyncView>
    </>
  )
}
