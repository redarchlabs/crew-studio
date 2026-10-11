import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { LoginPage } from './LoginPage'
import { api, apiError, auth, makeMe, makeStatus, renderRoute, resetAll, setAuth, stubLocationAssign } from '../admin/testUtils'

vi.mock('../../api/endpoints', async () => (await import('../admin/testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('../admin/testUtils')).authMock)

const MS = { key: 'microsoft', label: 'Microsoft Entra' } as const
const CLERK = { key: 'clerk', label: 'Clerk' } as const

function renderLogin(search = '') {
  return renderRoute(<LoginPage />, { path: '/login', url: `/login${search}`, extra: ['/', '/setup', '/orders/5'] })
}

function fill(email: string, password: string) {
  fireEvent.change(screen.getByLabelText('Email'), { target: { value: email } })
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: password } })
}

const submit = () => fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))

beforeEach(() => {
  resetAll()
  setAuth({ me: null })
})

describe('LoginPage states', () => {
  it('shows a loading message while the status is unknown', () => {
    setAuth({ status: null, loading: true })
    renderLogin()
    expect(screen.getByText('Checking sign-in options…')).toBeTruthy()
  })

  it('falls back to a generic loading label when not loading and no status', () => {
    setAuth({ status: null, loading: false })
    renderLogin()
    expect(screen.getByText('Loading…')).toBeTruthy()
  })

  it('shows the error with a retry that reloads', () => {
    setAuth({ status: null, error: apiError(500, 'database down') })
    renderLogin()
    expect(screen.getByText('database down')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(auth.reload).toHaveBeenCalledTimes(1)
  })

  it('redirects to setup when first-run setup is required', () => {
    setAuth({ status: makeStatus({ setup_required: true }) })
    renderLogin()
    expect(screen.getByTestId('location').textContent).toBe('/setup')
  })

  it('redirects an already signed-in person to returnTo', () => {
    setAuth({ me: makeMe(), status: makeStatus() })
    renderLogin('?return_to=%2Forders%2F5')
    expect(screen.getByTestId('location').textContent).toBe('/orders/5')
  })

  it('tells the person when no sign-in method is configured', () => {
    setAuth({ status: makeStatus({ local_login: false, providers: [] }) })
    renderLogin()
    expect(screen.getByText(/No sign-in method is configured yet/)).toBeTruthy()
    expect(screen.queryByLabelText('Password')).toBeNull()
  })
})

describe('LoginPage SSO', () => {
  it('shows one button per enabled provider with friendly names', () => {
    setAuth({ status: makeStatus({ providers: [MS, CLERK] }) })
    renderLogin()
    expect(screen.getByRole('button', { name: 'Continue with Microsoft' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Continue with Clerk' })).toBeTruthy()
  })

  it('shows only the providers that are enabled', () => {
    setAuth({ status: makeStatus({ providers: [CLERK], local_login: false }) })
    renderLogin()
    expect(screen.queryByRole('button', { name: 'Continue with Microsoft' })).toBeNull()
    expect(screen.queryByText('Administrator sign-in')).toBeNull()
  })

  it('starts the provider sign-in as a full-page navigation carrying the sanitised returnTo', () => {
    const assign = stubLocationAssign()
    setAuth({ status: makeStatus({ providers: [MS, CLERK] }) })
    renderLogin('?return_to=%2Forders%2F5%3Fx%3D1')
    fireEvent.click(screen.getByRole('button', { name: 'Continue with Microsoft' }))
    expect(api.authApi.ssoLoginUrl).toHaveBeenCalledWith('microsoft', '/orders/5?x=1')
    expect(assign).toHaveBeenCalledWith('/api/auth/microsoft/login?return_to=%2Forders%2F5%3Fx%3D1')
    // Once leaving, every provider button is locked so a second click cannot start another sign-in
    expect((screen.getByRole('button', { name: 'Continue with Clerk' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it.each([
    ['//evil.example', '/'],
    ['https://evil.example/', '/'],
    ['/\\evil.example', '/'],
    ['/login', '/'],
  ])('never forwards the hostile returnTo %s to the provider', (raw, expected) => {
    const assign = stubLocationAssign()
    setAuth({ status: makeStatus({ providers: [MS] }) })
    renderLogin(`?return_to=${encodeURIComponent(raw)}`)
    fireEvent.click(screen.getByRole('button', { name: 'Continue with Microsoft' }))
    expect(api.authApi.ssoLoginUrl).toHaveBeenCalledWith('microsoft', expected)
    expect(assign).toHaveBeenCalledTimes(1)
  })

  it('folds the administrator form away under SSO buttons and expands on demand', () => {
    setAuth({ status: makeStatus({ providers: [MS] }) })
    renderLogin()
    expect(screen.queryByLabelText('Password')).toBeNull()
    const toggle = screen.getByRole('button', { name: /Administrator sign-in/ })
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('Password')).toBeTruthy()
    fireEvent.click(toggle)
    expect(screen.queryByLabelText('Password')).toBeNull()
  })

  it('hides the password form completely when local login is disabled', () => {
    setAuth({ status: makeStatus({ providers: [MS], local_login: false }) })
    renderLogin()
    expect(screen.queryByLabelText('Password')).toBeNull()
    expect(screen.queryByText(/Administrator sign-in/)).toBeNull()
  })
})

describe('LoginPage SSO callback errors', () => {
  it.each([
    ['not_invited', /hasn't been set up yet/],
    ['disabled', /has been disabled/],
    ['unverified_email', /not verified your email/],
    ['expired', /took too long/],
    ['provider_error', /reported a problem/],
    ['something_new', /did not complete/],
  ])('maps ?error=%s to a message', (code, text) => {
    setAuth({ status: makeStatus({ providers: [MS] }) })
    renderLogin(`?error=${code}`)
    expect(screen.getByRole('alert').textContent).toMatch(text)
  })

  it('shows no alert without an error parameter', () => {
    setAuth({ status: makeStatus({ providers: [MS] }) })
    renderLogin()
    expect(screen.queryByRole('alert')).toBeNull()
  })
})

describe('LoginPage password sign-in', () => {
  it('signs in, stores the principal and goes to returnTo', async () => {
    const me = makeMe()
    api.authApi.login.mockResolvedValue(me)
    renderLogin('?return_to=%2Forders%2F5')
    fill('  admin@example.com  ', 's3cret-pass-phrase')
    submit()
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/orders/5'))
    expect(api.authApi.login).toHaveBeenCalledWith('admin@example.com', 's3cret-pass-phrase')
    expect(auth.setMe).toHaveBeenCalledWith(me)
  })

  it('goes to / when returnTo is absent', async () => {
    api.authApi.login.mockResolvedValue(makeMe())
    renderLogin()
    fill('a@b.co', 'pw')
    submit()
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/'))
  })

  it('refuses to submit empty credentials without calling the API', () => {
    renderLogin()
    submit()
    expect(screen.getByRole('alert').textContent).toBe('Enter your email and password.')
    expect(api.authApi.login).not.toHaveBeenCalled()
  })

  it('treats a whitespace-only email as empty', () => {
    renderLogin()
    fill('   ', 'pw')
    submit()
    expect(api.authApi.login).not.toHaveBeenCalled()
  })

  it('does not trim the password', async () => {
    api.authApi.login.mockResolvedValue(makeMe())
    renderLogin()
    fill('a@b.co', ' pw ')
    submit()
    await waitFor(() => expect(api.authApi.login).toHaveBeenCalledWith('a@b.co', ' pw '))
  })

  it.each([
    [apiError(401, 'nope', 'invalid_credentials'), /don't match an administrator account/],
    [apiError(423, 'locked', 'locked'), /locked for 15 minutes/],
    [apiError(429, 'slow down'), /Too many sign-in attempts from this network/],
    [apiError(403, 'forbidden'), /only for administrators/],
    [apiError(500, 'Server exploded'), /Server exploded/],
    [new Error('offline'), /offline/],
  ])('shows a friendly message for %o', async (err, text) => {
    api.authApi.login.mockRejectedValue(err)
    renderLogin()
    fill('a@b.co', 'pw')
    submit()
    expect((await screen.findByRole('alert')).textContent).toMatch(text)
    expect(auth.setMe).not.toHaveBeenCalled()
    // The form is usable again for a retry
    expect((screen.getByRole('button', { name: 'Sign in' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('does not reveal whether the account or the password was wrong', async () => {
    api.authApi.login.mockRejectedValue(apiError(401, 'user not found', 'invalid_credentials'))
    renderLogin()
    fill('ghost@example.com', 'pw')
    submit()
    const text = (await screen.findByRole('alert')).textContent ?? ''
    expect(text).not.toMatch(/not found|no such|unknown user/i)
  })

  it('disables the button while the request is in flight', async () => {
    let resolve!: (m: unknown) => void
    api.authApi.login.mockReturnValue(new Promise((r) => { resolve = r }))
    renderLogin()
    fill('a@b.co', 'pw')
    submit()
    await waitFor(() => expect((screen.getByRole('button', { name: 'Sign in' }) as HTMLButtonElement).disabled).toBe(true))
    resolve(makeMe())
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/'))
  })

  it('clears a previous error when retrying', async () => {
    api.authApi.login.mockRejectedValueOnce(apiError(401, 'x', 'invalid_credentials')).mockResolvedValueOnce(makeMe())
    renderLogin()
    fill('a@b.co', 'bad')
    submit()
    await screen.findByRole('alert')
    fill('a@b.co', 'good')
    submit()
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })
})
