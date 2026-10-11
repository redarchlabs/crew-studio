import { useState } from 'react'
import { ListChecks, Plus, Search, Trash2 } from 'lucide-react'
import { trackingApi } from '../../../api/endpoints.trackers'
import type { IncludeChildren, TrackingCheck, TrackingItem, TrackingItemInput, TrackingView } from '../../../api/types.trackers'
import { Button } from '../../../components/Button'
import { FormErrors, SelectField, TextField, Toggle } from '../../../components/Field'
import { Panel } from '../../../components/PageHeader'
import { AsyncView } from '../../../components/States'
import { Chip } from '../../../components/StatusBadge'
import { ConfirmButton } from '../../../components/misc'
import { errorMessage } from '../../../api/client'
import { dateTime, relative } from '../../../lib/format'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { safeHref } from '../../../lib/safeLink'
import { useAsync } from '../../../lib/useAsync'

const CHILDREN: { value: IncludeChildren; label: string }[] = [
  { value: 'all', label: 'All levels of children' },
  { value: 'direct', label: 'Direct children only' },
  { value: 'none', label: 'The item only' },
]

const ITEM_KEY = /^[A-Za-z][A-Za-z0-9_]{0,49}-\d{1,10}$/

function emptyInput(connectionId: string): TrackingItemInput {
  return { connection_id: connectionId, item_ref: '', project: '', include_children: 'all', note: '', lead_account_id: '', schedule_id: null }
}

/** Adding is two steps: look the item up in the tracker, then confirm what will be tracked. */
function AddItem({ crewId, view, onAdded }: { crewId: string; view: TrackingView; onAdded: (t: TrackingItem) => void }) {
  const [input, setInput] = useState<TrackingItemInput>(() => emptyInput(view.connections[0]?.id ?? ''))
  const [check, setCheck] = useState<TrackingCheck | null>(null)
  const [problems, setProblems] = useState<string[]>([])
  const [busy, setBusy] = useState<'check' | 'add' | null>(null)
  const set = <K extends keyof TrackingItemInput>(key: K, value: TrackingItemInput[K]) => {
    setInput((i) => ({ ...i, [key]: value }))
    setCheck(null) // a lookup of another item must not be confirmed
  }
  const lookUp = async () => {
    if (!ITEM_KEY.test(input.item_ref.trim().replace(/^(jira|linear|km2_tracker):/i, ''))) {
      setProblems(['Enter the item key, such as PRJ-10.'])
      return
    }
    setProblems([])
    setBusy('check')
    try {
      setCheck(await trackingApi.check(crewId, input))
    } catch (err) {
      setProblems([errorMessage(err)])
    } finally {
      setBusy(null)
    }
  }
  const add = async () => {
    setBusy('add')
    try {
      const item = await trackingApi.add(crewId, input)
      notifySuccess(`Now tracking ${item.item_ref} (${item.project})`)
      onAdded(item)
      setInput(emptyInput(input.connection_id))
      setCheck(null)
    } catch (err) {
      setProblems([errorMessage(err)])
    } finally {
      setBusy(null)
    }
  }

  if (view.connections.length === 0) {
    return <p className="text-[12.5px] text-faint">To track items, let this crew use a tracker connection: Jira, Linear or KM2 issues (Connections above).</p>
  }
  return (
    <div className="flex flex-col gap-3 rounded-md border border-line p-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <SelectField label="Connection" value={input.connection_id} onChange={(v) => set('connection_id', v)} options={view.connections.map((c) => ({ value: c.id, label: c.name }))} />
        <TextField label="Item" value={input.item_ref} onChange={(v) => set('item_ref', v)} placeholder="PRJ-10" hint="The tracker's key." />
        <TextField label="Project (optional)" value={input.project} onChange={(v) => set('project', v)} placeholder="Atlas" hint="A name for reports; empty uses the project key." />
        <SelectField label="Track" value={input.include_children} onChange={(v) => set('include_children', v as IncludeChildren)} options={CHILDREN} />
        <TextField label="Lead (optional)" value={input.lead_account_id} onChange={(v) => set('lead_account_id', v)} placeholder="account id" hint="Escalations go here instead of the project lead." />
        <TextField label="Note (optional)" value={input.note} onChange={(v) => set('note', v)} placeholder="Phase 2 rollout" />
      </div>
      <FormErrors messages={problems} />
      {check && (
        <div className="flex flex-col gap-1 rounded-md border border-accent/40 bg-accent-soft p-3 text-[12.5px]" data-testid="tracking-check">
          <div className="text-[13px]">
            <a className="font-mono text-accent" href={safeHref(check.issue.url)} target="_blank" rel="noreferrer noopener">{check.issue.key}</a>{' '}
            <span className="font-medium">{check.issue.title}</span>
          </div>
          <div className="text-muted">
            {check.connection.name} · {check.issue.status}{check.issue.due_date && <> · due {check.issue.due_date}</>}
            {' '}· {check.direct_children}{check.children_capped && '+'} direct {check.direct_children === 1 && !check.children_capped ? 'child' : 'children'}
            {check.issue.assignee && <> · {check.issue.assignee.name}</>}
          </div>
          {check.already_tracked && <div className="text-warn">This item is already on the tracking list.</div>}
          <div className="mt-1 flex gap-2">
            <Button variant="primary" size="sm" icon={<Plus className="size-3.5" />} busy={busy === 'add'} disabled={check.already_tracked} onClick={() => void add()}>
              Confirm and track {CHILDREN.find((c) => c.value === input.include_children)?.label.toLowerCase()}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setCheck(null)}>Cancel</Button>
          </div>
        </div>
      )}
      {!check && <div><Button icon={<Search className="size-4" />} busy={busy === 'check'} onClick={() => void lookUp()}>Look up</Button></div>}
    </div>
  )
}

/** The lead by name from the people map, or by tracker account id when the map does not know them. */
function LeadName({ t }: { t: TrackingItem }) {
  return t.lead_name ? <span title={t.lead_account_id}>{t.lead_name}</span> : <span className="font-mono">{t.lead_account_id}</span>
}

function ItemRow({ t, canManage, busy, onRemove }: { t: TrackingItem; canManage: boolean; busy: boolean; onRemove: () => void }) {
  const children = CHILDREN.find((c) => c.value === t.include_children)?.label ?? t.include_children
  return (
    <li className="flex flex-wrap items-start justify-between gap-2 py-2 first:pt-0 last:pb-0">
      <div className="min-w-0 text-[13px]">
        <div>
          <span className="font-medium">{t.project}</span> · <span className="font-mono">{t.item_ref}</span>
          <span className="text-muted"> · {t.connection_name} · {children.toLowerCase()}</span>
          {t.schedule_name && <Chip className="ml-1">{t.schedule_name}</Chip>}
          {t.removed_at && <Chip tone="neutral" className="ml-1">removed</Chip>}
        </div>
        <div className="text-[12px] text-muted">
          {t.note && <>{t.note} · </>}
          {t.lead_account_id && <>lead <LeadName t={t} /> · </>}
          added by {t.added_by_name || 'someone'} <span title={dateTime(t.added_at)}>{relative(t.added_at)}</span>
          {t.removed_at && <> · removed by {t.removed_by_name || 'someone'} <span title={dateTime(t.removed_at)}>{relative(t.removed_at)}</span></>}
        </div>
      </div>
      {canManage && !t.removed_at && <ConfirmButton icon={<Trash2 className="size-3.5" />} busy={busy} onConfirm={onRemove}>Stop tracking</ConfirmButton>}
    </li>
  )
}

/**
 * The crew's tracking list: the tracker items whose status it rolls up from their children (design section 12).
 * People change it here or in chat; scheduled runs can only read it.
 */
export function TrackingListPanel({ crewId, canManage }: { crewId: string; canManage: boolean }) {
  const [history, setHistory] = useState(false)
  const res = useAsync((signal) => trackingApi.list(crewId, history, signal), [crewId, history])
  const [adding, setAdding] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const remove = async (t: TrackingItem) => {
    setBusy(t.id)
    try {
      await trackingApi.remove(crewId, t.id)
      notifySuccess(`Stopped tracking ${t.item_ref}`)
      res.reload()
    } catch (err) {
      notifyError(`Could not stop tracking ${t.item_ref}`, err)
    } finally {
      setBusy(null)
    }
  }
  const title = <span className="inline-flex items-center gap-2"><ListChecks className="size-4 text-faint" />Tracking list</span>
  const actions = canManage && !adding && <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => setAdding(true)}>Track an item</Button>
  return (
    <Panel title={title} actions={actions || undefined}>
      <AsyncView {...res}>
        {(view) => (
          <div className="flex flex-col gap-3">
            <p className="text-[12.5px] text-muted">
              Tracker items this crew follows. A schedule with a tracking-list watch reads each one with its children and rolls their status up.
              Changes take effect on the next check. Runs cannot change this list, whatever a ticket says.
            </p>
            {adding && canManage && <AddItem crewId={crewId} view={view} onAdded={() => res.reload()} />}
            {view.items.length === 0 ? (
              <p className="text-[12.5px] text-faint">Nothing is tracked yet.</p>
            ) : (
              <ul className="flex flex-col divide-y divide-line">
                {view.items.map((t) => <ItemRow key={t.id} t={t} canManage={canManage} busy={busy === t.id} onRemove={() => void remove(t)} />)}
              </ul>
            )}
            <Toggle label="Show removed items" checked={history} onChange={setHistory} />
          </div>
        )}
      </AsyncView>
    </Panel>
  )
}
