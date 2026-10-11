import { useMemo, useState, type ReactNode } from 'react'
import { Check, Search } from 'lucide-react'

export type MultiOption = { value: string; label: string; hint?: ReactNode; disabled?: boolean; disabledReason?: string }

type MultiSelectProps = {
  label: ReactNode
  options: readonly MultiOption[]
  value: readonly string[]
  onChange: (value: string[]) => void
  error?: string
  emptyText?: string
  maxHeight?: string
}

/** Searchable checkbox list. Keeps the selection order stable (insertion order). */
export function MultiSelect({ label, options, value, onChange, error, emptyText = 'Nothing to choose from yet.', maxHeight = 'max-h-56' }: MultiSelectProps) {
  const [query, setQuery] = useState('')
  const selected = useMemo(() => new Set(value), [value])
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? options.filter((o) => `${o.label} ${o.value}`.toLowerCase().includes(q)) : options
  }, [options, query])

  const toggle = (v: string) => onChange(selected.has(v) ? value.filter((x) => x !== v) : [...value, v])

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-baseline justify-between">
        <span className="text-[12px] font-medium text-muted">{label}</span>
        <span className="font-mono text-[11px] text-faint">
          {value.length}/{options.length}
        </span>
      </div>
      <div className={`rounded-md border bg-raised ${error ? 'border-danger' : 'border-line'}`}>
        {options.length > 6 && (
          <div className="flex items-center gap-2 border-b border-line px-2 py-1">
            <Search className="size-3.5 text-faint" />
            <input className="w-full bg-transparent py-0.5 text-[12.5px] outline-none" placeholder="Filter…" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
        )}
        <ul className={`${maxHeight} overflow-y-auto py-1`} role="listbox" aria-multiselectable="true">
          {options.length === 0 && <li className="px-3 py-2 text-[12px] text-faint">{emptyText}</li>}
          {visible.map((o) => {
            const on = selected.has(o.value)
            const disabled = o.disabled && !on
            return (
              <li key={o.value}>
                <button
                  type="button"
                  role="option"
                  aria-selected={on}
                  disabled={disabled}
                  title={disabled ? o.disabledReason : undefined}
                  onClick={() => toggle(o.value)}
                  className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left hover:bg-panel-2 disabled:cursor-not-allowed disabled:opacity-45"
                >
                  <span className={`mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-[3px] border ${on ? 'border-accent bg-accent text-accent-fg' : 'border-line-strong'}`}>
                    {on && <Check className="size-3" strokeWidth={3} />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[12.5px]">{o.label}</span>
                    {o.hint && <span className="block text-[11px] text-faint">{o.hint}</span>}
                    {disabled && o.disabledReason && <span className="block text-[11px] text-warn">{o.disabledReason}</span>}
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      </div>
      {error && <div className="text-[12px] text-danger">{error}</div>}
    </div>
  )
}
