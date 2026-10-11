import { useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { Button } from '../../components/Button'
import { CopyButton } from '../../components/misc'
import { NODE_VERIFY, PYTHON_VERIFY } from './hookExamples'

/** The trigger's callback signing secret (present only for triggers.manage) and how to verify signatures. */
export function SigningSecret({ secret }: { secret: string | undefined }) {
  const [shown, setShown] = useState(false)
  const [lang, setLang] = useState<'node' | 'python'>('node')
  const snippet = lang === 'node' ? NODE_VERIFY : PYTHON_VERIFY
  return (
    <div className="flex flex-col gap-2.5 text-[12.5px]">
      {secret ? (
        <div className="flex items-center gap-1.5">
          <code className="min-w-0 flex-1 truncate rounded border border-line bg-raised px-2 py-1 font-mono text-[11.5px]">{shown ? secret : '•'.repeat(Math.min(40, secret.length))}</code>
          <Button size="sm" variant="ghost" icon={shown ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />} onClick={() => setShown((s) => !s)}>{shown ? 'Hide' : 'Reveal'}</Button>
          <CopyButton text={secret} />
        </div>
      ) : (
        <p className="text-faint">Only people who can manage triggers see the signing secret.</p>
      )}
      <p className="text-muted">
        Callbacks carry <code className="font-mono">X-CrewStudio-Timestamp</code> and <code className="font-mono">X-CrewStudio-Signature: sha256=&lt;hex&gt;</code>, the HMAC-SHA256 of
        <code className="font-mono"> "&lt;timestamp&gt;.&lt;body&gt;"</code> with this secret. Recompute it over the raw body, compare in constant time, and reject timestamps older than 5 minutes.
      </p>
      <div className="flex items-center gap-1">
        {(['node', 'python'] as const).map((l) => (
          <button key={l} type="button" aria-pressed={lang === l} onClick={() => setLang(l)} className={`rounded-full border px-2 py-0.5 text-[11.5px] ${lang === l ? 'border-accent/60 bg-accent-soft' : 'border-line text-muted'}`}>
            {l === 'node' ? 'Node.js' : 'Python'}
          </button>
        ))}
        <span className="ml-auto"><CopyButton text={snippet} /></span>
      </div>
      <pre className="max-h-64 overflow-auto rounded-md border border-line bg-raised px-2.5 py-2 font-mono text-[11px] leading-relaxed">{snippet}</pre>
    </div>
  )
}
