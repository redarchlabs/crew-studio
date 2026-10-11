import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { KeyRound, Plus, Search } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import type { AccountApiKey, ApiKey, NewApiKey, UserKind } from '../../api/types'
import { Pagination } from '../../components/Pagination'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { HistoryLink } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { dateTime, relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { NewKeyModal, SecretModal } from './apiKeyModals'
import { accountPath, keyStatus, maskedKey, sortKeys, type KeyFilter, type KeyStatus } from './apiKeys'
import { KeyStatusChip, RevokeKeyButton } from './ApiKeysPanel'
import { KeyAccess, useAreaNames } from './ApiKeyLimitsFields'
import { scopeOfUser } from './apiKeyLimits'
import type { KeyAccountOption } from './apiKeyModals'

const KIND_LABEL: Record<UserKind, string> = { human: 'Person', service: 'Service account' }

type KeyFiltersProps = { search: string; onSearch: (q: string) => void; filter: KeyFilter; onChange: (f: KeyFilter) => void; showKind: boolean }

function KeyFilters({ search, onSearch, filter, onChange, showKind }: KeyFiltersProps) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <div className="flex w-full max-w-sm items-center gap-2 rounded-md border border-line bg-raised px-2.5">
        <Search className="size-3.5 text-faint" />
        <input className="w-full bg-transparent py-1.5 outline-none" placeholder="Filter by key, account or prefix…" aria-label="Search keys" value={search} onChange={(e) => onSearch(e.target.value)} />
      </div>
      {showKind && (
        <select aria-label="Filter by account" className="field-input w-full sm:w-44" value={filter.kind} onChange={(e) => onChange({ ...filter, kind: e.target.value as KeyFilter['kind'] })}>
          <option value="">All accounts</option>
          <option value="human">People</option>
          <option value="service">Service accounts</option>
        </select>
      )}
      <select aria-label="Filter by status" className="field-input w-full sm:w-36" value={filter.status} onChange={(e) => onChange({ ...filter, status: e.target.value as '' | KeyStatus })}>
        <option value="">Any status</option>
        <option value="active">Active</option>
        <option value="expired">Expired</option>
        <option value="revoked">Revoked</option>
      </select>
    </div>
  )
}

function AllKeysTable({ keys, onRevoke }: { keys: readonly AccountApiKey[]; onRevoke: (key: ApiKey) => void }) {
  const areaName = useAreaNames()
  return (
    <div className="overflow-x-auto rounded-lg border border-line bg-panel">
      <table className="w-full min-w-[1080px] text-left">
        <thead className="border-b border-line">
          <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
            <th>Name</th><th>Key</th><th>Account</th><th>Access</th><th>Status</th><th>Created</th><th>Expires</th><th>Last used</th><th />
          </tr>
        </thead>
        <tbody>
          {sortKeys(keys).map((k) => (
            <tr key={k.id} className={`border-b border-line align-top last:border-0 [&>td]:px-3 [&>td]:py-1.5 ${keyStatus(k) === 'active' ? '' : 'text-muted'}`}>
              <td className="font-medium">{k.name}</td>
              <td className="font-mono text-[11.5px]">{maskedKey(k.prefix)}</td>
              <td>
                <Link to={accountPath(k)} className="hover:text-accent">{k.user_name}</Link>
                <div><Chip tone="neutral">{KIND_LABEL[k.user_kind]}</Chip></div>
              </td>
              <td><KeyAccess k={k} areaName={areaName} /></td>
              <td><KeyStatusChip k={k} /></td>
              <td className="whitespace-nowrap text-[12px]">
                {dateTime(k.created_at)}
                {k.created_by_name && <div className="text-faint">by {k.created_by_name}</div>}
              </td>
              <td className="whitespace-nowrap text-[12px]">{k.expires_at ? dateTime(k.expires_at) : <span className="text-faint">never</span>}</td>
              <td className="whitespace-nowrap text-[12px]" title={dateTime(k.last_used_at)}>{k.last_used_at ? relative(k.last_used_at) : <span className="text-faint">never</span>}</td>
              <td className="text-right"><RevokeKeyButton k={k} onRevoke={onRevoke} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export const PAGE_SIZE = 50
export const SEARCH_DEBOUNCE_MS = 300

/** /admin/api-keys: every key of the accounts the admin manages, with revoke, and new service account keys. */
export function AllApiKeysPage() {
  const { can } = useAuth()
  const canIssue = can('admin.service_accounts')
  const [filter, setFilter] = useState<KeyFilter>({ query: '', kind: '', status: '' })
  const [search, setSearch] = useState('')
  const [offset, setOffset] = useState(0)
  const res = useAsync(
    (signal) => adminApi.apiKeys({ limit: PAGE_SIZE, offset, q: filter.query || undefined, kind: filter.kind || undefined, status: filter.status || undefined }, signal),
    [offset, filter.query, filter.kind, filter.status],
  )
  const [accounts, setAccounts] = useState<KeyAccountOption[] | null>(null)
  const [created, setCreated] = useState<NewApiKey | null>(null)
  const filtering = filter.query !== '' || filter.kind !== '' || filter.status !== ''

  const changeFilter = (next: KeyFilter) => {
    setFilter(next)
    setOffset(0)
  }

  // Debounce the search box into the server-side filter
  useEffect(() => {
    const q = search.trim()
    if (q === filter.query) return
    const t = setTimeout(() => {
      setFilter((f) => ({ ...f, query: q }))
      setOffset(0)
    }, SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [search, filter.query])

  const startNew = async () => {
    try {
      const users = await adminApi.users.list('service')
      setAccounts(users.filter((u) => u.status === 'active').map((u) => ({ value: u.id, label: u.name, scope: scopeOfUser(u) })))
    } catch (err) {
      notifyError('Could not load service accounts', err)
    }
  }

  const onCreated = (c: NewApiKey) => {
    setAccounts(null)
    setCreated(c)
    // The new key is active and newest, so it heads the first page
    if (offset === 0) res.reload()
    else setOffset(0)
  }

  const revoke = async (key: ApiKey) => {
    try {
      await adminApi.users.revokeApiKey(key.user_id, key.id)
      const revokedAt = new Date().toISOString()
      res.setData((prev) => prev && { ...prev, items: prev.items.map((k) => (k.id === key.id ? { ...k, revoked_at: revokedAt } : k)) })
      // A revoked key no longer belongs in a list of active keys
      if (filter.status === 'active') res.reload()
      notifySuccess(`Revoked ${key.name}`)
    } catch (err) {
      notifyError(`Could not revoke ${key.name}`, err)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="API keys"
        description="Every key that can call the Crew Studio API: service accounts' keys and people's personal keys. Secrets are stored only as one-way hashes and are shown once, when a key is created."
        actions={
          <>
            {can('admin.audit') && <HistoryLink type="api_key" />}
            {canIssue && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => void startNew()}>New service account key</Button>}
          </>
        }
      />
      <KeyFilters search={search} onSearch={setSearch} filter={filter} onChange={changeFilter} showKind={can('admin.users') && canIssue} />
      <AsyncView {...res}>
        {(page) =>
          page.items.length === 0 && page.meta.offset > 0 ? (
            // Keys were revoked or removed elsewhere since this page was chosen
            <>
              <div className="py-6 text-muted">No keys on this page.</div>
              <Pagination meta={page.meta} onOffset={setOffset} />
            </>
          ) : page.items.length === 0 ? (
            filtering ? (
              <div className="py-6 text-muted">No keys match these filters.</div>
            ) : (
              <EmptyState icon={<KeyRound className="size-6" />} title="No API keys yet" hint="Issue service account keys here or on their pages; people create their own under My API keys." />
            )
          ) : (
            <>
              <AllKeysTable keys={page.items} onRevoke={(k) => void revoke(k)} />
              <Pagination meta={page.meta} onOffset={setOffset} />
            </>
          )
        }
      </AsyncView>
      {accounts && (
        <NewKeyModal accounts={accounts} who="account" create={(body, accountId) => adminApi.users.createApiKey(accountId, body)} onClose={() => setAccounts(null)} onCreated={onCreated} />
      )}
      {created && <SecretModal created={created} onClose={() => setCreated(null)} />}
    </>
  )
}
