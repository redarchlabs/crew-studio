import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { humanApi } from '../../api/endpoints.human'
import type { HumanQuestion } from '../../api/types.human'

const MAX_BACKOFF_MS = 30_000
const NONE: readonly HumanQuestion[] = []

const WaitingQuestionsContext = createContext<readonly HumanQuestion[]>(NONE)

/** Questions agents are waiting on the signed-in person to answer, kept current by Studio's question stream. */
export function useWaitingQuestions(): readonly HumanQuestion[] {
  return useContext(WaitingQuestionsContext)
}

/** Opens one question stream for the app; without EventSource (tests, old browsers) it loads the list once. */
export function WaitingQuestionsProvider({ enabled, children }: { enabled: boolean; children: ReactNode }) {
  const [questions, setQuestions] = useState<readonly HumanQuestion[]>(NONE)

  useEffect(() => {
    if (!enabled) {
      setQuestions(NONE)
      return
    }
    if (typeof EventSource === 'undefined') {
      const ctrl = new AbortController()
      humanApi.mine(ctrl.signal).then(setQuestions, () => undefined)
      return () => ctrl.abort()
    }
    let source: EventSource | null = null
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let attempts = 0
    let disposed = false
    const connect = () => {
      if (disposed) return
      source = new EventSource(humanApi.streamUrl)
      source.addEventListener('questions', (ev) => {
        attempts = 0
        try {
          const list = JSON.parse((ev as MessageEvent<string>).data) as HumanQuestion[] | null
          setQuestions(list ?? NONE)
        } catch {
          // A malformed event is skipped; the next one replaces the whole list
        }
      })
      source.addEventListener('error', () => {
        source?.close()
        if (disposed) return
        attempts += 1
        retryTimer = setTimeout(connect, Math.min(MAX_BACKOFF_MS, 1000 * 2 ** Math.min(attempts - 1, 5)))
      })
    }
    connect()
    return () => {
      disposed = true
      source?.close()
      clearTimeout(retryTimer)
    }
  }, [enabled])

  return <WaitingQuestionsContext.Provider value={questions}>{children}</WaitingQuestionsContext.Provider>
}
