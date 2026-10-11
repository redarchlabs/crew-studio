import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { NODE_VERIFY, PYTHON_VERIFY } from './hookExamples'
import { SigningSecret } from './SigningSecret'

const writeText = vi.fn()
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
})
afterEach(cleanup)

describe('SigningSecret', () => {
  it('masks the secret until revealed and hides it again', () => {
    render(<SigningSecret secret="whsec_abc123" />)
    expect(screen.queryByText('whsec_abc123')).toBeNull()
    expect(screen.getByText('•'.repeat(12))).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Reveal' }))
    expect(screen.getByText('whsec_abc123')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Hide' }))
    expect(screen.queryByText('whsec_abc123')).toBeNull()
  })

  it('caps the mask at 40 characters so length is not leaked beyond that', () => {
    render(<SigningSecret secret={'s'.repeat(100)} />)
    expect(screen.getByText('•'.repeat(40))).toBeTruthy()
  })

  it('copies the secret', async () => {
    render(<SigningSecret secret="whsec_abc123" />)
    fireEvent.click(screen.getAllByRole('button', { name: 'Copy' })[0])
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('whsec_abc123'))
    expect(await screen.findByText('Copied')).toBeTruthy()
  })

  it('tells people without the permission why there is no secret', () => {
    render(<SigningSecret secret={undefined} />)
    expect(screen.getByText(/Only people who can manage triggers/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Reveal' })).toBeNull()
  })

  it('treats an empty secret like a missing one', () => {
    render(<SigningSecret secret="" />)
    expect(screen.getByText(/Only people who can manage triggers/)).toBeTruthy()
  })

  it('shows the Node verifier by default and switches to Python', () => {
    const { container } = render(<SigningSecret secret="x" />)
    expect(container.querySelector('pre')?.textContent).toBe(NODE_VERIFY)
    expect(screen.getByRole('button', { name: 'Node.js' }).getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(screen.getByRole('button', { name: 'Python' }))
    expect(container.querySelector('pre')?.textContent).toBe(PYTHON_VERIFY)
    expect(screen.getByRole('button', { name: 'Python' }).getAttribute('aria-pressed')).toBe('true')
    expect(screen.getByRole('button', { name: 'Node.js' }).getAttribute('aria-pressed')).toBe('false')
  })

  it('copies the selected snippet', async () => {
    render(<SigningSecret secret={undefined} />)
    fireEvent.click(screen.getByRole('button', { name: 'Python' }))
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(PYTHON_VERIFY))
  })
})
