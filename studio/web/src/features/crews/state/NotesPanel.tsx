import { useState } from 'react'
import { History, NotebookText } from 'lucide-react'
import { crewStateApi } from '../../../api/endpoints.crewstate'
import type { NoteInfo } from '../../../api/types.crewstate'
import { Button } from '../../../components/Button'
import { Panel } from '../../../components/PageHeader'
import { AsyncView, EmptyState } from '../../../components/States'
import { dateTime, relative } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { formatBytes } from '../../workorders/files/attachments'
import { NoteHistory } from './NoteHistory'

function noteId(n: NoteInfo): string {
  return `${n.schedule_id ?? ''}|${n.subject}|${n.name}`
}

/** The crew's notes (latest version of each), with their history one click away. */
export function NotesPanel({ crewId, canManage }: { crewId: string; canManage: boolean }) {
  const res = useAsync((signal) => crewStateApi.notes(crewId, '', signal), [crewId])
  const [open, setOpen] = useState<NoteInfo | null>(null)
  return (
    <Panel title="Notes" bodyClassName="p-0">
      <AsyncView {...res}>
        {(notes) =>
          notes.length === 0 ? (
            <div className="p-4">
              <EmptyState icon={<NotebookText className="size-6" />} title="No notes yet" hint="Agents with the Read Note and Write Note tools keep working memory here, such as last week's report." />
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[720px] text-left">
                <thead className="border-b border-line">
                  <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                    <th>Note</th><th>Subject</th><th>Schedule</th><th className="text-right">Version</th><th className="text-right">Size</th><th>Last written</th><th />
                  </tr>
                </thead>
                <tbody>
                  {[...notes].sort((a, b) => a.name.localeCompare(b.name) || a.subject.localeCompare(b.subject)).map((n) => (
                    <tr key={noteId(n)} className="border-b border-line last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2">
                      <td className="font-medium">{n.name}</td>
                      <td className="text-[12.5px] text-muted">{n.subject || <span className="text-faint">—</span>}</td>
                      <td className="text-[12.5px]">{n.schedule_id ? n.schedule_name || 'deleted schedule' : <span className="text-faint">Crew-wide</span>}</td>
                      <td className="text-right font-mono tabular">v{n.version}</td>
                      <td className="text-right font-mono text-[12px] tabular">{formatBytes(n.bytes)}</td>
                      <td className="text-[12.5px] text-muted" title={dateTime(n.updated_at)}>{relative(n.updated_at)} by {n.written_by}</td>
                      <td className="text-right"><Button size="sm" variant="ghost" icon={<History className="size-3.5" />} onClick={() => setOpen(n)}>History</Button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </AsyncView>
      {open && <NoteHistory crewId={crewId} note={open} canManage={canManage} onClose={() => setOpen(null)} onChanged={res.reload} />}
    </Panel>
  )
}
