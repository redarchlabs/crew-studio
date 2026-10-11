import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { Bot, Plus } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import type { Role } from '../../api/types'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { HistoryLink } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { byId } from '../../lib/catalog'
import { dateTime, relative } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { AccountAreas, RoleChips, StatusChip } from './AccountBits'
import { AccountFilters } from './AccountFilters'
import { filterAccounts, type StatusFilter } from './accountList'

export function ServiceAccountsPage() {
  const { can, canAny } = useAuth()
  // Role names need admin.roles or admin.users; without them the column shows counts only
  const canListRoles = canAny('admin.roles', 'admin.users')
  const res = useAsync(async (signal) => {
    const [users, roles] = await Promise.all([adminApi.users.list('service', signal), canListRoles ? adminApi.roles.list(signal) : Promise.resolve<Role[]>([])])
    return { users, roles: byId(roles) }
  }, [canListRoles])
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('')
  const visible = useMemo(() => filterAccounts(res.data?.users ?? [], query, status), [res.data, query, status])

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Service accounts"
        description="Accounts for other systems. They call the API with keys created here, and their runs' callbacks are signed with the account's signing secret."
        actions={
          <>
            {can('admin.audit') && <HistoryLink type="user" />}
            <LinkButton to="/admin/service-accounts/new" variant="primary" icon={<Plus className="size-4" />}>New service account</LinkButton>
          </>
        }
      />
      <AsyncView {...res}>
        {({ users, roles }) =>
          users.length === 0 ? (
            <EmptyState
              icon={<Bot className="size-6" />}
              title="No service accounts yet"
              hint="Create one for each system that starts runs or work orders through the API."
              action={<LinkButton to="/admin/service-accounts/new" variant="primary">Create a service account</LinkButton>}
            />
          ) : (
            <>
              <AccountFilters query={query} onQuery={setQuery} status={status} onStatus={setStatus} placeholder="Filter by name…" />
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[760px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Name</th><th>Roles</th><th>Areas</th><th>Status</th><th>Last active</th><th>Created</th>
                    </tr>
                  </thead>
                  <tbody>
                    {visible.map((u) => (
                      <tr key={u.id} className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                        <td className="font-medium"><Link to={`/admin/service-accounts/${u.id}`} className="hover:text-accent">{u.name}</Link></td>
                        <td><RoleChips roleIds={u.role_ids ?? []} roles={roles} /></td>
                        <td><AccountAreas user={u} /></td>
                        <td><StatusChip status={u.status} /></td>
                        <td className="whitespace-nowrap text-[12px] text-muted" title={dateTime(u.last_login_at)}>{u.last_login_at ? relative(u.last_login_at) : <span className="text-faint">never</span>}</td>
                        <td className="whitespace-nowrap text-[12px] text-muted">{dateTime(u.created_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {visible.length === 0 && <div className="py-6 text-muted">No service accounts match these filters.</div>}
            </>
          )
        }
      </AsyncView>
    </>
  )
}
