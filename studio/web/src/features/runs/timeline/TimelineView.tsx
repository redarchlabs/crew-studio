import type { ReactNode } from 'react'
import { useMemo } from 'react'
import { Bot, X } from 'lucide-react'
import { EmptyState } from '../../../components/States'
import { EVENT_CATEGORIES, type EventCategory } from './payload'
import { filterGroups, type TaskStatus, type TimelineFilter, type TimelineFocus, type TimelineGroup, type TimelineState } from './timelineReducer'
import { TimelineItemRow } from './TimelineItemRow'

const STATUS_CLS: Record<TaskStatus, string> = {
  running: 'bg-accent-soft text-accent',
  done: 'bg-ok-soft text-ok',
  failed: 'bg-danger-soft text-danger',
}

function GroupView({ group }: { group: TimelineGroup }) {
  return (
    <section className="rounded-lg border border-line bg-panel">
      <header className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2">
        <span className={`size-2 rounded-full ${group.task ? 'bg-blue' : 'bg-accent'} ${group.status === 'running' ? 'pulse-dot' : ''}`} />
        <span className="font-medium">{group.title}</span>
        {group.status && <span className={`rounded px-1.5 py-px font-mono text-[10.5px] uppercase ${STATUS_CLS[group.status]}`}>{group.status}</span>}
        <span className="ml-auto flex flex-wrap items-center gap-1 text-[11.5px] text-faint">
          {group.agents.map((a) => (
            <span key={a} className="inline-flex items-center gap-1 rounded bg-teal-soft px-1.5 py-px text-teal"><Bot className="size-3" />{a}</span>
          ))}
          <span className="font-mono">{group.items.length} ev</span>
        </span>
      </header>
      <ol className="flex flex-col gap-px border-l-2 border-line/0 p-1.5">
        {group.items.map((item) => (
          <TimelineItemRow key={item.seq} item={item} />
        ))}
      </ol>
    </section>
  )
}

/** Grouped (by task) event list, already filtered. */
export function TimelineList({ state, filter }: { state: TimelineState; filter: TimelineFilter }) {
  const groups = useMemo(() => filterGroups(state.groups, filter), [state.groups, filter])
  if (state.groups.length === 0) return <EmptyState title="No events yet" hint="Events appear here as the crew works." />
  if (groups.length === 0) return <EmptyState title="Nothing matches these filters" />
  return (
    <div className="flex flex-col gap-3">
      {groups.map((g) => (
        <GroupView key={g.key} group={g} />
      ))}
    </div>
  )
}

function ChipButton({ on, onClick, children }: { on: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" aria-pressed={on} onClick={onClick} className={`rounded-full border px-2 py-0.5 text-[11.5px] transition-colors ${on ? 'border-accent/60 bg-accent-soft text-fg' : 'border-line text-faint line-through decoration-faint/60 hover:text-muted'}`}>
      {children}
    </button>
  )
}

type FiltersProps = {
  agents: readonly string[]
  filter: TimelineFilter
  onChange: (filter: TimelineFilter) => void
}

export function TimelineFilters({ agents, filter, onChange }: FiltersProps) {
  const toggleCategory = (c: EventCategory) => {
    const hidden = new Set(filter.hidden)
    if (hidden.has(c)) hidden.delete(c)
    else hidden.add(c)
    onChange({ ...filter, hidden })
  }
  return (
    <div className="flex flex-col gap-2 rounded-lg border border-line bg-panel px-3 py-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="eyebrow mr-1 w-12">Events</span>
        {EVENT_CATEGORIES.map((c) => (
          <ChipButton key={c} on={!filter.hidden.has(c)} onClick={() => toggleCategory(c)}>{c}</ChipButton>
        ))}
      </div>
      {agents.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="eyebrow mr-1 w-12">Agent</span>
          <button type="button" onClick={() => onChange({ ...filter, agent: null })} className={`rounded-full border px-2 py-0.5 text-[11.5px] ${filter.agent === null ? 'border-teal/60 bg-teal-soft text-fg' : 'border-line text-faint'}`}>all</button>
          {agents.map((a) => (
            <button key={a} type="button" onClick={() => onChange({ ...filter, agent: filter.agent === a ? null : a })} className={`rounded-full border px-2 py-0.5 text-[11.5px] ${filter.agent === a ? 'border-teal/60 bg-teal-soft text-fg' : 'border-line text-muted hover:text-fg'}`}>{a}</button>
          ))}
        </div>
      )}
      {filter.focus && <FocusChip focus={filter.focus} onClear={() => onChange({ ...filter, focus: null })} />}
    </div>
  )
}

export function FocusChip({ focus, onClear }: { focus: TimelineFocus; onClear: () => void }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="eyebrow mr-1 w-12">Focus</span>
      <span className="inline-flex items-center gap-1 rounded-full border border-accent/60 bg-accent-soft px-2 py-0.5 text-[11.5px]">
        {focus.kind}: <span className="font-medium">{focus.value}</span>
        <button type="button" aria-label="Clear focus" className="ml-0.5 text-muted hover:text-fg" onClick={onClear}><X className="size-3" /></button>
      </span>
    </div>
  )
}
