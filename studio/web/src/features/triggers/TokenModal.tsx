import { useMemo } from 'react'
import { TriangleAlert } from 'lucide-react'
import type { Trigger } from '../../api/types'
import { triggersApi } from '../../api/endpoints'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { CopyButton } from '../../components/misc'
import { curlExample, exampleIdempotencyKey, samplePayload } from './hookExamples'

function Snippet({ label, text, mono = true }: { label: string; text: string; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <div className="flex items-center justify-between">
        <span className="eyebrow">{label}</span>
        <CopyButton text={text} />
      </div>
      <pre className={`max-h-56 overflow-auto whitespace-pre-wrap break-all rounded-md border border-line bg-raised px-2.5 py-2 text-[11.5px] leading-relaxed ${mono ? 'font-mono' : ''}`}>{text}</pre>
    </div>
  )
}

/** Shows a freshly created or rotated token exactly once, with ready-to-use examples. */
export function TokenModal({ trigger, token, onClose }: { trigger: Trigger; token: string; onClose: () => void }) {
  const url = triggersApi.hookUrl(trigger.id)
  const payload = useMemo(() => samplePayload(trigger.input_map), [trigger.input_map])
  const key = useMemo(() => exampleIdempotencyKey(), [])
  return (
    <Modal title="Trigger token" onClose={onClose} width="max-w-2xl" footer={<Button variant="primary" onClick={onClose}>I have saved the token</Button>}>
      <div className="flex flex-col gap-4">
        <p className="flex items-start gap-2 rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px] text-warn">
          <TriangleAlert className="mt-0.5 size-4 shrink-0" />
          Copy the token now. It is shown only once; if you lose it, rotate the token and update the caller.
        </p>
        <Snippet label="Token" text={token} />
        <Snippet label="Hook URL" text={url} />
        <Snippet label="curl" text={curlExample(url, token, payload, key)} />
        <Snippet label="Sample JSON payload" text={JSON.stringify(payload, null, 2)} />
        <p className="text-[12px] text-muted">
          Send <code className="font-mono">Authorization: Bearer &lt;token&gt;</code> (or <code className="font-mono">X-CrewStudio-Token</code>) with a JSON body up to 1 MB.
          Reuse an <code className="font-mono">Idempotency-Key</code> when retrying so the same event never creates two work orders. The answer is <code className="font-mono">202</code> with the new work order&apos;s id, number and status URL.
        </p>
      </div>
    </Modal>
  )
}
