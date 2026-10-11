import { act, fireEvent, screen } from '@testing-library/react'
import { Link } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SKIP_GUARD, useUnsavedGuard } from './useUnsavedGuard'
import { autoCleanup, renderDataRouter } from './testUtils'

autoCleanup()

function Page({ dirty }: { dirty: boolean }) {
  useUnsavedGuard(dirty)
  return (
    <div>
      <span>editor</span>
      <Link to="/other">go other</Link>
      <Link to="/editor?tab=2">same path</Link>
      <Link to="/other" state={SKIP_GUARD}>go skipping</Link>
    </div>
  )
}

const routes = (dirty: boolean) => [
  { path: '/editor', element: <Page dirty={dirty} /> },
  { path: '/other', element: <div>other page</div> },
]

let confirm: ReturnType<typeof vi.fn>
beforeEach(() => {
  confirm = vi.fn()
  vi.stubGlobal('confirm', confirm)
})
afterEach(() => vi.unstubAllGlobals())

describe('useUnsavedGuard', () => {
  it('lets a clean form navigate without asking', () => {
    const { router } = renderDataRouter(routes(false), ['/editor'])
    fireEvent.click(screen.getByText('go other'))
    expect(confirm).not.toHaveBeenCalled()
    expect(router.state.location.pathname).toBe('/other')
  })

  it('asks before leaving a dirty form, and stays if declined', () => {
    confirm.mockReturnValue(false)
    const { router } = renderDataRouter(routes(true), ['/editor'])
    fireEvent.click(screen.getByText('go other'))
    expect(confirm).toHaveBeenCalledWith('You have unsaved changes. Leave without saving?')
    expect(router.state.location.pathname).toBe('/editor')
    expect(screen.getByText('editor')).toBeTruthy()
  })

  it('leaves when the person confirms', () => {
    confirm.mockReturnValue(true)
    const { router } = renderDataRouter(routes(true), ['/editor'])
    fireEvent.click(screen.getByText('go other'))
    expect(router.state.location.pathname).toBe('/other')
    expect(screen.getByText('other page')).toBeTruthy()
  })

  it('does not block navigation that only changes the query string', () => {
    const { router } = renderDataRouter(routes(true), ['/editor'])
    fireEvent.click(screen.getByText('same path'))
    expect(confirm).not.toHaveBeenCalled()
    expect(router.state.location.search).toBe('?tab=2')
  })

  it('does not block navigation flagged with SKIP_GUARD (after save/delete)', () => {
    const { router } = renderDataRouter(routes(true), ['/editor'])
    fireEvent.click(screen.getByText('go skipping'))
    expect(confirm).not.toHaveBeenCalled()
    expect(router.state.location.pathname).toBe('/other')
  })

  it('cancels beforeunload only while dirty', () => {
    const { unmount } = renderDataRouter(routes(true), ['/editor'])
    const evt = new Event('beforeunload', { cancelable: true })
    act(() => {
      window.dispatchEvent(evt)
    })
    expect(evt.defaultPrevented).toBe(true)
    unmount()
    const after = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(after)
    expect(after.defaultPrevented).toBe(false)
  })

  it('does not register beforeunload for a clean form', () => {
    renderDataRouter(routes(false), ['/editor'])
    const evt = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(evt)
    expect(evt.defaultPrevented).toBe(false)
  })

  it('exposes SKIP_GUARD as navigation state', () => {
    expect(SKIP_GUARD).toEqual({ skipUnsavedGuard: true })
  })
})
