import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router'
import { ApiError, UNAUTHENTICATED_EVENT } from '../api/client'
import { authApi } from '../api/endpoints'
import type { AuthStatus, Me, Permission } from '../api/types'
import { ErrorState, Loading } from '../components/States'

type AuthState = {
  me: Me | null
  status: AuthStatus | null
  /** True while the first /auth/status + /auth/me round trip is in flight */
  loading: boolean
  error: unknown
  can: (permission: Permission) => boolean
  canAny: (...permissions: Permission[]) => boolean
  /** Whether an item in these areas is visible/editable to the signed-in person */
  inMyAreas: (areaIds: string[]) => boolean
  setMe: (me: Me | null) => void
  reload: () => void
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [status, setStatus] = useState<AuthStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(undefined)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    const ctrl = new AbortController()
    setLoading(true)
    setError(undefined)
    Promise.all([
      authApi.status(ctrl.signal),
      authApi.me(ctrl.signal).catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) return null
        throw err
      }),
    ])
      .then(([s, m]) => {
        if (ctrl.signal.aborted) return
        setStatus(s)
        setMe(m)
      })
      .catch((err: unknown) => {
        if (!ctrl.signal.aborted) setError(err)
      })
      .finally(() => {
        if (!ctrl.signal.aborted) setLoading(false)
      })
    return () => ctrl.abort()
  }, [tick])

  // Any 401 from the API means the session ended: forget the person so guarded routes go to /login
  useEffect(() => {
    const onUnauthenticated = () => setMe(null)
    window.addEventListener(UNAUTHENTICATED_EVENT, onUnauthenticated)
    return () => window.removeEventListener(UNAUTHENTICATED_EVENT, onUnauthenticated)
  }, [])

  const reload = useCallback(() => setTick((t) => t + 1), [])
  const logout = useCallback(async () => {
    try {
      await authApi.logout()
    } finally {
      setMe(null)
    }
  }, [])

  const value = useMemo<AuthState>(() => {
    const perms = new Set(me?.permissions ?? [])
    return {
      me,
      status,
      loading,
      error,
      can: (p) => perms.has(p),
      canAny: (...ps) => ps.some((p) => perms.has(p)),
      inMyAreas: (areaIds) => !!me && (me.all_areas || areaIds.some((id) => me.area_ids.includes(id))),
      setMe,
      reload,
      logout,
    }
  }, [me, status, loading, error, reload, logout])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}

/** Renders children only when the signed-in person holds the permission (or any of several). */
export function Can({ perm, any, children, fallback = null }: {
  perm?: Permission
  any?: Permission[]
  children: ReactNode
  fallback?: ReactNode
}) {
  const { can, canAny } = useAuth()
  const allowed = perm ? can(perm) : any ? canAny(...any) : false
  return <>{allowed ? children : fallback}</>
}

/** Route element: the app shell's children render only for a signed-in person. */
export function RequireAuth() {
  const { me, status, loading, error, reload } = useAuth()
  const location = useLocation()
  if (loading && !me) return <Loading label="Signing in…" />
  if (error && !me) return <ErrorState error={error} onRetry={reload} title="Cannot reach Crew Studio" />
  if (status?.setup_required) return <Navigate to="/setup" replace />
  if (!me) {
    const returnTo = `${location.pathname}${location.search}`
    return <Navigate to={`/login?return_to=${encodeURIComponent(returnTo)}`} replace />
  }
  return <Outlet />
}

/** Wraps a page that needs a permission; shows a clear "no access" state instead of a broken page. */
export function RequirePermission({ perm, any, children }: { perm?: Permission; any?: Permission[]; children: ReactNode }) {
  const { can, canAny } = useAuth()
  const allowed = perm ? can(perm) : any ? canAny(...any) : true
  if (!allowed) return <NoAccess needs={perm ? [perm] : (any ?? [])} />
  return <>{children}</>
}

export function NoAccess({ needs }: { needs: Permission[] }) {
  const navigate = useNavigate()
  return (
    <div className="mx-auto mt-16 max-w-md rounded-lg border border-line bg-raised p-6 text-center">
      <div className="text-[15px] font-semibold">You don&apos;t have access to this page</div>
      <p className="mt-2 text-muted">
        It needs {needs.length > 1 ? 'one of these permissions' : 'the permission'}{' '}
        {needs.map((n) => (
          <code key={n} className="mx-0.5 rounded bg-panel-2 px-1 font-mono text-[12px]">{n}</code>
        ))}
        . Ask an administrator to grant it.
      </p>
      <button type="button" className="mt-4 text-accent" onClick={() => navigate('/')}>Back to overview</button>
    </div>
  )
}
