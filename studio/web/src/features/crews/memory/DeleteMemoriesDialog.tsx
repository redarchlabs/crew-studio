import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import { errorMessage } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import type { MemoryDeleteRequest } from '../../../api/types.memory'
import { Button } from '../../../components/Button'
import { FormErrors, TextField } from '../../../components/Field'
import { Modal } from '../../../components/Modal'
import { notifySuccess } from '../../../lib/notify'
import { normalizeScope, plural, scopeProblem } from './memoryText'

type Props = {
  crewId: string
  /** Delete exactly these memories (the selection) instead of matching by scope and text */
  ids?: readonly string[]
  /** Starting scope, such as the one being browsed */
  initialScope?: string
  onClose: () => void
  onDeleted: (deleted: number) => void
}

/**
 * Deletes many memories at once in two steps: count what matches (a dry run), then delete. Covers resetting a
 * scope, deleting everything that mentions a person, and deleting a selection.
 */
export function DeleteMemoriesDialog({ crewId, ids = [], initialScope = '', onClose, onDeleted }: Props) {
  const bySelection = ids.length > 0
  const [scope, setScope] = useState(initialScope)
  const [contains, setContains] = useState('')
  const [matched, setMatched] = useState<number | null>(null)
  const [problems, setProblems] = useState<string[]>([])
  const [busy, setBusy] = useState<'preview' | 'delete' | null>(null)

  const edit = (setter: (x: string) => void) => (x: string) => {
    setter(x)
    setMatched(null) // a count for other filters must not be confirmed
  }

  const request = (): MemoryDeleteRequest | null => {
    if (bySelection) return { ids: [...ids], dry_run: true }
    const text = contains.trim()
    const prefix = scope.trim() ? normalizeScope(scope) : ''
    if (!prefix && !text) {
      setProblems(['Enter a scope, some text, or both. To delete every memory, use the scope /.'])
      return null
    }
    const problem = prefix ? scopeProblem(prefix) : ''
    if (problem) {
      setProblems([`Scope: ${problem}`])
      return null
    }
    return { ...(prefix ? { scope_prefix: prefix } : {}), ...(text ? { contains: text } : {}), dry_run: true }
  }

  const preview = async () => {
    const body = request()
    if (!body) return
    setProblems([])
    setBusy('preview')
    try {
      setMatched((await memoryApi.bulkDelete(crewId, body)).matched)
    } catch (err) {
      setProblems([errorMessage(err)])
    } finally {
      setBusy(null)
    }
  }

  const remove = async () => {
    const body = request()
    if (!body) return
    setBusy('delete')
    try {
      const { deleted } = await memoryApi.bulkDelete(crewId, { ...body, dry_run: false })
      notifySuccess(`Deleted ${plural(deleted, 'memory', 'memories')}`)
      onDeleted(deleted)
    } catch (err) {
      setProblems([errorMessage(err)])
      setBusy(null)
    }
  }

  return (
    <Modal
      title={bySelection ? `Delete ${plural(ids.length, 'selected memory', 'selected memories')}` : 'Delete memories'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          {matched === null || matched === 0 ? (
            <Button variant="primary" busy={busy === 'preview'} onClick={() => void preview()}>Count matches</Button>
          ) : (
            <Button variant="danger" icon={<Trash2 className="size-4" />} busy={busy === 'delete'} onClick={() => void remove()}>
              Delete {plural(matched, 'memory', 'memories')}
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {bySelection ? (
          <p className="text-[12.5px] text-muted">The selected memories are deleted for good. Runs will no longer recall them.</p>
        ) : (
          <>
            <p className="text-[12.5px] text-muted">
              Delete every memory under a scope, every memory that mentions some text, or only those that match both. To remove what the crew
              remembers about a person, enter their scope (such as /people/sam), their name or email, or both.
            </p>
            <TextField label="Scope" value={scope} onChange={edit(setScope)} placeholder="/people/sam" hint="This scope and everything below it. / is every memory." />
            <TextField label="Contains" value={contains} onChange={edit(setContains)} placeholder="Sam Lee" hint="Matches anywhere in the memory, in any case." />
          </>
        )}
        <FormErrors messages={problems} />
        {matched !== null && (
          <p className="rounded-md border border-line bg-panel-2 px-3 py-2 text-[12.5px]" role="status" data-testid="memory-delete-count">
            {matched === 0 ? 'No memories match.' : `${plural(matched, 'memory matches', 'memories match')}. Deleting cannot be undone, and pinned memories are deleted too.`}
          </p>
        )}
      </div>
    </Modal>
  )
}
