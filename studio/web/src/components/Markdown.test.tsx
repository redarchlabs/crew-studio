import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Markdown } from './Markdown'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()

describe('Markdown', () => {
  it('renders headings, emphasis and lists', () => {
    render(<Markdown>{'# Title\n\nSome **bold** and *em*\n\n- one\n- two'}</Markdown>)
    expect(screen.getByRole('heading', { level: 1, name: 'Title' })).toBeTruthy()
    expect(screen.getByText('bold').tagName).toBe('STRONG')
    expect(screen.getAllByRole('listitem')).toHaveLength(2)
  })

  it('renders GitHub-flavoured tables and task lists', () => {
    render(<Markdown>{'| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done'}</Markdown>)
    expect(screen.getByRole('table')).toBeTruthy()
    expect(screen.getAllByRole('columnheader').map((c) => c.textContent)).toEqual(['a', 'b'])
    expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(true)
  })

  it('opens links in a new tab with noopener noreferrer nofollow', () => {
    render(<Markdown>{'[docs](https://example.com/x)'}</Markdown>)
    const a = screen.getByRole('link', { name: 'docs' })
    expect(a.getAttribute('href')).toBe('https://example.com/x')
    expect(a.getAttribute('target')).toBe('_blank')
    expect(a.getAttribute('rel')).toBe('noopener noreferrer nofollow')
  })

  it('never loads remote images: shows a link with the alt text instead', () => {
    const { container } = render(<Markdown>{'![a cat](https://evil.test/track.png)'}</Markdown>)
    expect(container.querySelector('img')).toBeNull()
    const a = screen.getByRole('link', { name: '[image: a cat]' })
    expect(a.getAttribute('href')).toBe('https://evil.test/track.png')
    expect(a.getAttribute('rel')).toContain('noopener')
  })

  it('labels images without alt text as untitled', () => {
    render(<Markdown>{'![](https://x.test/i.png)'}</Markdown>)
    expect(screen.getByRole('link', { name: '[image: untitled]' })).toBeTruthy()
  })

  it('does not render raw HTML from model output', () => {
    const { container } = render(<Markdown>{'before <script>window.__pwned = 1</script><b>x</b> after'}</Markdown>)
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('b')).toBeNull()
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined()
  })

  it('neutralises javascript: URLs in links', () => {
    render(<Markdown>{'[click](javascript:alert(1))'}</Markdown>)
    const a = screen.queryByRole('link')
    expect(a?.getAttribute('href') ?? '').not.toMatch(/^javascript:/i)
  })

  it('wraps content in the prose container and accepts a className', () => {
    const { container } = render(<Markdown className="extra">text</Markdown>)
    expect(container.firstElementChild!.className).toContain('prose-console')
    expect(container.firstElementChild!.className).toContain('extra')
  })

  it('renders an empty string without crashing', () => {
    const { container } = render(<Markdown>{''}</Markdown>)
    expect(container.querySelector('.prose-console')!.textContent).toBe('')
  })

  it('renders code blocks verbatim', () => {
    const { container } = render(<Markdown>{'```\n<b>not html</b>\n```'}</Markdown>)
    expect(container.querySelector('pre code')!.textContent).toContain('<b>not html</b>')
  })
})
