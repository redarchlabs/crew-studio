import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api/endpoints', () => ({ authApi: { status: vi.fn(), me: vi.fn(), logout: vi.fn() } }))

import { ApiError, UNAUTHENTICATED_EVENT } from '../api/client'
import { authApi } from '../api/endpoints'
import type { Permission } from '../api/types'
import { AuthProvider, Can, NoAccess, RequireAuth, RequirePermission, useAuth } from './auth'
import { LocationProbe, autoCleanup, makeMe, makeStatus } from './testUtils'

autoCleanup()
const status = authApi.status as unknown as ReturnType<typeof vi.fn>
const me = authApi.me as unknown as ReturnType<typeof vi.fn>
const logout = authApi.logout as unknown as ReturnType<typeof vi.fn>
const unauth = () => new ApiError(401, { code: 'unauthenticated', message: 'no session' })

beforeEach(() => {
  status.mockReset().mockResolvedValue(makeStatus())
  me.mockReset().mockResolvedValue(makeMe({ permissions: ['runs.view', 'crews.view'] }))
  logout.mockReset().mockResolvedValue(undefined)
})

const wrapper = ({ children }: { children: ReactNode }) => (
  <MemoryRouter>
    <AuthProvider>{children}</AuthProvider>
  </MemoryRouter>
)

async function loaded() {
  const hook = renderHook(() => useAuth(), { wrapper })
  await waitFor(() => expect(hook.result.current.loading).toBe(false))
  return hook
}

describe('AuthProvider', () => {
  it('loads status and the signed-in person', async () => {
    const { result } = renderHook(() => useAuth(), { wrapper })
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.me?.email).toBe('ada@example.com')
    expect(result.current.status?.local_login).toBe(true)
    expect(result.current.error).toBeUndefined()
  })

  it('treats a 401 from /auth/me as "not signed in", not as an error', async () => {
    me.mockRejectedValue(unauth())
    const { result } = await loaded()
    expect(result.current.me).toBeNull()
    expect(result.current.error).toBeUndefined()
    expect(result.current.status).not.toBeNull()
  })

  it('surfaces other /auth/me failures as an error', async () => {
    const boom = new ApiError(500, { code: 'internal', message: 'oops' })
    me.mockRejectedValue(boom)
    const { result } = await loaded()
    expect(result.current.error).toBe(boom)
    expect(result.current.me).toBeNull()
  })

  it('surfaces /auth/status failures as an error', async () => {
    const boom = new ApiError(0, { code: 'network_error', message: 'down' })
    status.mockRejectedValue(boom)
    const { result } = await loaded()
    expect(result.current.error).toBe(boom)
  })

  it('reload refetches and clears a previous error', async () => {
    status.mockRejectedValueOnce(new Error('first'))
    const { result } = await loaded()
    expect(result.current.error).toBeInstanceOf(Error)
    act(() => result.current.reload())
    await waitFor(() => expect(result.current.me).not.toBeNull())
    expect(result.current.error).toBeUndefined()
    expect(status).toHaveBeenCalledTimes(2)
  })

  it('forgets the person when the API reports 401 (UNAUTHENTICATED_EVENT)', async () => {
    const { result } = await loaded()
    expect(result.current.me).not.toBeNull()
    act(() => {
      window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT))
    })
    expect(result.current.me).toBeNull()
    expect(result.current.can('runs.view')).toBe(false)
  })

  it('stops listening for the event after unmount', async () => {
    const hook = await loaded()
    const remove = vi.spyOn(window, 'removeEventListener')
    hook.unmount()
    expect(remove).toHaveBeenCalledWith(UNAUTHENTICATED_EVENT, expect.any(Function))
    remove.mockRestore()
  })

  it('setMe replaces the signed-in person (login flow)', async () => {
    me.mockRejectedValue(unauth())
    const { result } = await loaded()
    act(() => result.current.setMe(makeMe({ name: 'Grace', permissions: ['agents.view'] })))
    expect(result.current.me?.name).toBe('Grace')
    expect(result.current.can('agents.view')).toBe(true)
  })

  it('logout calls the API and clears the person', async () => {
    const { result } = await loaded()
    await act(async () => {
      await result.current.logout()
    })
    expect(logout).toHaveBeenCalledTimes(1)
    expect(result.current.me).toBeNull()
  })

  it('logout clears the person even when the API call fails, and still rejects', async () => {
    logout.mockRejectedValue(new Error('network'))
    const { result } = await loaded()
    let caught: unknown
    await act(async () => {
      await result.current.logout().catch((e: unknown) => (caught = e))
    })
    expect(caught).toBeInstanceOf(Error)
    expect(result.current.me).toBeNull()
  })

  it('ignores results that arrive after unmount', async () => {
    let resolve!: (v: unknown) => void
    status.mockReturnValue(new Promise((r) => (resolve = r)))
    const err = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const { unmount } = renderHook(() => useAuth(), { wrapper })
    unmount()
    resolve(makeStatus())
    await Promise.resolve()
    expect(err).not.toHaveBeenCalled()
    err.mockRestore()
  })
})

describe('permission helpers', () => {
  it('can / canAny reflect the permission set', async () => {
    const { result } = await loaded()
    expect(result.current.can('runs.view')).toBe(true)
    expect(result.current.can('admin.users')).toBe(false)
    expect(result.current.canAny('admin.users', 'crews.view')).toBe(true)
    expect(result.current.canAny('admin.users', 'admin.roles')).toBe(false)
    expect(result.current.canAny()).toBe(false)
  })

  it('everything is denied when signed out', async () => {
    me.mockRejectedValue(unauth())
    const { result } = await loaded()
    expect(result.current.can('runs.view')).toBe(false)
    expect(result.current.canAny('runs.view')).toBe(false)
    expect(result.current.inMyAreas(['area-1'])).toBe(false)
  })

  it('inMyAreas: all_areas sees everything, others need an overlap', async () => {
    me.mockResolvedValue(makeMe({ all_areas: false, area_ids: ['a', 'b'] }))
    const scoped = await loaded()
    expect(scoped.result.current.inMyAreas(['b', 'z'])).toBe(true)
    expect(scoped.result.current.inMyAreas(['z'])).toBe(false)
    expect(scoped.result.current.inMyAreas([])).toBe(false)
    scoped.unmount()

    me.mockResolvedValue(makeMe({ all_areas: true, area_ids: [] }))
    const all = await loaded()
    expect(all.result.current.inMyAreas(['anything'])).toBe(true)
    expect(all.result.current.inMyAreas([])).toBe(true)
  })
})

describe('useAuth', () => {
  it('throws a helpful error outside the provider', () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    expect(() => renderHook(() => useAuth())).toThrow('useAuth must be used inside <AuthProvider>')
    err.mockRestore()
  })
})

function renderWithAuth(ui: ReactNode, route = '/') {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <AuthProvider>{ui}</AuthProvider>
    </MemoryRouter>,
  )
}

describe('Can', () => {
  async function renderCan(props: { perm?: Permission; any?: Permission[]; fallback?: ReactNode }) {
    renderWithAuth(<Can {...props}><span>secret</span></Can>)
    await waitFor(() => expect(status).toHaveBeenCalled())
    await act(async () => {})
  }
  it('renders children when the permission is held', async () => {
    await renderCan({ perm: 'runs.view' })
    expect(screen.getByText('secret')).toBeTruthy()
  })
  it('hides children (or shows the fallback) when it is not', async () => {
    await renderCan({ perm: 'admin.users', fallback: <span>nope</span> })
    expect(screen.queryByText('secret')).toBeNull()
    expect(screen.getByText('nope')).toBeTruthy()
  })
  it('supports "any of" permissions', async () => {
    await renderCan({ any: ['admin.users', 'crews.view'] })
    expect(screen.getByText('secret')).toBeTruthy()
  })
  it('denies when neither perm nor any is given', async () => {
    await renderCan({})
    expect(screen.queryByText('secret')).toBeNull()
  })
})

function guardedApp(route: string) {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <AuthProvider>
        <LocationProbe />
        <Routes>
          <Route path="/login" element={<div>login page</div>} />
          <Route path="/setup" element={<div>setup page</div>} />
          <Route element={<RequireAuth />}>
            <Route path="/runs" element={<div>runs page</div>} />
          </Route>
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  )
}

describe('RequireAuth', () => {
  it('shows a loading status first, then the protected page', async () => {
    guardedApp('/runs')
    expect(screen.getByRole('status').textContent).toContain('Signing in')
    expect(await screen.findByText('runs page')).toBeTruthy()
  })

  it('redirects signed-out visitors to /login with an encoded return_to', async () => {
    me.mockRejectedValue(unauth())
    guardedApp('/runs?status=failed&x=1')
    await screen.findByText('login page')
    expect(screen.getByTestId('location').textContent).toBe(`/login?return_to=${encodeURIComponent('/runs?status=failed&x=1')}`)
  })

  it('redirects to /setup when first-run setup is required (even if signed out)', async () => {
    status.mockResolvedValue(makeStatus({ setup_required: true }))
    me.mockRejectedValue(unauth())
    guardedApp('/runs')
    await screen.findByText('setup page')
    expect(screen.getByTestId('location').textContent).toBe('/setup')
  })

  it('shows an error state with retry when the API is unreachable, and recovers', async () => {
    status.mockRejectedValueOnce(new ApiError(0, { code: 'network_error', message: 'Cannot reach it' }))
    guardedApp('/runs')
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Cannot reach Crew Studio')
    expect(alert.textContent).toContain('Cannot reach it')
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(await screen.findByText('runs page')).toBeTruthy()
  })

  it('does not bounce a signed-in person to /login while a reload is in flight', async () => {
    guardedApp('/runs')
    await screen.findByText('runs page')
    expect(screen.getByTestId('location').textContent).toBe('/runs')
  })

  it('sends the person to /login when the session ends mid-visit', async () => {
    guardedApp('/runs')
    await screen.findByText('runs page')
    act(() => {
      window.dispatchEvent(new Event(UNAUTHENTICATED_EVENT))
    })
    await screen.findByText('login page')
    expect(screen.getByTestId('location').textContent).toContain('return_to=%2Fruns')
  })
})

describe('RequirePermission / NoAccess', () => {
  async function renderGuard(props: { perm?: Permission; any?: Permission[] }) {
    renderWithAuth(<RequirePermission {...props}><div>page body</div></RequirePermission>)
    await act(async () => {})
    await waitFor(() => expect(status).toHaveBeenCalled())
    await act(async () => {})
  }

  it('renders children when the single permission is held', async () => {
    await renderGuard({ perm: 'runs.view' })
    expect(screen.getByText('page body')).toBeTruthy()
  })

  it('renders children when any of several is held', async () => {
    await renderGuard({ any: ['admin.users', 'crews.view'] })
    expect(screen.getByText('page body')).toBeTruthy()
  })

  it('renders children when no requirement is given', async () => {
    await renderGuard({})
    expect(screen.getByText('page body')).toBeTruthy()
  })

  it('shows a no-access state naming the single missing permission', async () => {
    await renderGuard({ perm: 'admin.users' })
    expect(screen.queryByText('page body')).toBeNull()
    expect(screen.getByText(/don.t have access/i)).toBeTruthy()
    expect(screen.getByText('admin.users').tagName).toBe('CODE')
    expect(document.body.textContent).toContain('the permission')
  })

  it('lists all alternatives when none of "any" are held', async () => {
    await renderGuard({ any: ['admin.users', 'admin.roles'] })
    expect(screen.queryByText('page body')).toBeNull()
    expect(document.body.textContent).toContain('one of these permissions')
    expect(screen.getByText('admin.users')).toBeTruthy()
    expect(screen.getByText('admin.roles')).toBeTruthy()
  })

  it('"Back to overview" navigates home', () => {
    render(
      <MemoryRouter initialEntries={['/denied']}>
        <LocationProbe />
        <NoAccess needs={['admin.audit']} />
      </MemoryRouter>,
    )
    expect(screen.getByTestId('location').textContent).toBe('/denied')
    fireEvent.click(screen.getByRole('button', { name: 'Back to overview' }))
    expect(screen.getByTestId('location').textContent).toBe('/')
  })
})
