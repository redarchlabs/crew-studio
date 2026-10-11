import { useEffect, useRef } from 'react'
import { CircleHelp } from 'lucide-react'
import { humanApi } from '../../../api/endpoints.human'
import type { HumanQuestion } from '../../../api/types.human'
import { useAsync } from '../../../lib/useAsync'
import { OpenQuestion, PastQuestion } from '../../human/OpenQuestion'
import { useWaitingQuestions } from '../../human/waitingQuestions'

type Props = {
  workOrderId: string
  /** Changes when the attempt's journal gains a question or an answer, so the list is fetched again */
  refreshKey: number
}

/** The questions the crew's agents asked about this work order: answer open ones, read the earlier ones. */
export function QuestionsPanel({ workOrderId, refreshKey }: Props) {
  // The pushed list of questions waiting on this person also says when one here was asked or answered
  const waiting = useWaitingQuestions()
  const waitingHere = waiting.filter((q) => q.work_order_id === workOrderId).map((q) => q.id).join(',')
  const res = useAsync((signal) => humanApi.workOrderQuestions(workOrderId, signal), [workOrderId])
  const { reload } = res
  const seen = useRef(`${refreshKey}|${waitingHere}`)
  useEffect(() => {
    // Reload (keeping the list on screen) when a question here is asked or answered
    const key = `${refreshKey}|${waitingHere}`
    if (key !== seen.current) {
      seen.current = key
      reload()
    }
  }, [refreshKey, waitingHere, reload])
  const questions = res.data ?? []
  if (questions.length === 0) return null
  const open = questions.filter((q) => q.status === 'open')
  const earlier = questions.filter((q) => q.status !== 'open')
  const replace = (next: HumanQuestion) => res.setData((list) => (list ?? []).map((q) => (q.id === next.id ? next : q)))
  return (
    <section aria-label="Questions from the crew" className={`mb-4 rounded-lg border ${open.length > 0 ? 'border-accent/60 bg-accent-soft' : 'border-line bg-panel'}`}>
      <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
        <CircleHelp className={`size-4 ${open.length > 0 ? 'text-accent' : 'text-muted'}`} />
        <span className="eyebrow">{open.length > 0 ? `Waiting for you (${open.length})` : 'Questions from the crew'}</span>
      </div>
      <div className="flex flex-col divide-y divide-line">
        {open.map((q) => <OpenQuestion key={q.id} question={q} onAnswered={replace} />)}
        {earlier.length > 0 && (
          <details className="px-4 py-2.5" open={open.length === 0}>
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
