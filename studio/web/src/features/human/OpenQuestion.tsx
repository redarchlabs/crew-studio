import { useState } from 'react'
import { MessageSquareReply } from 'lucide-react'
import { humanApi } from '../../api/endpoints.human'
import type { HumanQuestion, QuestionStatus } from '../../api/types.human'
import { Button } from '../../components/Button'
import { TextAreaField } from '../../components/Field'
import { useAuth } from '../../lib/auth'
import { dateTime, timeOfDay } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'

// A question an agent asked a person during a run, as the work order's Questions panel and a chat's Waiting for you
// card show it. Everything in it was written by an agent: it is shown as text, never as markup.

export const STATUS_LABEL: Record<QuestionStatus, string> = {
  open: 'waiting',
  answered: 'answered',
  timed_out: 'no answer in time',
  closed: 'run ended',
}

/** Shown on every question from an agent chat's hand-off: its text was written by a model, which text it read can steer */
export const AI_NOTICE = 'Written by an AI agent. Never share passwords or secrets in an answer.'

/** Who asked and when: "Market analyst through Research lead" when the lead asked for a specialist (a hand-off). */
export function Asker({ q }: { q: HumanQuestion }) {
  const who = q.asked_for ? `${q.asked_for} through ${q.agent_role || 'the lead'}` : q.agent_role || 'An agent'
  return (
    <span className="text-[11.5px] text-muted">
      {q.asked_for ? 'Asked by ' : ''}{who}{q.task_name ? ` · ${q.task_name}` : ''} · {timeOfDay(q.asked_at)}
    </span>
  )
}

/** Whether the signed-in person may answer: the person it waits on, or (unaddressed) a work order operator. A
 * hand-off's question is answered signed in, as its cards are decided. */
function useMayAnswer(q: HumanQuestion): boolean {
  const { me, can } = useAuth()
  // Only the conversation's owner, signed in: never a work order operator
  if (q.handoff_id) return me?.via === 'session' && Boolean(q.assignee_id) && q.assignee_id === me?.id
  return q.assignee_id ? q.assignee_id === me?.id : can('work_orders.cancel')
}

/** An open question with the answer form. */
export function OpenQuestion({ question: q, onAnswered }: { question: HumanQuestion; onAnswered: (q: HumanQuestion) => void }) {
  const mayAnswer = useMayAnswer(q)
  const [answer, setAnswer] = useState('')
  const [busy, setBusy] = useState(false)
  const rules = q.handoff_id ? "the agent's rules" : "the crew's rules"

  const send = async () => {
    setBusy(true)
    try {
      onAnswered(await humanApi.answer(q.id, answer.trim()))
      notifySuccess('Sent your answer; the run carries on')
    } catch (err) {
      notifyError('Could not send the answer', err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-2 px-4 py-3">
      <Asker q={q} />
      <p className="whitespace-pre-wrap text-[13.5px] font-medium">{q.question}</p>
      {q.context && <p className="whitespace-pre-wrap text-[12.5px] text-muted">{q.context}</p>}
      <p className="text-[11.5px] text-faint">The run is waiting. Without an answer by {dateTime(q.deadline_at)} it follows {rules}.</p>
      {q.handoff_id && <p className="text-[11.5px] text-warn">{AI_NOTICE}</p>}
      {mayAnswer ? (
        <>
          <TextAreaField label="Your answer" value={answer} onChange={setAnswer} rows={3} maxLength={4000} />
          <div className="flex justify-end">
            <Button size="sm" variant="primary" icon={<MessageSquareReply className="size-4" />} busy={busy} disabled={answer.trim() === ''} onClick={() => void send()}>
              Send answer
            </Button>
          </div>
        </>
      ) : (
        <p className="text-[12px] text-muted">{q.handoff_id ? 'Waiting on the person this conversation belongs to, signed in.' : 'Waiting on the person who started this run.'}</p>
      )}
    </div>
  )
}

/** A question no longer waiting: answered, timed out, or its run ended. */
export function PastQuestion({ question: q }: { question: HumanQuestion }) {
  return (
    <li className="flex flex-col gap-0.5 text-[12.5px]">
      <div className="flex flex-wrap items-center gap-2">
        <Asker q={q} />
        <span className="rounded border border-line px-1 font-mono text-[10.5px] text-faint">{STATUS_LABEL[q.status]}</span>
      </div>
      <p className="whitespace-pre-wrap">{q.question}</p>
      {q.status === 'answered' && (
        <p className="whitespace-pre-wrap text-muted"><span className="text-faint">{q.answered_by_name || 'Someone'}:</span> {q.answer}</p>
      )}
    </li>
  )
}
