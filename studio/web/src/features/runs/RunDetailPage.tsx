import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { ClipboardList, ListTree } from 'lucide-react'
import { runsApi } from '../../api/endpoints'
import type { Run } from '../../api/types'
import { Button } from '../../components/Button'
import { Panel } from '../../components/PageHeader'
import { EmptyState, ErrorState, Loading } from '../../components/States'
import { Tabs, type TabDef } from '../../components/Tabs'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAuth } from '../../lib/auth'
import { useAsync } from '../../lib/useAsync'
import { RunHeader } from './RunHeader'
import { InteractionsTab } from './tabs/InteractionsTab'
import { LlmCallsTab } from './tabs/LlmCallsTab'
import { LogsTab } from './tabs/LogsTab'
import { OutputsTab } from './tabs/OutputsTab'
import { SpecTab } from './tabs/SpecTab'
import { CallbacksTab } from './tabs/CallbacksTab'
import { FilesTab } from './tabs/FilesTab'
import { JournalTab } from './tabs/journal/JournalTab'
import { isTerminal } from './timeline/payload'
import type { TimelineFilter, TimelineFocus } from './timeline/timelineReducer'
import { useRunTimeline } from './timeline/useRunTimeline'
import { FocusChip, TimelineFilters, TimelineList } from './timeline/TimelineView'
import { LlmCallsContext, useLlmCallsResource } from './useLlmCalls'

const TAB_KEYS = ['interactions', 'timeline', 'journal', 'llm', 'outputs', 'files', 'logs', 'callbacks', 'spec'] as const
type TabKey = (typeof TAB_KEYS)[number]

const INTERACTIONS_REFRESH_MS = 1000

function isTabKey(v: string | null): v is TabKey {
  return v !== null && (TAB_KEYS as readonly string[]).includes(v)
}

const NO_FILTER: TimelineFilter = { hidden: new Set(), agent: null, focus: null }

function RunDetail({ run, reloadRun, replaceRun }: { run: Run; reloadRun: () => void; replaceRun: (run: Run) => void }) {
  const [params, setParams] = useSearchParams()
  const tab: TabKey = isTabKey(params.get('tab')) ? (params.get('tab') as TabKey) : 'interactions'
  const setTab = (key: TabKey) => setParams((p) => { const next = new URLSearchParams(p); next.set('tab', key); return next }, { replace: true })

  const runFinished = isTerminal(run.status)
  const { state: timeline, stream } = useRunTimeline(run.id, !runFinished)
  const status = runFinished ? run.status : (timeline.runStatus ?? run.status)
  const live = !isTerminal(status)
  const { can } = useAuth()
  // As for cancelling: runs.cancel, or work_orders.cancel for a work order's attempt; a chat turn is too short to compact,
  // and a hand-off compacts by itself
  const mayCompact = run.kind !== 'chat' && run.kind !== 'handoff' && (can('runs.cancel') || (run.work_order_id !== null && can('work_orders.cancel')))
  const llmCalls = useLlmCallsResource(run.id)
  const [filter, setFilter] = useState<TimelineFilter>(NO_FILTER)
  const [cancelling, setCancelling] = useState(false)

  // The stream saw a terminal status: fetch the final run (output, task outputs, usage).
  useEffect(() => {
    if (!runFinished && isTerminal(timeline.runStatus)) reloadRun()
  }, [runFinished, timeline.runStatus, reloadRun])

  const interactions = useAsync((signal) => runsApi.interactions(run.id, signal), [run.id])
  const journal = useAsync((signal) => runsApi.journal(run.id, signal), [run.id])
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const reloadInteractions = interactions.reload
  const reloadJournal = journal.reload
  useEffect(() => {
    // Throttled to one refetch per second while events keep arriving.
    if (runFinished || timeline.eventCount === 0 || refreshTimer.current) return
    refreshTimer.current = setTimeout(() => {
      refreshTimer.current = undefined
      reloadInteractions()
      reloadJournal()
    }, INTERACTIONS_REFRESH_MS)
  }, [runFinished, timeline.eventCount, reloadInteractions, reloadJournal])
  useEffect(() => () => clearTimeout(refreshTimer.current), [])
  useEffect(() => {
    if (!runFinished) return
    reloadInteractions()
    reloadJournal()
  }, [runFinished, reloadInteractions, reloadJournal])

  const cancel = async () => {
    setCancelling(true)
    try {
      replaceRun(await runsApi.cancel(run.id))
      notifySuccess('Run cancelled')
    } catch (err) {
      notifyError('Could not cancel the run', err)
    } finally {
      setCancelling(false)
    }
  }

  const spent = runFinished ? (run.cost_usd ?? timeline.spentUsd ?? 0) : (timeline.spentUsd ?? run.cost_usd ?? 0)
  const setFocus = (focus: TimelineFocus | null) => setFilter((f) => ({ ...f, focus }))
  const focusOnly = useMemo<TimelineFilter>(() => ({ ...NO_FILTER, focus: filter.focus }), [filter.focus])

  const tabs: TabDef<TabKey>[] = [
    { key: 'interactions', label: 'Interactions', badge: interactions.data?.nodes.length },
    { key: 'timeline', label: 'Timeline', badge: timeline.eventCount - timeline.logs.length || undefined },
    { key: 'journal', label: 'Journal', badge: journal.data?.entries.length || undefined },
    { key: 'llm', label: 'LLM calls' },
    { key: 'outputs', label: 'Outputs', badge: run.task_outputs?.length || undefined },
    { key: 'files', label: 'Files' },
    { key: 'logs', label: 'Logs', badge: timeline.logs.length || undefined },
    { key: 'callbacks', label: 'Callbacks' },
    { key: 'spec', label: 'Spec' },
  ]

  return (
    <LlmCallsContext.Provider value={llmCalls}>
      <RunHeader run={run} status={status} spent={spent} budgetState={timeline.budgetState} stream={stream} cancelling={cancelling} onCancel={() => void cancel()} />
      {run.work_order_id && <WorkOrderLink run={run} />}
      <Tabs tabs={tabs} active={tab} onChange={setTab} />
      <div className="pt-4">
        {tab === 'interactions' && (
          <div className="flex flex-col gap-4">
            <InteractionsTab data={interactions.data} error={interactions.error} loading={interactions.loading} reload={interactions.reload} focus={filter.focus} onFocus={setFocus} />
            {filter.focus && (
              <Panel
                title="Timeline for selection"
                actions={<Button size="sm" variant="ghost" icon={<ListTree className="size-3.5" />} onClick={() => setTab('timeline')}>Open in timeline</Button>}
                bodyClassName="p-3 flex flex-col gap-3"
              >
                <FocusChip focus={filter.focus} onClear={() => setFocus(null)} />
                <TimelineList state={timeline} filter={focusOnly} />
              </Panel>
            )}
          </div>
        )}
        {tab === 'timeline' && (
          <div className="flex flex-col gap-3">
            <TimelineFilters agents={timeline.agents} filter={filter} onChange={setFilter} />
            {stream === 'loading' && timeline.eventCount === 0 ? <Loading label="Loading events…" /> : <TimelineList state={timeline} filter={filter} />}
          </div>
        )}
        {tab === 'journal' && <JournalTab journal={journal.data} error={journal.error} loading={journal.loading} reload={journal.reload} live={live} compactRunId={status === 'running' && mayCompact ? run.id : null} />}
        {tab === 'llm' && <LlmCallsTab live={live} />}
        {tab === 'outputs' && <OutputsTab run={run} timeline={timeline} />}
        {tab === 'files' && <FilesTab inputs={run.spec?.work_order?.attachments ?? []} runId={run.id} live={live} />}
        {tab === 'logs' && <LogsTab logs={timeline.logs} />}
        {tab === 'callbacks' && <CallbacksTab runId={run.id} live={live} />}
        {tab === 'spec' && <SpecTab run={run} />}
      </div>
    </LlmCallsContext.Provider>
  )
}

/** This run is an attempt of a work order: link back to it. */
function WorkOrderLink({ run }: { run: Run }) {
  const wo = run.spec?.work_order
  return (
    <Link to={`/work-orders/${run.work_order_id}?run=${run.id}`} className="-mt-2 mb-3 inline-flex max-w-full items-center gap-2 self-start rounded-md border border-line bg-panel px-2.5 py-1 text-[12.5px] hover:border-accent/60">
      <ClipboardList className="size-3.5 text-accent" />
      <span className="text-muted">Work order</span>
      <span className="font-mono font-medium">{wo ? `WO-${wo.number}` : 'open'}</span>
      {wo && <span className="truncate text-muted">{wo.title}</span>}
    </Link>
  )
}

export default function RunDetailPage() {
  const { id = '' } = useParams()
  const res = useAsync((signal) => runsApi.get(id, signal), [id])
  if (res.error && !res.data) {
    return (
      <div className="flex flex-col gap-3">
        <ErrorState error={res.error} onRetry={res.reload} title="Could not load this run" />
        <Link to="/runs" className="text-accent">Back to runs</Link>
      </div>
    )
  }
  if (!res.data) return res.loading ? <Loading label="Loading run…" /> : <EmptyState title="Run not found" />
  return <RunDetail key={res.data.id} run={res.data} reloadRun={res.reload} replaceRun={(r) => res.setData((prev) => ({ ...prev, ...r, spec: r.spec ?? prev?.spec, task_outputs: r.task_outputs ?? prev?.task_outputs }))} />
}
