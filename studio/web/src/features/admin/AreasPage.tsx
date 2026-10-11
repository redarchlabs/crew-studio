import { useState } from 'react'
import { Check, Pencil, Plus, Shapes, Trash2, X } from 'lucide-react'
import { areasApi } from '../../api/endpoints'
import type { Area, AreaInput } from '../../api/types'
import { Button, IconButton } from '../../components/Button'
import { FormErrors } from '../../components/Field'
import { PageHeader } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { HistoryLink, ConfirmButton } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { NO_ERRORS, parseFieldErrors, type FieldErrors } from '../../lib/fieldErrors'
import { notifyError, notifySuccess } from '../../lib/notify'
import { invalidateAreas } from '../../lib/useAreas'
import { useAsync } from '../../lib/useAsync'
import { AREA_FIELDS, itemsInArea, sortAreas, usageSummary, validateArea } from './areaForm'

const NEW_ROW = '__new__'

type EditRowProps = { initial: AreaInput; errors: Record<string, string>; saving: boolean; onSave: (v: AreaInput) => void; onCancel: () => void }

function AreaEditRow({ initial, errors, saving, onSave, onCancel }: EditRowProps) {
  const [v, setV] = useState<AreaInput>(initial)
  return (
    <tr
      className="border-b border-line bg-accent-soft/40 align-top [&>td]:px-2 [&>td]:py-1.5"
      onKeyDown={(e) => {
        if (e.key === 'Enter') onSave(v)
        if (e.key === 'Escape') onCancel()
      }}
    >
      <td>
        <input aria-label="Area name" aria-invalid={Boolean(errors.name)} className="field-input" placeholder="Finance" value={v.name} onChange={(e) => setV({ ...v, name: e.target.value })} autoFocus />
        {errors.name && <div className="mt-0.5 text-[11.5px] text-danger">{errors.name}</div>}
      </td>
      <td>
        <input aria-label="Area description" className="field-input" placeholder="What belongs here" value={v.description} onChange={(e) => setV({ ...v, description: e.target.value })} />
        {errors.description && <div className="mt-0.5 text-[11.5px] text-danger">{errors.description}</div>}
      </td>
      <td />
      <td className="whitespace-nowrap text-right">
        <IconButton label="Save" disabled={saving} icon={<Check className="size-4 text-ok" />} onClick={() => onSave(v)} />
        <IconButton label="Cancel" icon={<X className="size-4" />} onClick={onCancel} />
      </td>
    </tr>
  )
}

function AreaViewRow({ area, onEdit, onDelete }: { area: Area; onEdit: () => void; onDelete: () => void }) {
  const held = itemsInArea(area.usage)
  return (
    <tr className="border-b border-line align-top last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2" onDoubleClick={onEdit}>
      <td className="font-medium">{area.name}</td>
      <td className="max-w-md text-[12.5px] text-muted">{area.description || <span className="text-faint">—</span>}</td>
      <td className="whitespace-nowrap text-[12px] text-muted">{usageSummary(area.usage)}</td>
      <td className="whitespace-nowrap text-right">
        <IconButton label={`Rename ${area.name}`} icon={<Pencil className="size-3.5" />} onClick={onEdit} />
        <span title={held > 0 ? `Holds ${held} crews, agents or tasks: move them to another area first` : undefined}>
          <ConfirmButton icon={<Trash2 className="size-3.5" />} disabled={held > 0} onConfirm={onDelete}>Delete</ConfirmButton>
        </span>
      </td>
    </tr>
  )
}

export function AreasPage() {
  const { can } = useAuth()
  const res = useAsync(async (signal) => sortAreas(await areasApi.list(signal)), [])
  const [editing, setEditing] = useState<string | null>(null)
  const [errors, setErrors] = useState<FieldErrors>(NO_ERRORS)
  const [saving, setSaving] = useState(false)

  const startEdit = (id: string) => {
    setErrors(NO_ERRORS)
    setEditing(id)
  }

  const save = async (id: string, value: AreaInput) => {
    const others = (res.data ?? []).filter((a) => a.id !== id)
    const local = validateArea(value, others)
    if (Object.keys(local).length > 0) return setErrors({ fields: local, general: [] })
    setSaving(true)
    const body = { name: value.name.trim(), description: value.description.trim() }
    try {
      const saved = id === NEW_ROW ? await areasApi.create(body) : await areasApi.update(id, body)
      invalidateAreas()
      // Keep the usage counts we already have; a fresh area has none
      res.setData((prev) => prev && sortAreas([...prev.filter((a) => a.id !== saved.id), { ...saved, usage: saved.usage ?? prev.find((a) => a.id === saved.id)?.usage ?? { crews: 0, agents: 0, tasks: 0, users: 0 } }]))
      notifySuccess(id === NEW_ROW ? `Added ${saved.name}` : `Saved ${saved.name}`)
      setEditing(null)
      setErrors(NO_ERRORS)
    } catch (err) {
      setErrors(parseFieldErrors(err, AREA_FIELDS))
      notifyError('Could not save the area', err)
    } finally {
      setSaving(false)
    }
  }

  const remove = async (area: Area) => {
    try {
      await areasApi.remove(area.id)
      invalidateAreas()
      res.setData((prev) => prev?.filter((a) => a.id !== area.id))
      notifySuccess(`Deleted ${area.name}`)
    } catch (err) {
      notifyError(`Could not delete ${area.name}`, err)
      res.reload()
    }
  }

  const editRow = (id: string, initial: AreaInput) => (
    <AreaEditRow key={id} initial={initial} errors={errors.fields} saving={saving} onSave={(v) => void save(id, v)} onCancel={() => setEditing(null)} />
  )

  return (
    <>
      <PageHeader
        eyebrow="Admin"
        title="Functional areas"
        description="Areas such as Finance, IT or HR scope crews, agents and tasks. People see an item only when they share one of its areas (or have all areas)."
        actions={
          <>
            {can('admin.audit') && <HistoryLink type="area" />}
            <Button variant="primary" icon={<Plus className="size-4" />} disabled={editing === NEW_ROW} onClick={() => startEdit(NEW_ROW)}>Add area</Button>
          </>
        }
      />
      <AsyncView {...res}>
        {(areas) => (
          <div className="flex flex-col gap-3">
            <FormErrors messages={errors.general} />
            {areas.length === 0 && editing !== NEW_ROW ? (
              <EmptyState
                icon={<Shapes className="size-6" />}
                title="No functional areas yet"
                hint="Every crew, agent and task needs at least one area. Add your first, e.g. Finance."
                action={<Button variant="primary" onClick={() => startEdit(NEW_ROW)}>Add the first area</Button>}
              />
            ) : (
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[720px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th className="w-56">Name</th><th>Description</th><th>In use</th><th className="w-36" />
                    </tr>
                  </thead>
                  <tbody>
                    {editing === NEW_ROW && editRow(NEW_ROW, { name: '', description: '' })}
                    {areas.map((a) =>
                      editing === a.id ? editRow(a.id, { name: a.name, description: a.description }) : <AreaViewRow key={a.id} area={a} onEdit={() => startEdit(a.id)} onDelete={() => void remove(a)} />,
                    )}
                  </tbody>
                </table>
              </div>
            )}
            <p className="text-[12px] text-faint">
              An area that still holds crews, agents or tasks cannot be deleted: move them to another area first. Accounts in a deleted area simply lose it.
              Double-click a row to edit; Enter saves, Escape cancels.
            </p>
          </div>
        )}
      </AsyncView>
    </>
  )
}
