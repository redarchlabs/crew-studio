import { act, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PageHeader, Panel } from './PageHeader'
import { LockableFieldset, ReadOnlyNote } from './ReadOnlyNote'
import { Collapsible, ConfirmButton, CopyButton, HistoryLink, JsonBlock, Stat } from './misc'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('PageHeader', () => {
  it('renders only the title when nothing else is given', () => {
    const { container } = render(<PageHeader title="Agents" />)
    expect(screen.getByRole('heading', { level: 1, name: 'Agents' })).toBeTruthy()
    expect(container.querySelector('.eyebrow')).toBeNull()
  })
  it('renders eyebrow, description and actions', () => {
    render(<PageHeader title="Runs" eyebrow="Console" description="All runs" actions={<button>New</button>} />)
    expect(screen.getByText('Console')).toBeTruthy()
    expect(screen.getByText('All runs')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'New' })).toBeTruthy()
  })
})

describe('Panel', () => {
  it('renders without a header when there is no title or actions', () => {
    const { container } = render(<Panel>body</Panel>)
    expect(container.querySelector('.eyebrow')).toBeNull()
    expect(screen.getByText('body')).toBeTruthy()
  })
  it('renders a header with title and actions, and honours class overrides', () => {
    const { container } = render(<Panel title="Inputs" actions={<button>Add</button>} className="mine" bodyClassName="p-0">content</Panel>)
    expect(screen.getByText('Inputs')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add' })).toBeTruthy()
    expect(container.querySelector('section')!.className).toContain('mine')
    expect(screen.getByText('content').className).toContain('p-0')
  })
  it('renders a header for actions alone', () => {
    render(<Panel actions={<button>Only</button>}>x</Panel>)
    expect(screen.getByRole('button', { name: 'Only' })).toBeTruthy()
  })
})

describe('ReadOnlyNote', () => {
  it('names the missing permission', () => {
    render(<ReadOnlyNote perm="agents.edit" />)
    expect(screen.getByRole('note').textContent).toContain('Read-only: you need agents.edit to change this.')
    expect(screen.getByText('agents.edit').tagName).toBe('CODE')
  })
  it('custom children replace the default text', () => {
    render(<ReadOnlyNote perm="agents.edit">Archived items cannot be edited.</ReadOnlyNote>)
    expect(screen.getByRole('note').textContent).toBe('Archived items cannot be edited.')
  })
})

describe('LockableFieldset', () => {
  it('disables controls inside when locked', () => {
    render(<LockableFieldset locked><input aria-label="x" /></LockableFieldset>)
    expect(screen.getByLabelText('x').matches(':disabled')).toBe(true)
  })
  it('leaves them enabled when unlocked and merges className', () => {
    const { container } = render(<LockableFieldset locked={false} className="extra"><input aria-label="x" /></LockableFieldset>)
    expect(screen.getByLabelText('x').matches(':disabled')).toBe(false)
    expect(container.querySelector('fieldset')!.className).toContain('extra')
  })
})

describe('HistoryLink', () => {
  it('links to the history filtered by type and id', () => {
    render(<MemoryRouter><HistoryLink type="task" id="t 1" /></MemoryRouter>)
    expect(screen.getByRole('link', { name: /History/ }).getAttribute('href')).toBe('/history?entity_type=task&entity_id=t+1')
  })
  it('filters by type alone when there is no id', () => {
    render(<MemoryRouter><HistoryLink type="llm" /></MemoryRouter>)
    expect(screen.getByRole('link').getAttribute('href')).toBe('/history?entity_type=llm')
  })
})

describe('ConfirmButton', () => {
  afterEach(() => vi.useRealTimers())
  it('arms on first click, confirms on second', () => {
    const onConfirm = vi.fn()
    render(<ConfirmButton onConfirm={onConfirm} icon={<span data-testid="i" />}>Delete</ConfirmButton>)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onConfirm).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Delete' })).toBeTruthy() // disarmed again
  })
  it('disarms itself after 3 seconds', () => {
    vi.useFakeTimers()
    const onConfirm = vi.fn()
    render(<ConfirmButton onConfirm={onConfirm}>Delete</ConfirmButton>)
    fireEvent.click(screen.getByRole('button'))
    expect(screen.getByRole('button').textContent).toBe('Confirm?')
    act(() => {
      vi.advanceTimersByTime(2999)
    })
    expect(screen.getByRole('button').textContent).toBe('Confirm?')
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(screen.getByRole('button').textContent).toBe('Delete')
    fireEvent.click(screen.getByRole('button'))
    expect(onConfirm).not.toHaveBeenCalled() // first click re-arms rather than confirming
  })
  it('never confirms while disabled or busy', () => {
    const onConfirm = vi.fn()
    const { rerender } = render(<ConfirmButton onConfirm={onConfirm} disabled>Delete</ConfirmButton>)
    fireEvent.click(screen.getByRole('button'))
    rerender(<ConfirmButton onConfirm={onConfirm} busy>Delete</ConfirmButton>)
    fireEvent.click(screen.getByRole('button'))
    expect(onConfirm).not.toHaveBeenCalled()
  })
})

describe('CopyButton', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })
  it('copies the text and briefly says Copied', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    render(<CopyButton text="secret" />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    expect(writeText).toHaveBeenCalledWith('secret')
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeTruthy()
  })
  it('reverts to its label after the confirmation timeout', async () => {
    vi.stubGlobal('navigator', { clipboard: { writeText: () => Promise.resolve() } })
    render(<CopyButton text="x" label="Copy key" />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy key' }))
    await screen.findByRole('button', { name: 'Copied' })
    expect(await screen.findByRole('button', { name: 'Copy key' }, { timeout: 2500 })).toBeTruthy()
  })
  it('does not claim success when the clipboard rejects', async () => {
    const toast = await import('sonner')
    const errSpy = vi.spyOn(toast.toast, 'error').mockImplementation(() => 'id')
    vi.stubGlobal('navigator', { clipboard: { writeText: () => Promise.reject(new Error('denied')) } })
    render(<CopyButton text="x" />)
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await vi.waitFor(() => expect(errSpy).toHaveBeenCalledWith('Copy failed', { description: 'denied' }))
    expect(screen.getByRole('button', { name: 'Copy' })).toBeTruthy()
    errSpy.mockRestore()
  })
})

describe('Collapsible', () => {
  it('is collapsed by default and toggles aria-expanded', () => {
    render(<Collapsible title="Details" meta="3 items">hidden body</Collapsible>)
    const btn = screen.getByRole('button', { name: /Details/ })
    expect(btn.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText('hidden body')).toBeNull()
    expect(screen.getByText('3 items')).toBeTruthy()
    fireEvent.click(btn)
    expect(btn.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByText('hidden body')).toBeTruthy()
    fireEvent.click(btn)
    expect(screen.queryByText('hidden body')).toBeNull()
  })
  it('can start open', () => {
    render(<Collapsible title="T" defaultOpen>body</Collapsible>)
    expect(screen.getByText('body')).toBeTruthy()
  })
})

describe('JsonBlock', () => {
  it('pretty-prints objects', () => {
    const { container } = render(<JsonBlock value={{ a: [1, 2] }} />)
    expect(container.querySelector('pre')!.textContent).toBe(JSON.stringify({ a: [1, 2] }, null, 2))
  })
  it('prints strings verbatim (no extra quotes) and applies className', () => {
    const { container } = render(<JsonBlock value="raw text" className="big" />)
    expect(container.querySelector('pre')!.textContent).toBe('raw text')
    expect(container.querySelector('pre')!.className).toContain('big')
  })
  it('prints null as null', () => {
    const { container } = render(<JsonBlock value={null} />)
    expect(container.querySelector('pre')!.textContent).toBe('null')
  })
})

describe('Stat', () => {
  it('renders label and value, with optional sub text', () => {
    const { rerender } = render(<Stat label="Cost" value="$1.00" />)
    expect(screen.getByText('Cost')).toBeTruthy()
    expect(screen.getByText('$1.00')).toBeTruthy()
    rerender(<Stat label="Cost" value="$1.00" sub="of $5.00" />)
    expect(screen.getByText('of $5.00')).toBeTruthy()
  })
})
