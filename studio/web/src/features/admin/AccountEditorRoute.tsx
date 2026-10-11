import { Link, Navigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { adminApi } from '../../api/endpoints'
import type { User, UserKind } from '../../api/types'
import { EmptyState, ErrorState, Loading } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { useAsync } from '../../lib/useAsync'
import { ACCOUNT_BASE, AccountEditor } from './AccountEditor'
import { useAdminRefs } from './useAdminRefs'

/** Loads the account (or starts a new one for id "new") plus roles and the permission catalog. */
export function AccountEditorRoute({ kind }: { kind: UserKind }) {
  const { id = 'new' } = useParams()
  const { canAny } = useAuth()
  const refs = useAdminRefs(canAny('admin.users', 'admin.roles'))
  const res = useAsync<User | null>((signal) => (id === 'new' ? Promise.resolve(null) : adminApi.users.get(id, signal)), [id])
  const base = ACCOUNT_BASE[kind]

  const error = res.error ?? refs.error
  if (error) {
    if (error instanceof ApiError && error.status === 404) {
      return <EmptyState title="Account not found" hint="It may have been deleted." action={<Link className="text-accent" to={base}>Back to the list</Link>} />
    }
    return <ErrorState error={error} onRetry={() => { res.reload(); refs.reload() }} />
  }
  if (res.data === undefined || !refs.data) return <Loading />
  // A link to the wrong screen (person vs service account) goes to the right one
  if (res.data && res.data.kind !== kind) return <Navigate to={`${ACCOUNT_BASE[res.data.kind]}/${res.data.id}`} replace />
  return <AccountEditor key={id} kind={kind} user={res.data} refs={refs.data} />
}
