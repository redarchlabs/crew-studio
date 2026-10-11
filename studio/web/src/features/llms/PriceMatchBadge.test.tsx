import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it } from 'vitest'
import { makePrice } from '../../lib/testFixtures'
import { PriceMatchBadge } from './PriceMatchBadge'

afterEach(cleanup)
const prices = [makePrice({ id: 'p1', model: 'gpt-x', input_per_mtok: 2, output_per_mtok: 8 }), makePrice({ id: 'p2', model: 'openai/exact', input_per_mtok: 1, output_per_mtok: 3 })]
const show = (model: string, verbose?: boolean) => render(<MemoryRouter><PriceMatchBadge model={model} prices={prices} verbose={verbose} /></MemoryRouter>)

describe('PriceMatchBadge', () => {
  it('shows the exact match and its prices', () => {
    show('openai/exact')
    expect(screen.getByText('openai/exact')).toBeTruthy()
    expect(screen.getByText('$1.00 in · $3.00 out')).toBeTruthy()
    expect(screen.getByTitle('Exact model match')).toBeTruthy()
    expect(screen.queryByText(/exact match\)/)).toBeNull()
  })

  it('shows a match without the provider prefix, with verbose detail', () => {
    show('openai/gpt-x', true)
    expect(screen.getByTitle('Matched after removing the provider prefix')).toBeTruthy()
    expect(screen.getByText('(matched without provider prefix)')).toBeTruthy()
  })

  it('verbose exact match says so', () => {
    show('openai/exact', true)
    expect(screen.getByText('(exact match)')).toBeTruthy()
  })

  it('warns when no price exists, with a link only when verbose', () => {
    const { unmount } = show('openai/none')
    expect(screen.getByText(/No price entry/)).toBeTruthy()
    expect(screen.queryByRole('link')).toBeNull()
    unmount()
    show('openai/none', true)
    expect(screen.getByRole('link', { name: 'add one' }).getAttribute('href')).toBe('/pricing')
  })

  it('warns for an empty model', () => {
    show('  ')
    expect(screen.getByText(/No price entry/)).toBeTruthy()
  })
})
