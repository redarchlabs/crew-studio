import { Shapes } from 'lucide-react'
import { useAreas } from '../lib/useAreas'
import { useAuth } from '../lib/auth'
import { Chip } from './StatusBadge'
import { MultiSelect } from './MultiSelect'

/** Small chips naming the functional areas an item belongs to. */
export function AreaChips({ areaIds, max = 3 }: { areaIds: readonly string[]; max?: number }) {
  const { byId } = useAreas()
  if (areaIds.length === 0) return <span className="text-[11px] text-faint">No area</span>
  const names = areaIds.map((id) => byId.get(id)?.name ?? 'Unknown area')
  const shown = names.slice(0, max)
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {shown.map((name, i) => (
        <Chip key={`${areaIds[i]}`} tone="teal" title={name}>
          <Shapes className="size-3" />
          {name}
        </Chip>
      ))}
      {names.length > max && <Chip title={names.slice(max).join(', ')}>+{names.length - max}</Chip>}
    </span>
  )
}

type AreaPickerProps = {
  value: readonly string[]
  onChange: (value: string[]) => void
  error?: string
  label?: string
}

/**
 * Picks functional areas for an item. People can only add or remove their own areas (the server keeps the
 * others), so areas outside theirs are shown checked and locked.
 */
export function AreaPicker({ value, onChange, error, label = 'Functional areas' }: AreaPickerProps) {
  const { areas, loading } = useAreas()
  const { me } = useAuth()
  const mine = (id: string) => !!me && (me.all_areas || me.area_ids.includes(id))
  const options = areas.map((a) => ({
    value: a.id,
    label: a.name,
    hint: a.description || undefined,
    disabled: !mine(a.id),
    disabledReason: 'You are not in this area, so you cannot add or remove it.',
  }))
  const toggle = (next: string[]) => {
    // Keep locked areas exactly as they were
    const locked = value.filter((id) => !mine(id))
    onChange([...locked, ...next.filter((id) => mine(id) && !locked.includes(id))])
  }
  return (
    <MultiSelect
      label={label}
      options={options}
      value={value}
      onChange={toggle}
      error={error}
      emptyText={loading ? 'Loading areas…' : 'No functional areas yet. An administrator can add them.'}
    />
  )
}

/** Default areas for a new item: the person's only area, or none so they choose. */
export function defaultAreaIds(me: { all_areas: boolean; area_ids: string[] } | null): string[] {
  if (!me || me.all_areas) return []
  return me.area_ids.length === 1 ? [...me.area_ids] : []
}
