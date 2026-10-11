import { useEffect, useState } from 'react'
import { Download } from 'lucide-react'
import { attachmentsApi, runsApi } from '../../../api/endpoints'
import type { Run } from '../../../api/types'
import { buttonClass } from '../../../components/Button'
import { Loading } from '../../../components/States'
import { useAsync } from '../../../lib/useAsync'
import { CallbacksTab } from '../../runs/tabs/CallbacksTab'
import { LlmCallsTab } from '../../runs/tabs/LlmCallsTab'
import { LogsTab } from '../../runs/tabs/LogsTab'
import { SpecTab } from '../../runs/tabs/SpecTab'
import type { TimelineFilter, TimelineState } from '../../runs/timeline/timelineReducer'
import type { StreamState } from '../../runs/timeline/useRunTimeline'
import { TimelineFilters, TimelineList } from '../../runs/timeline/TimelineView'
import { formatBytes } from '../files/attachments'
import { SpendBreakdown } from './SpendBreakdown'

const VIEWS = [
  { key: 'llm', label: 'LLM calls' },
  { key: 'events', label: 'Events' },
  { key: 'console', label: 'Console' },
  { key: 'spec', label: 'Spec' },
  { key: 'callbacks', label: 'Callbacks' },
] as const
type View = (typeof VIEWS)[number]['key']

const NO_FILTER: TimelineFilter = { hidden: new Set(), agent: null, focus: null }

/** Download links for the full transcript and console log (stored when the run finishes). */
function TranscriptLinks({ runId, live }: { runId: string; live: boolean }) {
  const res = useAsync((signal) => runsApi.artifacts(runId, signal), [runId])
  const { reload } = res
  useEffect(() => {
    if (!live) reload()
  }, [live, reload])
  const transcripts = (res.data ?? []).filter((a) => a.kind === 'transcript')
  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-line bg-panel px-3 py-2 text-[12px]">
      <span className="eyebrow mr-1">Full record</span>
      {transcripts.length === 0 && <span className="text-faint">{live ? 'The transcript and console log are saved when the run finishes.' : res.loading ? 'Loading…' : 'No transcript stored for this run.'}</span>}
      {transcripts.map((t) => (
        <a key={t.id} href={attachmentsApi.contentUrl(t.id, true)} download className={buttonClass('secondary', 'sm')} title={`sha256 ${t.sha256}`}>
          <Download className="size-3.5" /> {t.filename} <span className="text-faint">{formatBytes(t.size_bytes)}</span>
        </a>
      ))}
    </div>
  )
}

type DebugTabProps = {
  run: Run
  live: boolean
  timeline: TimelineState
  stream: StreamState
}

/** Everything for debugging an attempt: spend by source, LLM calls, raw events, console output, the frozen spec and callbacks. */
export function DebugTab({ run, live, timeline, stream }: DebugTabProps) {
  const [view, setView] = useState<View>('llm')
  const [filter, setFilter] = useState<TimelineFilter>(NO_FILTER)
  return (
    <div className="flex flex-col gap-3">
      <TranscriptLinks runId={run.id} live={live} />
      <SpendBreakdown tally={timeline.tally} />
      <div role="tablist" aria-label="Debug view" className="flex gap-1 overflow-x-auto">
        {VIEWS.map((v) => (
          <button key={v.key} type="button" role="tab" aria-selected={view === v.key} onClick={() => setView(v.key)} className={`whitespace-nowrap rounded-full border px-2.5 py-0.5 text-[12px] ${view === v.key ? 'border-accent/60 bg-accent-soft text-fg' : 'border-line text-muted hover:text-fg'}`}>
            {v.label}
            {v.key === 'events' && timeline.eventCount > 0 && <span className="ml-1 font-mono text-[10.5px] text-faint">{timeline.eventCount - timeline.logs.length}</span>}
            {v.key === 'console' && timeline.logs.length > 0 && <span className="ml-1 font-mono text-[10.5px] text-faint">{timeline.logs.length}</span>}
          </button>
        ))}
      </div>
      {view === 'llm' && <LlmCallsTab live={live} refreshKey={timeline.tally.llmCalls} />}
      {view === 'events' && (
        <div className="flex flex-col gap-3">
          <TimelineFilters agents={timeline.agents} filter={filter} onChange={setFilter} />
          {stream === 'loading' && timeline.eventCount === 0 ? <Loading label="Loading events…" /> : <TimelineList state={timeline} filter={filter} />}
        </div>
      )}
      {view === 'console' && <LogsTab logs={timeline.logs} />}
      {view === 'spec' && <SpecTab run={run} />}
      {view === 'callbacks' && <CallbacksTab runId={run.id} live={live} />}
    </div>
  )
}
