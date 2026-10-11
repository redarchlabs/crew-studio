import { useMemo, useState } from 'react'
import type { Journal, JournalEntry, JournalEntryKind } from '../../../api/types'
import { Button } from '../../../components/Button'
import { compact, timeOfDay, usd } from '../../../lib/format'
import { AgentAvatar, KindIcon } from '../../runs/tabs/journal/journalKinds'
import { avatarClass, filterEntries, KIND_GROUPS, NO_JOURNAL_FILTER, type JournalFilter } from '../../runs/tabs/journal/journalModel'
import { arrowPath, CHIP_INSET, laneCenter, layoutSequence, rowTop, SEQ, type SeqBand } from './sequenceLayout'

const PAGE = 1500

const BAND_TONE: Record<NonNullable<SeqBand['status']>, string> = {
  pending: 'border-line',
  running: 'border-accent',
  done: 'border-ok/60',
  failed: 'border-danger',
}

type SequenceDiagramProps = {
  journal: Journal
  selectedSeq: number | null
  onSelect: (entry: JournalEntry) => void
}

/**
 * Swimlanes: one lane per agent plus a Crew lane, time flowing downward, one row per journal entry.
 * Task boundaries are horizontal bands; delegations are red arrows between lanes. Lanes scroll horizontally
 * on narrow screens while the time column stays put.
 */
export function SequenceDiagram({ journal, selectedSeq, onSelect }: SequenceDiagramProps) {
  const [hidden, setHidden] = useState<ReadonlySet<JournalEntryKind>>(new Set())
  const [limit, setLimit] = useState(PAGE)
  const filter: JournalFilter = useMemo(() => ({ ...NO_JOURNAL_FILTER, hidden }), [hidden])
  const entries = useMemo(() => filterEntries(journal.entries, filter), [journal.entries, filter])
  const layout = useMemo(() => layoutSequence(journal, entries, limit), [journal, entries, limit])
  const lanesWidth = layout.lanes.length * SEQ.laneWidth
  const height = Math.max(layout.rows, 1) * SEQ.rowHeight
  const toggle = (kinds: readonly JournalEntryKind[]) => {
    const next = new Set(hidden)
    const off = kinds.every((k) => next.has(k))
    for (const k of kinds) {
      if (off) next.delete(k)
      else next.add(k)
    }
    setHidden(next)
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-1">
        <span className="eyebrow mr-1">Show</span>
        {KIND_GROUPS.map((g) => {
          const on = !g.kinds.every((k) => hidden.has(k))
          return (
            <button key={g.key} type="button" aria-pressed={on} onClick={() => toggle(g.kinds)} className={`rounded-full border px-2 py-0.5 text-[11.5px] ${on ? 'border-accent/60 bg-accent-soft text-fg' : 'border-line text-faint line-through'}`}>
              {g.label}
            </button>
          )
        })}
        <span className="ml-auto font-mono text-[11px] text-faint">{layout.rows}/{journal.entries.length} entries</span>
      </div>
      <div className="max-h-[640px] overflow-auto rounded-lg border border-line bg-raised">
        <div style={{ width: SEQ.timeColumn + lanesWidth }}>
          <div className="sticky top-0 z-20 flex border-b border-line bg-panel" style={{ height: SEQ.header }}>
            <div className="eyebrow sticky left-0 z-10 flex shrink-0 items-center border-r border-line bg-panel px-2" style={{ width: SEQ.timeColumn }}>Time</div>
            {layout.lanes.map((l) => (
              <div key={l.role} className="flex shrink-0 items-center gap-2 border-r border-line px-2.5" style={{ width: SEQ.laneWidth }}>
                <AgentAvatar role={l.role} />
                <span className="truncate text-[12px] font-medium" title={l.label}>{l.label}</span>
              </div>
            ))}
          </div>
          <div className="flex" style={{ height }}>
            <div className="sticky left-0 z-10 shrink-0 border-r border-line bg-raised" style={{ width: SEQ.timeColumn, height }}>
              {layout.items.map((it) => (
                <div key={it.entry.seq} className="absolute flex items-center px-2 font-mono text-[10px] text-faint tabular" style={{ top: rowTop(it.row), height: SEQ.rowHeight }}>
                  {timeOfDay(it.entry.at)}
                </div>
              ))}
            </div>
            <div className="relative shrink-0" style={{ width: lanesWidth, height }}>
              {layout.bands.map((b) => (
                <div key={`${b.task}:${b.startRow}`} className={`absolute inset-x-0 border-t-2 border-dashed bg-blue-soft/30 ${b.status ? BAND_TONE[b.status] : 'border-line'}`} style={{ top: rowTop(b.startRow), height: (b.endRow - b.startRow + 1) * SEQ.rowHeight }}>
                  <span className="sticky left-0 inline-block -translate-y-1/2 rounded-r bg-panel px-1.5 font-mono text-[10px] text-blue">{b.task}{b.status ? ` · ${b.status}` : ''}</span>
                </div>
              ))}
              {layout.lanes.map((l) => (
                <div key={l.role} className="absolute inset-y-0 w-px bg-line" style={{ left: laneCenter(l.index) - SEQ.timeColumn }} />
              ))}
              <svg className="pointer-events-none absolute inset-0" width={lanesWidth} height={height} aria-hidden="true">
                <defs>
                  <marker id="seq-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                    <path d="M0,0 L10,5 L0,10 z" fill="var(--danger)" />
                  </marker>
                </defs>
                {layout.items.map((it) => {
                  const a = arrowPath(it)
                  if (!a) return null
                  return <line key={it.entry.seq} x1={a.x1 - SEQ.timeColumn} x2={a.x2 - SEQ.timeColumn} y1={a.y} y2={a.y} stroke="var(--danger)" strokeWidth={2} markerEnd="url(#seq-arrow)" />
                })}
              </svg>
              {layout.items.map((it) => (
                <SeqChip key={it.entry.seq} entry={it.entry} lane={it.lane} row={it.row} selected={it.entry.seq === selectedSeq} onSelect={onSelect} />
              ))}
            </div>
          </div>
        </div>
      </div>
      {entries.length > limit && <Button className="self-center" onClick={() => setLimit((l) => l + PAGE)}>Show more ({entries.length - limit} left)</Button>}
      <SequenceLegend />
    </div>
  )
}

function SeqChip({ entry, lane, row, selected, onSelect }: { entry: JournalEntry; lane: number; row: number; selected: boolean; onSelect: (e: JournalEntry) => void }) {
  const delegation = entry.kind === 'delegation'
  const tone = delegation ? 'border-danger/60 bg-danger-soft' : entry.kind === 'llm_call' ? 'border-line bg-panel' : `border-transparent ${avatarClass(entry.agent_role)}`
  const meta = entry.kind === 'llm_call' ? [entry.tokens > 0 ? `${compact(entry.tokens)}t` : '', entry.cost_usd > 0 ? usd(entry.cost_usd) : ''].filter(Boolean).join(' ') : ''
  return (
    <button
      type="button"
      onClick={() => onSelect(entry)}
      title={entry.title}
      className={`absolute flex items-center gap-1.5 overflow-hidden rounded border px-1.5 text-left text-[11px] text-fg hover:border-accent ${tone} ${selected ? 'outline outline-2 outline-accent' : ''}`}
      style={{ top: rowTop(row) + 4, height: SEQ.rowHeight - 8, left: lane * SEQ.laneWidth + CHIP_INSET, width: SEQ.laneWidth - 2 * CHIP_INSET }}
    >
      <KindIcon kind={entry.kind} className="size-3" />
      <span className="min-w-0 flex-1 truncate">{entry.title}</span>
      {meta && <span className="shrink-0 font-mono text-[9.5px] text-faint">{meta}</span>}
    </button>
  )
}

function SequenceLegend() {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-faint">
      <span className="inline-flex items-center gap-1.5"><svg width="22" height="6" aria-hidden="true"><line x1="0" y1="3" x2="22" y2="3" stroke="var(--danger)" strokeWidth="2" /></svg>delegation</span>
      <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-5 border-t-2 border-dashed border-ok/60 bg-blue-soft/30" />task (done)</span>
      <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-5 border-t-2 border-dashed border-accent bg-blue-soft/30" />task (running)</span>
      <span>Time flows down; click an entry for details.</span>
    </div>
  )
}
