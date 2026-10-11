import { useState } from 'react'
import { History, Pencil, RotateCcw, Trash2 } from 'lucide-react'
import { crewStateApi } from '../../../api/endpoints.crewstate'
import type { NoteInfo, NoteKey, NoteVersion } from '../../../api/types.crewstate'
import { Button } from '../../../components/Button'
import { FormErrors, TextAreaField } from '../../../components/Field'
import { Modal } from '../../../components/Modal'
import { AsyncView } from '../../../components/States'
import { ConfirmButton } from '../../../components/misc'
import { errorMessage } from '../../../api/client'
import { dateTime } from '../../../lib/format'
import { notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'

type Props = {
  crewId: string
  note: NoteInfo
  canManage: boolean
  onClose: () => void
  /** Called after an edit, rollback or delete so the list can refresh */
  onChanged: () => void
}

export function noteKeyOf(note: Pick<NoteInfo, 'schedule_id' | 'subject' | 'name'>): NoteKey {
  return { schedule_id: note.schedule_id, subject: note.subject, name: note.name }
}

function VersionList({ versions, selected, onSelect }: { versions: NoteVersion[]; selected: number; onSelect: (v: number) => void }) {
  return (
    <ol className="flex max-h-[50vh] flex-col gap-0.5 overflow-y-auto" aria-label="Versions">
      {versions.map((v) => (
        <li key={v.id}>
          <button
            type="button"
            onClick={() => onSelect(v.version)}
            aria-current={v.version === selected}
            className={`w-full rounded px-2 py-1 text-left text-[12px] ${v.version === selected ? 'bg-accent-soft text-fg' : 'text-muted hover:bg-panel-2'}`}
          >
            <span className="font-mono">v{v.version}</span> · {v.written_by}
            <div className="text-[11px] text-faint">{dateTime(v.created_at)}</div>
          </button>
        </li>
      ))}
    </ol>
  )
}

/** A note's versions, newest first, with edit, roll back and delete for people holding state.manage. */
export function NoteHistory({ crewId, note, canManage, onClose, onChanged }: Props) {
  const key = noteKeyOf(note)
  const res = useAsync((signal) => crewStateApi.versions(crewId, key, signal), [crewId, note.schedule_id, note.subject, note.name])
  const [selected, setSelected] = useState<number | null>(null)
  const [draft, setDraft] = useState<string | null>(null)
  const [problem, setProblem] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const act = async (action: () => Promise<unknown>, done: string, close = false) => {
    setBusy(true)
    setProblem(null)
    try {
      await action()
      notifySuccess(done)
      onChanged()
      if (close) onClose()
      else {
        setDraft(null)
        setSelected(null)
        res.reload()
      }
    } catch (err) {
      setProblem(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal title={<span className="inline-flex items-center gap-2"><History className="size-4" /> {note.name}{note.subject && <span className="font-normal text-muted"> · {note.subject}</span>}</span>} onClose={onClose} width="max-w-4xl">
      <AsyncView {...res}>
        {(versions) => {
          const latest = versions[0]
          if (!latest) return <p className="text-muted">This note has no versions left.</p>
          const shown = versions.find((v) => v.version === (selected ?? latest.version)) ?? latest
          const isLatest = shown.version === latest.version
          return (
            <div className="grid gap-4 md:grid-cols-[200px_minmax(0,1fr)]">
              <VersionList versions={versions} selected={shown.version} onSelect={(v) => { setSelected(v); setDraft(null) }} />
              <div className="flex min-w-0 flex-col gap-2">
                {draft === null ? (
                  <pre className="max-h-[50vh] overflow-auto whitespace-pre-wrap rounded-md border border-line bg-panel-2 p-3 font-mono text-[12px]">{shown.content || '(empty)'}</pre>
                ) : (
                  <TextAreaField label={`Edit (saves as v${latest.version + 1})`} rows={14} value={draft} onChange={setDraft} />
                )}
                <FormErrors messages={problem ? [problem] : []} />
                {canManage && (
                  <div className="flex flex-wrap justify-end gap-2">
                    <ConfirmButton icon={<Trash2 className="size-3.5" />} busy={busy} onConfirm={() => void act(() => crewStateApi.deleteNote(crewId, key), `Deleted ${note.name}`, true)}>Delete note</ConfirmButton>
                    {draft !== null && <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>Cancel</Button>}
                    {draft !== null && <Button size="sm" variant="primary" busy={busy} onClick={() => void act(() => crewStateApi.putNote(crewId, key, draft, latest.version), `Saved ${note.name}`)}>Save</Button>}
                    {draft === null && isLatest && <Button size="sm" icon={<Pencil className="size-3.5" />} onClick={() => setDraft(latest.content)}>Edit</Button>}
                    {draft === null && !isLatest && (
                      <Button size="sm" icon={<RotateCcw className="size-3.5" />} busy={busy} onClick={() => void act(() => crewStateApi.rollback(crewId, key, shown.version, latest.version), `Restored v${shown.version} of ${note.name}`)}>
                        Restore v{shown.version}
                      </Button>
                    )}
                  </div>
                )}
              </div>
            </div>
          )
        }}
      </AsyncView>
    </Modal>
  )
}
