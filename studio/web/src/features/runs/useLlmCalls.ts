import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { runsApi } from '../../api/endpoints'
import type { LlmCall } from '../../api/types'

export type LlmCallsResource = {
  calls: LlmCall[] | undefined
  loading: boolean
  error: unknown
  /** Fetches the list; refetches when `force` or when a wanted seq is not loaded yet. */
  load: (opts?: { force?: boolean; wantSeq?: number }) => void
}

/** Lazily loads /runs/{id}/llm-calls (the payloads can be large, so only on demand). */
export function useLlmCallsResource(runId: string): LlmCallsResource {
  const [calls, setCalls] = useState<LlmCall[] | undefined>(undefined)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(undefined)
  const inflight = useRef(false)
  const callsRef = useRef<LlmCall[] | undefined>(undefined)

  useEffect(() => {
    setCalls(undefined)
    callsRef.current = undefined
    setError(undefined)
  }, [runId])

  const load = useCallback(
    (opts: { force?: boolean; wantSeq?: number } = {}) => {
      const have = callsRef.current
      const missing = opts.wantSeq !== undefined && !have?.some((c) => c.seq === opts.wantSeq)
      if (inflight.current || (have && !opts.force && !missing)) return
      inflight.current = true
      setLoading(true)
      setError(undefined)
      runsApi
        .llmCalls(runId)
        .then((list) => {
          const sorted = [...list].sort((a, b) => a.seq - b.seq)
          callsRef.current = sorted
          setCalls(sorted)
        })
        .catch((err: unknown) => setError(err))
        .finally(() => {
          inflight.current = false
          setLoading(false)
        })
    },
    [runId],
  )

  return { calls, loading, error, load }
}

export const LlmCallsContext = createContext<LlmCallsResource | null>(null)

export function useLlmCalls(): LlmCallsResource {
  const ctx = useContext(LlmCallsContext)
  if (!ctx) throw new Error('useLlmCalls must be used inside LlmCallsContext')
  return ctx
}
