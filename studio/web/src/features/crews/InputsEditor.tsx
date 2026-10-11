import { Plus, Trash2, Wand2 } from 'lucide-react'
import type { CrewInput } from '../../api/types'
import { Button, IconButton } from '../../components/Button'
import { INPUT_KEY_RE } from './crewForm'

type InputsEditorProps = {
  inputs: readonly CrewInput[]
  onChange: (inputs: CrewInput[]) => void
  onDetect: () => void
  usedKeys: ReadonlySet<string>
  error?: string
}

/** Editable rows of crew inputs (key / label / default / required). */
export function InputsEditor({ inputs, onChange, onDetect, usedKeys, error }: InputsEditorProps) {
  const patch = (idx: number, change: Partial<CrewInput>) => onChange(inputs.map((i, n) => (n === idx ? { ...i, ...change } : i)))
  const keyCounts = inputs.reduce<Record<string, number>>((acc, i) => ({ ...acc, [i.key]: (acc[i.key] ?? 0) + 1 }), {})

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-[12px] font-medium text-muted">Inputs</span>
        <div className="flex gap-1.5">
          <Button size="sm" variant="ghost" icon={<Wand2 className="size-3.5" />} onClick={onDetect} title="Scan this crew's tasks and agents for {placeholders}">
            Detect from placeholders
          </Button>
          <Button size="sm" icon={<Plus className="size-3.5" />} onClick={() => onChange([...inputs, { key: '', label: '', default: '', required: true }])}>
            Add
          </Button>
        </div>
      </div>
      {inputs.length === 0 ? (
        <div className="rounded-md border border-dashed border-line px-3 py-3 text-[12px] text-faint">No inputs. Runs of this crew take no parameters.</div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[520px] text-left">
            <thead>
              <tr className="eyebrow [&>th]:px-1 [&>th]:pb-1 [&>th]:font-semibold">
                <th>Key</th><th>Label</th><th>Default</th><th className="text-center">Req.</th><th />
              </tr>
            </thead>
            <tbody>
              {inputs.map((input, idx) => {
                const badKey = !INPUT_KEY_RE.test(input.key) || (keyCounts[input.key] ?? 0) > 1
                const unused = input.key !== '' && !usedKeys.has(input.key)
                return (
                  <tr key={idx} className="[&>td]:px-1 [&>td]:py-0.5">
                    <td className="w-[28%]">
                      <input aria-label={`Input ${idx + 1} key`} aria-invalid={badKey} className="field-input font-mono text-[12px]" placeholder="company" value={input.key} onChange={(e) => patch(idx, { key: e.target.value })} title={unused ? 'Not referenced by any task or agent in this crew' : undefined} />
                    </td>
                    <td><input aria-label={`Input ${idx + 1} label`} className="field-input" placeholder="Company" value={input.label} onChange={(e) => patch(idx, { label: e.target.value })} /></td>
                    <td><input aria-label={`Input ${idx + 1} default`} className="field-input" placeholder="(none)" value={input.default} onChange={(e) => patch(idx, { default: e.target.value })} /></td>
                    <td className="text-center">
                      <input type="checkbox" aria-label={`Input ${idx + 1} required`} className="size-3.5 accent-[var(--accent)]" checked={input.required} onChange={(e) => patch(idx, { required: e.target.checked })} />
                    </td>
                    <td className="w-8">
                      <IconButton label="Remove input" tone="danger" icon={<Trash2 className="size-3.5" />} onClick={() => onChange(inputs.filter((_, n) => n !== idx))} />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      {inputs.some((i) => i.key !== '' && !usedKeys.has(i.key)) && (
        <div className="text-[11.5px] text-faint">Unused keys: {inputs.filter((i) => i.key !== '' && !usedKeys.has(i.key)).map((i) => i.key).join(', ')}</div>
      )}
      {error && <div className="text-[12px] text-danger">{error}</div>}
    </div>
  )
}
