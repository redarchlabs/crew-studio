import { useEffect, useRef, useState, type RefObject } from 'react'
import { Link, useLocation, useParams } from 'react-router'
import { LifeBuoy } from 'lucide-react'
import { ApiError } from '../../api/client'
import { PageHeader } from '../../components/PageHeader'
import { EmptyState, ErrorState, Loading } from '../../components/States'
import { ImageLightbox, type LightboxImage } from '../../components/ImageLightbox'
import { useAsync } from '../../lib/useAsync'
import { HelpMarkdown } from './HelpMarkdown'
import { HelpSearchBox, HelpSearchResults, MIN_QUERY, useHelpSearch } from './HelpSearch'
import { HelpToc } from './HelpToc'
import { helpApi, helpPath, type HelpArticle, type HelpCategory } from './helpApi'

/** The section id in a URL hash; a malformed escape (a mangled pasted link) is used as written. */
export function hashTarget(hash: string): string {
  const raw = hash.replace(/^#/, '')
  try {
    return decodeURIComponent(raw)
  } catch {
    return raw
  }
}

/** Brings the section in the URL hash, or else the top of the article, into view when either changes. */
function useScrollIntoView(hash: string, slug: string, top: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const id = hashTarget(hash)
    const el = id ? document.getElementById(id) : top.current
    if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ block: 'start' })
  }, [hash, slug, top])
}

function HelpHome({ categories }: { categories: HelpCategory[] }) {
  return (
    <div className="flex flex-col gap-6">
      {categories.map((cat) => (
        <section key={cat.title}>
          <h2 className="eyebrow mb-2">{cat.title}</h2>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {cat.articles.map((a) => (
              <Link key={a.slug} to={helpPath(a.slug)} className="rounded-lg border border-line bg-panel px-4 py-3 hover:border-line-strong">
                <div className="font-medium">{a.title}</div>
                <div className="mt-0.5 text-[12.5px] text-muted">{a.summary}</div>
              </Link>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function ArticleBody({ article, hash }: { article: HelpArticle; hash: string }) {
  const [shot, setShot] = useState<LightboxImage | null>(null)
  const top = useRef<HTMLElement>(null)
  useScrollIntoView(hash, article.slug, top)
  return (
    <article ref={top} aria-labelledby="help-article-title" className="scroll-mt-4">
      <div className="eyebrow mb-1">{article.category}</div>
      <h2 id="help-article-title" className="text-[20px] font-semibold tracking-tight">{article.title}</h2>
      {article.summary && <p className="mt-1 text-muted">{article.summary}</p>}
      {article.sections.map((s) => (
        <section key={s.id} id={s.id} aria-labelledby={s.title ? `${s.id}--heading` : undefined} className="mt-6 scroll-mt-4">
          {s.title && <h3 id={`${s.id}--heading`} className="mb-2 border-b border-line pb-1 text-[16px] font-semibold">{s.title}</h3>}
          <HelpMarkdown onImage={(src, title) => setShot({ src, title })}>{s.markdown}</HelpMarkdown>
        </section>
      ))}
      {shot && <ImageLightbox images={[shot]} index={0} onIndex={() => {}} onClose={() => setShot(null)} />}
    </article>
  )
}

function ArticleView({ slug, hash }: { slug: string; hash: string }) {
  const { data, error, loading, reload } = useAsync((signal) => helpApi.article(slug, signal), [slug])
  if (error instanceof ApiError && error.status === 404) {
    return (
      <EmptyState
        title="No help article here"
        hint="It may not exist, or it covers a screen your access does not include."
        action={<Link className="text-accent" to="/help">Back to help contents</Link>}
      />
    )
  }
  if (error) return <ErrorState error={error} onRetry={reload} />
  if (loading || !data) return <Loading />
  return <ArticleBody article={data} hash={hash} />
}

export function HelpPage() {
  const { slug } = useParams()
  const { hash } = useLocation()
  const [query, setQuery] = useState('')
  const [tocOpen, setTocOpen] = useState<boolean | null>(null)
  // Until toggled, phones show the contents on the help home and hide them on an article
  const tocShown = tocOpen ?? !slug
  // Opening another article folds the phone contents away again
  useEffect(() => setTocOpen(null), [slug])
  const search = useHelpSearch(query)
  const contents = useAsync((signal) => helpApi.contents(signal), [])
  const searching = query.trim().length >= MIN_QUERY
  const categories = contents.data?.categories ?? []

  let main
  if (slug) main = <ArticleView key={slug} slug={slug} hash={hash} />
  else if (contents.error) main = <ErrorState error={contents.error} onRetry={contents.reload} />
  else if (contents.loading) main = <Loading />
  else if (categories.length === 0) main = <EmptyState title="No help yet" hint="There is no help for the screens you can use." />
  else main = <HelpHome categories={categories} />

  return (
    <div>
      <PageHeader
        eyebrow="Help"
        title={<span className="inline-flex items-center gap-2"><LifeBuoy className="size-5 text-accent" /> Help</span>}
        description="Guides for the parts of Crew Studio you can use. Search in your own words, or browse the contents."
        actions={<HelpSearchBox value={query} onChange={setQuery} />}
      />
      <div className="grid gap-6 md:grid-cols-[230px_minmax(0,1fr)]">
        <aside className="md:sticky md:top-0 md:max-h-[calc(100vh-7rem)] md:self-start md:overflow-y-auto">
          {contents.data && (
            <>
              {/* Phones keep the contents folded away while reading an article */}
              <button type="button" className="mb-2 font-medium md:hidden" aria-expanded={tocShown} onClick={() => setTocOpen(!tocShown)}>
                {tocShown ? 'Hide contents' : 'Show contents'}
              </button>
              <div className={tocShown ? 'block' : 'hidden md:block'}>
                <HelpToc categories={categories} activeSlug={slug} activeSection={hash.slice(1)} />
              </div>
            </>
          )}
        </aside>
        <div className="min-w-0 max-w-3xl">
          {searching && <HelpSearchResults state={search} onPick={() => setQuery('')} />}
          {/* Kept mounted while searching so clearing the search returns to the same place in the article */}
          <div hidden={searching}>{main}</div>
        </div>
      </div>
    </div>
  )
}

export default HelpPage
