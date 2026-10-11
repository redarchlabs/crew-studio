import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react'
import { isAbort } from '../api/client'

export type AsyncState<T> = {
  data: T | undefined
  error: unknown
  loading: boolean
  reload: () => void
  /** Replace data locally (e.g. after a mutation) without refetching. */
  setData: (update: (prev: T | undefined) => T | undefined) => void
}

/** Runs an abortable loader whenever deps change; exposes reload and local updates. */
export function useAsync<T>(loader: (signal: AbortSignal) => Promise<T>, deps: DependencyList): AsyncState<T> {
  const [data, setDataState] = useState<T | undefined>(undefined)
  const [error, setError] = useState<unknown>(undefined)
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)
  const loaderRef = useRef(loader)
  loaderRef.current = loader
  const lastTick = useRef(tick)

  useEffect(() => {
    const ctrl = new AbortController()
    // A deps change means a different resource: drop stale data. A reload() keeps it visible.
    if (lastTick.current === tick) setDataState(undefined)
    lastTick.current = tick
    setLoading(true)
    setError(undefined)
    loaderRef
      .current(ctrl.signal)
      .then((value) => {
        if (!ctrl.signal.aborted) setDataState(value)
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted && !isAbort(err)) setError(err)
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false)
      })
    return () => ctrl.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])

  const reload = useCallback(() => setTick((t) => t + 1), [])
  const setData = useCallback((update: (prev: T | undefined) => T | undefined) => setDataState(update), [])
  return { data, error, loading, reload, setData }
}
