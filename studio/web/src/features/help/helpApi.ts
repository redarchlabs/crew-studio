import { request } from '../../api/client'

export type HelpHeading = { id: string; title: string }
export type HelpContentsArticle = { slug: string; title: string; summary: string; headings: HelpHeading[] }
export type HelpCategory = { title: string; articles: HelpContentsArticle[] }
export type HelpContents = { categories: HelpCategory[] }

export type HelpSection = { id: string; title: string; markdown: string }
export type HelpArticle = { slug: string; title: string; category: string; summary: string; sections: HelpSection[] }

export type HelpSearchMode = 'semantic' | 'keyword'
export type HelpResult = {
  slug: string
  article_title: string
  category: string
  section_id: string
  section_title: string
  snippet: string
  score: number
}
export type HelpResults = { query: string; mode: HelpSearchMode; results: HelpResult[] }

/** Screenshots are served from here; anything else in help Markdown is not loaded as an image. */
export const HELP_IMAGE_PREFIX = '/api/help/images/'

/** The server filters all of this by the signed-in person's permissions. */
export const helpApi = {
  contents: (signal?: AbortSignal) => request<HelpContents>('GET', '/help', { signal }),
  article: (slug: string, signal?: AbortSignal) => request<HelpArticle>('GET', `/help/articles/${encodeURIComponent(slug)}`, { signal }),
  search: (q: string, signal?: AbortSignal) => request<HelpResults>('GET', '/help/search', { query: { q }, signal }),
}

/** Where a search result or heading lives in the help page. */
export function helpPath(slug: string, sectionId?: string): string {
  return `/help/${slug}${sectionId && sectionId !== 'intro' ? `#${sectionId}` : ''}`
}
