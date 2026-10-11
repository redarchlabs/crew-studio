import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { RunStatus } from '../api/types'
import { Chip, StatusBadge } from './StatusBadge'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('StatusBadge', () => {
  it.each([
    ['queued', 'text-blue', true],
    ['running', 'text-accent', true],
    ['succeeded', 'text-ok', false],
    ['failed', 'text-danger', false],
    ['cancelled', 'text-muted', false],
  ] as [RunStatus, string, boolean][])('%s uses %s and pulses=%s', (status, cls, live) => {
    const { container } = render(<StatusBadge status={status} />)
    expect(screen.getByText(status)).toBeTruthy()
    expect(container.firstElementChild!.className).toContain(cls)
    expect(container.querySelector('.pulse-dot') !== null).toBe(live)
  })
})

describe('Chip', () => {
  it('renders children with the neutral tone by default', () => {
    render(<Chip>tag</Chip>)
    expect(screen.getByText('tag').className).toContain('text-muted')
  })
  it.each(['accent', 'teal', 'blue', 'violet', 'ok', 'warn', 'danger'] as const)('tone %s', (tone) => {
    render(<Chip tone={tone}>x</Chip>)
    expect(screen.getByText('x').className).toContain(`text-${tone}`)
  })
  it('sets title and merges className', () => {
    render(<Chip title="hover me" className="extra">x</Chip>)
    const el = screen.getByText('x')
    expect(el.getAttribute('title')).toBe('hover me')
    expect(el.className).toContain('extra')
  })
})
