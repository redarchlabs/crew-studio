import { useState } from 'react'
import { RotateCcw } from 'lucide-react'
import { workOrdersApi } from '../../../api/endpoints'
import { followUpApi } from '../../../api/endpoints.followup'
import { ATTACHMENT_LIMITS } from '../../../api/types'
import type { ChatMessage, ConversationView } from '../../../api/types.chat'
import type { Run } from '../../../api/types'
import type { WorkOrderFeedback } from '../../../api/types.followup'
import { Button } from '../../../components/Button'
import { TextAreaField } from '../../../components/Field'
import { Panel } from '../../../components/PageHeader'
import { AsyncView } from '../../../components/States'
import { useAuth } from '../../../lib/auth'
import { dateTime, usd } from '../../../lib/format'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'
import { useNow } from '../../../lib/useNow'
import { usePollWhile } from '../../../lib/usePollWhile'
import { isWaiting } from '../../chat/chatLogic'
import { CompactBar } from '../../chat/CompactBar'
import { Composer } from '../../chat/Composer'
import { MessageList } from '../../chat/MessageList'
import { AttachButton, PendingFileList, usePendingFiles } from '../../chat/pendingFiles'

/** How often the conversation is re-read while a reply is being written (as on the Chat page) */
export const REPLY_POLL_MS = 1500
const MAX_FEEDBACK = 8000
// Room kept in the feedback for the line naming the files sent with it
const FILES_NOTE_ROOM = 600
const WORK_ORDER_FILE_ACCEPT = 'image/png,image/jpeg,image/gif,image/webp,application/pdf,text/*,.md,.csv,.json,.xml,.yaml,.yml,.html,.doc,.docx,.xls,.xlsx,.ppt,.pptx'

type Props = {
  workOrderId: string
  crewId: string | null
  /** The latest attempt is still going: a revision has to wait for it */
  active: boolean
  onRevised: (run: Run) => void
}

/** Talk with the crew about the work order, and revise it with feedback (a new attempt that builds on the last). */
export function ConversationTab({ workOrderId, crewId, active, onRevised }: Props) {
  const { can } = useAuth()
  const feedback = useAsync((signal) => followUpApi.feedback(workOrderId, signal), [workOrderId])
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
      {can('crews.chat') ? (
        <Thread workOrderId={workOrderId} crewId={crewId} />
      ) : (
        <Panel title="Conversation">
          <p className="text-[12.5px] text-muted">Talking with crews needs the Chat with crews and agents permission.</p>
        </Panel>
      )}
      <div className="flex flex-col gap-4">
        {can('work_orders.create') && (
          <ReviseCard workOrderId={workOrderId} active={active} onRevised={(run, note) => {
            feedback.setData((list) => [...(list ?? []), note])
            onRevised(run)
          }} />
        )}
        <Panel title="Feedback given">
          <AsyncView {...feedback}>{(list) => <FeedbackList items={list} />}</AsyncView>
        </Panel>
      </div>
    </div>
  )
}

function Thread({ workOrderId, crewId }: { workOrderId: string; crewId: string | null }) {
  const view = useAsync((signal) => followUpApi.conversation(workOrderId, signal), [workOrderId])
  const waiting = view.data ? isWaiting(view.data.messages) : false
  usePollWhile(waiting, view.reload, REPLY_POLL_MS)
  const now = useNow(true, 30_000)

  const onSend = async (text: string, files: File[]): Promise<boolean> => {
    try {
      const next = await followUpApi.send(workOrderId, text, files)
      view.setData(() => next)
      return true
    } catch (err) {
      notifyError('Could not send the message', err)
      return false
    }
  }
  const onCompact = async () => {
    try {
      const next = await followUpApi.compact(workOrderId)
      view.setData(() => next)
    } catch (err) {
      notifyError('Could not compact the conversation', err)
    }
  }
  const replace = (m: ChatMessage) =>
    view.setData((prev) => prev && { ...prev, messages: prev.messages.map((x) => (x.id === m.id ? m : x)) })

  return (
    <Panel title="Conversation" bodyClassName="flex flex-col gap-4 p-4">
      <AsyncView {...view}>
        {(v) => (
          <>
            {v ? (
              <MessageList messages={v.messages} crewId={crewId ?? v.conversation.crew_id ?? ''} frontDesk="Crew" now={now} onChange={replace} onStale={view.reload} />
            ) : (
              <p className="text-[12.5px] text-muted">
                Ask the crew about this work order: why it concluded something, what it would change, where a number came from.
                It has the brief and the latest output. To have the work redone, use Revise with feedback.
              </p>
            )}
            {v && <CompactBar messages={v.messages} disabled={waiting || v.budget.remaining_usd <= 0} onCompact={onCompact} />}
            {v && <BudgetLine view={v} />}
            <Composer
              disabled={waiting || (v !== null && v.budget.remaining_usd <= 0)}
              reason={waiting ? 'Wait for the reply before sending another message.' : v && v.budget.remaining_usd <= 0 ? 'The daily chat budget is used up; try again later.' : undefined}
              placeholder="Ask the crew about this work order…"
              onSend={onSend}
            />
          </>
        )}
      </AsyncView>
    </Panel>
  )
}

function BudgetLine({ view }: { view: ConversationView }) {
  const { budget } = view
  return (
    <p className="text-[11.5px] text-faint">
      {usd(budget.spent_last_24h_usd)} of {usd(budget.daily_budget_usd)} spent on this conversation in the last 24 hours.
    </p>
  )
}

type ReviseProps = { workOrderId: string; active: boolean; onRevised: (run: Run, note: WorkOrderFeedback) => void }

/** The feedback as sent: the text, and which files went with it (they join the work order's files). */
export function feedbackWithFiles(text: string, names: readonly string[]): string {
  return names.length === 0 ? text : `${text}\n\nAttached with this feedback: ${names.join(', ')}`
}

function ReviseCard({ workOrderId, active, onRevised }: ReviseProps) {
  const [text, setText] = useState('')
  const [withConversation, setWithConversation] = useState(true)
  const [busy, setBusy] = useState(false)
  const pending = usePendingFiles({ limits: ATTACHMENT_LIMITS })
  // Files already uploaded for this feedback: if the revision then fails, a retry names them without uploading again
  const [uploaded, setUploaded] = useState<string[]>([])
  const trimmed = text.trim()

  const revise = async () => {
    setBusy(true)
    try {
      // The files join the work order's files, which the revision (like every attempt) is given
      let names = uploaded
      if (pending.files.length > 0) {
        const added = await workOrdersApi.upload(workOrderId, pending.files)
        names = [...uploaded, ...added.map((f) => f.filename)]
        setUploaded(names)
        pending.clear()
      }
      const result = await followUpApi.revise(workOrderId, { feedback: feedbackWithFiles(trimmed, names), include_conversation: withConversation })
      setText('')
      setUploaded([])
      notifySuccess('Started a revision with your feedback')
      onRevised(result.run, result.feedback)
    } catch (err) {
      notifyError('Could not start the revision', err)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel title="Revise with feedback" bodyClassName="flex flex-col gap-3 p-4">
      <p className="text-[12px] text-muted">Starts a new attempt that gets the latest output and your feedback, and revises it rather than starting over.</p>
      <div className={`rounded-md ${pending.dragging ? 'outline-2 outline-offset-2 outline-dashed outline-accent' : ''}`} {...pending.dragProps}>
        <TextAreaField label="What should change?" value={text} onChange={setText} rows={5} maxLength={MAX_FEEDBACK - FILES_NOTE_ROOM}
          onPaste={pending.onPaste} placeholder="Add Claude Haiku to the table and cite a source for every price. Paste screenshots or drop files here." />
      </div>
      <div className="flex items-center gap-2">
        <AttachButton pending={pending} accept={WORK_ORDER_FILE_ACCEPT} disabled={busy} />
        <span className="text-[11.5px] text-faint">Files you add join the work order's files, which the revision gets.</span>
      </div>
      <PendingFileList pending={pending} disabled={busy} />
      {uploaded.length > 0 && <p className="text-[11.5px] text-muted">Already added to the work order's files: {uploaded.join(', ')}</p>}
      <label className="flex items-center gap-2 text-[12.5px] text-muted">
        <input type="checkbox" className="size-3.5 accent-[var(--accent)]" checked={withConversation} onChange={(e) => setWithConversation(e.target.checked)} />
        Include my conversation with the crew
      </label>
      <div className="flex items-center justify-end gap-2">
        {active && <span className="text-[11.5px] text-faint">Wait for the current attempt to finish.</span>}
        <Button size="sm" variant="primary" icon={<RotateCcw className="size-4" />} busy={busy} disabled={!trimmed || active} onClick={() => void revise()}>
          Revise with feedback
        </Button>
      </div>
    </Panel>
  )
}

function FeedbackList({ items }: { items: readonly WorkOrderFeedback[] }) {
  if (items.length === 0) return <p className="text-[12px] text-faint">No feedback yet.</p>
  return (
    <ol className="flex flex-col gap-3">
      {[...items].reverse().map((f) => (
        <li key={f.id} className="flex flex-col gap-0.5 text-[12.5px]">
          <span className="text-[11.5px] text-muted">
            {f.created_by_name || 'Someone'} · {dateTime(f.created_at)}{f.with_conversation ? ' · with the conversation' : ''}
          </span>
          <p className="whitespace-pre-wrap">{f.feedback}</p>
          {!f.revision_run_id && <span className="text-[11px] text-faint">The revision did not start.</span>}
        </li>
      ))}
    </ol>
  )
}
