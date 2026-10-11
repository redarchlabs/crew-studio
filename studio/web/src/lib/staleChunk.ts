// After Crew Studio is rebuilt, a tab opened earlier still asks for the old fingerprinted chunks, which no longer
// exist. Loading the page again picks up the new build. Reload at most once per minute so a chunk that is really
// missing (or a server that is down) shows an error instead of reloading forever.

const RELOAD_KEY = 'crewstudio.staleChunkReloadAt'
const RELOAD_WINDOW_MS = 60_000

const STALE_CHUNK = /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS|ChunkLoadError/i

/** Whether an error is a lazily loaded chunk that could not be fetched, typically from an older build. */
export function isStaleChunkError(error: unknown): boolean {
  if (error instanceof Error) return STALE_CHUNK.test(`${error.name} ${error.message}`)
  return typeof error === 'string' && STALE_CHUNK.test(error)
}

type ReloadDeps = { storage?: Pick<Storage, 'getItem' | 'setItem'> | null; now?: () => number; reload?: () => void }

function sessionStore(): Pick<Storage, 'getItem' | 'setItem'> | null {
  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

/** Reloads the page unless it already did so in the last minute. Returns whether it reloaded. */
export function reloadOnceForStaleChunk({ storage = sessionStore(), now = Date.now, reload = () => window.location.reload() }: ReloadDeps = {}): boolean {
  const at = now()
  try {
    const last = Number(storage?.getItem(RELOAD_KEY) ?? 0)
    if (last && at - last < RELOAD_WINDOW_MS) return false
    storage?.setItem(RELOAD_KEY, String(at))
  } catch {
    // Without storage there is no loop guard, so do not reload automatically
    return false
  }
  if (!storage) return false
  reload()
  return true
}

/** Vite fires vite:preloadError when a dynamic import or its preloads fail; reload once instead of crashing. */
export function installStaleChunkReload(target: Pick<Window, 'addEventListener'> = window, deps: ReloadDeps = {}): void {
  target.addEventListener('vite:preloadError', (event) => {
    if (reloadOnceForStaleChunk(deps)) event.preventDefault()
  })
}
