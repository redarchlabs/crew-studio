import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { Search, Sparkles, X } from 'lucide-react'
import { errorMessage, isAbort } from '../../api/client'
import { helpApi, helpPath, type HelpResults } from './helpApi'

export const SEARCH_DEBOUNCE_MS = 300
export const MIN_QUERY = 2
export const MAX_QUERY = 200

type SearchState = { status: 'idle' } | { status: 'loading' } | { status: 'done'; data: HelpResults } | { status: 'error'; message: string }

/** Debounced search over the help the person may see; results come back already permission-filtered. */
export function useHelpSearch(query: string): SearchState {
  const [state, setState] = useState<SearchState>({ status: 'idle' })
  const q = query.trim()
  useEffect(() => {
    if (q.length < MIN_QUERY) {
      setState({ status: 'idle' })
      return
    }
    setState({ status: 'loading' })
    const ctrl = new AbortController()
    const timer = setTimeout(() => {
      helpApi
        .search(q.slice(0, MAX_QUERY), ctrl.signal)
        .then((data) => setState({ status: 'done', data }))
        .catch((err: unknown) => {
          if (!isAbort(err)) setState({ status: 'error', message: errorMessage(err) })
        })
    }, SEARCH_DEBOUNCE_MS)
    return () => {
      clearTimeout(timer)
      ctrl.abort()
    }
  }, [q])
  return state
}

type SearchBoxProps = { value: string; onChange: (value: string) => void }

export function HelpSearchBox({ value, onChange }: SearchBoxProps) {
  return (
    <div className="relative w-full max-w-xl">
      <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-faint" />
      <input
        type="search"
        aria-label="Search help"
        placeholder="Describe what you want to do…"
        className="field-input w-full !pl-8 !pr-8"
        maxLength={MAX_QUERY}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && onChange('')}
      />
      {value && (
        <button type="button" aria-label="Clear search" className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-0.5 text-faint hover:text-fg" onClick={() => onChange('')}>
          <X className="size-4" />
        </button>
      )}
    </div>
  )
}

type ResultsProps = { state: SearchState; onPick: () => void }

export function HelpSearchResults({ state, onPick }: ResultsProps) {
  if (state.status === 'idle') return null
  if (state.status === 'loading') return <div role="status" className="py-6 text-muted">Searching…</div>
  if (state.status === 'error') return <div role="alert" className="py-6 text-danger">{state.message}</div>
  const { results, mode } = state.data
  return (
    <section aria-label="Search results">
      <div className="mb-2 flex items-center gap-2 text-[12px] text-muted">
        <span role="status" aria-live="polite">{results.length === 0 ? 'No matching help' : `${results.length} result${results.length === 1 ? '' : 's'}`}</span>
        {mode === 'semantic' ? (
          <span className="inline-flex items-center gap-1 rounded-full bg-accent-soft px-2 py-0.5 text-accent" title="Ranked by meaning and by keywords">
            <Sparkles className="size-3" /> Semantic
          </span>
        ) : (
          <span className="rounded-full bg-panel-2 px-2 py-0.5" title="Ranked by matching words; semantic search is unavailable right now">
            Keyword
          </span>
        )}
      </div>
      {results.length === 0 ? (
        <p className="text-muted">Try other words, or browse the contents. Help only covers screens you have access to.</p>
      ) : (
        <ol className="flex flex-col gap-2">
          {results.map((r) => (
            <li key={`${r.slug}#${r.section_id}`}>
              <Link to={helpPath(r.slug, r.section_id)} onClick={onPick} className="block rounded-lg border border-line bg-panel px-4 py-3 hover:border-line-strong">
                <div className="text-[11.5px] text-faint">{r.category} · {r.article_title}</div>
                <div className="font-medium">{r.section_title || r.article_title}</div>
                {r.snippet && <div className="mt-1 line-clamp-2 text-[12.5px] text-muted">{r.snippet}</div>}
              </Link>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
