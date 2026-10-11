import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { CircleHelp } from 'lucide-react'
import { chatApi } from '../../api/endpoints.chat'
import type { ConversationView } from '../../api/types.chat'
import type { HumanQuestion } from '../../api/types.human'
import { useAsync } from '../../lib/useAsync'
import { OpenQuestion, PastQuestion } from '../human/OpenQuestion'
import { useWaitingQuestions } from '../human/waitingQuestions'

// Questions during a hand-off (docs/agent-chat-handoffs-design.md, section 6): when the lead brings a specialist's
// question to the person, it waits in the conversation as a Waiting for you card under its hand-off. What the agents
// wrote (the question, its context) is shown as text, never as markup.

export type ConversationQuestions = {
  /** Every question of the conversation's hand-offs known so far, the latest state of each */
  questions: HumanQuestion[]
  /** Records an answer given here, so its card is replaced at once */
  answered: (q: HumanQuestion) => void
}

/**
 * The questions of a conversation's hand-offs: the conversation's list (GET /chat/conversations/{id}/questions) merged
 * with the open ones the conversation view and the pushed question stream know of. The list is read again whenever a
 * question is asked or answered elsewhere.
 */
export function useConversationQuestions(view: ConversationView): ConversationQuestions {
  const id = view.conversation.id
  const pushed = useWaitingQuestions()
  const hasHandoffs = (view.handoffs?.length ?? 0) > 0 || view.messages.some((m) => m.handoff)
  const list = useAsync(async (signal) => (hasHandoffs ? ((await chatApi.questions(id, signal)) ?? []) : []), [id, hasHandoffs])
  const [overrides, setOverrides] = useState<Record<string, HumanQuestion>>({})
  const open = useMemo(
    () => [...(view.questions ?? []), ...pushed.filter((q) => q.conversation_id === id)],
    [view.questions, pushed, id],
  )
  const key = open.map((q) => q.id).sort().join(',')
  const seen = useRef(key)
  const { reload } = list
  useEffect(() => {
    if (key !== seen.current) {
      seen.current = key
      reload()
    }
  }, [key, reload])

  const questions = useMemo(() => {
    const byId = new Map<string, HumanQuestion>()
    for (const q of open) byId.set(q.id, q)
    // The conversation's list knows the latest state; an answer given here is newer still
    for (const q of list.data ?? []) byId.set(q.id, q)
    for (const q of Object.values(overrides)) byId.set(q.id, q)
    return [...byId.values()]
  }, [open, list.data, overrides])
  const answered = useCallback((q: HumanQuestion) => setOverrides((prev) => ({ ...prev, [q.id]: q })), [])
  return { questions, answered }
}

type Props = {
  /** The questions of one hand-off */
  questions: readonly HumanQuestion[]
  onAnswered: (q: HumanQuestion) => void
}

/** A hand-off's questions: each open one as a Waiting for you card with the answer form, earlier ones folded away. */
export function ChatQuestions({ questions, onAnswered }: Props) {
  if (questions.length === 0) return null
  const open = questions.filter((q) => q.status === 'open')
  const earlier = questions.filter((q) => q.status !== 'open')
  return (
    <section aria-label={open.length > 0 ? 'Waiting for you' : 'Questions from the hand-off'}
      className={`mt-2 rounded-md border ${open.length > 0 ? 'border-accent/60 bg-accent-soft' : 'border-line bg-raised'}`}>
      {open.length > 0 && (
        <div className="flex items-center gap-2 border-b border-line px-4 py-2">
          <CircleHelp className="size-4 text-accent" />
          <span className="eyebrow">Waiting for you</span>
        </div>
      )}
      <div className="flex flex-col divide-y divide-line">
        {open.map((q) => <OpenQuestion key={q.id} question={q} onAnswered={onAnswered} />)}
        {earlier.length > 0 && (
          <details className="px-4 py-2">
            <summary className="cursor-pointer text-[12px] text-muted">Earlier questions ({earlier.length})</summary>
            <ul className="mt-2 flex flex-col gap-2">
              {earlier.map((q) => <PastQuestion key={q.id} question={q} />)}
            </ul>
          </details>
        )}
      </div>
    </section>
  )
}

/** Conversations other than the one open whose hand-offs wait on the person, each a link to it. */
export function WaitingConversations({ current }: { current?: string }) {
  const waiting = useWaitingQuestions().filter((q) => q.conversation_id && q.conversation_id !== current)
  if (waiting.length === 0) return null
  const byConversation = new Map<string, { title: string; count: number }>()
  for (const q of waiting) {
    const id = q.conversation_id as string
    const entry = byConversation.get(id) ?? { title: q.conversation_title || 'A conversation', count: 0 }
    byConversation.set(id, { ...entry, count: entry.count + 1 })
  }
  return (
    <section aria-label="Conversations waiting for you" className="mb-4 flex flex-wrap items-center gap-2 rounded-md border border-accent/60 bg-accent-soft px-3 py-2 text-[12.5px]">
      <CircleHelp className="size-4 text-accent" />
      <span className="font-medium">Waiting for you:</span>
      {[...byConversation.entries()].map(([id, c]) => (
        <Link key={id} to={`/chat/${id}`} className="text-accent hover:underline">
          {c.title}{c.count > 1 ? ` (${c.count})` : ''}
        </Link>
      ))}
    </section>
  )
}
