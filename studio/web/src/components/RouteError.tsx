import { useEffect, useState } from 'react'
import { useRouteError } from 'react-router'
import { isStaleChunkError, reloadOnceForStaleChunk } from '../lib/staleChunk'
import { Button } from './Button'
import { ErrorState, Loading } from './States'

/** The router's error screen: a page from an older build reloads once; anything else shows the error and a Reload button. */
export function RouteError() {
  const error = useRouteError()
  const stale = isStaleChunkError(error)
  const [reloading, setReloading] = useState(stale)
  useEffect(() => {
    if (stale) setReloading(reloadOnceForStaleChunk())
  }, [stale])
  if (reloading) return <div className="p-6"><Loading label="Crew Studio was updated. Loading the new version…" /></div>
  return (
    <div className="space-y-3 p-6">
      <ErrorState
        error={error}
        title={stale ? 'Crew Studio was updated, and this page could not load' : 'Something went wrong on this page'}
      />
      <Button onClick={() => window.location.reload()}>Reload the page</Button>
    </div>
  )
}
