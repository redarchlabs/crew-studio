import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Modal } from './Modal'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('Modal', () => {
  it('renders an accessible modal dialog with a title, body and footer', () => {
    render(<Modal title="Delete agent" onClose={() => undefined} footer={<button>Confirm</button>}><p>Are you sure?</p></Modal>)
    const dialog = screen.getByRole('dialog')
    expect(dialog.getAttribute('aria-modal')).toBe('true')
    expect(screen.getByRole('heading', { name: 'Delete agent' })).toBeTruthy()
    expect(screen.getByText('Are you sure?')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeTruthy()
  })

  it('omits the footer when none is given', () => {
    const { container } = render(<Modal title="T" onClose={() => undefined}>body</Modal>)
    expect(container.querySelector('.border-t')).toBeNull()
  })

  it('applies a custom width class', () => {
    render(<Modal title="T" width="max-w-3xl" onClose={() => undefined}>b</Modal>)
    expect(screen.getByRole('dialog').className).toContain('max-w-3xl')
  })

  it('closes on Escape, ignoring other keys', () => {
    const onClose = vi.fn()
    render(<Modal title="T" onClose={onClose}>b</Modal>)
    fireEvent.keyDown(document, { key: 'Enter' })
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes from the Close button', () => {
    const onClose = vi.fn()
    render(<Modal title="T" onClose={onClose}>b</Modal>)
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes when the backdrop itself is pressed, but not when the panel is', () => {
    const onClose = vi.fn()
    render(<Modal title="T" onClose={onClose}>b</Modal>)
    fireEvent.mouseDown(screen.getByRole('dialog'))
    fireEvent.mouseDown(screen.getByText('b'))
    expect(onClose).not.toHaveBeenCalled()
    fireEvent.mouseDown(screen.getByRole('dialog').parentElement!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('moves focus to the first form control, skipping the Close button', () => {
    render(
      <Modal title="T" onClose={() => undefined}>
        <input aria-label="first" />
        <button>second</button>
      </Modal>,
    )
    expect(document.activeElement).toBe(screen.getByLabelText('first'))
  })

  it('focuses the first non-close button when there is no input', () => {
    render(<Modal title="T" onClose={() => undefined}><button>Action</button></Modal>)
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Action' }))
  })

  it('stops listening for Escape after unmount', () => {
    const onClose = vi.fn()
    const { unmount } = render(<Modal title="T" onClose={onClose}>b</Modal>)
    unmount()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})

describe('Modal focus stability', () => {
  // A parent that re-renders with a new inline onClose (typing in a field that updates its state) must not move focus
  it('does not steal focus when the parent re-renders with a new onClose', () => {
    const { rerender } = render(
      <Modal title="T" onClose={() => undefined}>
        <input aria-label="first" />
        <input aria-label="second" />
      </Modal>,
    )
    screen.getByLabelText('second').focus()
    rerender(
      <Modal title="T" onClose={() => undefined}>
        <input aria-label="first" />
        <input aria-label="second" />
      </Modal>,
    )
    expect(document.activeElement).toBe(screen.getByLabelText('second'))
  })

  it('Escape calls the latest onClose after a re-render', () => {
    const first = vi.fn()
    const latest = vi.fn()
    const { rerender } = render(<Modal title="T" onClose={first}><input aria-label="a" /></Modal>)
    rerender(<Modal title="T" onClose={latest}><input aria-label="a" /></Modal>)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(first).not.toHaveBeenCalled()
    expect(latest).toHaveBeenCalledTimes(1)
  })
})
