import { useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { peopleApi } from '../../api/endpoints.trackers'
import type { Connection } from '../../api/types.crewstate'
import type { PeopleMapEntry, PeopleView } from '../../api/types.trackers'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { AsyncView } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'

type Props = { connection: Connection; canManage: boolean; onClose: () => void }

const LINKS: Record<PeopleMapEntry['link'], { label: string; tone: 'ok' | 'blue' | 'neutral' }> = {
  email: { label: 'by email', tone: 'ok' },
  manual: { label: 'by hand', tone: 'blue' },
  '': { label: 'not linked', tone: 'neutral' },
}

function PersonRow({ p, view, canManage, busy, onLink }: { p: PeopleMapEntry; view: PeopleView; canManage: boolean; busy: boolean; onLink: (userId: string | null) => void }) {
  const link = LINKS[p.link]
  return (
    <tr className="border-b border-line align-top last:border-0 [&>td]:px-2 [&>td]:py-1.5">
      <td>
        <div className="font-medium">{p.display_name || p.account_id}</div>
        <div className="font-mono text-[11.5px] text-faint">{p.account_id}</div>
      </td>
      <td className="text-[12.5px] text-muted">{p.email || <span className="text-faint">hidden by the tracker</span>}</td>
      <td>
        {canManage ? (
          <select aria-label={`Studio user for ${p.display_name || p.account_id}`} className="field-input py-1 text-[12.5px]" value={p.user_id ?? ''} disabled={busy}
            onChange={(e) => onLink(e.target.value || null)}>
            <option value="">Not linked</option>
            {view.users.map((u) => <option key={u.id} value={u.id}>{u.name}{u.email ? ` (${u.email})` : ''}</option>)}
          </select>
        ) : (
          <span className="text-[12.5px]">{p.user_name || <span className="text-faint">not linked</span>}</span>
        )}
      </td>
      <td><Chip tone={link.tone}>{link.label}</Chip></td>
    </tr>
  )
}

/**
 * A tracker connection's people map: tracker accounts and the Studio users they are, matched by email where
 * the tracker shows one, or chosen by hand. Follow-ups may only mention people the crew knows, and reports use
 * the Studio names.
 */
export function PeopleMapModal({ connection, canManage, onClose }: Props) {
  const res = useAsync((signal) => peopleApi.list(connection.id, signal), [connection.id])
  const [busy, setBusy] = useState<string | null>(null)

  const sync = async () => {
    setBusy('sync')
    try {
      const out = await peopleApi.sync(connection.id)
      res.setData(() => ({ people: out.people, users: out.users }))
      notifySuccess(`Read ${out.read} people from ${connection.name}; ${out.linked} are linked to Studio users`)
    } catch (err) {
      notifyError(`Could not read people from ${connection.name}`, err)
    } finally {
      setBusy(null)
    }
  }
  const link = async (p: PeopleMapEntry, userId: string | null) => {
    setBusy(p.account_id)
    try {
      const saved = await peopleApi.link(connection.id, p.account_id, userId)
      res.setData((v) => v && { ...v, people: v.people.map((x) => (x.account_id === saved.account_id ? saved : x)) })
    } catch (err) {
      notifyError('Could not change the link', err)
    } finally {
      setBusy(null)
    }
  }

  return (
    <Modal title={`People in ${connection.name}`} onClose={onClose} width="max-w-3xl"
      footer={<>{canManage && <Button icon={<RefreshCw className="size-4" />} busy={busy === 'sync'} onClick={() => void sync()}>Read people from the tracker</Button>}<Button variant="ghost" onClick={onClose}>Close</Button></>}>
      <p className="mb-3 text-[12.5px] text-muted">
        Tracker accounts this connection has seen, and the Studio users they are. Accounts are matched by email when the tracker shows it; Jira often hides
        emails, so link those by hand. A link you choose is kept when people are read again. Crews may only @mention people on this list, an
        issue&apos;s assignee and lead, and tracking-list leads.
      </p>
      <AsyncView {...res}>
        {(view) => view.people.length === 0 ? (
          <p className="text-[12.5px] text-faint">No people yet.{canManage && ' Read them from the tracker.'}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[560px] text-left text-[13px]">
              <thead className="border-b border-line">
                <tr className="eyebrow [&>th]:px-2 [&>th]:py-1.5 [&>th]:font-semibold"><th>Tracker account</th><th>Email</th><th>Studio user</th><th>Link</th></tr>
              </thead>
              <tbody>
                {view.people.map((p) => <PersonRow key={p.account_id} p={p} view={view} canManage={canManage} busy={busy === p.account_id} onLink={(u) => void link(p, u)} />)}
              </tbody>
            </table>
            <p className="mt-2 text-[11.5px] text-faint">Last read {relative(view.people.reduce((a, p) => (p.seen_at > a ? p.seen_at : a), ''))}.</p>
          </div>
        )}
      </AsyncView>
    </Modal>
  )
}
