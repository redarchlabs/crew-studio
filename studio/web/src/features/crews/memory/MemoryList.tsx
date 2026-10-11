import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'
import { BookUp, Brain, Check, Pencil, Pin, PinOff, Plus, Search, Trash2 } from 'lucide-react'
import { memoryApi } from '../../../api/endpoints.memory'
import type { MemoryRecord, MemoryScopeCount, MemoryStatus } from '../../../api/types.memory'
import { Button } from '../../../components/Button'
import { Toggle } from '../../../components/Field'
import { Panel } from '../../../components/PageHeader'
import { Pagination } from '../../../components/Pagination'
import { AsyncView, EmptyState } from '../../../components/States'
import { Chip } from '../../../components/StatusBadge'
import { ConfirmButton } from '../../../components/misc'
import { dateTime, relative, shortId } from '../../../lib/format'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'
import { DeleteMemoriesDialog } from './DeleteMemoriesDialog'
import { MemoryEditor } from './MemoryEditor'
import { memoryOrigin, plural, scopeTree } from './memoryText'
import { PENDING_PROMOTE_HINT, PromoteToKM2Dialog } from './PromoteToKM2Dialog'

const PAGE = 25

type Filters = { q: string; scope: string; status: MemoryStatus | ''; pinned: boolean }

type Dialog = { kind: 'edit'; memory: MemoryRecord | null } | { kind: 'delete'; ids: string[] } | { kind: 'promote'; memories: MemoryRecord[] } | null

const STATUSES: { value: MemoryStatus | ''; label: string }[] = [
  { value: '', label: 'Any status' },
  { value: 'active', label: 'Active' },
  { value: 'pending', label: 'Waiting for review' },
]

/** The scope tree: every scope with how many memories are in and below it; clicking one filters the list. */
function ScopeTree({ scopes, active, onPick }: { scopes: readonly MemoryScopeCount[]; active: string; onPick: (scope: string) => void }) {
  const total = scopes.reduce((n, s) => n + s.count, 0)
  const item = (path: string, label: string, count: number, depth: number) => (
    <li key={path || 'all'}>
      <button type="button" aria-current={active === path ? 'true' : undefined} onClick={() => onPick(path)} style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}
        className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1 text-left text-[12.5px] hover:bg-panel-2 ${active === path ? 'bg-accent-soft text-accent' : ''}`}>
        <span className={path ? 'truncate font-mono text-[12px]' : 'truncate'}>{label}</span>
        <span className="tabular text-[11px] text-faint">{count}</span>
      </button>
    </li>
  )
  return (
    <nav aria-label="Scopes" className="min-w-0">
      <ul className="flex flex-col gap-0.5">
        {item('', 'All scopes', total, 0)}
        {scopeTree(scopes).map((n) => item(n.path, `/${n.name}`, n.count, n.depth + 1))}
      </ul>
    </nav>
  )
}

/** Where a memory came from: the run (with its task and agent) or the person who added it, and when. */
function Provenance({ m }: { m: MemoryRecord }) {
  const origin = memoryOrigin(m)
  return (
    <div className="text-[12px] text-muted">
      {origin.kind === 'run' && <>Saved by run <Link className="font-mono text-accent" to={`/runs/${origin.runId}`}>{shortId(origin.runId)}</Link></>}
      {origin.kind === 'deleted-run' && <>Saved by a run that was since deleted</>}
      {origin.kind === 'person' && <>Added by {origin.name}</>}
      {m.task_name && <> · task {m.task_name}</>}
      {m.agent_role && <> · agent {m.agent_role}</>}
      {' '}· <span title={dateTime(m.created_at)}>{relative(m.created_at)}</span>
      {m.updated_at !== m.created_at && <> · edited <span title={dateTime(m.updated_at)}>{relative(m.updated_at)}</span></>}
      {' '}· last recalled <span title={dateTime(m.last_accessed)}>{relative(m.last_accessed)}</span>
    </div>
  )
}

type RowProps = {
  m: MemoryRecord
  canManage: boolean
  selected: boolean
  busy: boolean
  onSelect: (on: boolean) => void
  onApprove: () => void
  onPin: () => void
  onEdit: () => void
  onDelete: () => void
}

function MemoryRow({ m, canManage, selected, busy, onSelect, onApprove, onPin, onEdit, onDelete }: RowProps) {
  return (
    <li className="flex items-start gap-3 px-4 py-3" data-testid="memory-row">
      {canManage && (
        <input type="checkbox" className="mt-1" aria-label={`Select memory ${shortId(m.id)}`} checked={selected} onChange={(e) => onSelect(e.target.checked)} />
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="whitespace-pre-wrap break-words text-[13px]">{m.content}</p>
        <div className="flex flex-wrap items-center gap-1">
          <Chip className="font-mono">{m.scope}</Chip>
          {m.status === 'pending' && <Chip tone="warn" title="Runs cannot recall it until someone approves it">waiting for review</Chip>}
          {m.pinned && <Chip tone="accent">pinned</Chip>}
          {m.km2_document_id && <Chip tone="teal" title={`KM2 document ${m.km2_document_id}`}>in KM2</Chip>}
          {m.categories.map((c) => <Chip key={c} tone="blue">{c}</Chip>)}
          <span className="text-[11px] text-faint">importance {m.importance.toFixed(1)}</span>
        </div>
        <Provenance m={m} />
      </div>
      {canManage && (
        <div className="flex shrink-0 flex-wrap justify-end gap-1">
          {m.status === 'pending' && <Button size="sm" variant="primary" icon={<Check className="size-3.5" />} busy={busy} onClick={onApprove}>Approve</Button>}
          <Button size="sm" variant="ghost" icon={m.pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />} disabled={busy} onClick={onPin}>{m.pinned ? 'Unpin' : 'Pin'}</Button>
          <Button size="sm" variant="ghost" icon={<Pencil className="size-3.5" />} onClick={onEdit}>Edit</Button>
          <ConfirmButton icon={<Trash2 className="size-3.5" />} disabled={busy} onConfirm={onDelete}>Delete</ConfirmButton>
        </div>
      )}
    </li>
  )
}

/** The crew's memories: search by meaning, filter by scope, status and pin, and (with state.manage) change them. */
export function MemoryList({ crewId, canManage }: { crewId: string; canManage: boolean }) {
  const [typed, setTyped] = useState('')
  const [filters, setFilters] = useState<Filters>({ q: '', scope: '', status: '', pinned: false })
  const [offset, setOffset] = useState(0)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  const [busy, setBusy] = useState<string | null>(null)
  const [dialog, setDialog] = useState<Dialog>(null)
  const res = useAsync((signal) => memoryApi.list(crewId, { ...filters, limit: PAGE, offset }, signal), [crewId, filters, offset])
  const scopes = useAsync((signal) => memoryApi.scopes(crewId, signal), [crewId])

  const filter = (next: Partial<Filters>) => {
    setOffset(0)
    setSelected(new Set())
    setFilters((prev) => ({ ...prev, ...next }))
  }
  const search = (e: FormEvent) => {
    e.preventDefault()
    filter({ q: typed.trim() })
  }
  const refresh = () => {
    setSelected(new Set())
    res.reload()
    scopes.reload()
  }
  const select = (id: string, on: boolean) => setSelected((prev) => (on ? new Set([...prev, id]) : new Set([...prev].filter((x) => x !== id))))

  const act = async (m: MemoryRecord, run: () => Promise<unknown>, done: string, failed: string) => {
    setBusy(m.id)
    try {
      await run()
      notifySuccess(done)
      refresh()
    } catch (err) {
      notifyError(failed, err)
    } finally {
      setBusy(null)
    }
  }
  const approve = (m: MemoryRecord) => act(m, () => memoryApi.approve(crewId, m.id), 'Approved the memory', 'Could not approve the memory')
  const pin = (m: MemoryRecord) =>
    act(m, () => memoryApi.update(crewId, m.id, { pinned: !m.pinned }), m.pinned ? 'Unpinned the memory' : 'Pinned the memory', 'Could not change the pin')
  const remove = (m: MemoryRecord) => act(m, () => memoryApi.remove(crewId, m.id), 'Deleted the memory', 'Could not delete the memory')
  const closeAfter = () => {
    setDialog(null)
    refresh()
  }

  const items = res.data?.items ?? []
  const picked = items.filter((m) => selected.has(m.id))
  const pickedPending = picked.some((m) => m.status === 'pending')
  const filtered = Boolean(filters.q || filters.scope || filters.status || filters.pinned)
  const title = <span className="inline-flex items-center gap-2"><Brain className="size-4 text-faint" />Memories</span>
  const actions = canManage && (
    <div className="flex gap-2">
      <Button size="sm" variant="ghost" icon={<Trash2 className="size-3.5" />} onClick={() => setDialog({ kind: 'delete', ids: [] })}>Delete memories…</Button>
      <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setDialog({ kind: 'edit', memory: null })}>Add a memory</Button>
    </div>
  )

  return (
    <Panel title={title} actions={actions || undefined} bodyClassName="p-0">
      <form onSubmit={search} className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-2.5">
        <input aria-label="Search memories" className="field-input max-w-72" placeholder="Search by meaning or words" value={typed} onChange={(e) => setTyped(e.target.value)} />
        <Button type="submit" size="sm" icon={<Search className="size-3.5" />}>Search</Button>
        <select aria-label="Status filter" className="field-input max-w-48" value={filters.status} onChange={(e) => filter({ status: e.target.value as MemoryStatus | '' })}>
          {STATUSES.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
        </select>
        <Toggle label="Pinned only" checked={filters.pinned} onChange={(x) => filter({ pinned: x })} />
      </form>
      {canManage && picked.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 border-b border-line bg-accent-soft/40 px-4 py-2 text-[12.5px]" data-testid="memory-selection">
          <span>{plural(picked.length, 'memory', 'memories')} selected</span>
          <Button size="sm" icon={<BookUp className="size-3.5" />} disabled={pickedPending} title={pickedPending ? PENDING_PROMOTE_HINT : undefined}
            onClick={() => setDialog({ kind: 'promote', memories: picked })}>Promote to KM2</Button>
          <Button size="sm" variant="ghost" icon={<Trash2 className="size-3.5" />} onClick={() => setDialog({ kind: 'delete', ids: picked.map((m) => m.id) })}>Delete selected</Button>
          <Button size="sm" variant="ghost" onClick={() => setSelected(new Set())}>Clear</Button>
          {pickedPending && <span className="text-warn">{PENDING_PROMOTE_HINT}</span>}
        </div>
      )}
      <div className="grid gap-0 md:grid-cols-[200px_minmax(0,1fr)]">
        <div className="border-b border-line p-2 md:border-r md:border-b-0">
          <ScopeTree scopes={scopes.data ?? []} active={filters.scope} onPick={(scope) => filter({ scope })} />
        </div>
        <div className="min-w-0">
          <AsyncView {...res}>
            {({ items: page, meta }) =>
              page.length === 0 ? (
                <div className="p-4">
                  <EmptyState icon={<Brain className="size-6" />} title={filtered ? 'No matching memories' : 'No memories yet'}
                    hint="Memories are short lessons the crew keeps between runs. Runs save them when memory is set to Explicit saves or Automatic; people can add them here." />
                </div>
              ) : (
                <>
                  <ul className="flex flex-col divide-y divide-line">
                    {page.map((m) => (
                      <MemoryRow key={m.id} m={m} canManage={canManage} selected={selected.has(m.id)} busy={busy === m.id} onSelect={(on) => select(m.id, on)}
                        onApprove={() => void approve(m)} onPin={() => void pin(m)} onEdit={() => setDialog({ kind: 'edit', memory: m })} onDelete={() => void remove(m)} />
                    ))}
                  </ul>
                  <div className="px-3"><Pagination meta={meta} onOffset={setOffset} /></div>
                </>
              )
            }
          </AsyncView>
        </div>
      </div>
      {dialog?.kind === 'edit' && (
        <MemoryEditor crewId={crewId} memory={dialog.memory} defaultScope={filters.scope || '/'} onClose={() => setDialog(null)} onSaved={closeAfter} />
      )}
      {dialog?.kind === 'delete' && (
        <DeleteMemoriesDialog crewId={crewId} ids={dialog.ids} initialScope={filters.scope} onClose={() => setDialog(null)} onDeleted={closeAfter} />
      )}
      {dialog?.kind === 'promote' && (
        <PromoteToKM2Dialog crewId={crewId} memories={dialog.memories} onClose={() => setDialog(null)} onPromoted={refresh} />
      )}
    </Panel>
  )
}
