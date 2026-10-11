import { useState, type ReactNode } from 'react'
import { KeyRound, Plus, XCircle } from 'lucide-react'
import { accountApi, adminApi, type NewApiKeyInput } from '../../api/endpoints'
import type { AccountApiKey, ApiKey, Me, NewApiKey, Page, User } from '../../api/types'
import { Button } from '../../components/Button'
import { Panel } from '../../components/PageHeader'
import { Pagination } from '../../components/Pagination'
import { AsyncView } from '../../components/States'
import { Chip, type Tone } from '../../components/StatusBadge'
import { ConfirmButton } from '../../components/misc'
import { dateTime, relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { KeyAccess, useAreaNames } from './ApiKeyLimitsFields'
import { NewKeyModal, SecretModal } from './apiKeyModals'
import { scopeOfMe, scopeOfUser, type KeyScope } from './apiKeyLimits'
import { keyStatus, maskedKey, sortKeys, type KeyStatus } from './apiKeys'

/** Keys per page in an account's panel */
export const PANEL_PAGE_SIZE = 20

export const STATUS_TONE: Record<KeyStatus, Tone> = { active: 'ok', expired: 'warn', revoked: 'danger' }

/** Where a panel's keys come from. Omit `create` for a list people can only revoke from. */
export type KeySource = {
  /** Changes when the source points at different keys */
  id: string
  list: (page: { limit: number; offset: number }, signal: AbortSignal) => Promise<Page<AccountApiKey>>
  create?: (body: NewApiKeyInput) => Promise<NewApiKey>
  revoke: (keyId: string) => Promise<void>
  /** What the account holds, the most a new key can be given; whose key it is, for the form's wording */
  scope: KeyScope
  who: 'you' | 'account'
}

/** Keys of a person or service account, managed by an admin; only service accounts get new keys here. */
export function accountKeySource(user: User, canCreate: boolean): KeySource {
  return {
    id: `account:${user.id}`,
    list: (page, signal) => adminApi.users.apiKeys(user.id, page, signal),
    create: canCreate ? (body) => adminApi.users.createApiKey(user.id, body) : undefined,
    revoke: (keyId) => adminApi.users.revokeApiKey(user.id, keyId),
    scope: scopeOfUser(user),
    who: 'account',
  }
}

/** The signed-in person's own keys. */
export function myKeySource(canCreate: boolean, me: Pick<Me, 'permissions' | 'all_areas' | 'area_ids'>): KeySource {
  return {
    id: 'me',
    list: (page, signal) => accountApi.apiKeys(page, signal),
    create: canCreate ? (body) => accountApi.createApiKey(body) : undefined,
    revoke: (keyId) => accountApi.revokeApiKey(keyId),
    scope: scopeOfMe(me),
    who: 'you',
  }
}

export function KeyStatusChip({ k }: { k: ApiKey }) {
  return <Chip tone={STATUS_TONE[keyStatus(k)]} title={k.revoked_at ? `Revoked ${dateTime(k.revoked_at)}` : undefined}>{keyStatus(k)}</Chip>
}

/** Revoke, for keys that are not revoked yet and that the caller may revoke (the server says which). */
export function RevokeKeyButton({ k, onRevoke }: { k: AccountApiKey; onRevoke: (key: ApiKey) => void }) {
  if (k.revoked_at) return null
  if (!k.can_revoke) {
    return <span className="text-[11.5px] text-faint" title="Revoking needs every permission and functional area this account holds.">Cannot revoke</span>
  }
  return <ConfirmButton icon={<XCircle className="size-3.5" />} onConfirm={() => onRevoke(k)}>Revoke</ConfirmButton>
}

function KeysTable({ keys, onRevoke, emptyText }: { keys: readonly AccountApiKey[]; onRevoke: (key: ApiKey) => void; emptyText: string }) {
  const areaName = useAreaNames()
  if (keys.length === 0) return <div className="px-4 py-6 text-center text-[12.5px] text-faint">{emptyText}</div>
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[860px] text-left">
        <thead className="border-b border-line">
          <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
            <th>Name</th><th>Key</th><th>Access</th><th>Status</th><th>Created</th><th>Expires</th><th>Last used</th><th />
          </tr>
        </thead>
        <tbody>
          {sortKeys(keys).map((k) => (
            <tr key={k.id} className={`border-b border-line last:border-0 [&>td]:px-3 [&>td]:py-1.5 ${keyStatus(k) === 'active' ? '' : 'text-muted'}`}>
              <td className="font-medium">{k.name}</td>
              <td className="font-mono text-[11.5px]">{maskedKey(k.prefix)}</td>
              <td><KeyAccess k={k} areaName={areaName} /></td>
              <td><KeyStatusChip k={k} /></td>
              <td className="whitespace-nowrap text-[12px]">{dateTime(k.created_at)}</td>
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

type ApiKeysPanelProps = { source: KeySource; intro: ReactNode; emptyText: string; title?: string }

/**
 * A page of API keys: create (secret shown once) when the source allows it, list and revoke. Render it with
 * key={source.id} so switching accounts resets its state.
 */
export function ApiKeysPanel({ source, intro, emptyText, title = 'API keys' }: ApiKeysPanelProps) {
  const [offset, setOffset] = useState(0)
  const res = useAsync((signal) => source.list({ limit: PANEL_PAGE_SIZE, offset }, signal), [source.id, offset])
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<NewApiKey | null>(null)
  const create = source.create

  const revoke = async (key: ApiKey) => {
    try {
      await source.revoke(key.id)
      const revokedAt = new Date().toISOString()
      res.setData((prev) => prev && { ...prev, items: prev.items.map((k) => (k.id === key.id ? { ...k, revoked_at: revokedAt } : k)) })
      notifySuccess(`Revoked ${key.name}`)
    } catch (err) {
      notifyError(`Could not revoke ${key.name}`, err)
    }
  }

  return (
    <Panel
      title={title}
      bodyClassName="p-0"
      actions={create && <Button size="sm" variant="primary" icon={<Plus className="size-3.5" />} onClick={() => setCreating(true)}>New key</Button>}
    >
      <div className="flex items-start gap-2 border-b border-line px-4 py-2 text-[12px] text-muted">
        <KeyRound className="mt-0.5 size-3.5 shrink-0 text-faint" />
        <div>{intro}</div>
      </div>
      <div className="[&>div[role=alert]]:m-3">
        <AsyncView {...res}>
          {(page) => (
            <>
              <KeysTable keys={page.items} onRevoke={(k) => void revoke(k)} emptyText={emptyText} />
              {(page.meta.total > page.meta.limit || page.meta.offset > 0) && (
                <div className="border-t border-line px-3 py-2">
                  <Pagination meta={page.meta} onOffset={setOffset} />
                </div>
              )}
            </>
          )}
        </AsyncView>
      </div>
      {creating && create && (
        <NewKeyModal
          scope={source.scope}
          who={source.who}
          create={(body) => create(body)}
          onClose={() => setCreating(false)}
          onCreated={(c) => {
            setCreating(false)
            setCreated(c)
            // The new key is active and newest, so it heads the first page
            if (offset === 0) res.reload()
            else setOffset(0)
          }}
        />
      )}
      {created && <SecretModal created={created} onClose={() => setCreated(null)} />}
    </Panel>
  )
}
