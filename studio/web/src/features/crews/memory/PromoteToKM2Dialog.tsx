import { useState } from 'react'
import { BookUp } from 'lucide-react'
import { ApiError, errorMessage } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import type { MemoryPromoteResult, MemoryRecord } from '../../../api/types.memory'
import { Button } from '../../../components/Button'
import { FormErrors, TextField } from '../../../components/Field'
import { Modal } from '../../../components/Modal'
import { Chip } from '../../../components/StatusBadge'
import { CopyButton } from '../../../components/misc'
import { parseFieldErrors, type FieldErrors } from '../../../lib/fieldErrors'
import { truncate } from '../../../lib/format'
import { notifySuccess } from '../../../lib/notify'
import { plural } from './memoryText'
import { KM2FolderSelect } from '../../connections/KM2FolderSelect'
import { useKM2Folders } from '../../connections/km2Folders'

export const MAX_PROMOTE = 50
/** The server refuses to promote memories waiting for review */
export const PENDING_PROMOTE_HINT = 'Approve pending memories before promoting them to KM2.'
const FIELDS = ['title', 'folder', 'external_ref', 'memory_ids'] as const
const SHOWN = 5
/** The server's per-person cap on promotions in 24 hours */
export const PROMOTE_CAP_MESSAGE =
  'You have reached your daily limit of KM2 promotions (it counts the last 24 hours). Try again tomorrow, or ask someone else in the crew\'s areas to promote these.'

type Props = {
  crewId: string
  memories: readonly MemoryRecord[]
  onClose: () => void
  onPromoted: (result: MemoryPromoteResult) => void
}

/** KM2's outcome in words: created, updated, unchanged and so on. */
export function outcomeText(outcome: string): string {
  return outcome.replaceAll('_', ' ') || 'saved'
}

function promoteError(err: unknown): FieldErrors {
  if (err instanceof ApiError && err.code === 'km2_promote_cap') return { fields: {}, general: [PROMOTE_CAP_MESSAGE] }
  const parsed = parseFieldErrors(err, FIELDS)
  const known = Object.keys(parsed.fields).length + parsed.general.length > 0
  return known ? parsed : { fields: {}, general: [errorMessage(err)] }
}

/** Turns one or more memories into a single KM2 document through the crew's KM2 connection. */
export function PromoteToKM2Dialog({ crewId, memories, onClose, onPromoted }: Props) {
  const [title, setTitle] = useState('')
  const [folder, setFolder] = useState('')
  const folders = useKM2Folders(`promote:${crewId}`, () => memoryApi.promoteFolders(crewId))
  const [externalRef, setExternalRef] = useState('')
  const [errors, setErrors] = useState<FieldErrors>({ fields: {}, general: [] })
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<MemoryPromoteResult | null>(null)
  const tooMany = memories.length > MAX_PROMOTE
  const hasPending = memories.some((m) => m.status === 'pending')

  const promote = async () => {
    if (!title.trim()) {
      setErrors({ fields: { title: 'Give the document a title.' }, general: [] })
      return
    }
    setErrors({ fields: {}, general: [] })
    setBusy(true)
    try {
      const res = await memoryApi.promote(crewId, {
        memory_ids: memories.map((m) => m.id),
        title: title.trim(),
        ...(folder.trim() ? { folder: folder.trim() } : {}),
        ...(externalRef.trim() ? { external_ref: externalRef.trim() } : {}),
      })
      setResult(res)
      notifySuccess(res.outcome === 'already_saved' ? `"${title.trim()}" was already in KM2` : `Saved "${title.trim()}" to KM2`)
      onPromoted(res)
    } catch (err) {
      setErrors(promoteError(err))
    } finally {
      setBusy(false)
    }
  }

  if (result) {
    const already = result.outcome === 'already_saved'
    return (
      <Modal title={already ? 'Already in KM2' : 'Saved to KM2'} onClose={onClose} footer={<Button variant="primary" onClick={onClose}>Done</Button>}>
        <dl className="grid grid-cols-[max-content_minmax(0,1fr)] items-center gap-x-4 gap-y-2 text-[13px]" data-testid="memory-promote-result">
          <dt className="text-muted">Outcome</dt>
          <dd><Chip tone={result.outcome === 'created' || result.outcome === 'updated' ? 'ok' : 'neutral'}>{outcomeText(result.outcome)}</Chip></dd>
          <dt className="text-muted">Document id</dt>
          <dd className="flex items-center gap-2"><span className="truncate font-mono text-[12px]">{result.document_id || '—'}</span>{result.document_id && <CopyButton text={result.document_id} />}</dd>
          <dt className="text-muted">External ref</dt>
          <dd className="truncate font-mono text-[12px]">{result.external_ref || '—'}</dd>
          <dt className="text-muted">Folder id</dt>
          <dd className="truncate font-mono text-[12px]">{result.folder_id || '—'}</dd>
        </dl>
        {already && (
          <p className="mt-3 text-[12.5px]">
            You already saved these memories to KM2 with the same title and details{result.document_id ? <> as document <span className="font-mono">{result.document_id}</span></> : ''}, so
            nothing new was sent.
          </p>
        )}
        <p className="mt-3 text-[12px] text-muted">The save is recorded in the crew&apos;s action log as km2:save. The memories stay in memory, marked as in KM2.</p>
      </Modal>
    )
  }

  return (
    <Modal
      title={`Promote ${plural(memories.length, 'memory', 'memories')} to KM2`}
      onClose={onClose}
      width="max-w-xl"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" icon={<BookUp className="size-4" />} busy={busy} disabled={tooMany || hasPending} onClick={() => void promote()}>Save to KM2</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-[12.5px] text-muted">
          Saves the memories as one KM2 document, so other crews and people can rely on them. It uses this crew&apos;s KM2 connection, which must be in one of
          your functional areas, and its folder rules apply. KM2 extracts facts from every document it saves, which costs money on KM2&apos;s side.
        </p>
        <ul className="flex flex-col gap-1 rounded-md border border-line bg-panel-2 px-3 py-2 text-[12.5px]">
          {memories.slice(0, SHOWN).map((m) => <li key={m.id} className="truncate">{truncate(m.content, 140)}</li>)}
          {memories.length > SHOWN && <li className="text-faint">and {memories.length - SHOWN} more</li>}
        </ul>
        {tooMany && <FormErrors messages={[`Promote at most ${MAX_PROMOTE} memories at a time.`]} />}
        {hasPending && <FormErrors messages={[PENDING_PROMOTE_HINT]} />}
        <TextField label="Title" value={title} onChange={setTitle} error={errors.fields.title} placeholder="Vendor status page lessons" />
        <div className="grid gap-3 sm:grid-cols-2">
          <KM2FolderSelect state={folders} value={folder} onChange={setFolder} error={errors.fields.folder} what="the document" />
          <TextField label="External ref (optional)" value={externalRef} onChange={setExternalRef} error={errors.fields.external_ref} placeholder="atlas-vendor-lessons"
            hint="Saving again with the same ref replaces that document instead of adding a copy." />
        </div>
        <FormErrors messages={[...(errors.fields.memory_ids ? [errors.fields.memory_ids] : []), ...errors.general]} />
      </div>
    </Modal>
  )
}
