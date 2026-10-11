import { useEffect, useState } from 'react'
import { areasApi } from '../api/endpoints'
import type { Area } from '../api/types'

// Areas change rarely; one shared fetch serves every picker and chip on the page.
let cache: Promise<Area[]> | null = null

/** Forget the cached list (call after creating, renaming or deleting an area). */
export function invalidateAreas(): void {
  cache = null
}

export type AreasState = { areas: Area[]; byId: Map<string, Area>; loading: boolean; error: unknown }

export function useAreas(): AreasState {
  const [state, setState] = useState<AreasState>({ areas: [], byId: new Map(), loading: true, error: undefined })
  useEffect(() => {
    let alive = true
    if (!cache) cache = areasApi.list()
    const pending = cache
    pending
      .then((areas) => {
        if (alive) setState({ areas, byId: new Map(areas.map((a) => [a.id, a])), loading: false, error: undefined })
      })
      .catch((error: unknown) => {
        if (cache === pending) cache = null
        if (alive) setState((s) => ({ ...s, loading: false, error }))
      })
    return () => {
      alive = false
    }
  }, [])
  return state
}
