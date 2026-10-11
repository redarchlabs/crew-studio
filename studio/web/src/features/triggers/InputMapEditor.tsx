import { useId, useState } from 'react'
import { ArrowRight, Plus, X } from 'lucide-react'
import { Button, IconButton } from '../../components/Button'
import type { InputSpec } from '../workorders/target/targetInputs'
import { mapFromRows, rowsFromMap, type MapRow } from './triggerForm'

type InputMapEditorProps = {
  value: Record<string, string>
  onChange: (value: Record<string, string>) => void
  specs: readonly InputSpec[]
  error?: string
}

/** Rows of input key -> dotted path in the webhook payload. Keys are suggested from the crew's inputs. */
export function InputMapEditor({ value, onChange, specs, error }: InputMapEditorProps) {
  const listId = useId()
  // Local rows keep blank and in-progress rows that the map itself cannot hold
  const [rows, setRows] = useState<MapRow[]>(() => rowsFromMap(value))
  const update = (next: MapRow[]) => {
    setRows(next)
    onChange(mapFromRows(next))
  }
  const used = new Set(rows.map((r) => r.key))
  const unmapped = specs.filter((s) => !used.has(s.key))

  return (
    <div className="flex flex-col gap-2">
      <datalist id={listId}>
        {specs.map((s) => <option key={s.key} value={s.key}>{s.label}</option>)}
      </datalist>
      {rows.length === 0 && <p className="text-[12px] text-faint">No mapping: inputs use their defaults, and the payload is only added to the brief.</p>}
      {rows.map((r, i) => (
        <div key={i} className="flex items-center gap-2 max-sm:flex-wrap">
          <div className="w-full sm:w-44 sm:shrink-0"><input className="field-input font-mono" list={listId} aria-label="Input key" placeholder="input_key" value={r.key} onChange={(e) => update(rows.map((x, j) => (j === i ? { ...x, key: e.target.value } : x)))} /></div>
          <ArrowRight className="hidden size-3.5 shrink-0 text-faint sm:block" />
          <div className="min-w-0 flex-1"><input className="field-input font-mono" aria-label="Payload path" placeholder="customer.name" value={r.path} onChange={(e) => update(rows.map((x, j) => (j === i ? { ...x, path: e.target.value } : x)))} /></div>
          <IconButton label="Remove mapping" tone="danger" icon={<X className="size-3.5" />} onClick={() => update(rows.filter((_, j) => j !== i))} />
        </div>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => update([...rows, { key: unmapped[0]?.key ?? '', path: '' }])}>Add mapping</Button>
        {unmapped.length > 0 && (
          <Button size="sm" variant="ghost" onClick={() => update([...rows, ...unmapped.map((s) => ({ key: s.key, path: s.key }))])}>
            Map all {unmapped.length} crew inputs
          </Button>
        )}
      </div>
      {error && <div className="text-[12px] text-danger" role="alert">{error}</div>}
      {!error && unmapped.some((s) => s.required) && rows.length > 0 && (
        <div className="text-[11.5px] text-warn">Unmapped required inputs: {unmapped.filter((s) => s.required).map((s) => s.key).join(', ')}. The webhook answers 422 if they end up empty.</div>
      )}
    </div>
  )
}
