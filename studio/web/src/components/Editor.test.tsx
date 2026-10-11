import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { EditorHeader } from './EditorHeader'
import { EditorLayout, UsageList } from './EditorLayout'
import { ItemEditorHeader } from './ItemEditorHeader'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

type HeaderProps = Parameters<typeof EditorHeader>[0]
const base: HeaderProps = {
  backTo: '/agents',
  backLabel: 'Agents',
  title: 'Writer',
  isNew: false,
  dirty: false,
  saving: false,
  entityType: 'agent',
  entityId: 'ag-1',
  onSave: () => undefined,
}
const inRouter = (ui: React.ReactNode) => render(<MemoryRouter>{ui}</MemoryRouter>)

describe('EditorHeader', () => {
  it('shows back link, title and a History link for an existing item', () => {
    inRouter(<EditorHeader {...base} />)
    const back = screen.getByRole('link', { name: /Agents/ })
    expect(back.getAttribute('href')).toBe('/agents')
    expect(screen.getByRole('heading', { level: 1, name: 'Writer' })).toBeTruthy()
    expect(screen.getByRole('link', { name: /History/ }).getAttribute('href')).toBe('/history?entity_type=agent&entity_id=ag-1')
  })

  it('hides History and Delete and says Create for a new item', () => {
    inRouter(<EditorHeader {...base} isNew entityId={undefined} onDelete={() => undefined} />)
    expect(screen.queryByRole('link', { name: /History/ })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Create' })).toBeTruthy()
  })

  it('Save is disabled until the form is dirty, but Create is always enabled', () => {
    const { rerender } = inRouter(<EditorHeader {...base} />)
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    rerender(<MemoryRouter><EditorHeader {...base} dirty /></MemoryRouter>)
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false)
    expect(screen.getByText('unsaved')).toBeTruthy()
    rerender(<MemoryRouter><EditorHeader {...base} isNew /></MemoryRouter>)
    expect((screen.getByRole('button', { name: 'Create' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('calls onSave, and a saving header is busy/disabled', () => {
    const onSave = vi.fn()
    const { rerender } = inRouter(<EditorHeader {...base} dirty onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledTimes(1)
    rerender(<MemoryRouter><EditorHeader {...base} dirty saving onSave={onSave} /></MemoryRouter>)
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('does not show "unsaved" when clean', () => {
    inRouter(<EditorHeader {...base} />)
    expect(screen.queryByText('unsaved')).toBeNull()
  })

  it('delete needs two clicks (arm, then confirm)', () => {
    const onDelete = vi.fn()
    inRouter(<EditorHeader {...base} onDelete={onDelete} />)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    expect(onDelete).toHaveBeenCalledTimes(1)
  })

  it('renders extra content', () => {
    inRouter(<EditorHeader {...base} extra={<button>Run now</button>} />)
    expect(screen.getByRole('button', { name: 'Run now' })).toBeTruthy()
  })

  it('has no Delete button without onDelete', () => {
    inRouter(<EditorHeader {...base} />)
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
  })
})

describe('ItemEditorHeader', () => {
  it('delegates to the full header when the person can save', () => {
    const onDelete = vi.fn()
    inRouter(<ItemEditorHeader {...base} dirty canSave canDelete onDelete={onDelete} />)
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByText(/read-only/)).toBeNull()
  })

  it('drops Delete when the person can save but not delete', () => {
    inRouter(<ItemEditorHeader {...base} canSave canDelete={false} onDelete={() => undefined} />)
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy()
  })

  it('shows a read-only header without Save/Delete when the person cannot save', () => {
    inRouter(<ItemEditorHeader {...base} canSave={false} canDelete onDelete={() => undefined} extra={<span>extra!</span>} />)
    expect(screen.getByText('read-only')).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.getByRole('heading', { name: 'Writer' })).toBeTruthy()
    expect(screen.getByRole('link', { name: /Agents/ }).getAttribute('href')).toBe('/agents')
    expect(screen.getByRole('link', { name: /History/ })).toBeTruthy()
    expect(screen.getByText('extra!')).toBeTruthy()
  })

  it('read-only header for a new item has no History link', () => {
    inRouter(<ItemEditorHeader {...base} isNew entityId={undefined} canSave={false} canDelete={false} />)
    expect(screen.queryByRole('link', { name: /History/ })).toBeNull()
  })
})

describe('EditorLayout / UsageList', () => {
  it('renders main content and side panels in separate regions', () => {
    render(<EditorLayout main={<div>form</div>} side={<div>panel</div>} />)
    expect(screen.getByText('form')).toBeTruthy()
    expect(screen.getByRole('complementary').textContent).toBe('panel')
  })

  it('UsageList shows the empty text when there are no items', () => {
    render(<UsageList title="Used by" items={[]} empty="Nothing uses this" />)
    expect(screen.getByText('Used by')).toBeTruthy()
    expect(screen.getByText('Nothing uses this')).toBeTruthy()
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('UsageList renders each item node', () => {
    render(<UsageList title="Used by" empty="none" items={[{ key: 'a', node: <a href="/a">A</a> }, { key: 'b', node: 'B' }]} />)
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
    expect(screen.getByRole('link', { name: 'A' })).toBeTruthy()
    expect(screen.queryByText('none')).toBeNull()
  })
})
