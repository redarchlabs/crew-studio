import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeTool } from '../../lib/testFixtures'
import { ToolPicker } from './ToolPicker'

afterEach(cleanup)

const tools = [
  makeTool({ key: 'web', name: 'Web search', description: 'Searches' }),
  makeTool({ key: 'mail', name: 'Mailer', available: false, requires_env: ['SMTP_URL', 'SMTP_KEY'] }),
  makeTool({ key: 'cfg', name: 'Cfg', available: false, requires_env: [] }),
]

describe('ToolPicker', () => {
  it('lists tools with key and description', () => {
    render(<ToolPicker tools={tools} value={[]} onChange={() => {}} />)
    expect(screen.getByRole('option', { name: /Web search/ }).textContent).toMatch(/web · Searches/)
  })

  it('toggles an available tool', () => {
    const onChange = vi.fn()
    render(<ToolPicker tools={tools} value={['web']} onChange={onChange} />)
    fireEvent.click(screen.getByRole('option', { name: /Web search/ }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('disables unavailable tools and says what is missing', () => {
    const onChange = vi.fn()
    render(<ToolPicker tools={tools} value={[]} onChange={onChange} />)
    const mail = screen.getByRole('option', { name: /Mailer/ }) as HTMLButtonElement
    expect(mail.disabled).toBe(true)
    expect(mail.textContent).toMatch(/missing SMTP_URL, SMTP_KEY/)
    expect((screen.getByRole('option', { name: /Cfg/ }) as HTMLButtonElement).textContent).toMatch(/missing configuration/)
    fireEvent.click(mail)
    expect(onChange).not.toHaveBeenCalled()
  })

  it('keeps an already selected unavailable tool removable', () => {
    const onChange = vi.fn()
    render(<ToolPicker tools={tools} value={['mail']} onChange={onChange} />)
    const mail = screen.getByRole('option', { name: /Mailer/ }) as HTMLButtonElement
    expect(mail.disabled).toBe(false)
    fireEvent.click(mail)
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('shows unknown selected keys so they can be removed', () => {
    const onChange = vi.fn()
    render(<ToolPicker tools={tools} value={['ghost']} onChange={onChange} />)
    const ghost = screen.getByRole('option', { name: /ghost/ })
    expect(ghost.textContent).toMatch(/Unknown tool/)
    fireEvent.click(ghost)
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('marks each tool Reads or Changes and says on hover what happens in chat', () => {
    render(<ToolPicker value={[]} onChange={() => {}} tools={[
      makeTool({ key: 'web', name: 'Web' }),
      makeTool({ key: 'issue', name: 'Issue', needs: ['tracker'] }),
      makeTool({ key: 'km', name: 'KM', needs: ['km2'] }),
      makeTool({ key: 'note', name: 'Note', needs: ['crew'] }),
      makeTool({ key: 'post', name: 'Post', effect: 'changes', needs: ['tracker'] }),
      makeTool({ key: 'card', name: 'Card', effect: 'proposes' }),
      makeTool({ key: 'tracker_comment', name: 'Comment', effect: 'changes', needs: ['tracker'] }),
    ]} />)
    const chip = (name: string) => screen.getByRole('option', { name: new RegExp(`^${name}`) }).querySelector('[title]') as HTMLElement
    expect(chip('Web').textContent).toBe('Reads')
    expect(chip('Web').title).toMatch(/agent chat/)
    expect(chip('Issue').title).toMatch(/Jira, Linear or KM2 issues connection listed in the agent's chat settings/)
    expect(chip('KM').title).toMatch(/KM2 connection listed/)
    expect(chip('Note').title).toMatch(/needs a crew/)
    expect(chip('Post').textContent).toBe('Changes')
    expect(chip('Post').title).toMatch(/never given to an agent in chat/)
    expect(chip('Card').textContent).toBe('Proposes')
    // A tracker or KM2 write becomes a card in agent chat
    expect(chip('Comment').title).toMatch(/proposes the change as a card the person confirms/)
  })

  it('shows empty text and error', () => {
    render(<ToolPicker tools={[]} value={[]} onChange={() => {}} error="bad tools" />)
    expect(screen.getByText('The server reports no tools.')).toBeTruthy()
    expect(screen.getByText('bad tools')).toBeTruthy()
  })
})
