import { useRef, type ReactNode } from 'react'
import { Braces } from 'lucide-react'
import { findPlaceholders, splitPlaceholders } from '../lib/placeholders'
import { Field } from './Field'

type PlaceholderTextareaProps = {
  label: ReactNode
  value: string
  onChange: (value: string) => void
  rows?: number
  error?: string
  hint?: ReactNode
  placeholder?: string
  knownKeys?: readonly string[]
}

/**
 * Textarea that highlights {placeholders} using a mirrored layer behind
 * a transparent textarea, plus a chip list of the keys found.
 */
export function PlaceholderTextarea({ label, value, onChange, rows = 5, error, hint, placeholder, knownKeys }: PlaceholderTextareaProps) {
  const mirrorRef = useRef<HTMLDivElement>(null)
  const keys = findPlaceholders(value)
  const known = knownKeys ? new Set(knownKeys) : null

  return (
    <Field label={label} error={error} hint={hint}>
      {(id, invalid) => (
        <>
          <div className="hl-wrap rounded-md bg-raised">
            <div ref={mirrorRef} className="hl-mirror rounded-md" aria-hidden="true">
              {splitPlaceholders(value).map((seg, i) => (seg.placeholder ? <mark key={i}>{seg.text}</mark> : <span key={i}>{seg.text}</span>))}
              {/* Trailing newline keeps the mirror height in sync with the textarea. */}
              {'\n'}
            </div>
            <textarea
              id={id}
              rows={rows}
              aria-invalid={invalid}
              className="field-input hl-textarea"
              value={value}
              placeholder={placeholder}
              spellCheck
              onChange={(e) => onChange(e.target.value)}
              onScroll={(e) => {
                if (mirrorRef.current) mirrorRef.current.scrollTop = e.currentTarget.scrollTop
              }}
            />
          </div>
          {keys.length > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <Braces className="size-3 text-faint" />
              {keys.map((k) => {
                const missing = known !== null && !known.has(k)
                return (
                  <span
                    key={k}
                    title={missing ? 'No crew input defines this key yet' : 'Filled from run inputs'}
                    className={`rounded px-1.5 py-px font-mono text-[11px] ${missing ? 'border border-dashed border-warn bg-warn-soft text-warn' : 'bg-accent-soft text-accent'}`}
                  >
                    {`{${k}}`}
                  </span>
                )
              })}
            </div>
          )}
        </>
      )}
    </Field>
  )
}
