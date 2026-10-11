import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { AsyncView, EmptyState, ErrorState, Loading } from './States'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('Loading', () => {
  it('is a status region with a default and custom label', () => {
    const { rerender } = render(<Loading />)
    expect(screen.getByRole('status').textContent).toBe('Loading…')
    rerender(<Loading label="Fetching runs" />)
    expect(screen.getByRole('status').textContent).toBe('Fetching runs')
  })
})

describe('EmptyState', () => {
  it('shows the title only by default', () => {
    render(<EmptyState title="Nothing here" />)
    expect(screen.getByText('Nothing here')).toBeTruthy()
  })
  it('shows hint, action and a custom icon', () => {
    render(<EmptyState title="T" hint="Try adding one" action={<button>Add</button>} icon={<span data-testid="ico" />} />)
    expect(screen.getByText('Try adding one')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add' })).toBeTruthy()
    expect(screen.getByTestId('ico')).toBeTruthy()
  })
})

describe('ErrorState', () => {
  it('is an alert with title and the error message', () => {
    render(<ErrorState error={new Error('boom')} />)
    const alert = screen.getByRole('alert')
    expect(alert.textContent).toContain('Could not load this page')
    expect(alert.textContent).toContain('boom')
    expect(screen.queryByRole('button')).toBeNull()
  })
  it('shows the API error code for ApiErrors only', () => {
    const { rerender } = render(<ErrorState error={new ApiError(404, { code: 'not_found', message: 'Gone' })} title="Missing" />)
    expect(screen.getByText('not_found')).toBeTruthy()
    expect(screen.getByText('Missing')).toBeTruthy()
    rerender(<ErrorState error={new Error('plain')} />)
    expect(screen.queryByText('not_found')).toBeNull()
  })
  it('offers Retry when onRetry is given', () => {
    const onRetry = vi.fn()
    render(<ErrorState error="weird" onRetry={onRetry} />)
    expect(screen.getByText('Something went wrong.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(onRetry).toHaveBeenCalledTimes(1)
  })
})

describe('AsyncView', () => {
  const base = { reload: () => undefined }
  it('shows the error (with retry) before anything else', () => {
    const reload = vi.fn()
    render(<AsyncView data="stale" error={new Error('bad')} loading={false} reload={reload}>{(d) => <span>{d}</span>}</AsyncView>)
    expect(screen.getByRole('alert')).toBeTruthy()
    expect(screen.queryByText('stale')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(reload).toHaveBeenCalled()
  })
  it('shows Loading while there is no data yet', () => {
    render(<AsyncView<string> data={undefined} error={undefined} loading {...base}>{(d) => <span>{d}</span>}</AsyncView>)
    expect(screen.getByRole('status')).toBeTruthy()
  })
  it('renders nothing when not loading and no data', () => {
    const { container } = render(<AsyncView<string> data={undefined} error={undefined} loading={false} {...base}>{(d) => <span>{d}</span>}</AsyncView>)
    expect(container.firstChild).toBeNull()
  })
  it('renders children with the data, even while reloading', () => {
    render(<AsyncView data="hello" error={undefined} loading {...base}>{(d) => <span>{d} world</span>}</AsyncView>)
    expect(screen.getByText('hello world')).toBeTruthy()
    expect(screen.queryByRole('status')).toBeNull()
  })
  it('treats falsy-but-defined data (0, empty array) as data', () => {
    render(<AsyncView data={0} error={undefined} loading={false} {...base}>{(d) => <span>count {d}</span>}</AsyncView>)
    expect(screen.getByText('count 0')).toBeTruthy()
  })
})
