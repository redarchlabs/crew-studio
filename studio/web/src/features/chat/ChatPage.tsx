import { useEffect, useRef } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { MessagesSquare, Plus, Trash2 } from 'lucide-react'
import { chatApi } from '../../api/endpoints.chat'
import type { ChatAgent, ChatCrew, ChatMessage, ConversationView } from '../../api/types.chat'
import type { HumanQuestion } from '../../api/types.human'
import { LinkButton } from '../../components/Button'
import { ConfirmButton } from '../../components/misc'
import { PageHeader, Panel } from '../../components/PageHeader'
import { AsyncView, EmptyState } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { usd } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useNow } from '../../lib/useNow'
import { usePollWhile } from '../../lib/usePollWhile'
import { isWaiting, pendingHandoffs } from './chatLogic'
import { belongsTo, composerText, pickTarget, targetUrl, type ChatKind, type ChatTarget } from './chatTarget'
import { ChatTargetPicker } from './ChatTargetPicker'
import { useConversationQuestions, WaitingConversations } from './ChatQuestions'
import { CompactBar } from './CompactBar'
import { Composer } from './Composer'
import { ConversationList } from './ConversationList'
import { MessageList } from './MessageList'

/** How often an open conversation is re-read while a reply is being written */
export const REPLY_POLL_MS = 1500
/** How often it is re-read while only hand-offs are working in the background */
export const HANDOFF_POLL_MS = 10_000

type Targets = { crews: ChatCrew[]; agents: ChatAgent[] }

/** /chat and /chat/:conversationId: pick a crew or an agent with chat on, see your conversations with it, and talk. */
export default function ChatPage() {
  const targets = useAsync(async (signal): Promise<Targets> => {
    const [crews, agents] = await Promise.all([chatApi.crews(signal), chatApi.agents(signal)])
    return { crews, agents }
  }, [])
  return (
    <>
      <PageHeader eyebrow="Console" title="Chat" description="Talk to a crew's front desk, or straight to an agent. Anything they want to change waits for your Confirm." />
      <AsyncView {...targets}>
        {(t) => (t.crews.length + t.agents.length === 0 ? <NoChatTargets /> : <ChatWorkspace crews={t.crews} agents={t.agents} />)}
      </AsyncView>
    </>
  )
}

function NoChatTargets() {
  const { can } = useAuth()
  const hint = can('crews.edit') || can('agents.edit')
    ? 'Turn chat on in a crew’s Chat panel (on its Design tab), or in an agent’s Chat panel.'
    : 'Ask someone who can edit crews or agents to turn chat on for one in your areas.'
  return <EmptyState icon={<MessagesSquare className="size-6" />} title="No crew or agent you can chat with" hint={hint} />
}

function ChatWorkspace({ crews, agents }: Targets) {
  const { conversationId } = useParams()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const view = useAsync((signal) => (conversationId ? chatApi.conversation(conversationId, signal) : Promise.resolve(null)), [conversationId])
  const waiting = view.data ? isWaiting(view.data.messages) : false
  const handoffs = view.data ? pendingHandoffs(view.data.messages).length : 0
  usePollWhile(waiting, view.reload, REPLY_POLL_MS)
  usePollWhile(handoffs > 0 && !waiting, view.reload, HANDOFF_POLL_MS)
  const now = useNow(true, 30_000)

  const target = pickTarget(view.data?.conversation, params, crews, agents)
  // Only this crew's or agent's conversations, so others cannot crowd them out of the list
  const convs = useAsync((signal) => (target.kind === 'agent'
    ? chatApi.conversations(undefined, signal, target.id)
    : chatApi.conversations(target.id, signal)), [target.kind, target.id])
  const crew = target.kind === 'crew' ? crews.find((c) => c.id === target.id) : undefined
  const agent = target.kind === 'agent' ? agents.find((a) => a.id === target.id) : undefined
  const available = crew !== undefined || agent !== undefined
  const name = crew?.name ?? agent?.name
  const answerer = crew?.front_desk_name ?? agent?.name ?? (target.kind === 'crew' ? 'Front desk' : view.data?.conversation.agent_name || 'Agent')
  const mine = (convs.data ?? []).filter((c) => belongsTo(c, target))

  // Once a reply or a hand-off's result lands, the list's order and titles may have changed
  const wasWaiting = useRef(false)
  const wasHandoffs = useRef(0)
  const { reload: reloadConvs } = convs
  useEffect(() => {
    if ((wasWaiting.current && !waiting) || handoffs < wasHandoffs.current) reloadConvs()
    wasWaiting.current = waiting
    wasHandoffs.current = handoffs
  }, [waiting, handoffs, reloadConvs])

  const onSend = async (text: string, files: File[] = []): Promise<boolean> => {
    try {
      if (conversationId) {
        const next = await chatApi.send(conversationId, text, files)
        view.setData(() => next)
      } else {
        const next = target.kind === 'agent' ? await chatApi.startWithAgent(target.id, text, files) : await chatApi.start(target.id, text, files)
        navigate(`/chat/${next.conversation.id}`)
      }
      convs.reload()
      return true
    } catch (err) {
      notifyError('Could not send the message', err)
      return false
    }
  }

  const onCompact = async () => {
    if (!conversationId) return
    try {
      const next = await chatApi.compact(conversationId)
      view.setData(() => next)
    } catch (err) {
      notifyError('Could not compact the conversation', err)
    }
  }

  const replace = (m: ChatMessage) =>
    view.setData((prev) => prev && { ...prev, messages: prev.messages.map((x) => (x.id === m.id ? m : x)) })

  const remove = async (id: string) => {
    try {
      await chatApi.remove(id)
      notifySuccess('Conversation deleted')
      convs.reload()
      navigate(targetUrl(target))
    } catch (err) {
      notifyError('Could not delete the conversation', err)
    }
  }

  return (
    <>
      <WaitingConversations current={conversationId} />
      <div className="grid gap-4 lg:grid-cols-[260px_minmax(0,1fr)]">
        <Panel title={target.kind === 'crew' ? 'Crew' : 'Agent'} bodyClassName="flex flex-col gap-3 p-3">
          <ChatTargetPicker crews={crews} agents={agents} target={target} onPick={(t) => navigate(targetUrl(t))}
            fallbackName={view.data ? view.data.conversation.agent_name || view.data.conversation.crew_name : undefined} />
          <LinkButton size="sm" icon={<Plus className="size-3.5" />} to={targetUrl(target)}>New conversation</LinkButton>
          <AsyncView {...convs}>{() => <ConversationList items={mine} now={now} />}</AsyncView>
        </Panel>
        <Panel
          title={view.data?.conversation.title || (conversationId ? 'Conversation' : `New conversation${name ? ` with ${name}` : ''}`)}
          actions={conversationId && <ConfirmButton icon={<Trash2 className="size-3.5" />} disabled={waiting || handoffs > 0} onConfirm={() => void remove(conversationId)}>Delete</ConfirmButton>}
          bodyClassName="flex flex-col gap-4 p-4"
        >
          {conversationId ? (
            <AsyncView {...view}>
              {(v) => v && <Thread view={v} kind={target.kind} frontDesk={answerer} now={now} onChange={replace} onStale={view.reload} onSend={onSend} onCompact={onCompact} waiting={waiting} />}
            </AsyncView>
          ) : (
            <>
              <Intro kind={target.kind} answerer={answerer} agent={agent} />
              <Composer disabled={!available} reason={available ? undefined : `Chat is off for this ${target.kind}.`} {...composerText(target.kind, answerer)} onSend={onSend} />
            </>
          )}
        </Panel>
      </div>
    </>
  )
}

function Intro({ kind, answerer, agent }: { kind: ChatKind; answerer: string; agent?: ChatAgent }) {
  const help = <>See <Link className="text-accent" to="/help/chat">Chat help</Link>.</>
  if (kind === 'crew') {
    return (
      <p className="text-[12.5px] text-muted">
        Ask {answerer} what the crew knows, or ask it to start a run. It reads the crew’s notes and action log; to start a run or
        change a note it shows you a card to confirm. {help}
      </p>
    )
  }
  const helpers = agent && agent.specialists.length > 0 ? ` It can bring in ${agent.specialists.map((s) => s.name).join(', ')} for parts of the work.` : ''
  const runs = agent && agent.crews.length > 0 ? ' When a crew on its list fits the job, it shows you a card to start a run; nothing runs until you confirm.' : ''
  return <p className="text-[12.5px] text-muted">Ask {answerer} anything.{helpers}{runs} {help}</p>
}

type ThreadProps = {
  view: ConversationView
  kind: ChatTarget['kind']
  frontDesk: string
  now: number
  waiting: boolean
  onChange: (m: ChatMessage) => void
  onStale: () => void
  onSend: (text: string, files: File[]) => Promise<boolean>
  onCompact: () => Promise<void>
}

function Thread({ view, kind, frontDesk, now, waiting, onChange, onStale, onSend, onCompact }: ThreadProps) {
  const { budget } = view
  const { questions, answered } = useConversationQuestions(view)
  const onAnswered = (q: HumanQuestion) => {
    answered(q)
    // The hand-off carries on; its card and the conversation catch up
    onStale()
  }
  const spent = budget.remaining_usd <= 0
  // A reply being written reserves what is left of the budget, so say to wait before saying it is used up
  const reason = !view.chat_enabled ? `Chat is turned off for this ${kind}; you can still read this conversation.`
    : waiting ? 'Wait for the reply before sending another message.'
      : spent ? 'The daily chat budget is used up; try again later.' : undefined
  return (
    <>
      <MessageList messages={view.messages} crewId={view.conversation.crew_id ?? ''} frontDesk={frontDesk} now={now} onChange={onChange} onStale={onStale}
        questions={questions} onAnswered={onAnswered} />
      <CompactBar messages={view.messages} disabled={reason !== undefined} onCompact={onCompact} />
      <p className="text-[11.5px] text-faint">
        {usd(budget.spent_last_24h_usd)} of {usd(budget.daily_budget_usd)} spent in the last 24 hours.
        {budget.person_daily_budget_usd !== undefined && ` You: ${usd(budget.person_spent_last_24h_usd ?? 0)} of ${usd(budget.person_daily_budget_usd)} with this ${kind}.`}
      </p>
      <Composer disabled={reason !== undefined} reason={reason} {...composerText(kind, frontDesk)} onSend={onSend} />
    </>
  )
}
