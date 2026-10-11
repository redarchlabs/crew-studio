import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { Button, IconButton, LinkButton, buttonClass } from './Button'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('buttonClass', () => {
  it('defaults to secondary/md', () => {
    const cls = buttonClass()
    expect(cls).toContain('bg-panel-2')
    expect(cls).toContain('h-8')
  })
  it.each([
    ['primary', 'bg-accent'],
    ['secondary', 'bg-panel-2'],
    ['ghost', 'bg-transparent'],
    ['danger', 'bg-danger-soft'],
  ] as const)('variant %s', (variant, token) => expect(buttonClass(variant)).toContain(token))
  it('small size is shorter', () => expect(buttonClass('primary', 'sm')).toContain('h-7'))
})

describe('Button', () => {
  it('renders a type=button by default so it never submits forms accidentally', () => {
    render(<Button>Save</Button>)
    expect(screen.getByRole('button', { name: 'Save' }).getAttribute('type')).toBe('button')
  })
  it('allows type=submit', () => {
    render(<Button type="submit">Go</Button>)
    expect(screen.getByRole('button').getAttribute('type')).toBe('submit')
  })
  it('fires onClick', () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Go</Button>)
    fireEvent.click(screen.getByRole('button'))
    expect(onClick).toHaveBeenCalledTimes(1)
  })
  it('does not fire when disabled', () => {
    const onClick = vi.fn()
    render(<Button disabled onClick={onClick}>Go</Button>)
    const btn = screen.getByRole('button') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    fireEvent.click(btn)
    expect(onClick).not.toHaveBeenCalled()
  })
  it('busy disables the button and replaces the icon with a spinner', () => {
    const onClick = vi.fn()
    const { container } = render(<Button busy icon={<span data-testid="ico" />} onClick={onClick}>Save</Button>)
    const btn = screen.getByRole('button') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    expect(screen.queryByTestId('ico')).toBeNull()
    expect(container.querySelector('svg.animate-spin')).not.toBeNull()
    fireEvent.click(btn)
    expect(onClick).not.toHaveBeenCalled()
  })
  it('shows the icon when not busy and merges className', () => {
    render(<Button icon={<span data-testid="ico" />} className="extra" variant="danger" size="sm">X</Button>)
    expect(screen.getByTestId('ico')).toBeTruthy()
    const cls = screen.getByRole('button').className
    expect(cls).toContain('extra')
    expect(cls).toContain('bg-danger-soft')
    expect(cls).toContain('h-7')
  })
})

describe('IconButton', () => {
  it('is named by its label (aria-label and title)', () => {
    render(<IconButton label="Delete row" icon={<span>x</span>} />)
    const btn = screen.getByRole('button', { name: 'Delete row' })
    expect(btn.getAttribute('title')).toBe('Delete row')
    expect(btn.getAttribute('type')).toBe('button')
  })
  it('applies the danger tone and forwards props', () => {
    const onClick = vi.fn()
    render(<IconButton label="Del" tone="danger" icon={<span />} onClick={onClick} className="mine" data-x="1" />)
    const btn = screen.getByRole('button')
    expect(btn.className).toContain('hover:text-danger')
    expect(btn.className).toContain('mine')
    expect(btn.getAttribute('data-x')).toBe('1')
    fireEvent.click(btn)
    expect(onClick).toHaveBeenCalled()
  })
  it('default tone is neutral', () => {
    render(<IconButton label="Hi" icon={<span />} />)
    expect(screen.getByRole('button').className).toContain('hover:text-fg')
  })
})

describe('LinkButton', () => {
  it('renders an anchor styled as a button with icon and href', () => {
    render(
      <MemoryRouter>
        <LinkButton to="/agents/new" variant="primary" icon={<span data-testid="i" />} className="mx">New agent</LinkButton>
      </MemoryRouter>,
    )
    const link = screen.getByRole('link', { name: 'New agent' })
    expect(link.getAttribute('href')).toBe('/agents/new')
    expect(link.className).toContain('bg-accent')
    expect(link.className).toContain('mx')
    expect(screen.getByTestId('i')).toBeTruthy()
  })
})
