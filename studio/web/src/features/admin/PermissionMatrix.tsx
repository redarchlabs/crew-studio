import { useEffect, useRef } from 'react'
import type { Permission, PermissionDef } from '../../api/types'
import { groupPermissions, groupState, setGroup, togglePermission } from './permissions'

/** Checkbox that can show the "some selected" state. */
function TriCheckbox({ state, onChange, label, disabled }: { state: 'all' | 'some' | 'none'; onChange: (on: boolean) => void; label: string; disabled?: boolean }) {
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = state === 'some'
  }, [state])
  return (
    <input
      ref={ref}
      type="checkbox"
      aria-label={label}
      className="size-3.5 accent-[var(--accent)]"
      checked={state === 'all'}
      disabled={disabled}
      onChange={(e) => onChange(e.target.checked)}
    />
  )
}

type PermissionMatrixProps = {
  defs: readonly PermissionDef[]
  value: readonly Permission[]
  onChange: (value: Permission[]) => void
  disabled?: boolean
  /** Permissions already held another way (e.g. through a role); shown with a note */
  inherited?: ReadonlySet<Permission>
  inheritedLabel?: string
}

/** Permission catalog as one row per feature with a per-feature "select all". */
export function PermissionMatrix({ defs, value, onChange, disabled, inherited, inheritedLabel = 'via role' }: PermissionMatrixProps) {
  const groups = groupPermissions(defs)
  return (
    <div className="divide-y divide-line rounded-md border border-line bg-raised">
      {groups.map((g) => {
        const state = groupState(value, g)
        return (
          <div key={g.feature} className="flex flex-col gap-2 px-3 py-2.5 sm:flex-row sm:items-start">
            <label className="flex w-40 shrink-0 items-center gap-2 font-medium">
              <TriCheckbox state={state} label={`All ${g.feature} permissions`} disabled={disabled} onChange={(on) => onChange(setGroup(value, g, on))} />
              {g.feature}
            </label>
            <div className="flex flex-wrap gap-x-4 gap-y-1.5">
              {g.defs.map((d) => {
                const viaOther = inherited?.has(d.key) && !value.includes(d.key)
                return (
                  <label key={d.key} className="inline-flex items-center gap-1.5 text-[12.5px]" title={`${d.description}${d.description && d.description !== d.key ? ` (${d.key})` : ''}`}>
                    <input
                      type="checkbox"
                      className="size-3.5 accent-[var(--accent)]"
                      checked={value.includes(d.key)}
                      disabled={disabled}
                      onChange={() => onChange(togglePermission(value, d.key))}
                    />
                    <span>{d.label}</span>
                    {viaOther && <span className="text-[10.5px] text-teal">{inheritedLabel}</span>}
                  </label>
                )
              })}
            </div>
          </div>
        )
      })}
    </div>
  )
}
