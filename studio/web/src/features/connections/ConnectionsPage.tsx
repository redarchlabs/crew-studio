import { useState } from 'react'
import { KeyRound, Pencil, PlugZap, Plus, TriangleAlert, Trash2, Users, Wrench, Zap } from 'lucide-react'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { Connection, ConnectionKind, ConnectionTestResult } from '../../api/types.crewstate'
import { isTrackerKind } from '../../api/types.trackers'
import { AreaChips } from '../../components/AreaChips'
import { Button } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { ConfirmButton } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { dateTime, relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { secretFields, secretLabel } from './connectionForm'
import { ConnectionEditor } from './ConnectionEditor'
import { KM2TestDetails } from './KM2TestDetails'
import { MCPToolsModal } from './MCPToolsModal'
import { PeopleMapModal } from './PeopleMapModal'
import { SecretModal } from './SecretModal'

type Editing = { kind: 'edit'; connection: Connection | null } | { kind: 'secret' | 'people' | 'tools'; connection: Connection } | null

const isTracker = (c: Connection) => isTrackerKind(c.kind)

function NoKeyBanner() {
  return (
    <div className="mb-4 flex items-start gap-2 rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px]" role="status">
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-warn" />
      <span>Connections are off: the server has no <code className="font-mono">STUDIO_SECRETS_KEY</code> to encrypt their secrets. Whoever runs Crew Studio can set it and restart.</span>
    </div>
  )
}

function TestOutcome({ result }: { result: ConnectionTestResult }) {
  const tone = !result.supported ? 'neutral' : result.ok ? 'ok' : 'danger'
  return <Chip tone={tone} title={result.message} className="mt-1 whitespace-normal">{result.message}</Chip>
}

type RowProps = {
  c: Connection
  kind: ConnectionKind | undefined
  canManage: boolean
  test: ConnectionTestResult | undefined
  onEdit: () => void
  onSecret: () => void
  onTest: () => void
  onDelete: () => void
  onPeople: () => void
  onTools: () => void
  busy: string | null
}

function ConnectionRow({ c, kind, canManage, test, onEdit, onSecret, onTest, onDelete, onPeople, onTools, busy }: RowProps) {
  const hasSecrets = kind ? secretFields(kind).length > 0 : false
  return (
    <tr className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
      <td className="max-w-xs">
        <div className="font-medium">{c.name}</div>
        <div className="mt-1"><AreaChips areaIds={c.area_ids} max={2} /></div>
        {isTracker(c) && <Button className="mt-1" size="sm" variant="ghost" icon={<Users className="size-3.5" />} onClick={onPeople}>People map</Button>}
        {c.kind === 'mcp' && <Button className="mt-1" size="sm" variant="ghost" icon={<Wrench className="size-3.5" />} onClick={onTools}>Tools</Button>}
        {test && <TestOutcome result={test} />}
        {test && (test.folders !== undefined || (test.warnings ?? []).length > 0) && <KM2TestDetails result={test} config={c.config} />}
      </td>
      <td className="text-[12.5px]">{kind?.label ?? c.kind}</td>
      <td>
        <div className="flex flex-wrap gap-1">
          <Chip tone={c.enabled ? 'ok' : 'neutral'}>{c.enabled ? 'enabled' : 'disabled'}</Chip>
          {c.missing_secrets.length > 0 && <Chip tone="warn" title="Runs cannot use it until these are set">missing {c.missing_secrets.map((k) => secretLabel(kind, k)).join(', ')}</Chip>}
        </div>
      </td>
      <td className="text-[12.5px] text-muted">
        {c.secret_keys.length === 0 ? <span className="text-faint">none</span> : <span title={dateTime(c.secret_updated_at)}>{c.secret_keys.map((k) => secretLabel(kind, k)).join(', ')} · set {relative(c.secret_updated_at)}</span>}
      </td>
      <td className="text-right font-mono tabular">{c.crew_count}</td>
      {canManage && (
        <td className="whitespace-nowrap text-right">
          <Button size="sm" variant="ghost" icon={<Pencil className="size-3.5" />} onClick={onEdit}>Edit</Button>
          {hasSecrets && <Button size="sm" variant="ghost" icon={<KeyRound className="size-3.5" />} onClick={onSecret}>Replace secret</Button>}
          <Button size="sm" variant="ghost" icon={<Zap className="size-3.5" />} busy={busy === `test:${c.id}`} onClick={onTest} title={kind?.testable ? 'Try to reach the system now' : 'Testing is not supported yet for this kind'}>Test</Button>
          <ConfirmButton icon={<Trash2 className="size-3.5" />} busy={busy === `delete:${c.id}`} onConfirm={onDelete}>Delete</ConfirmButton>
        </td>
      )}
    </tr>
  )
}

/** /connections: credentials for outside systems, in the viewer's functional areas. Secrets are write-only. */
export function ConnectionsPage() {
  const { can } = useAuth()
  const canManage = can('connections.manage')
  const res = useAsync(async (signal) => {
    const [kinds, list] = await Promise.all([connectionsApi.kinds(signal), connectionsApi.list(signal)])
    return { kinds, list }
  }, [])
  const [editing, setEditing] = useState<Editing>(null)
  const [tests, setTests] = useState<Record<string, ConnectionTestResult>>({})
  const [busy, setBusy] = useState<string | null>(null)

  const upsert = (c: Connection) => {
    res.setData((prev) => prev && { ...prev, list: [...prev.list.filter((x) => x.id !== c.id), c] })
    setEditing(null)
    notifySuccess(`Saved ${c.name}`)
  }

  const runTest = async (c: Connection) => {
    setBusy(`test:${c.id}`)
    try {
      const result = await connectionsApi.test(c.id)
      setTests((t) => ({ ...t, [c.id]: result }))
    } catch (err) {
      notifyError(`Could not test ${c.name}`, err)
    } finally {
      setBusy(null)
    }
  }

  const remove = async (c: Connection) => {
    setBusy(`delete:${c.id}`)
    try {
      await connectionsApi.remove(c.id)
      res.setData((prev) => prev && { ...prev, list: prev.list.filter((x) => x.id !== c.id) })
      notifySuccess(`Deleted ${c.name}`)
    } catch (err) {
      notifyError(`Could not delete ${c.name}`, err)
    } finally {
      setBusy(null)
    }
  }

  const ready = res.data?.kinds.secrets_ready ?? false
  const newButton = canManage && ready && <Button variant="primary" icon={<Plus className="size-4" />} onClick={() => setEditing({ kind: 'edit', connection: null })}>New connection</Button>
  return (
    <>
      <PageHeader
        eyebrow="Configure"
        title="Connections"
        description="Credentials for outside systems such as Jira, Linear, KM2 (knowledge or issues), Slack, Teams, email, JSON APIs and MCP servers. Crews choose which connections their tools may use; secrets are encrypted and never shown again."
        actions={newButton}
      />
      <AsyncView {...res}>
        {({ kinds, list }) => (
          <>
            {!kinds.secrets_ready && <NoKeyBanner />}
            {list.length === 0 ? (
              <EmptyState icon={<PlugZap className="size-6" />} title="No connections yet" hint="A connection holds the address and credentials of an outside system, for the functional areas you choose." action={newButton} />
            ) : (
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[860px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th>Connection</th><th>Kind</th><th>Status</th><th>Secrets</th><th className="text-right">Crews</th>{canManage && <th />}
                    </tr>
                  </thead>
                  <tbody>
                    {[...list].sort((a, b) => a.name.localeCompare(b.name)).map((c) => (
                      <ConnectionRow
                        key={c.id}
                        c={c}
                        kind={kinds.kinds.find((k) => k.kind === c.kind)}
                        canManage={canManage}
                        test={tests[c.id]}
                        busy={busy}
                        onEdit={() => setEditing({ kind: 'edit', connection: c })}
                        onSecret={() => setEditing({ kind: 'secret', connection: c })}
                        onTest={() => void runTest(c)}
                        onDelete={() => void remove(c)}
                        onPeople={() => setEditing({ kind: 'people', connection: c })}
                        onTools={() => setEditing({ kind: 'tools', connection: c })}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {editing?.kind === 'edit' && <ConnectionEditor kinds={kinds.kinds} connection={editing.connection} onClose={() => setEditing(null)} onSaved={upsert} />}
            {editing?.kind === 'people' && <PeopleMapModal connection={editing.connection} canManage={canManage} onClose={() => setEditing(null)} />}
            {editing?.kind === 'tools' && <MCPToolsModal connection={editing.connection} canManage={canManage} onClose={() => setEditing(null)} />}
            {editing?.kind === 'secret' && (
              <SecretModal connection={editing.connection} kind={kinds.kinds.find((k) => k.kind === editing.connection.kind) ?? kinds.kinds[0]} onClose={() => setEditing(null)} onSaved={upsert} />
            )}
          </>
        )}
      </AsyncView>
    </>
  )
}
