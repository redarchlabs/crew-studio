import { useState } from 'react'
import { ApiError, errorMessage } from '../../../api/client'
import { memoryApi } from '../../../api/endpoints.memory'
import type { MemoryInput, MemoryRecord } from '../../../api/types.memory'
import { Button } from '../../../components/Button'
import { FormErrors, NumberField, TextAreaField, TextField, Toggle } from '../../../components/Field'
import { Modal } from '../../../components/Modal'
import { parseFieldErrors, type FieldErrors } from '../../../lib/fieldErrors'
import { notifySuccess } from '../../../lib/notify'
import { categoriesProblem, MAX_CONTENT, normalizeScope, scopeProblem, splitCategories } from './memoryText'

type Props = {
  crewId: string
  /** null adds a new memory */
  memory: MemoryRecord | null
  /** Where a new memory starts, such as the scope being browsed */
  defaultScope?: string
  onClose: () => void
  onSaved: (m: MemoryRecord) => void
}

const FIELDS = ['content', 'scope', 'categories', 'importance', 'pinned'] as const
export const DUPLICATE_MESSAGE = 'That memory already exists in this scope.'

type Draft = { content: string; scope: string; categories: string; importance: number; pinned: boolean }

function check(d: Draft): FieldErrors {
  const fields: Record<string, string> = {}
  const content = d.content.trim()
  if (!content) fields.content = 'Write what the crew should remember.'
  else if (content.length > MAX_CONTENT) fields.content = `Use at most ${MAX_CONTENT} characters.`
  const scope = scopeProblem(normalizeScope(d.scope))
  if (scope) fields.scope = scope
  const categories = categoriesProblem(splitCategories(d.categories))
  if (categories) fields.categories = categories
  if (Number.isNaN(d.importance) || d.importance < 0 || d.importance > 1) fields.importance = 'Use a number from 0 to 1.'
  return { fields, general: [] }
}

/** Adds a memory by hand, or edits one. Memories people write are active at once. */
export function MemoryEditor({ crewId, memory, defaultScope = '/', onClose, onSaved }: Props) {
  const [d, setD] = useState<Draft>(() =>
    memory
      ? { content: memory.content, scope: memory.scope, categories: memory.categories.join(', '), importance: memory.importance, pinned: memory.pinned }
      : { content: '', scope: defaultScope, categories: '', importance: 0.5, pinned: false },
  )
  const [errors, setErrors] = useState<FieldErrors>({ fields: {}, general: [] })
  const [saving, setSaving] = useState(false)
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setD((prev) => ({ ...prev, [key]: value }))
  const f = errors.fields

  const save = async () => {
    const problems = check(d)
    setErrors(problems)
    if (Object.keys(problems.fields).length > 0) return
    const body: MemoryInput = {
      content: d.content.trim(), scope: normalizeScope(d.scope), categories: splitCategories(d.categories), importance: d.importance, pinned: d.pinned,
    }
    setSaving(true)
    try {
      const saved = memory ? await memoryApi.update(crewId, memory.id, body) : await memoryApi.create(crewId, body)
      notifySuccess(memory ? 'Saved the memory' : 'Added the memory')
      onSaved(saved)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'memory_exists') {
        setErrors({ fields: { content: DUPLICATE_MESSAGE }, general: [] })
        return
      }
      const parsed = parseFieldErrors(err, FIELDS)
      const known = Object.keys(parsed.fields).length + parsed.general.length > 0
      setErrors(known ? parsed : { fields: {}, general: [errorMessage(err)] })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      title={memory ? 'Edit memory' : 'Add a memory'}
      onClose={onClose}
      width="max-w-xl"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button variant="primary" busy={saving} onClick={() => void save()}>{memory ? 'Save memory' : 'Add memory'}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <TextAreaField label="Memory" rows={4} value={d.content} onChange={(x) => set('content', x)} error={f.content}
          placeholder="Sam usually replies late on Fridays."
          hint={`${d.content.trim().length}/${MAX_CONTENT} characters. A short lesson, not a document: exact records belong in the action log or notes.`} />
        <div className="grid gap-3 sm:grid-cols-2">
          <TextField label="Scope" value={d.scope} onChange={(x) => set('scope', x)} error={f.scope} placeholder="/people/sam"
            hint="Groups memories, such as /people/sam or /projects/atlas. / is the whole crew." />
          <NumberField label="Importance" value={d.importance} onChange={(x) => set('importance', x ?? Number.NaN)} min={0} max={1} step={0.1} error={f.importance}
            hint="0 to 1. More important memories rank higher in recall." />
        </div>
        <TextField label="Categories (optional)" value={d.categories} onChange={(x) => set('categories', x)} error={f.categories} placeholder="people, scheduling"
          hint="Separate them with commas." />
        <Toggle label="Pin this memory" checked={d.pinned} onChange={(x) => set('pinned', x)} hint="Pinned memories are never deleted by retention." />
        {memory?.status === 'pending' && <p className="text-[12px] text-muted">Saving does not approve it; use Approve in the list.</p>}
        <FormErrors messages={errors.general} />
      </div>
    </Modal>
  )
}
