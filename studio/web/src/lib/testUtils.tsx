import { cleanup, render } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, useLocation, type RouteObject } from 'react-router'
import { afterEach } from 'vitest'
import type { Area, AuthStatus, Me } from '../api/types'

/** Vitest runs with globals off, so testing-library's automatic cleanup is not registered. */
export function autoCleanup(): void {
  afterEach(() => {
    cleanup()
  })
}

export function makeMe(over: Partial<Me> = {}): Me {
  return {
    id: 'user-1',
    kind: 'human',
    name: 'Ada Lovelace',
    email: 'ada@example.com',
    all_areas: false,
    area_ids: ['area-1'],
    permissions: [],
    via: 'session',
    ...over,
  }
}

export function makeStatus(over: Partial<AuthStatus> = {}): AuthStatus {
  return { setup_required: false, local_login: true, providers: [], ...over }
}

export function makeArea(over: Partial<Area> & { id: string }): Area {
  const ts = '2026-01-01T00:00:00Z'
  return { name: over.id, description: '', created_at: ts, updated_at: ts, ...over }
}

/** Shows the router's current location so tests can assert on redirects. */
export function LocationProbe() {
  const loc = useLocation()
  return <div data-testid="location">{`${loc.pathname}${loc.search}`}</div>
}

/** Data-router render (needed for useBlocker and friends). Returns the router for navigation. */
export function renderDataRouter(routes: RouteObject[], initialEntries: string[] = ['/']) {
  const router = createMemoryRouter(routes, { initialEntries })
  const result = render(<RouterProvider router={router} />)
  return { router, ...result }
}
