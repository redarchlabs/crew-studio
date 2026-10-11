import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { ApiError } from '../../api/client'
import { autoCleanup, LocationProbe, renderDataRouter } from '../../lib/testUtils'
import { hashTarget, HelpPage } from './HelpPage'
import { HelpMarkdown, isHelpImage } from './HelpMarkdown'
import type { HelpArticle, HelpContents, HelpResults } from './helpApi'

const helpMock = vi.hoisted(() => ({ contents: vi.fn(), article: vi.fn(), search: vi.fn() }))
vi.mock('./helpApi', async (importOriginal) => ({ ...(await importOriginal<typeof import('./helpApi')>()), helpApi: helpMock }))

autoCleanup()

const CONTENTS: HelpContents = {
  categories: [
    { title: 'Getting started', articles: [{ slug: 'getting-started', title: 'Getting started', summary: 'Sign in and find your way.', headings: [] }] },
    {
      title: 'Build',
      articles: [
        { slug: 'crews', title: 'Crews', summary: 'Build crews.', headings: [{ id: 'run-a-crew', title: 'Run a crew' }] },
      ],
    },
  ],
}

const CREWS: HelpArticle = {
  slug: 'crews',
  title: 'Crews',
  category: 'Build',
  summary: 'Build crews.',
  sections: [
    { id: 'intro', title: '', markdown: 'A crew is a team.\n\n![The crews list.](/api/help/images/crews.png)\n\nSee [agents](/help/agents).' },
    { id: 'run-a-crew', title: 'Run a crew', markdown: 'Press **Run**.' },
  ],
}

const RESULTS: HelpResults = {
  query: 'spending cap',
  mode: 'semantic',
  results: [
    { slug: 'crews', article_title: 'Crews', category: 'Build', section_id: 'run-a-crew', section_title: 'Run a crew', snippet: 'Set a budget.', score: 0.03 },
  ],
}

function renderHelp(path = '/help') {
  return renderDataRouter(
    [
      { path: '/help', element: <><HelpPage /><LocationProbe /></> },
      { path: '/help/:slug', element: <><HelpPage /><LocationProbe /></> },
    ],
    [path],
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  helpMock.contents.mockResolvedValue(CONTENTS)
  helpMock.article.mockResolvedValue(CREWS)
  helpMock.search.mockResolvedValue(RESULTS)
})

describe('HelpPage contents', () => {
  it('lists the categories and articles the person may see', async () => {
    renderHelp()
    const toc = await screen.findByRole('navigation', { name: 'Help contents' })
    expect(within(toc).getByRole('link', { name: 'Crews' }).getAttribute('href')).toBe('/help/crews')
    expect(screen.getByText('Sign in and find your way.')).toBeTruthy()
    expect(helpMock.article).not.toHaveBeenCalled()
  })

  it('shows an error with retry when the contents fail', async () => {
    helpMock.contents.mockRejectedValueOnce(new ApiError(500, { code: 'internal', message: 'help is down' }))
    renderHelp()
    expect(await screen.findByText('help is down')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await screen.findByText('Sign in and find your way.')
  })

  it('says so when there is no help for this person', async () => {
    helpMock.contents.mockResolvedValue({ categories: [] })
    renderHelp()
    expect(await screen.findByText('No help yet')).toBeTruthy()
  })
})

describe('HelpPage article', () => {
  it('renders sections, screenshots with captions and the section headings in the contents', async () => {
    renderHelp('/help/crews')
    expect(await screen.findByRole('heading', { name: 'Crews', level: 2 })).toBeTruthy()
    expect(helpMock.article).toHaveBeenCalledWith('crews', expect.anything())
    const img = screen.getByRole('img', { name: 'The crews list.' })
    expect(img.getAttribute('src')).toBe('/api/help/images/crews.png')
    expect(screen.getByText('The crews list.', { selector: '.help-caption' })).toBeTruthy()
    expect(document.getElementById('run-a-crew')).toBeTruthy()
    const toc = screen.getByRole('navigation', { name: 'Help contents' })
    expect(within(toc).getByRole('link', { name: 'Run a crew' }).getAttribute('href')).toBe('/help/crews#run-a-crew')
  })

  it('opens a screenshot full size and closes it', async () => {
    renderHelp('/help/crews')
    fireEvent.click(await screen.findByRole('button', { name: 'Enlarge screenshot: The crews list.' }))
    expect(screen.getByRole('dialog', { name: 'The crews list.' })).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('keeps help links inside the app', async () => {
    const { router } = renderHelp('/help/crews')
    fireEvent.click(await screen.findByRole('link', { name: 'agents' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/help/agents'))
  })

  it('scrolls to the section in the address', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    renderHelp('/help/crews#run-a-crew')
    await screen.findByRole('heading', { name: 'Run a crew', level: 3 })
    await waitFor(() => expect(scroll).toHaveBeenCalled())
  })

  it('survives a malformed address hash', async () => {
    expect(hashTarget('#%E0%A4%A')).toBe('%E0%A4%A')
    expect(hashTarget('#run%20it')).toBe('run it')
    renderHelp('/help/crews#%E0%A4%A')
    expect(await screen.findByRole('heading', { name: 'Crews', level: 2 })).toBeTruthy()
  })

  it('keeps the article while searching and returns to it afterwards', async () => {
    renderHelp('/help/crews')
    await screen.findByRole('heading', { name: 'Crews', level: 2 })
    const box = screen.getByRole('searchbox', { name: 'Search help' })
    fireEvent.change(box, { target: { value: 'spending cap' } })
    await screen.findByRole('region', { name: 'Search results' })
    expect(screen.queryByRole('heading', { name: 'Crews', level: 2 })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(screen.getByRole('heading', { name: 'Crews', level: 2 })).toBeTruthy()
    expect(helpMock.article).toHaveBeenCalledTimes(1)
  })

  it('explains a missing or hidden article', async () => {
    helpMock.article.mockRejectedValue(new ApiError(404, { code: 'not_found', message: 'Not found.' }))
    renderHelp('/help/admin-people')
    expect(await screen.findByText('No help article here')).toBeTruthy()
  })

  it('shows other article errors with retry', async () => {
    helpMock.article.mockRejectedValueOnce(new ApiError(500, { code: 'internal', message: 'boom' }))
    renderHelp('/help/crews')
    expect(await screen.findByText('boom')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('heading', { name: 'Crews', level: 2 })).toBeTruthy()
  })
})

describe('HelpPage search', () => {
  it('searches after a pause and shows ranked results with the mode', async () => {
    renderHelp()
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search help' }), { target: { value: 'spending cap' } })
    expect(screen.getByRole('status').textContent).toContain('Searching')
    const results = await screen.findByRole('region', { name: 'Search results' })
    expect(helpMock.search).toHaveBeenCalledTimes(1)
    expect(helpMock.search).toHaveBeenCalledWith('spending cap', expect.anything())
    expect(within(results).getByText('Semantic')).toBeTruthy()
    expect(within(results).getByText('Set a budget.')).toBeTruthy()

    fireEvent.click(within(results).getByRole('link', { name: /Run a crew/ }))
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/help/crews'))
    expect((screen.getByRole('searchbox', { name: 'Search help' }) as HTMLInputElement).value).toBe('')
  })

  it('does not search for a single character and clears with Escape', async () => {
    renderHelp()
    const box = await screen.findByRole('searchbox', { name: 'Search help' })
    fireEvent.change(box, { target: { value: 'a' } })
    await new Promise((r) => setTimeout(r, 400))
    expect(helpMock.search).not.toHaveBeenCalled()
    fireEvent.change(box, { target: { value: 'ab' } })
    fireEvent.keyDown(box, { key: 'Escape' })
    expect((box as HTMLInputElement).value).toBe('')
  })

  it('shows keyword mode, empty results and errors', async () => {
    helpMock.search.mockResolvedValueOnce({ query: 'zz', mode: 'keyword', results: [] })
    renderHelp()
    const box = screen.getByRole('searchbox', { name: 'Search help' })
    fireEvent.change(box, { target: { value: 'zz' } })
    expect(await screen.findByText('No matching help')).toBeTruthy()
    expect(screen.getByText('Keyword')).toBeTruthy()

    helpMock.search.mockRejectedValueOnce(new ApiError(429, { code: 'too_many_requests', message: 'Searching too quickly; wait a moment.' }))
    fireEvent.change(box, { target: { value: 'zzz' } })
    expect((await screen.findByRole('alert')).textContent).toContain('Searching too quickly')
    fireEvent.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(screen.queryByRole('region', { name: 'Search results' })).toBeNull()
  })
})

describe('HelpMarkdown', () => {
  it('only loads screenshots from the help image endpoint', () => {
    render(
      <MemoryRouter>
        <HelpMarkdown>{'![remote](https://evil.example/x.png) ![sneaky](/api/help/images/../x.png) [site](https://example.com)'}</HelpMarkdown>
      </MemoryRouter>,
    )
    expect(screen.queryByRole('img')).toBeNull()
    expect(screen.getByText('[remote]')).toBeTruthy()
    const external = screen.getByRole('link', { name: 'site' })
    expect(external.getAttribute('target')).toBe('_blank')
    expect(external.getAttribute('rel')).toContain('noopener')
    expect(isHelpImage('/api/help/images/a.png')).toBe(true)
    expect(isHelpImage(undefined)).toBe(false)
  })
})
