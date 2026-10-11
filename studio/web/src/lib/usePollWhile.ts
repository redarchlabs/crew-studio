import { useEffect } from 'react'

/** Calls reload every intervalMs while active is true (e.g. while a listed run is still in progress). */
export function usePollWhile(active: boolean, reload: () => void, intervalMs = 3000): void {
  useEffect(() => {
    if (!active) return
    const id = window.setInterval(reload, intervalMs)
    return () => window.clearInterval(id)
  }, [active, reload, intervalMs])
}
