import { useState } from 'react'
import { Link } from 'react-router'
import { Check, Undo2, X } from 'lucide-react'
import { ApiError } from '../../api/client'
import { chatApi } from '../../api/endpoints.chat'
import type { ChatMessage, ProposalState } from '../../api/types.chat'
import { Button } from '../../components/Button'
import { Chip, type Tone } from '../../components/StatusBadge'
import { dateTime } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAuth } from '../../lib/auth'
import { canUndo, proposalDetails, resultLink, resultNote, studioAdds } from './chatLogic'

const STATE: Record<ProposalState, { tone: Tone; label: string }> = {
  pending: { tone: 'warn', label: 'Waiting for you' },
  confirmed: { tone: 'ok', label: 'Confirmed' },
  declined: { tone: 'neutral', label: 'Declined' },
  expired: { tone: 'neutral', label: 'Expired' },
}

type Props = {
  message: ChatMessage
  crewId: string
  /** Called with the updated message after a decision or undo */
  onChange: (m: ChatMessage) => void
  /** Re-reads the conversation (after a refusal such as an expired proposal) */
  onStale?: () => void
}

/** A change the front desk proposed: nothing happens until the person confirms it. */
export function ProposalCard({ message, crewId, onChange, onStale }: Props) {
  const [busy, setBusy] = useState<'confirm' | 'decline' | 'undo' | null>(null)
  const { me } = useAuth()
  const p = message.proposal
  if (!p || !message.proposal_state) return null
  const state = STATE[message.proposal_state]
  const link = resultLink(p.type, message.proposal_result, crewId)
  const note = resultNote(message.proposal_result)
  // A hand-off waits for the reply proposing it to finish: until then the reply holds the budget the hand-off needs
  const replyInFlight = p.type === 'start_handoff' && (message.run_status === 'queued' || message.run_status === 'running')

  const act = async (kind: 'confirm' | 'decline' | 'undo') => {
    setBusy(kind)
    try {
      const next = await chatApi[kind](message.id)
      onChange(next)
      notifySuccess(kind === 'confirm' ? 'Done' : kind === 'decline' ? 'Declined' : 'Undone')
      // A confirmed hand-off adds its result message to the conversation
      if (kind === 'confirm' && p.type === 'start_handoff') onStale?.()
    } catch (err) {
      notifyError(kind === 'confirm' ? 'Could not apply the change' : kind === 'decline' ? 'Could not decline' : 'Could not undo the change', err)
      // The card may be out of date (expired, decided elsewhere): show what the server has now
      if (err instanceof ApiError && err.status === 409) onStale?.()
    } finally {
      setBusy(null)
    }
  }

  return (
    <section aria-label="Proposed change" className="mt-2 rounded-md border border-accent/40 bg-accent-soft/40 p-3 text-[12.5px]">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span className="font-medium">{p.summary}</span>
        <Chip tone={state.tone}>{state.label}</Chip>
        {message.undone_at && <Chip tone="neutral">Undone</Chip>}
      </div>
      {message.handoff && (
        // Proposed by unattended work: the person was not there when it was made
        <p className="mb-2 text-[11.5px] text-muted">Proposed by {message.handoff.worker_name} during a hand-off, while nobody was watching. Check it before you confirm.</p>
      )}
      <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-3 gap-y-1">
        {proposalDetails(p).map((row) => (
          <div key={row.label} className="contents">
            <dt className="text-muted">{row.label}</dt>
            <dd className="min-w-0">
              {row.block ? <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words font-sans">{row.value}</pre> : row.value}
            </dd>
          </div>
        ))}
      </dl>
      {message.proposal_state === 'pending' && studioAdds(p, me?.name ?? '') && (
        <p className="mt-2 text-[11.5px] text-muted">{studioAdds(p, me?.name ?? '')}</p>
      )}
      {note && <p className="mt-2 text-[11.5px] text-warn" role="note">{note}</p>}
      {message.decided_by_name && message.proposal_state !== 'pending' && (
        <p className="mt-2 text-[11.5px] text-faint">
          {state.label} by {message.decided_by_name}{message.decided_at ? ` · ${dateTime(message.decided_at)}` : ''}
        </p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        {message.proposal_state === 'pending' && (
          <>
            <Button size="sm" variant="primary" icon={<Check className="size-3.5" />} busy={busy === 'confirm'} disabled={busy !== null || replyInFlight} onClick={() => void act('confirm')}>Confirm</Button>
            <Button size="sm" variant="ghost" icon={<X className="size-3.5" />} busy={busy === 'decline'} disabled={busy !== null} onClick={() => void act('decline')}>Decline</Button>
          </>
        )}
        {message.proposal_state === 'pending' && replyInFlight && <span className="text-[11.5px] text-faint">You can confirm once the reply has finished.</span>}
        {link && (link.external
          ? <a className="text-accent" href={link.to} target="_blank" rel="noreferrer noopener">{link.label}</a>
          : <Link className="text-accent" to={link.to}>{link.label}</Link>)}
        {canUndo(message) && (
          <Button size="sm" variant="ghost" icon={<Undo2 className="size-3.5" />} busy={busy === 'undo'} onClick={() => void act('undo')}>Undo</Button>
        )}
      </div>
    </section>
  )
}
