import type { ChatMessage, LlmCall } from '../../../api/types'
import { CopyButton } from '../../../components/misc'

const ROLE_TONE: Record<string, string> = {
  system: 'text-violet bg-violet-soft',
  user: 'text-blue bg-blue-soft',
  assistant: 'text-teal bg-teal-soft',
  tool: 'text-accent bg-accent-soft',
}

/** Message content may be a string or an array of typed parts; show text parts, JSON for the rest. */
export function contentText(content: unknown): string {
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part: unknown) => {
        if (typeof part === 'string') return part
        if (typeof part === 'object' && part !== null && 'text' in part && typeof (part as { text: unknown }).text === 'string') return (part as { text: string }).text
        return JSON.stringify(part)
      })
      .join('\n')
  }
  if (content === null || content === undefined) return ''
  return JSON.stringify(content, null, 2)
}

export function MessageBlock({ message }: { message: ChatMessage }) {
  const text = contentText(message.content)
  const tone = ROLE_TONE[message.role] ?? 'text-muted bg-panel-2'
  return (
    <div className="rounded-md border border-line bg-raised">
      <div className="flex items-center justify-between border-b border-line px-2.5 py-1">
        <span className={`rounded px-1.5 py-px font-mono text-[10.5px] uppercase ${tone}`}>{message.role}</span>
        <span className="font-mono text-[10.5px] text-faint">{text.length.toLocaleString()} chars</span>
      </div>
      <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words px-2.5 py-2 font-mono text-[11.5px] leading-relaxed">{text || <span className="text-faint">(empty)</span>}</pre>
    </div>
  )
}

/** Full prompt (messages) and response of one LLM call. */
export function LlmCallBody({ call }: { call: LlmCall }) {
  return (
    <div className="grid gap-3 lg:grid-cols-2">
      <div className="flex min-w-0 flex-col gap-2">
        <div className="eyebrow">Messages ({call.messages.length})</div>
        {call.messages.map((m, i) => (
          <MessageBlock key={i} message={m} />
        ))}
      </div>
      <div className="flex min-w-0 flex-col gap-2">
        <div className="flex items-center justify-between">
          <div className="eyebrow">Response</div>
          <CopyButton text={call.response} />
        </div>
        <pre className="max-h-[560px] overflow-auto whitespace-pre-wrap break-words rounded-md border border-line bg-raised px-2.5 py-2 font-mono text-[11.5px] leading-relaxed">{call.response || <span className="text-faint">(empty)</span>}</pre>
      </div>
    </div>
  )
}
