import { useEffect, useRef } from 'react'
import { runsApi, workOrdersApi } from '../../../api/endpoints'
import type { Interactions, Journal, Run, RunStatus, WorkOrder } from '../../../api/types'
import { useAsync, type AsyncState } from '../../../lib/useAsync'
import { usePollWhile } from '../../../lib/usePollWhile'
import { isTerminal } from '../../runs/timeline/payload'
import type { TimelineState } from '../../runs/timeline/timelineReducer'
import { useRunTimeline, type StreamState } from '../../runs/timeline/useRunTimeline'
import { useLlmCallsResource, type LlmCallsResource } from '../../runs/useLlmCalls'

const REFRESH_MS = 1500
const POLL_MS = 3000

export type Attempt = {
  run: Run | null
  status: RunStatus | null
  live: boolean
  timeline: TimelineState
  stream: StreamState
  journal: AsyncState<Journal | undefined>
  interactions: AsyncState<Interactions | undefined>
  llmCalls: LlmCallsResource
  reloadRun: () => void
}

/** Calls each reload at most once per interval while `trigger` keeps changing. */
function useThrottledReload(trigger: number, enabled: boolean, reloads: readonly (() => void)[]): void {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const latest = useRef(reloads)
  latest.current = reloads
  useEffect(() => {
    if (!enabled || trigger === 0 || timer.current) return
    timer.current = setTimeout(() => {
      timer.current = undefined
      latest.current.forEach((r) => r())
    }, REFRESH_MS)
  }, [trigger, enabled])
  useEffect(() => () => clearTimeout(timer.current), [])
}

/**
 * Everything about one attempt (run) of a work order, kept live while it is queued or running: events stream
 * over SSE when the person can view runs, otherwise the journal and work order are polled.
 */
export function useAttempt(workOrder: WorkOrder, runId: string | null, canViewRuns: boolean, onFinished: () => void): Attempt {
  const summary = workOrder.runs?.find((r) => r.id === runId) ?? null
  const runRes = useAsync((signal) => (runId && canViewRuns ? runsApi.get(runId, signal) : Promise.resolve(undefined)), [runId, canViewRuns])
  const run = runRes.data ?? summary
  const runFinished = run ? isTerminal(run.status) : true
  const { state: timeline, stream } = useRunTimeline(runId ?? '', runId && canViewRuns && run ? !runFinished : undefined)
  const status: RunStatus | null = run ? (runFinished ? run.status : (timeline.runStatus ?? run.status)) : null
  const live = status !== null && !isTerminal(status)

  const journal = useAsync((signal) => (runId ? workOrdersApi.journal(workOrder.id, runId, signal) : Promise.resolve(undefined)), [workOrder.id, runId])
  const interactions = useAsync((signal) => (runId && canViewRuns ? runsApi.interactions(runId, signal) : Promise.resolve(undefined)), [runId, canViewRuns])
  const llmCalls = useLlmCallsResource(runId ?? '')

  useThrottledReload(timeline.eventCount, live && canViewRuns, [journal.reload, interactions.reload])
  // Without the event stream, poll instead
  usePollWhile(live && !canViewRuns, onFinished, POLL_MS)
  usePollWhile(live && !canViewRuns, journal.reload, POLL_MS)

  // The stream reported a terminal status: fetch the final run, journal, graph and work order
  const reloadRun = runRes.reload
  const finishedRef = useRef(onFinished)
  finishedRef.current = onFinished
  useEffect(() => {
    if (runFinished || !isTerminal(timeline.runStatus)) return
    reloadRun()
    journal.reload()
    interactions.reload()
    finishedRef.current()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runFinished, timeline.runStatus, reloadRun])

  return { run, status, live, timeline, stream, journal, interactions, llmCalls, reloadRun }
}
