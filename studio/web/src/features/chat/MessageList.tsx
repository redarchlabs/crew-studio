import { Link } from 'react-router'
import { FoldVertical, Loader2 } from 'lucide-react'
import { attachmentsApi } from '../../api/endpoints'
import type { ChatAttachment, ChatMessage } from '../../api/types.chat'
import type { HumanQuestion } from '../../api/types.human'
import { Markdown } from '../../components/Markdown'
import { useAuth } from '../../lib/auth'
import { relative, usd } from '../../lib/format'
import { isImageType } from '../workorders/files/attachments'
import { FileIcon } from '../workorders/files/FileIcon'
import { replyState, threadOrder } from './chatLogic'
import { ChatQuestions } from './ChatQuestions'
import { HandoffCard } from './HandoffCard'
import { ProposalCard } from './ProposalCard'

type Props = {
  messages: readonly ChatMessage[]
  crewId: string
  frontDesk: string
  now: number
  onChange: (m: ChatMessage) => void
  onStale?: () => void
  /** The questions of the conversation's hand-offs; each shows under its hand-off */
  questions?: readonly HumanQuestion[]
  onAnswered?: (q: HumanQuestion) => void
}

function Reply({ m }: { m: ChatMessage }) {
  switch (replyState(m)) {
    case 'text':
      return <Markdown>{m.content}</Markdown>
    case 'waiting':
      return (
        <p className="flex items-center gap-2 text-muted" role="status">
          <Loader2 className="size-3.5 animate-spin" /> {m.run_status === 'queued' ? 'Waiting for a free slot…' : 'Thinking…'}
        </p>
      )
    case 'failed':
      return <p className="text-danger">The reply failed{m.run_error ? `: ${m.run_error}` : '.'}</p>
    default:
      return <p className="text-faint">No reply.</p>
  }
}

/** Files a person sent with a message: images as thumbnails, other files as links; each opens in a new tab. */
function SentFiles({ files }: { files: readonly ChatAttachment[] }) {
  return (
    <ul className="flex flex-wrap justify-end gap-1.5" aria-label="Sent files">
      {files.map((f) => (
        <li key={f.id}>
          <a href={attachmentsApi.contentUrl(f.id)} target="_blank" rel="noreferrer" title={f.filename}
            className="flex items-center gap-1.5 overflow-hidden rounded-md border border-line bg-raised text-[12px] hover:border-accent">
            {isImageType(f.content_type)
              ? <img src={attachmentsApi.contentUrl(f.id)} alt={f.filename} className="max-h-40 max-w-[240px] object-contain" />
              : <span className="flex items-center gap-1.5 px-2 py-1"><FileIcon contentType={f.content_type} filename={f.filename} className="size-4" />{f.filename}</span>}
          </a>
        </li>
      ))}
    </ul>
  )
}

/** A compaction: from here on the crew is given this summary instead of the messages before it. */
function Summary({ m, now }: { m: ChatMessage; now: number }) {
  const { can } = useAuth()
  const state = replyState(m)
  return (
    <li className="w-full rounded-lg border border-dashed border-line bg-raised px-3 py-2 text-[12.5px]" aria-label="Conversation compacted">
      <div className="mb-1 flex flex-wrap items-center gap-2 text-[11px] text-faint">
        <FoldVertical className="size-3.5 text-blue" />
        <span className="font-medium text-muted">Conversation compacted</span>
        <span>{relative(m.created_at, now)}</span>
        {m.cost_usd !== null && <span className="font-mono">{usd(m.cost_usd)}</span>}
        {m.run_id && can('runs.view') && <Link className="hover:text-accent" to={`/runs/${m.run_id}`}>run</Link>}
      </div>
      {state === 'text' && (
        <>
          <p className="text-[11.5px] text-muted">From here on the crew is given this summary instead of the messages above.</p>
          <details className="mt-1">
            <summary className="cursor-pointer text-[12px] text-muted hover:text-fg">Show the summary</summary>
            <div className="mt-2"><Markdown>{m.content}</Markdown></div>
          </details>
        </>
      )}
      {state === 'waiting' && (
        <p className="flex items-center gap-2 text-muted" role="status"><Loader2 className="size-3.5 animate-spin" /> Summarising…</p>
      )}
      {state === 'failed' && <p className="text-danger">The compaction failed{m.run_error ? `: ${m.run_error.replace(/\.$/, '')}` : ''}. The crew still gets the messages above.</p>}
      {state === 'empty' && <p className="text-faint">No summary came back; the crew still gets the messages above.</p>}
    </li>
  )
}

/** The conversation, oldest first: the person's messages on the right, the front desk's replies on the left. A
 * hand-off's result shows as its card until the result arrives, then as a message headed by its worker. */
export function MessageList({ messages, crewId, frontDesk, now, onChange, onStale, questions = [], onAnswered }: Props) {
  const { can } = useAuth()
  const stale = onStale ?? (() => undefined)
  const answered = onAnswered ?? (() => undefined)
  const asked = (m: ChatMessage) => questions.filter((q) => m.handoff && q.handoff_id === m.handoff.id)
  // Who wrote an assistant message: the front desk or agent, or a hand-off's worker (its card has its own run link). A
  // card the hand-off proposed while it worked is the worker's too, not its result.
  const author = (m: ChatMessage) => (m.handoff
    ? `${m.handoff.worker_name} (${m.content && !m.handoff_proposal ? 'hand-off result' : 'hand-off'})` : frontDesk)
  const runLink = (m: ChatMessage) => Boolean(m.run_id) && can('runs.view') && !(m.handoff && !m.content)
  return (
    <ol className="flex flex-col gap-3" aria-label="Messages">
      {threadOrder(messages).map((m) =>
        m.role === 'summary' ? (
          <Summary key={m.id} m={m} now={now} />
        ) : m.role === 'user' ? (
          <li key={m.id} className="ml-auto flex max-w-[80%] flex-col items-end gap-1.5">
            <div className="rounded-lg bg-accent px-3 py-2 text-[13px] whitespace-pre-wrap break-words text-white">{m.content}</div>
            {(m.attachments?.length ?? 0) > 0 && <SentFiles files={m.attachments ?? []} />}
          </li>
        ) : (
          <li key={m.id} className="mr-auto w-full max-w-[90%] rounded-lg border border-line bg-panel px-3 py-2 text-[13px]">
            <div className="mb-1 flex flex-wrap items-center gap-2 text-[11px] text-faint">
              <span className="font-medium text-muted">{author(m)}</span>
              <span>{relative(m.created_at, now)}</span>
              {m.cost_usd !== null && <span className="font-mono">{usd(m.cost_usd)}</span>}
              {runLink(m) && <Link className="hover:text-accent" to={`/runs/${m.run_id}`}>run</Link>}
            </div>
            {m.handoff_proposal && m.handoff ? (
              // One plain line: the card itself says what would change (its text is the model's, so never Markdown)
              <p className="text-[12.5px] text-muted">{m.handoff.worker_name} proposed this during the hand-off “{m.handoff.title}”.</p>
            ) : m.handoff && !m.content ? <HandoffCard message={m} now={now} onStale={stale} /> : <Reply m={m} />}
            {m.handoff && !m.handoff_proposal && <ChatQuestions questions={asked(m)} onAnswered={answered} />}
            {m.proposal && <ProposalCard message={m} crewId={crewId} onChange={onChange} onStale={onStale} />}
          </li>
        ),
      )}
    </ol>
  )
}
