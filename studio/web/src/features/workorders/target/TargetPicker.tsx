import { useEffect, useMemo, useRef, useState } from 'react'
import { Check, ChevronDown, Search } from 'lucide-react'
import type { Crew, Task } from '../../../api/types'
import { AreaChips } from '../../../components/AreaChips'
import type { TargetRef } from './targetInputs'
import type { TargetCatalogData } from './useTargetCatalog'

type TargetPickerProps = {
  value: TargetRef
  onChange: (value: TargetRef) => void
  catalog: TargetCatalogData
  error?: string
  disabled?: boolean
}

type Option = { id: string; name: string; hint: string; areaIds: string[] }

/**
 * The crew's tasks in run order, e.g. "Research → Write". Tasks outside the person's areas are counted;
 * when none can be named, falls back to the description or a task count.
 */
export function crewTaskLine(crew: Crew, tasksById: ReadonlyMap<string, Task>): string {
  const ids = crew.task_ids ?? []
  const names = ids.flatMap((id) => tasksById.get(id)?.name ?? [])
  if (names.length === 0) return crew.description || (ids.length === 0 ? 'No tasks yet' : `${ids.length} tasks · ${crew.process}`)
  const hidden = ids.length - names.length
  return hidden > 0 ? `${names.join(' → ')} · +${hidden} more` : names.join(' → ')
}

function crewOptions(catalog: TargetCatalogData): Option[] {
  const tasksById = new Map(catalog.tasks.map((t) => [t.id, t]))
  return catalog.crews.map((c) => ({ id: c.id, name: c.name, hint: crewTaskLine(c, tasksById), areaIds: c.area_ids }))
}

/** A searchable list of the crews you can see; each option names the crew's tasks in order. */
export function TargetPicker({ value, onChange, catalog, error, disabled }: TargetPickerProps) {
  const options = useMemo(() => crewOptions(catalog), [catalog])
  const selected = options.find((o) => o.id === value.crew_id)

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-[12px] font-medium text-muted">Crew</span>
      <SearchSelect options={options} selected={selected} onPick={(id) => onChange({ crew_id: id })} disabled={disabled} invalid={Boolean(error)} placeholder={catalog.forbidden.crews ? 'You cannot view crews' : 'Choose a crew…'} />
      {error ? (
        <div className="text-[12px] text-danger" role="alert">{error}</div>
      ) : (
        <div className="text-[11.5px] text-faint">
          Runs the whole crew with its inputs and budget. To run a single task, make a crew with just that task (Make a crew on the task's page).
        </div>
      )}
    </div>
  )
}

type SearchSelectProps = {
  options: Option[]
  selected: Option | undefined
  onPick: (id: string) => void
  disabled?: boolean
  invalid?: boolean
  placeholder: string
}

function SearchSelect({ options, selected, onPick, disabled, invalid, placeholder }: SearchSelectProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? options.filter((o) => `${o.name} ${o.hint}`.toLowerCase().includes(q)) : options
  }, [options, query])

  // Close on outside click or Escape
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const choose = (id: string) => {
    onPick(id)
    setOpen(false)
    setQuery('')
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-invalid={invalid}
        onClick={() => setOpen((o) => !o)}
        className="field-input flex min-h-9 items-center gap-2 text-left"
      >
        <span className="min-w-0 flex-1">
          {selected ? (
            <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
              <span className="truncate font-medium">{selected.name}</span>
              <AreaChips areaIds={selected.areaIds} max={2} />
            </span>
          ) : (
            <span className="text-faint">{placeholder}</span>
          )}
        </span>
        <ChevronDown className="size-4 shrink-0 text-faint" />
      </button>
      {open && (
        <div className="absolute inset-x-0 top-full z-30 mt-1 rounded-md border border-line-strong bg-panel shadow-[var(--shadow)]">
          <div className="flex items-center gap-2 border-b border-line px-2.5 py-1.5">
            <Search className="size-3.5 text-faint" />
            <input autoFocus className="w-full bg-transparent text-[12.5px] outline-none" placeholder="Search…" value={query} onChange={(e) => setQuery(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && visible[0] && (e.preventDefault(), choose(visible[0].id))} />
          </div>
          <ul role="listbox" className="max-h-72 overflow-y-auto py-1">
            {visible.length === 0 && <li className="px-3 py-2 text-[12px] text-faint">{options.length === 0 ? 'Nothing you can see yet.' : 'No matches.'}</li>}
            {visible.map((o) => (
              <li key={o.id}>
                <button type="button" role="option" aria-selected={o.id === selected?.id} onClick={() => choose(o.id)} className="flex w-full items-start gap-2 px-2.5 py-1.5 text-left hover:bg-panel-2">
                  <Check className={`mt-0.5 size-3.5 shrink-0 ${o.id === selected?.id ? 'text-accent' : 'text-transparent'}`} />
                  <span className="min-w-0 flex-1">
                    <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                      <span className="truncate text-[12.5px] font-medium">{o.name}</span>
                      <AreaChips areaIds={o.areaIds} max={2} />
                    </span>
                    {o.hint && <span className="block truncate text-[11px] text-faint">{o.hint}</span>}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
