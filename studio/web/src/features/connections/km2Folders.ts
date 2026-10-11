import { useEffect, useState } from 'react'
import { errorMessage } from '../../api/client'
import type { KM2FolderList } from '../../api/types.crewstate'

/** Where a folder list stands: loading, ready, or failed with a reason people can act on. */
export type FolderState = { status: 'loading' } | { status: 'ready'; list: KM2FolderList } | { status: 'failed'; message: string }

/** Folder lists by key (a connection id, or a crew's promotion connection), so rows sharing a connection list it once. */
const cache = new Map<string, Promise<KM2FolderList>>()

/** Forgets every listed folder (after a connection's settings change, and between tests). */
export function clearFolderCache(): void {
  cache.clear()
}

/** Loads a folder list once per key; a failed load is not kept, so it is tried again next time. */
export function loadFolders(key: string, load: () => Promise<KM2FolderList>): Promise<KM2FolderList> {
  let p = cache.get(key)
  if (!p) {
    p = Promise.resolve().then(load)
    cache.set(key, p)
    p.then((list) => { if (!list?.ok) cache.delete(key) }, () => cache.delete(key))
  }
  return p
}

export function stateOf(list: KM2FolderList | undefined): FolderState {
  if (list?.ok) return { status: 'ready', list }
  return { status: 'failed', message: list?.message || 'KM2 did not list the folders.' }
}

/** The folder list for a key, or null when there is nothing to list (no key given). */
export function useKM2Folders(key: string | null, load: () => Promise<KM2FolderList>): FolderState | null {
  const [state, setState] = useState<{ key: string; value: FolderState } | null>(null)
  useEffect(() => {
    if (!key) return
    let live = true
    loadFolders(key, load).then(
      (list) => { if (live) setState({ key, value: stateOf(list) }) },
      (err: unknown) => { if (live) setState({ key, value: { status: 'failed', message: errorMessage(err) } }) },
    )
    return () => { live = false }
    // load is a new function on every render; the key decides what is listed
  }, [key])
  if (!key) return null
  return state && state.key === key ? state.value : { status: 'loading' }
}

export const NOT_VISIBLE = '(not visible to this key)'

/** A folder by its path, or its id marked as one the key cannot see. */
export function folderLabel(list: KM2FolderList, id: string): string {
  const f = list.folders.find((x) => x.id === id.toLowerCase())
  return f ? f.path || f.name : `${id} ${NOT_VISIBLE}`
}

export function isListed(list: KM2FolderList, id: string): boolean {
  return list.folders.some((x) => x.id === id.toLowerCase())
}

/** Folder ids in a comma-separated setting, without blanks or repeats. */
export function splitIds(value: string): string[] {
  const out: string[] = []
  for (const id of value.split(/[\s,]+/)) if (id && !out.includes(id)) out.push(id)
  return out
}
