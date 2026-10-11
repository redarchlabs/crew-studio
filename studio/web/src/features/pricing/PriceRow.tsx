import { useState } from 'react'
import { Check, Pencil, Trash2, X } from 'lucide-react'
import type { ModelPrice, ModelPriceInput } from '../../api/types'
import { IconButton } from '../../components/Button'
import { ConfirmButton } from '../../components/misc'
import { usd } from '../../lib/format'

export const PRICE_FIELDS = ['model', 'input_per_mtok', 'cached_input_per_mtok', 'cache_write_per_mtok', 'output_per_mtok', 'notes'] as const

export const EMPTY_PRICE: ModelPriceInput = { model: '', input_per_mtok: 0, cached_input_per_mtok: 0, cache_write_per_mtok: null, output_per_mtok: 0, notes: '' }

export function toPriceInput(p: ModelPrice): ModelPriceInput {
  const { model, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok, output_per_mtok, notes } = p
  return { model, input_per_mtok, cached_input_per_mtok, cache_write_per_mtok: cache_write_per_mtok ?? null, output_per_mtok, notes }
}

export function validatePrice(v: ModelPriceInput): Record<string, string> {
  const errs: Record<string, string> = {}
  if (!v.model.trim()) errs.model = 'Required'
  for (const k of ['input_per_mtok', 'cached_input_per_mtok', 'output_per_mtok'] as const) {
    if (Number.isNaN(v[k]) || v[k] < 0) errs[k] = 'Must be ≥ 0'
  }
  // Blank (null) means the input price
  if (v.cache_write_per_mtok !== null && (Number.isNaN(v.cache_write_per_mtok) || v.cache_write_per_mtok < 0)) errs.cache_write_per_mtok = 'Must be ≥ 0, or blank'
  return errs
}

type EditRowProps = {
  initial: ModelPriceInput
  errors: Record<string, string>
  saving: boolean
  onSave: (value: ModelPriceInput) => void
  onCancel: () => void
}

/** A price cell that may be left blank: blank is null (the input price). */
function OptionalNumCell({ value, onChange, error, label, placeholder }: { value: number | null; onChange: (v: number | null) => void; error?: string; label: string; placeholder: string }) {
  return (
    <td>
      <input
        type="number"
        min={0}
        step="any"
        aria-label={label}
        aria-invalid={Boolean(error)}
        title={error ?? 'Blank: the input price'}
        placeholder={placeholder}
        className="field-input w-24 text-right font-mono tabular"
        value={value === null || Number.isNaN(value) ? '' : value}
        onChange={(e) => onChange(e.target.value === '' ? null : Number(e.target.value))}
      />
    </td>
  )
}

function NumCell({ value, onChange, error, label }: { value: number; onChange: (v: number) => void; error?: string; label: string }) {
  return (
    <td>
      <input
        type="number"
        min={0}
        step="any"
        aria-label={label}
        aria-invalid={Boolean(error)}
        title={error}
        className="field-input w-24 text-right font-mono tabular"
        value={Number.isNaN(value) ? '' : value}
        onChange={(e) => onChange(e.target.value === '' ? Number.NaN : Number(e.target.value))}
      />
    </td>
  )
}

export function PriceEditRow({ initial, errors, saving, onSave, onCancel }: EditRowProps) {
  const [v, setV] = useState<ModelPriceInput>(initial)
  const set = <K extends keyof ModelPriceInput>(k: K, val: ModelPriceInput[K]) => setV((prev) => ({ ...prev, [k]: val }))
  return (
    <tr
      className="border-b border-line bg-accent-soft/40 [&>td]:px-2 [&>td]:py-1.5"
      onKeyDown={(e) => {
        // Enter in a field saves; Enter on the Save/Cancel buttons is left to their own click
        if (e.key === 'Enter' && e.target instanceof HTMLInputElement && !saving) onSave(v)
        if (e.key === 'Escape') onCancel()
      }}
    >
      <td>
        <input aria-label="Model" aria-invalid={Boolean(errors.model)} title={errors.model} className="field-input font-mono" placeholder="gpt-6-luna" value={v.model} onChange={(e) => set('model', e.target.value)} autoFocus />
      </td>
      <NumCell label="Input per 1M tokens" value={v.input_per_mtok} onChange={(x) => set('input_per_mtok', x)} error={errors.input_per_mtok} />
      <NumCell label="Cached input per 1M tokens" value={v.cached_input_per_mtok} onChange={(x) => set('cached_input_per_mtok', x)} error={errors.cached_input_per_mtok} />
      <OptionalNumCell label="Cache write per 1M tokens" value={v.cache_write_per_mtok} onChange={(x) => set('cache_write_per_mtok', x)}
        error={errors.cache_write_per_mtok} placeholder={Number.isNaN(v.input_per_mtok) ? '' : String(v.input_per_mtok)} />
      <NumCell label="Output per 1M tokens" value={v.output_per_mtok} onChange={(x) => set('output_per_mtok', x)} error={errors.output_per_mtok} />
      <td>
        <input aria-label="Notes" className="field-input" placeholder="Pricing page URL" value={v.notes} onChange={(e) => set('notes', e.target.value)} />
      </td>
      <td />
      <td className="whitespace-nowrap text-right">
        <IconButton label="Save" disabled={saving} icon={<Check className="size-4 text-ok" />} onClick={() => onSave(v)} />
        <IconButton label="Cancel" icon={<X className="size-4" />} onClick={onCancel} />
      </td>
    </tr>
  )
}

/** onEdit/onDelete are omitted for people without pricing.manage; usedBy is null when LLMs are not visible to them. */
type ViewRowProps = { price: ModelPrice; usedBy: string[] | null; onEdit?: () => void; onDelete?: () => void }

function isHttpUrl(text: string): boolean {
  return /^https?:\/\/\S+$/.test(text.trim())
}

export function PriceViewRow({ price, usedBy, onEdit, onDelete }: ViewRowProps) {
  return (
    <tr className="border-b border-line last:border-0 hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-2" onDoubleClick={onEdit}>
      <td className="font-mono text-[12.5px]">{price.model}</td>
      <td className="text-right font-mono tabular">{usd(price.input_per_mtok)}</td>
      <td className="text-right font-mono tabular text-muted">{usd(price.cached_input_per_mtok)}</td>
      <td className="text-right font-mono tabular text-muted" title={price.cache_write_per_mtok === null ? 'Same as input' : undefined}>
        {price.cache_write_per_mtok === null ? <span className="text-faint">input</span> : usd(price.cache_write_per_mtok)}
      </td>
      <td className="text-right font-mono tabular">{usd(price.output_per_mtok)}</td>
      <td className="max-w-64 truncate text-muted" title={price.notes}>
        {isHttpUrl(price.notes) ? (
          <a className="text-blue hover:underline" href={price.notes.trim()} target="_blank" rel="noopener noreferrer">{price.notes}</a>
        ) : (
          price.notes || <span className="text-faint">—</span>
        )}
      </td>
      {usedBy && (
        <td className="text-[12px] text-muted" title={usedBy.join(', ')}>
          {usedBy.length === 0 ? <span className="text-faint">unused</span> : `${usedBy.length} LLM${usedBy.length === 1 ? '' : 's'}`}
        </td>
      )}
      {(onEdit || onDelete) && (
        <td className="whitespace-nowrap text-right">
          {onEdit && <IconButton label={`Edit ${price.model}`} icon={<Pencil className="size-3.5" />} onClick={onEdit} />}
          {onDelete && <ConfirmButton icon={<Trash2 className="size-3.5" />} onConfirm={onDelete}>Delete</ConfirmButton>}
        </td>
      )}
    </tr>
  )
}
