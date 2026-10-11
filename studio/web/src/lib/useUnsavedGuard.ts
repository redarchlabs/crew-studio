import { useEffect } from 'react'
import { useBlocker } from 'react-router'

/** Navigation state that bypasses the guard (after save/delete the form is clean). */
export const SKIP_GUARD = { skipUnsavedGuard: true } as const

function skips(state: unknown): boolean {
  return typeof state === 'object' && state !== null && 'skipUnsavedGuard' in state
}

/** Warns before leaving a page with unsaved edits (in-app navigation and tab close). */
export function useUnsavedGuard(dirty: boolean): void {
  const blocker = useBlocker(({ currentLocation, nextLocation }) => dirty && currentLocation.pathname !== nextLocation.pathname && !skips(nextLocation.state))

  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (window.confirm('You have unsaved changes. Leave without saving?')) blocker.proceed()
    else blocker.reset()
  }, [blocker])

  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])
}
