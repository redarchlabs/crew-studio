import { describe, expect, it, vi } from 'vitest'
import { installStaleChunkReload, isStaleChunkError, reloadOnceForStaleChunk } from './staleChunk'

function memoryStorage(initial: Record<string, string> = {}) {
  const data = { ...initial }
  return { getItem: (k: string) => data[k] ?? null, setItem: (k: string, v: string) => { data[k] = v }, data }
}

describe('isStaleChunkError', () => {
  it('recognises chunk load failures from the major browsers', () => {
    expect(isStaleChunkError(new TypeError('Failed to fetch dynamically imported module: http://x/assets/WorkOrderPage-DEezlQVe.js'))).toBe(true)
    expect(isStaleChunkError(new TypeError('Importing a module script failed.'))).toBe(true)
    expect(isStaleChunkError(new TypeError('error loading dynamically imported module: http://x/a.js'))).toBe(true)
    expect(isStaleChunkError(new Error('Unable to preload CSS for /assets/x.css'))).toBe(true)
    expect(isStaleChunkError('Failed to fetch dynamically imported module')).toBe(true)
  })

  it('ignores other errors', () => {
    expect(isStaleChunkError(new Error('The server answered 500.'))).toBe(false)
    expect(isStaleChunkError(null)).toBe(false)
    expect(isStaleChunkError({ message: 'Failed to fetch dynamically imported module' })).toBe(false)
  })
})

describe('reloadOnceForStaleChunk', () => {
  it('reloads once, then not again within a minute, then again after', () => {
    const storage = memoryStorage()
    const reload = vi.fn()
    let now = 1_000_000
    expect(reloadOnceForStaleChunk({ storage, now: () => now, reload })).toBe(true)
    now += 30_000
    expect(reloadOnceForStaleChunk({ storage, now: () => now, reload })).toBe(false)
    now += 31_000
    expect(reloadOnceForStaleChunk({ storage, now: () => now, reload })).toBe(true)
    expect(reload).toHaveBeenCalledTimes(2)
  })

  it('never reloads without storage, since nothing would stop a loop', () => {
    const reload = vi.fn()
    expect(reloadOnceForStaleChunk({ storage: null, reload })).toBe(false)
    const broken = { getItem: () => { throw new Error('denied') }, setItem: () => {} }
    expect(reloadOnceForStaleChunk({ storage: broken, reload })).toBe(false)
    expect(reload).not.toHaveBeenCalled()
  })
})

describe('installStaleChunkReload', () => {
  it('reloads on vite:preloadError and stops the error from being thrown', () => {
    const target = new EventTarget()
    const reload = vi.fn()
    installStaleChunkReload(target as unknown as Window, { storage: memoryStorage(), now: () => 5, reload })
    const event = new Event('vite:preloadError', { cancelable: true })
    target.dispatchEvent(event)
    expect(reload).toHaveBeenCalledOnce()
    expect(event.defaultPrevented).toBe(true)
  })

  it('lets the error through when it already reloaded recently', () => {
    const target = new EventTarget()
    const reload = vi.fn()
    installStaleChunkReload(target as unknown as Window, { storage: memoryStorage({ 'crewstudio.staleChunkReloadAt': '1' }), now: () => 2, reload })
    const event = new Event('vite:preloadError', { cancelable: true })
    target.dispatchEvent(event)
    expect(reload).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(false)
  })
})
