import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { SetupPage } from './SetupPage'
import { api, apiError, auth, makeMe, makeStatus, renderRoute, resetAll, setAuth } from '../admin/testUtils'

vi.mock('../../api/endpoints', async () => (await import('../admin/testUtils')).endpointsMock)
vi.mock('../../lib/auth', async () => (await import('../admin/testUtils')).authMock)

const GOOD_PW = 'correct horse battery'

function renderSetup(search = '') {
  return renderRoute(<SetupPage />, { path: '/setup', url: `/setup${search}`, extra: ['/', '/login'] })
}

function type(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } })
}

function fillAll(over: Partial<Record<'name' | 'email' | 'password' | 'confirm' | 'token', string>> = {}) {
  const v = { name: 'Ada Admin', email: 'ada@example.com', password: GOOD_PW, confirm: GOOD_PW, ...over }
  if (over.token !== undefined) type('Setup token', over.token)
  type('Your name', v.name)
  type('Email', v.email)
  type('Password', v.password)
  type('Confirm password', v.confirm)
}

const submit = () => fireEvent.click(screen.getByRole('button', { name: 'Create administrator' }))

beforeEach(() => {
  resetAll()
  setAuth({ me: null, status: makeStatus({ setup_required: true }) })
})

describe('SetupPage states', () => {
  it('shows loading while the status is unknown', () => {
    setAuth({ status: null })
    renderSetup()
    expect(screen.getByText('Loading…')).toBeTruthy()
  })

  it('says it is signing you in while reloading after setup', () => {
    setAuth({ me: makeMe(), loading: true })
    renderSetup()
    expect(screen.getByText('Signing you in…')).toBeTruthy()
  })

  it('shows an unreachable-server error with retry', () => {
    setAuth({ status: null, error: apiError(502, 'bad gateway') })
    renderSetup()
    expect(screen.getByText('bad gateway')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(auth.reload).toHaveBeenCalled()
  })

  it('sends signed-out visitors to login once setup is done', () => {
    setAuth({ status: makeStatus({ setup_required: false }) })
    renderSetup()
    expect(screen.getByTestId('location').textContent).toBe('/login')
  })

  it('sends signed-in people to the app once setup is done', () => {
    setAuth({ me: makeMe(), status: makeStatus({ setup_required: false }) })
    renderSetup()
    expect(screen.getByTestId('location').textContent).toBe('/')
  })
})

describe('SetupPage token', () => {
  it('uses the token from the link and does not ask for it', () => {
    renderSetup('?token=abc123')
    expect(screen.getByText('Setup token found in the link.')).toBeTruthy()
    expect(screen.queryByLabelText('Setup token')).toBeNull()
  })

  it('asks for the token when the link has none', () => {
    renderSetup()
    expect(screen.getByLabelText('Setup token')).toBeTruthy()
  })

  it('submits the URL token', async () => {
    api.authApi.setup.mockResolvedValue(makeMe())
    renderSetup('?token=abc123')
    fillAll()
    submit()
    await waitFor(() => expect(api.authApi.setup).toHaveBeenCalled())
    expect(api.authApi.setup).toHaveBeenCalledWith({ token: 'abc123', email: 'ada@example.com', name: 'Ada Admin', password: GOOD_PW })
  })

  it('requires a typed token when none came from the link', () => {
    renderSetup()
    fillAll()
    submit()
    expect(screen.getByText('Paste the setup token from the service log')).toBeTruthy()
    expect(api.authApi.setup).not.toHaveBeenCalled()
  })

  it('trims whitespace around typed values but not the password', async () => {
    api.authApi.setup.mockResolvedValue(makeMe())
    renderSetup()
    fillAll({ token: '  tok  ', name: '  Ada  ', email: '  ada@example.com ', password: `${GOOD_PW} `, confirm: `${GOOD_PW} ` })
    submit()
    await waitFor(() => expect(api.authApi.setup).toHaveBeenCalledWith({ token: 'tok', email: 'ada@example.com', name: 'Ada', password: `${GOOD_PW} ` }))
  })

  it('reveals the token field again when the server rejects the link token (expired)', async () => {
    api.authApi.setup.mockRejectedValue(apiError(401, 'token expired', 'invalid_token'))
    renderSetup('?token=old')
    fillAll()
    submit()
    expect((await screen.findByRole('alert')).textContent).toMatch(/setup token is not valid/)
    const field = screen.getByLabelText('Setup token') as HTMLInputElement
    expect(field.value).toBe('old')
    expect(screen.queryByText('Setup token found in the link.')).toBeNull()
  })
})

describe('SetupPage validation', () => {
  it('shows every missing field without calling the API', () => {
    renderSetup('?token=t')
    submit()
    expect(screen.getByText('Enter your name')).toBeTruthy()
    expect(screen.getByText('Enter a valid email address')).toBeTruthy()
    expect(screen.getByText('Use at least 12 characters')).toBeTruthy()
    expect(api.authApi.setup).not.toHaveBeenCalled()
  })

  it('rejects a short password', () => {
    renderSetup('?token=t')
    fillAll({ password: 'short', confirm: 'short' })
    submit()
    expect(screen.getByText('Use at least 12 characters')).toBeTruthy()
    expect(api.authApi.setup).not.toHaveBeenCalled()
  })

  it('rejects a password over the 72 character limit', () => {
    const long = 'x'.repeat(73)
    renderSetup('?token=t')
    fillAll({ password: long, confirm: long })
    submit()
    expect(screen.getByText('Use at most 72 characters')).toBeTruthy()
    expect(api.authApi.setup).not.toHaveBeenCalled()
  })

  it('accepts passwords at the exact limits', async () => {
    api.authApi.setup.mockResolvedValue(makeMe())
    const pw = 'y'.repeat(72)
    renderSetup('?token=t')
    fillAll({ password: pw, confirm: pw })
    submit()
    await waitFor(() => expect(api.authApi.setup).toHaveBeenCalled())
  })

  it('rejects a mismatched confirmation', () => {
    renderSetup('?token=t')
    fillAll({ confirm: `${GOOD_PW}!` })
    submit()
    expect(screen.getByText('Passwords do not match')).toBeTruthy()
    expect(api.authApi.setup).not.toHaveBeenCalled()
  })

  it('rejects a malformed email', () => {
    renderSetup('?token=t')
    fillAll({ email: 'not-an-email' })
    submit()
    expect(screen.getByText('Enter a valid email address')).toBeTruthy()
  })
})

describe('SetupPage outcome', () => {
  it('on success stores the principal and refreshes the sign-in status', async () => {
    const me = makeMe({ name: 'Ada Admin' })
    api.authApi.setup.mockResolvedValue(me)
    renderSetup('?token=t')
    fillAll()
    submit()
    await waitFor(() => expect(auth.setMe).toHaveBeenCalledWith(me))
    expect(auth.reload).toHaveBeenCalledTimes(1)
  })

  it.each([
    [apiError(403, 'forbidden'), /setup token is not valid/],
    [apiError(409, 'exists'), /Setup has already been completed/],
    [apiError(429, 'rate'), /Too many attempts/],
    [apiError(422, 'invalid', 'validation', ['password: too common', 'email: taken']), /password: too common · email: taken/],
    [apiError(500, 'Kaput'), /Kaput/],
    [new Error('network down'), /network down/],
  ])('reports %o', async (err, text) => {
    api.authApi.setup.mockRejectedValue(err)
    renderSetup('?token=t')
    fillAll()
    submit()
    expect((await screen.findByRole('alert')).textContent).toMatch(text)
    expect(auth.setMe).not.toHaveBeenCalled()
    expect((screen.getByRole('button', { name: 'Create administrator' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('keeps the entered values after a server error so the person can retry', async () => {
    api.authApi.setup.mockRejectedValue(apiError(429, 'rate'))
    renderSetup('?token=t')
    fillAll()
    submit()
    await screen.findByRole('alert')
    expect((screen.getByLabelText('Email') as HTMLInputElement).value).toBe('ada@example.com')
  })
})
