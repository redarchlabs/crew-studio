import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useSearchParams } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../api/endpoints', () => ({
  areasApi: { list: vi.fn() },
  authApi: { status: vi.fn(), me: vi.fn(), logout: vi.fn() },
}))

import { areasApi, authApi } from '../api/endpoints'
import type { Me } from '../api/types'
import { AuthProvider } from '../lib/auth'
import { invalidateAreas } from '../lib/useAreas'
import { autoCleanup, makeArea, makeMe, makeStatus } from '../lib/testUtils'
import { AreaChips, AreaPicker, defaultAreaIds } from './AreaChips'
import { AreaFilterSelect, useAreaFilter } from './AreaFilter'

autoCleanup()
const list = areasApi.list as unknown as ReturnType<typeof vi.fn>
const meApi = authApi.me as unknown as ReturnType<typeof vi.fn>
const statusApi = authApi.status as unknown as ReturnType<typeof vi.fn>

const AREAS = [
  makeArea({ id: 'a1', name: 'Zeta' }),
  makeArea({ id: 'a2', name: 'Alpha', description: 'The alpha team' }),
  makeArea({ id: 'a3', name: 'Mid' }),
]

beforeEach(() => {
  invalidateAreas()
  list.mockReset().mockResolvedValue(AREAS)
  statusApi.mockReset().mockResolvedValue(makeStatus())
  meApi.mockReset().mockResolvedValue(makeMe())
})

function withProviders(ui: React.ReactNode, route = '/') {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <AuthProvider>{ui}</AuthProvider>
    </MemoryRouter>,
  )
}

describe('defaultAreaIds', () => {
  const me = (over: Partial<Me>): Me => makeMe(over)
  it('is empty for signed-out people and all-area people', () => {
    expect(defaultAreaIds(null)).toEqual([])
    expect(defaultAreaIds(me({ all_areas: true, area_ids: ['a'] }))).toEqual([])
  })
  it('preselects the only area', () => {
    expect(defaultAreaIds(me({ area_ids: ['only'] }))).toEqual(['only'])
  })
  it('returns a copy, not the original array', () => {
    const m = me({ area_ids: ['only'] })
    expect(defaultAreaIds(m)).not.toBe(m.area_ids)
  })
  it('makes the person choose when they have none or several', () => {
    expect(defaultAreaIds(me({ area_ids: [] }))).toEqual([])
    expect(defaultAreaIds(me({ area_ids: ['a', 'b'] }))).toEqual([])
  })
})

describe('AreaChips', () => {
  it('says "No area" for an empty list', () => {
    render(<AreaChips areaIds={[]} />)
    expect(screen.getByText('No area')).toBeTruthy()
  })

  it('names the areas once they load', async () => {
    render(<AreaChips areaIds={['a1', 'a2']} />)
    expect(await screen.findByText('Zeta')).toBeTruthy()
    expect(screen.getByText('Alpha')).toBeTruthy()
  })

  it('shows "Unknown area" for ids it cannot resolve', async () => {
    render(<AreaChips areaIds={['ghost']} />)
    expect(await screen.findByText('Unknown area')).toBeTruthy()
  })

  it('collapses extra areas into a +N chip with the hidden names in its title', async () => {
    render(<AreaChips areaIds={['a1', 'a2', 'a3']} max={1} />)
    const more = await screen.findByText('+2')
    expect(more.getAttribute('title')).toBe('Alpha, Mid')
    expect(screen.queryByText('Mid')).toBeNull()
  })

  it('shows up to 3 areas by default without a +N chip', async () => {
    render(<AreaChips areaIds={['a1', 'a2', 'a3']} />)
    await screen.findByText('Mid')
    expect(screen.queryByText(/^\+/)).toBeNull()
  })
})

describe('AreaPicker', () => {
  async function renderPicker(me: Me, value: string[], onChange = vi.fn(), extra: { error?: string; label?: string } = {}) {
    meApi.mockResolvedValue(me)
    withProviders(<AreaPicker value={value} onChange={onChange} {...extra} />)
    await screen.findByRole('option', { name: /Zeta/ })
    await waitFor(() => expect((screen.getByRole('option', { name: /Zeta/ }) as HTMLButtonElement).disabled).toBe(!(me.all_areas || me.area_ids.includes('a1'))))
    return onChange
  }

  it('lists every area with the default label and the description as hint', async () => {
    await renderPicker(makeMe({ all_areas: true }), [])
    expect(screen.getByText('Functional areas')).toBeTruthy()
    expect(screen.getAllByRole('option')).toHaveLength(3)
    expect(screen.getByText('The alpha team')).toBeTruthy()
  })

  it('accepts a custom label and an error', async () => {
    await renderPicker(makeMe({ all_areas: true }), [], vi.fn(), { label: 'Visible to', error: 'Choose at least one' })
    expect(screen.getByText('Visible to')).toBeTruthy()
    expect(screen.getByText('Choose at least one')).toBeTruthy()
  })

  it('lets an all-areas person add any area', async () => {
    const onChange = await renderPicker(makeMe({ all_areas: true }), ['a1'])
    fireEvent.click(screen.getByRole('option', { name: /Alpha/ }))
    expect(onChange).toHaveBeenCalledWith(['a1', 'a2'])
  })

  it('locks areas outside the person\'s own, keeping them in the value', async () => {
    const me = makeMe({ area_ids: ['a1'] })
    const onChange = await renderPicker(me, ['a2'])
    const alpha = screen.getByRole('option', { name: /Alpha/ }) as HTMLButtonElement
    expect(alpha.getAttribute('aria-selected')).toBe('true')
    expect(alpha.disabled).toBe(false) // selected + locked can still be clicked, but changes nothing
    const mid = screen.getByRole('option', { name: /Mid/ }) as HTMLButtonElement
    expect(mid.disabled).toBe(true)
    expect(mid.title).toContain('You are not in this area')
    fireEvent.click(screen.getByRole('option', { name: /Zeta/ }))
    expect(onChange).toHaveBeenLastCalledWith(['a2', 'a1'])
  })

  it('cannot remove a locked area', async () => {
    const onChange = await renderPicker(makeMe({ area_ids: ['a1'] }), ['a2', 'a1'])
    fireEvent.click(screen.getByRole('option', { name: /Alpha/ })) // locked, selected: toggling off is discarded
    expect(onChange).toHaveBeenLastCalledWith(['a2', 'a1'])
  })

  it('can remove an area the person owns, preserving locked ones', async () => {
    const onChange = await renderPicker(makeMe({ area_ids: ['a1'] }), ['a2', 'a1'])
    fireEvent.click(screen.getByRole('option', { name: /Zeta/ }))
    expect(onChange).toHaveBeenLastCalledWith(['a2'])
  })

  it('shows a loading message before areas arrive and an empty message when there are none', async () => {
    let resolve!: (v: unknown[]) => void
    list.mockReturnValue(new Promise((r) => (resolve = r)))
    withProviders(<AreaPicker value={[]} onChange={() => undefined} />)
    expect(await screen.findByText('Loading areas…')).toBeTruthy()
    resolve([])
    expect(await screen.findByText(/No functional areas yet/)).toBeTruthy()
  })
})

describe('AreaFilterSelect', () => {
  it('offers All areas then areas sorted by name, and reports the choice', async () => {
    const onChange = vi.fn()
    withProviders(<AreaFilterSelect value="" onChange={onChange} />)
    await screen.findByRole('option', { name: 'Zeta' })
    const sel = screen.getByRole('combobox', { name: 'Filter by functional area' }) as HTMLSelectElement
    expect(Array.from(sel.options).map((o) => o.textContent)).toEqual(['All areas', 'Alpha', 'Mid', 'Zeta'])
    fireEvent.change(sel, { target: { value: 'a3' } })
    expect(onChange).toHaveBeenCalledWith('a3')
  })

  it('reflects the controlled value', async () => {
    withProviders(<AreaFilterSelect value="a2" onChange={() => undefined} />)
    await screen.findByRole('option', { name: 'Alpha' })
    expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('a2')
  })
})

describe('useAreaFilter', () => {
  function Harness() {
    const [area, setArea] = useAreaFilter()
    const [params] = useSearchParams()
    return (
      <div>
        <output data-testid="area">{area}</output>
        <output data-testid="qs">{params.toString()}</output>
        <button onClick={() => setArea('a2')}>pick</button>
        <button onClick={() => setArea('')}>clear</button>
      </div>
    )
  }
  const area = () => screen.getByTestId('area').textContent
  const qs = () => screen.getByTestId('qs').textContent

  it('reads ?area= and keeps it before areas load', () => {
    list.mockReturnValue(new Promise(() => undefined))
    withProviders(<Harness />, '/?area=a1')
    expect(area()).toBe('a1')
  })

  it('keeps a known area once loaded', async () => {
    withProviders(<Harness />, '/?area=a1')
    await waitFor(() => expect(list).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 0))
    expect(area()).toBe('a1')
  })

  it('drops a stale area id after areas load', async () => {
    withProviders(<Harness />, '/?area=deleted')
    await waitFor(() => expect(area()).toBe(''))
  })

  it('writes the selection to the URL, preserving other params, and clears it', async () => {
    withProviders(<Harness />, '/?q=hello')
    await waitFor(() => expect(list).toHaveBeenCalled())
    fireEvent.click(screen.getByText('pick'))
    expect(qs()).toBe('q=hello&area=a2')
    expect(area()).toBe('a2')
    fireEvent.click(screen.getByText('clear'))
    expect(qs()).toBe('q=hello')
  })
})
