import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Link } from 'react-router'
import { HELP_IMAGE_PREFIX } from './helpApi'

type HelpMarkdownProps = {
  children: string
  /** Called with the screenshot's src when someone clicks it, to open it full size. */
  onImage?: (src: string, caption: string) => void
}

/** Only our own screenshot endpoint is loaded; anything else stays a caption. */
export function isHelpImage(src: unknown): src is string {
  return typeof src === 'string' && src.startsWith(HELP_IMAGE_PREFIX) && !src.includes('..')
}

function components(onImage?: HelpMarkdownProps['onImage']): Components {
  return {
    // Sections are h3 on the page, so headings inside them sit one level lower
    h3: ({ children }) => <h4>{children}</h4>,
    h4: ({ children }) => <h5>{children}</h5>,
    // In-app links stay in the app; anything else opens in a new tab
    a: ({ href, children }) =>
      href?.startsWith('/') && !href.startsWith('//') ? (
        <Link to={href}>{children}</Link>
      ) : (
        <a href={href} target="_blank" rel="noopener noreferrer nofollow">
          {children}
        </a>
      ),
    img: ({ src, alt }) => {
      const caption = alt ?? ''
      if (!isHelpImage(src)) return <span className="text-faint">[{caption || 'image'}]</span>
      return (
        <span className="help-figure">
          <button type="button" className="help-shot" onClick={() => onImage?.(src, caption)} aria-label={`Enlarge screenshot: ${caption}`}>
            <img src={src} alt={caption} loading="lazy" />
          </button>
          {caption && <span className="help-caption">{caption}</span>}
        </span>
      )
    },
  }
}

/** Renders help article Markdown (trusted, from the server) with screenshots as captioned figures. */
export function HelpMarkdown({ children, onImage }: HelpMarkdownProps) {
  return (
    <div className="prose-console help-prose">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components(onImage)} skipHtml>
        {children}
      </ReactMarkdown>
    </div>
  )
}
