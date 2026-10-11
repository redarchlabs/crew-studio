import { useMemo, useState } from 'react'
import { BookOpen, RefreshCw, Search, TriangleAlert } from 'lucide-react'
import type { Journal, JournalEntryKind } from '../../../../api/types'
import { Button } from '../../../../components/Button'
import { EmptyState, ErrorState, Loading } from '../../../../components/States'
import { compact, usd } from '../../../../lib/format'
import { useNow } from '../../../../lib/useNow'
import { CompactContextButton } from '../../CompactContextButton'
import { JournalEntryRow } from './JournalEntryRow'
import { JournalRail } from './JournalRail'
import { filterEntries, groupByTask, KIND_GROUPS, NO_JOURNAL_FILTER, taskLabel, type JournalFilter } from './journalModel'

const PAGE = 800

type JournalTabProps = {
  journal: Journal | undefined
  error: unknown
  loading: boolean
  reload: () => void
  live: boolean
  /** The running attempt whose agents' context can be compacted; omit when it cannot (finished, or no right to) */
  compactRunId?: string | null
}

/** What each agent did on each task, oldest first, with agents and tasks as filters. */
export function JournalTab({ journal, error, loading, reload, live, compactRunId }: JournalTabProps) {
  const [filter, setFilter] = useState<JournalFilter>(NO_JOURNAL_FILTER)
  const [limit, setLimit] = useState(PAGE)
  const now = useNow(live)
  const entries = useMemo(() => (journal ? filterEntries(journal.entries, filter) : []), [journal, filter])
  const sections = useMemo(() => groupByTask(entries.slice(0, limit)), [entries, limit])

  if (error && !journal) return <ErrorState error={error} onRetry={reload} title="Could not load the journal" />
  if (!journal) return loading ? <Loading label="Loading journal…" /> : null
  if (journal.entries.length === 0) {
    return <EmptyState icon={<BookOpen className="size-6" />} title="Nothing in the journal yet" hint={live ? 'Entries appear as agents start working.' : 'This attempt recorded no activity.'} />
  }

  const toggleGroup = (kinds: readonly JournalEntryKind[]) => {
    const hidden = new Set(filter.hidden)
    const allHidden = kinds.every((k) => hidden.has(k))
    for (const k of kinds) {
      if (allHidden) hidden.delete(k)
      else hidden.add(k)
    }
    setFilter({ ...filter, hidden })
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
      <JournalRail journal={journal} agent={filter.agent} task={filter.task} onAgent={(agent) => setFilter({ ...filter, agent })} onTask={(task) => setFilter({ ...filter, task })} now={now} />
      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex min-w-[180px] flex-1 items-center gap-2 rounded-md border border-line bg-raised px-2.5 sm:max-w-xs">
            <Search className="size-3.5 text-faint" />
            <input className="w-full bg-transparent py-1 text-[12.5px] outline-none" placeholder="Search entries…" aria-label="Search journal" value={filter.query} onChange={(e) => setFilter({ ...filter, query: e.target.value })} />
          </div>
          <div className="flex flex-wrap gap-1">
            {KIND_GROUPS.map((g) => {
              const on = !g.kinds.every((k) => filter.hidden.has(k))
              return (
                <button key={g.key} type="button" aria-pressed={on} onClick={() => toggleGroup(g.kinds)} className={`rounded-full border px-2 py-0.5 text-[11.5px] ${on ? 'border-accent/60 bg-accent-soft text-fg' : 'border-line text-faint line-through'}`}>
                  {g.label}
                </button>
              )
            })}
          </div>
          <span className="ml-auto font-mono text-[11px] text-faint">{entries.length}/{journal.entries.length}</span>
          {compactRunId && <CompactContextButton runId={compactRunId} />}
          <Button size="sm" variant="ghost" icon={<RefreshCw className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />} onClick={reload}>Refresh</Button>
        </div>
        {journal.truncated && (
          <p className="flex items-start gap-1.5 rounded-md bg-warn-soft px-3 py-2 text-[12px] text-warn">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
            The journal is truncated to its first {journal.entries.length.toLocaleString()} entries. The full record is in the transcript under Files.
          </p>
        )}
        {sections.length === 0 && <EmptyState title="Nothing matches these filters" />}
        {sections.map((s) => (
          <section key={s.key} className="rounded-lg border border-line bg-panel">
            <header className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2">
              <span className={`size-2 rounded-full ${s.task ? 'bg-blue' : 'bg-accent'}`} />
              <span className="font-medium">{taskLabel(s.task)}</span>
              <span className="ml-auto font-mono text-[11px] text-faint">
                {s.entries.length} entries{s.tokens > 0 ? ` · ${compact(s.tokens)} tok` : ''}{s.cost > 0 ? ` · ${usd(s.cost)}` : ''}
              </span>
            </header>
            <ol className="flex flex-col gap-px p-1.5">
              {s.entries.map((e) => <JournalEntryRow key={e.seq} entry={e} />)}
            </ol>
          </section>
        ))}
        {entries.length > limit && (
          <Button className="self-center" onClick={() => setLimit((l) => l + PAGE)}>Show {Math.min(PAGE, entries.length - limit)} more</Button>
        )}
      </div>
    </div>
  )
}
