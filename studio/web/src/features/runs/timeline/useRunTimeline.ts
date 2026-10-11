import { useEffect, useReducer, useRef, useState } from 'react'
import { runsApi } from '../../../api/endpoints'
import { isAbort } from '../../../api/client'
import type { RunEvent } from '../../../api/types'
import { notifyError } from '../../../lib/notify'
import { isTerminal, parseRunEvent, runStatusOf } from './payload'
import { initialTimeline, timelineReducer, type TimelineState } from './timelineReducer'

export type StreamState = 'idle' | 'loading' | 'connecting' | 'live' | 'reconnecting' | 'closed'

const FLUSH_MS = 120
const MAX_BACKOFF_MS = 10_000
const PAGE_SIZE = 1000

/**
 * Feeds the timeline reducer for a run. Finished runs are loaded via
 * GET /runs/{id}/events; live runs stream over SSE, resuming with
 * ?after=<last seq> after a disconnect and stopping on a terminal run_status.
 */
export function useRunTimeline(runId: string, live: boolean | undefined): { state: TimelineState; stream: StreamState } {
  const [state, dispatch] = useReducer(timelineReducer, initialTimeline)
  const [stream, setStream] = useState<StreamState>('idle')
  const lastSeq = useRef(0)
  const terminal = useRef(false)

  useEffect(() => {
    dispatch({ type: 'reset' })
    lastSeq.current = 0
    terminal.current = false
  }, [runId])

  // Finished runs: page through stored events.
  useEffect(() => {
    if (live !== false) return
    const ctrl = new AbortController()
    setStream('loading')
    const load = async () => {
      for (;;) {
        const batch = await runsApi.events(runId, lastSeq.current, ctrl.signal)
        if (ctrl.signal.aborted) return
        if (batch.length > 0) {
          lastSeq.current = Math.max(lastSeq.current, ...batch.map((e) => e.seq))
          dispatch({ type: 'append', events: batch })
        }
        if (batch.length < PAGE_SIZE) break
      }
      setStream('closed')
    }
    load().catch((err: unknown) => {
      if (isAbort(err) || ctrl.signal.aborted) return
      setStream('closed')
      notifyError('Could not load run events', err)
    })
    return () => ctrl.abort()
  }, [runId, live])

  // Live runs: SSE with manual resume.
  useEffect(() => {
    if (live !== true) return
    let source: EventSource | null = null
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let flushTimer: ReturnType<typeof setTimeout> | undefined
    let buffer: RunEvent[] = []
    let attempts = 0
    let disposed = false

    const flush = () => {
      flushTimer = undefined
      if (buffer.length === 0) return
      const events = buffer
      buffer = []
      dispatch({ type: 'append', events })
    }

    const onMessage = (msg: MessageEvent<string>) => {
      const event = parseRunEvent(msg.data)
      if (!event || event.seq <= lastSeq.current) return
      lastSeq.current = event.seq
      attempts = 0
      buffer = [...buffer, event]
      if (event.type === 'run_status' && isTerminal(runStatusOf(event.payload))) {
        terminal.current = true
        source?.close()
        flush()
        setStream('closed')
        return
      }
      flushTimer ??= setTimeout(flush, FLUSH_MS)
    }

    const connect = () => {
      if (disposed) return
      setStream(attempts === 0 ? 'connecting' : 'reconnecting')
      source = new EventSource(runsApi.streamUrl(runId, lastSeq.current))
      source.addEventListener('open', () => setStream('live'))
      source.addEventListener('run_event', onMessage as EventListener)
      source.addEventListener('message', onMessage as EventListener)
      source.addEventListener('error', () => {
        source?.close()
        if (disposed || terminal.current) return
        attempts += 1
        setStream('reconnecting')
        const delay = Math.min(MAX_BACKOFF_MS, 1000 * 2 ** Math.min(attempts - 1, 4))
        retryTimer = setTimeout(connect, delay)
      })
    }

    connect()
    return () => {
      disposed = true
      source?.close()
      clearTimeout(retryTimer)
      clearTimeout(flushTimer)
      flush()
    }
  }, [runId, live])

  return { state, stream }
}
