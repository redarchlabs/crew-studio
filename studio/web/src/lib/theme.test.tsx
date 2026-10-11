import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, useTheme } from './theme'
import { autoCleanup } from './testUtils'

autoCleanup()

function Probe() {
  const { theme, toggle } = useTheme()
  return <button onClick={toggle}>{theme}</button>
}

beforeEach(() => {
  localStorage.clear()
  delete document.documentElement.dataset.theme
})
afterEach(() => vi.restoreAllMocks())

describe('ThemeProvider', () => {
  it('defaults to dark and applies it to <html>', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>)
    expect(screen.getByRole('button').textContent).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('crew-studio.theme')).toBe('dark')
  })

  it('restores a stored light theme', () => {
    localStorage.setItem('crew-studio.theme', 'light')
    render(<ThemeProvider><Probe /></ThemeProvider>)
    expect(screen.getByRole('button').textContent).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('treats an unknown stored value as dark', () => {
    localStorage.setItem('crew-studio.theme', 'purple')
    render(<ThemeProvider><Probe /></ThemeProvider>)
    expect(screen.getByRole('button').textContent).toBe('dark')
  })

  it('toggles between dark and light and persists the choice', () => {
    render(<ThemeProvider><Probe /></ThemeProvider>)
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByRole('button').textContent).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem('crew-studio.theme')).toBe('light')
    fireEvent.click(screen.getByRole('button'))
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('still works when storage throws on read and write', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked')
    })
    render(<ThemeProvider><Probe /></ThemeProvider>)
    expect(screen.getByRole('button').textContent).toBe('dark')
    act(() => fireEvent.click(screen.getByRole('button')))
    expect(document.documentElement.dataset.theme).toBe('light')
  })

  it('useTheme outside a provider returns a harmless default', () => {
    render(<Probe />)
    expect(screen.getByRole('button').textContent).toBe('dark')
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByRole('button').textContent).toBe('dark')
  })
})
