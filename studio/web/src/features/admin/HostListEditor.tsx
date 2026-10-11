import { useState, type KeyboardEvent } from 'react'
import { Plus, X } from 'lucide-react'
import { Button } from '../../components/Button'
import { hostProblem, normalizeHost } from './hosts'

type HostListEditorProps = { hosts: readonly string[]; onChange: (hosts: string[]) => void; disabled?: boolean }

/** Chip list of host names / IPs with an add box that validates each entry. */
export function HostListEditor({ hosts, onChange, disabled }: HostListEditorProps) {
  const [draft, setDraft] = useState('')
  const [error, setError] = useState<string | null>(null)

  const add = () => {
    const host = normalizeHost(draft)
    const problem = hostProblem(host, hosts)
    if (problem) return setError(problem)
    onChange([...hosts, host])
    setDraft('')
    setError(null)
  }

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      add()
    }
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex min-h-9 flex-wrap gap-1.5 rounded-md border border-line bg-raised p-2">
        {hosts.length === 0 && <span className="text-[12px] text-faint">No hosts: callbacks may only reach public https addresses.</span>}
        {hosts.map((h) => (
          <span key={h} className="inline-flex items-center gap-1 rounded border border-line bg-panel-2 py-px pl-1.5 pr-0.5 font-mono text-[12px]">
            {h}
            {!disabled && (
              <button type="button" aria-label={`Remove ${h}`} className="rounded p-0.5 text-faint hover:bg-danger-soft hover:text-danger" onClick={() => onChange(hosts.filter((x) => x !== h))}>
                <X className="size-3" />
              </button>
            )}
          </span>
        ))}
      </div>
      {!disabled && (
        <div className="flex gap-2">
          <input
            aria-label="Host to allow"
            aria-invalid={Boolean(error)}
            className="field-input font-mono"
            placeholder="erp.internal or 10.0.0.5"
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value)
              setError(null)
            }}
            onKeyDown={onKey}
          />
          <Button icon={<Plus className="size-3.5" />} disabled={!draft.trim()} onClick={add}>Add</Button>
        </div>
      )}
      {error && <div className="text-[12px] text-danger" role="alert">{error}</div>}
    </div>
  )
}
