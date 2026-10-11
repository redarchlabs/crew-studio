import { Link, NavLink } from 'react-router'
import type { HelpCategory } from './helpApi'
import { helpPath } from './helpApi'

type HelpTocProps = { categories: HelpCategory[]; activeSlug?: string; activeSection?: string }

/** Table of contents: categories, their articles and, for the open article, its sections. */
export function HelpToc({ categories, activeSlug, activeSection }: HelpTocProps) {
  return (
    <nav aria-label="Help contents" className="flex flex-col gap-4 text-[13px]">
      {categories.map((cat) => (
        <div key={cat.title}>
          <div className="eyebrow mb-1 px-2">{cat.title}</div>
          <ul className="flex flex-col">
            {cat.articles.map((a) => {
              const open = a.slug === activeSlug
              return (
                <li key={a.slug}>
                  <NavLink
                    to={helpPath(a.slug)}
                    title={a.summary}
                    className={`block rounded-md px-2 py-1 ${open ? 'bg-panel-2 font-medium text-fg' : 'text-muted hover:bg-panel-2/60 hover:text-fg'}`}
                  >
                    {a.title}
                  </NavLink>
                  {open && a.headings.length > 0 && (
                    <ul className="mb-1 ml-3 mt-0.5 flex flex-col border-l border-line pl-2">
                      {a.headings.map((h) => (
                        <li key={h.id}>
                          <Link
                            to={helpPath(a.slug, h.id)}
                            aria-current={activeSection === h.id ? 'location' : undefined}
                            className={`block rounded px-1.5 py-0.5 text-[12.5px] ${activeSection === h.id ? 'text-accent' : 'text-muted hover:text-fg'}`}
                          >
                            {h.title}
                          </Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}
