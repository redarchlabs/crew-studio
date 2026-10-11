import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router'

const reloadOnce = vi.fn()
vi.mock('../lib/staleChunk', async (orig) => ({
  ...(await orig<typeof import('../lib/staleChunk')>()),
  reloadOnceForStaleChunk: () => reloadOnce(),
}))

const { RouteError } = await import('./RouteError')

function renderThrowing(error: unknown) {
  const Boom = () => { throw error }
  const router = createMemoryRouter([{ path: '/', element: <Boom />, errorElement: <RouteError /> }])
  render(<RouterProvider router={router} />)
}

afterEach(() => {
  cleanup()
  reloadOnce.mockReset()
})

describe('RouteError', () => {
  it('reloads once when a page chunk from an older build fails to load', () => {
    reloadOnce.mockReturnValue(true)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderThrowing(new TypeError('Failed to fetch dynamically imported module: http://x/assets/WorkOrderPage-old.js'))
    expect(reloadOnce).toHaveBeenCalledOnce()
    expect(screen.getByText('Crew Studio was updated. Loading the new version…')).toBeTruthy()
  })

  it('shows the error and a Reload button when it already reloaded recently', () => {
    reloadOnce.mockReturnValue(false)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderThrowing(new TypeError('Failed to fetch dynamically imported module: http://x/a.js'))
    expect(screen.getByText('Crew Studio was updated, and this page could not load')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Reload the page' })).toBeTruthy()
  })

  it('shows other errors without reloading', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    renderThrowing(new Error('boom'))
    expect(reloadOnce).not.toHaveBeenCalled()
    expect(screen.getByText('Something went wrong on this page')).toBeTruthy()
    expect(screen.getByText('boom')).toBeTruthy()
  })
})
