import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Pagination } from './Pagination'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()
const prev = () => screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement
const next = () => screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement

describe('Pagination', () => {
  it('renders nothing when there are no results', () => {
    const { container } = render(<Pagination meta={{ total: 0, limit: 10, offset: 0 }} onOffset={() => undefined} />)
    expect(container.firstChild).toBeNull()
  })

  it('shows the visible range and disables Previous on the first page', () => {
    render(<Pagination meta={{ total: 25, limit: 10, offset: 0 }} onOffset={() => undefined} />)
    expect(screen.getByText('1–10 of 25')).toBeTruthy()
    expect(prev().disabled).toBe(true)
    expect(next().disabled).toBe(false)
  })

  it('clamps the range end to the total and disables Next on the last page', () => {
    render(<Pagination meta={{ total: 25, limit: 10, offset: 20 }} onOffset={() => undefined} />)
    expect(screen.getByText('21–25 of 25')).toBeTruthy()
    expect(next().disabled).toBe(true)
    expect(prev().disabled).toBe(false)
  })

  it('both are disabled when everything fits on one page', () => {
    render(<Pagination meta={{ total: 3, limit: 10, offset: 0 }} onOffset={() => undefined} />)
    expect(prev().disabled).toBe(true)
    expect(next().disabled).toBe(true)
  })

  it('Next advances by one limit', () => {
    const onOffset = vi.fn()
    render(<Pagination meta={{ total: 100, limit: 10, offset: 10 }} onOffset={onOffset} />)
    fireEvent.click(next())
    expect(onOffset).toHaveBeenCalledWith(20)
  })

  it('Previous goes back one limit but never below zero', () => {
    const onOffset = vi.fn()
    const { rerender } = render(<Pagination meta={{ total: 100, limit: 10, offset: 20 }} onOffset={onOffset} />)
    fireEvent.click(prev())
    expect(onOffset).toHaveBeenLastCalledWith(10)
    rerender(<Pagination meta={{ total: 100, limit: 10, offset: 4 }} onOffset={onOffset} />)
    fireEvent.click(prev())
    expect(onOffset).toHaveBeenLastCalledWith(0)
  })

  it('a final page that is exactly full has no Next', () => {
    render(<Pagination meta={{ total: 20, limit: 10, offset: 10 }} onOffset={() => undefined} />)
    expect(next().disabled).toBe(true)
  })
})
