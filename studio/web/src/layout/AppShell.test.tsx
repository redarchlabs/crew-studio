import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { lazy } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api/endpoints', () => ({ authApi: { status: vi.fn(), me: vi.fn(), logout: vi.fn() } }))
vi.mock('../api/endpoints.human', () => ({ humanApi: { mine: vi.fn(), streamUrl: '/api/questions/stream' } }))
vi.mock('sonner', () => ({ Toaster: (p: { theme?: string }) => <div data-testid="toaster" data-theme={p.theme} /> }))

import { authApi } from '../api/endpoints'
import { humanApi } from '../api/endpoints.human'
import type { Permission } from '../api/types'
import { AuthProvider } from '../lib/auth'
import { ThemeProvider } from '../lib/theme'
import { LocationProbe, autoCleanup, makeMe, makeStatus } from '../lib/testUtils'
import { AppShell } from './AppShell'

autoCleanup()
const statusApi = authApi.status as unknown as ReturnType<typeof vi.fn>
const meApi = authApi.me as unknown as ReturnType<typeof vi.fn>
const logoutApi = authApi.logout as unknown as ReturnType<typeof vi.fn>
const mineApi = humanApi.mine as unknown as ReturnType<typeof vi.fn>

beforeEach(() => {
  localStorage.clear()
  statusApi.mockReset().mockResolvedValue(makeStatus())
  meApi.mockReset()
  logoutApi.mockReset().mockResolvedValue(undefined)
  mineApi.mockReset().mockResolvedValue([])
})

async function renderShell(permissions: Permission[], route = '/', opts: { name?: string; email?: string } = {}) {
  meApi.mockResolvedValue(makeMe({ permissions, ...opts }))
  const Lazy = lazy(() => new Promise<{ default: () => React.ReactElement }>((r) => setTimeout(() => r({ default: () => <div>lazy page</div> }), 20)))
  const utils = render(
    <MemoryRouter initialEntries={[route]}>
      <ThemeProvider>
        <AuthProvider>
          <LocationProbe />
          <Routes>
            <Route element={<AppShell />}>
              <Route index element={<div>overview content</div>} />
              <Route path="runs" element={<div>runs content</div>} />
              <Route path="lazy" element={<Lazy />} />
              <Route path="*" element={<div>other content</div>} />
            </Route>
          </Routes>
        </AuthProvider>
      </ThemeProvider>
    </MemoryRouter>,
  )
  await screen.findByText(opts.name ?? 'Ada Lovelace')
  return utils
}

const sidebar = () => screen.getByRole('navigation')
const linkNames = () => within(sidebar()).getAllByRole('link').map((a) => a.textContent)

describe('AppShell navigation', () => {
  it('badges Work orders with the questions waiting on the person', async () => {
    mineApi.mockResolvedValue([{ id: 'q1' }, { id: 'q2' }])
    await renderShell(['work_orders.view'])
    const badge = await within(sidebar()).findByTitle('2 questions waiting for you')
    expect(badge.textContent).toBe('2')
  })

  it('badges Chat with the questions from hand-offs, and Work orders with the rest', async () => {
    mineApi.mockResolvedValue([{ id: 'q1' }, { id: 'q2', conversation_id: 'c1' }, { id: 'q3', conversation_id: 'c2' }])
    await renderShell(['work_orders.view', 'crews.chat'])
    expect((await within(sidebar()).findByTitle('2 questions waiting for you')).closest('a')?.textContent).toContain('Chat')
    expect(within(sidebar()).getByTitle('1 question waiting for you').closest('a')?.textContent).toContain('Work orders')
  })

  it('does not listen for questions when signed in with an API key', async () => {
    meApi.mockResolvedValue(makeMe({ permissions: ['work_orders.view'], via: 'api_key' }))
    render(<MemoryRouter><ThemeProvider><AuthProvider><Routes><Route element={<AppShell />}><Route index element={<div />} /></Route></Routes></AuthProvider></ThemeProvider></MemoryRouter>)
    await screen.findByText('Ada Lovelace')
    expect(mineApi).not.toHaveBeenCalled()
  })

  it('shows only the items the person has permission for, plus the always-visible API explorer and Help', async () => {
    await renderShell(['runs.view', 'crews.view'])
    expect(linkNames()).toEqual(['Runs', 'Crews', 'API explorer', 'Help'])
  })

  it('hides empty sections entirely (no orphan headings)', async () => {
    await renderShell(['runs.view'])
    const headings = Array.from(sidebar().querySelectorAll('.eyebrow')).map((e) => e.textContent)
    expect(headings).toEqual(['Console', 'Developers', 'Support'])
  })

  it('shows a person with no permissions only the API explorer and Help', async () => {
    await renderShell([])
    expect(linkNames()).toEqual(['API explorer', 'Help'])
  })

  it('"any of" items appear when either permission is held', async () => {
    await renderShell(['admin.audit'])
    expect(linkNames()).toContain('History')
    expect(linkNames()).toContain('Audit log')
    expect(linkNames()).not.toContain('Roles')
  })

  it('Roles appears for admin.users too, along with People', async () => {
    await renderShell(['admin.users'])
    expect(linkNames()).toEqual(expect.arrayContaining(['People', 'Roles']))
    expect(linkNames()).not.toContain('Service accounts')
  })

  it('shows everything for a person holding all permissions', async () => {
    const { PERMISSIONS } = await import('../api/types')
    const { NAV } = await import('./nav')
    await renderShell([...PERMISSIONS])
    expect(linkNames()).toEqual(NAV.flatMap((s) => s.items.map((i) => i.label)))
  })

  it('links point at their routes', async () => {
    await renderShell(['runs.view', 'overview.view'])
    expect(within(sidebar()).getByRole('link', { name: 'Runs' }).getAttribute('href')).toBe('/runs')
    expect(within(sidebar()).getByRole('link', { name: 'Overview' }).getAttribute('href')).toBe('/')
  })

  it('marks the current route active, and Overview only on exact "/"', async () => {
    await renderShell(['runs.view', 'overview.view'], '/runs')
    expect(within(sidebar()).getByRole('link', { name: 'Runs' }).getAttribute('aria-current')).toBe('page')
    expect(within(sidebar()).getByRole('link', { name: 'Overview' }).getAttribute('aria-current')).toBeNull()
  })

  it('renders the matched route inside the main region', async () => {
    await renderShell(['runs.view'], '/runs')
    expect(within(screen.getByRole('main')).getByText('runs content')).toBeTruthy()
  })

  it('shows a loading status while a lazy page loads, then the page', async () => {
    await renderShell([], '/lazy')
    expect(within(screen.getByRole('main')).getByRole('status')).toBeTruthy()
    expect(await screen.findByText('lazy page')).toBeTruthy()
  })

  it('navigating via the sidebar changes the route', async () => {
    await renderShell(['runs.view', 'overview.view'])
    fireEvent.click(within(sidebar()).getByRole('link', { name: 'Runs' }))
    expect(screen.getByTestId('location').textContent).toBe('/runs')
    expect(screen.getByText('runs content')).toBeTruthy()
  })
})

describe('UserMenu', () => {
  it('shows the person\'s name and email (with full text in titles)', async () => {
    await renderShell([], '/', { name: 'Grace Hopper', email: 'grace@navy.mil' })
    expect(screen.getByText('Grace Hopper').getAttribute('title')).toBe('Grace Hopper')
    expect(screen.getByText('grace@navy.mil').getAttribute('title')).toBe('grace@navy.mil')
  })

  it('Sign out calls the API and removes the user menu', async () => {
    await renderShell([])
    fireEvent.click(screen.getByRole('button', { name: 'Sign out' }))
    await waitFor(() => expect(logoutApi).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Sign out' })).toBeNull())
    expect(screen.queryByText('Ada Lovelace')).toBeNull()
  })

  it('renders nothing for a signed-out visitor', async () => {
    const { ApiError } = await import('../api/client')
    meApi.mockRejectedValue(new ApiError(401, { code: 'x', message: 'no' }))
    render(
      <MemoryRouter>
        <ThemeProvider>
          <AuthProvider>
            <Routes><Route element={<AppShell />}><Route index element={<div>c</div>} /></Route></Routes>
          </AuthProvider>
        </ThemeProvider>
      </MemoryRouter>,
    )
    await screen.findByText('c')
    await waitFor(() => expect(statusApi).toHaveBeenCalled())
    expect(screen.queryByRole('button', { name: 'Sign out' })).toBeNull()
  })
})

describe('theme toggle', () => {
  it('offers Light mode in dark theme and switches, updating the Toaster theme', async () => {
    await renderShell([])
    expect(screen.getByTestId('toaster').getAttribute('data-theme')).toBe('dark')
    fireEvent.click(screen.getByRole('button', { name: 'Light mode' }))
    expect(screen.getByRole('button', { name: 'Dark mode' })).toBeTruthy()
    expect(screen.getByTestId('toaster').getAttribute('data-theme')).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})

describe('mobile menu', () => {
  const aside = () => document.querySelector('aside')!

  it('starts closed (off-canvas) and has no overlay', async () => {
    await renderShell([])
    expect(aside().className).toContain('-translate-x-full')
    expect(document.querySelector('.bg-black\\/50')).toBeNull()
  })

  it('opens from the hamburger, showing an overlay', async () => {
    await renderShell([])
    fireEvent.click(screen.getByRole('button', { name: 'Open menu' }))
    expect(aside().className).toContain('translate-x-0')
    expect(aside().className).not.toContain('-translate-x-full')
    expect(document.querySelector('.bg-black\\/50')).not.toBeNull()
  })

  it('closes from the close button and by clicking the overlay', async () => {
    await renderShell([])
    fireEvent.click(screen.getByRole('button', { name: 'Open menu' }))
    fireEvent.click(screen.getByRole('button', { name: 'Close menu' }))
    expect(aside().className).toContain('-translate-x-full')
    fireEvent.click(screen.getByRole('button', { name: 'Open menu' }))
    fireEvent.click(document.querySelector('.bg-black\\/50')!)
    expect(aside().className).toContain('-translate-x-full')
  })

  it('closes after choosing a destination', async () => {
    await renderShell(['runs.view'])
    fireEvent.click(screen.getByRole('button', { name: 'Open menu' }))
    fireEvent.click(within(sidebar()).getByRole('link', { name: 'Runs' }))
    await act(async () => {})
    expect(aside().className).toContain('-translate-x-full')
  })
})
