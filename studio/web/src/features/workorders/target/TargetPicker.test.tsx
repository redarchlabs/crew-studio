import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { makeCrew, makeTask } from '../../../lib/testFixtures'
import { areasApi } from '../../../api/endpoints'
import { invalidateAreas } from '../../../lib/useAreas'
import { renderRoute } from '../testUtils'
import { crewTaskLine, TargetPicker } from './TargetPicker'
import type { TargetRef } from './targetInputs'
import type { TargetCatalogData } from './useTargetCatalog'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())

afterEach(cleanup)

const tasks = [
  makeTask({ id: 't1', name: 'Corporate Profile' }),
  makeTask({ id: 't2', name: 'Financial Picture' }),
  makeTask({ id: 't3', name: 'Investment Memo' }),
]

const catalog = (over: Partial<TargetCatalogData> = {}): TargetCatalogData => ({
  crews: [
    makeCrew({ id: 'c1', name: 'Alpha crew', description: 'Ignored when tasks are visible', task_ids: ['t1', 't2', 't3'], area_ids: ['area-1', 'area-2', 'area-3'] }),
    makeCrew({ id: 'c2', name: 'Beta crew', description: 'Vendor digest', task_ids: ['t3'], area_ids: [] }),
  ],
  tasks,
  forbidden: { crews: false, tasks: false },
  ...over,
})

const none: TargetRef = { crew_id: null }

const trigger = () => screen.getByRole('button', { expanded: false })

describe('crewTaskLine', () => {
  const byId = new Map(tasks.map((t) => [t.id, t]))

  it('names the tasks in crew order', () => {
    expect(crewTaskLine(makeCrew({ task_ids: ['t3', 't1'] }), byId)).toBe('Investment Memo → Corporate Profile')
  })

  it('counts tasks the person cannot see', () => {
    expect(crewTaskLine(makeCrew({ task_ids: ['t1', 'hidden', 'gone'] }), byId)).toBe('Corporate Profile · +2 more')
  })

  it('falls back to the description, then a task count, when no task can be named', () => {
    expect(crewTaskLine(makeCrew({ task_ids: ['x'], description: 'Blurb' }), byId)).toBe('Blurb')
    expect(crewTaskLine(makeCrew({ task_ids: ['x', 'y'], process: 'hierarchical' }), byId)).toBe('2 tasks · hierarchical')
    expect(crewTaskLine(makeCrew({ task_ids: [] }), byId)).toBe('No tasks yet')
    expect(crewTaskLine(makeCrew({ task_ids: null as unknown as string[] }), byId)).toBe('No tasks yet')
  })
})

describe('TargetPicker', () => {
  it('is a crew picker with no task/crew toggle, and explains how to run one task', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog()} />)
    expect(screen.getByText('Crew')).toBeTruthy()
    expect(screen.getByText('Choose a crew…')).toBeTruthy()
    expect(screen.queryByRole('radio')).toBeNull()
    expect(screen.getByText(/Runs the whole crew/)).toBeTruthy()
    expect(screen.getByText(/To run a single task, make a crew with just that task/)).toBeTruthy()
  })

  it("lists crews with their tasks' names in order, and picking one sends its id", () => {
    const onChange = vi.fn()
    renderRoute(<TargetPicker value={none} onChange={onChange} catalog={catalog()} />)
    fireEvent.click(trigger())
    const list = screen.getByRole('listbox')
    expect(within(list).getAllByRole('option')).toHaveLength(2)
    expect(within(list).getByText('Corporate Profile → Financial Picture → Investment Memo').className).toContain('truncate')
    expect(within(list).getByText('Investment Memo')).toBeTruthy()
    fireEvent.click(within(list).getByRole('option', { name: /Beta crew/ }))
    expect(onChange).toHaveBeenCalledWith({ crew_id: 'c2' })
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('falls back to descriptions when the person cannot list tasks', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog({ tasks: [], forbidden: { crews: false, tasks: true } })} />)
    fireEvent.click(trigger())
    expect(screen.getByText('Vendor digest')).toBeTruthy()
  })

  it('shows the selected crew with its area chips (max two plus a count)', async () => {
    invalidateAreas()
    vi.mocked(areasApi.list).mockResolvedValue([
      { id: 'area-1', name: 'Finance', description: '' },
      { id: 'area-2', name: 'Legal', description: '' },
      { id: 'area-3', name: 'Ops', description: '' },
    ] as never)
    renderRoute(<TargetPicker value={{ crew_id: 'c1' }} onChange={vi.fn()} catalog={catalog()} />)
    expect(screen.getByText('Alpha crew')).toBeTruthy()
    expect(await screen.findByText('Finance')).toBeTruthy()
    expect(screen.getByText('Legal')).toBeTruthy()
    expect(screen.getByText('+1')).toBeTruthy()
  })

  it('shows no-area chips for crews without areas', () => {
    renderRoute(<TargetPicker value={{ crew_id: 'c2' }} onChange={vi.fn()} catalog={catalog()} />)
    expect(screen.getByText('No area')).toBeTruthy()
  })

  it('filters by crew or task name, case-insensitively, and reports no matches', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog()} />)
    fireEvent.click(trigger())
    const search = screen.getByPlaceholderText('Search…')
    fireEvent.change(search, { target: { value: 'FINANCIAL' } })
    expect(screen.getAllByRole('option')).toHaveLength(1)
    fireEvent.change(search, { target: { value: 'zzz' } })
    expect(screen.queryAllByRole('option')).toHaveLength(0)
    expect(screen.getByText('No matches.')).toBeTruthy()
  })

  it('Enter picks the first visible match', () => {
    const onChange = vi.fn()
    renderRoute(<TargetPicker value={none} onChange={onChange} catalog={catalog()} />)
    fireEvent.click(trigger())
    const search = screen.getByPlaceholderText('Search…')
    fireEvent.change(search, { target: { value: 'beta' } })
    fireEvent.keyDown(search, { key: 'Enter' })
    expect(onChange).toHaveBeenCalledWith({ crew_id: 'c2' })
  })

  it('Enter with no matches does nothing', () => {
    const onChange = vi.fn()
    renderRoute(<TargetPicker value={none} onChange={onChange} catalog={catalog()} />)
    fireEvent.click(trigger())
    const search = screen.getByPlaceholderText('Search…')
    fireEvent.change(search, { target: { value: 'nope' } })
    fireEvent.keyDown(search, { key: 'Enter' })
    fireEvent.keyDown(search, { key: 'a' })
    expect(onChange).not.toHaveBeenCalled()
  })

  it('closes on Escape and on outside click, but not on inside click', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog()} />)
    fireEvent.click(trigger())
    fireEvent.mouseDown(screen.getByPlaceholderText('Search…'))
    expect(screen.getByRole('listbox')).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('listbox')).toBeNull()
    fireEvent.click(trigger())
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(screen.getByRole('listbox')).toBeTruthy()
    fireEvent.mouseDown(document.body)
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('toggles the list by clicking the trigger again', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog()} />)
    fireEvent.click(trigger())
    fireEvent.click(screen.getByRole('button', { expanded: true }))
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('marks the selected option', () => {
    renderRoute(<TargetPicker value={{ crew_id: 'c1' }} onChange={vi.fn()} catalog={catalog()} />)
    fireEvent.click(trigger())
    const options = screen.getAllByRole('option')
    expect(options.map((o) => o.getAttribute('aria-selected'))).toEqual(['true', 'false'])
  })

  it('says nothing is visible for an empty list, and explains forbidden crews', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog({ crews: [], forbidden: { crews: true, tasks: false } })} />)
    expect(screen.getByText('You cannot view crews')).toBeTruthy()
    fireEvent.click(trigger())
    expect(screen.getByText('Nothing you can see yet.')).toBeTruthy()
  })

  it('shows an error instead of the explanation and flags the trigger invalid', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog()} error="Choose a crew to run." />)
    expect(screen.getByRole('alert').textContent).toBe('Choose a crew to run.')
    expect(trigger().getAttribute('aria-invalid')).toBe('true')
    expect(screen.queryByText(/Runs the whole crew/)).toBeNull()
  })

  it('disables the trigger when disabled', () => {
    renderRoute(<TargetPicker value={none} onChange={vi.fn()} catalog={catalog()} disabled />)
    expect((trigger() as HTMLButtonElement).disabled).toBe(true)
  })

  it('keeps a stale selection (id no longer in the catalog) showing the placeholder', () => {
    renderRoute(<TargetPicker value={{ crew_id: 'gone' }} onChange={vi.fn()} catalog={catalog()} />)
    expect(screen.getByText('Choose a crew…')).toBeTruthy()
  })
})
