import type { ChatMessage, Proposal } from '../../api/types.chat'
import { usd } from '../../lib/format'

const inFlight = (m: ChatMessage) => m.run_status === 'queued' || m.run_status === 'running'

/** True while some reply in the conversation is still being written (its run is queued or running). Hand-offs run in
 * the background and do not count: the person may keep chatting while they work. */
export function isWaiting(messages: readonly ChatMessage[]): boolean {
  return messages.some((m) => m.role !== 'user' && m.run_kind !== 'handoff' && inFlight(m))
}

/** The thread in reading order: the cards a hand-off proposed while it worked sit just before its result (they are made
 * after the result message, which exists from the start). Everything else keeps the server's order. */
export function threadOrder(messages: readonly ChatMessage[]): ChatMessage[] {
  const cards = new Map<string, ChatMessage[]>()
  for (const m of messages) {
    if (m.handoff_proposal && m.handoff) cards.set(m.handoff.id, [...(cards.get(m.handoff.id) ?? []), m])
  }
  if (cards.size === 0) return [...messages]
  const placed = new Set<string>()
  const out: ChatMessage[] = []
  for (const m of messages) {
    if (m.handoff_proposal && m.handoff) {
      // Shown with its result; a card whose result is not in the list (cut off by the page) stays where it is
      if (!messages.some((r) => r.handoff?.id === m.handoff?.id && !r.handoff_proposal)) out.push(m)
      continue
    }
    if (m.handoff && !placed.has(m.handoff.id)) {
      placed.add(m.handoff.id)
      out.push(...(cards.get(m.handoff.id) ?? []))
    }
    out.push(m)
  }
  return out
}

/** The result messages of hand-offs still queued or running. */
export function pendingHandoffs(messages: readonly ChatMessage[]): ChatMessage[] {
  return messages.filter((m) => m.run_kind === 'handoff' && inFlight(m))
}

/** How many earlier messages each turn is given (HistoryMessages in the Go service), and how much of each */
export const HISTORY_MESSAGES = 20
const HISTORY_CHARS = 4000
const CHARS_PER_TOKEN = 4
/** Below this many messages since the last summary, the server refuses to compact */
export const MIN_COMPACT_MESSAGES = 2

/** Index of the latest finished summary, or -1. */
function lastSummary(messages: readonly ChatMessage[]): number {
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === 'summary' && messages[i].content) return i
  }
  return -1
}

export type ConversationContext = {
  /** Messages since the latest summary that the next turn would be given */
  messages: number
  /** Messages since the latest summary that no longer fit in what a turn is given */
  dropped: number
  /** Rough size of the conversation each turn is given */
  tokens: number
  canCompact: boolean
}

/** What the crew is given of the conversation with each message: the latest summary and the messages after it. */
export function conversationContext(messages: readonly ChatMessage[]): ConversationContext {
  const from = lastSummary(messages)
  const since = messages.slice(from + 1).filter((m) => m.role !== 'summary' && m.content)
  const given = since.slice(-HISTORY_MESSAGES)
  const chars = (from >= 0 ? messages[from].content.length : 0) + given.reduce((n, m) => n + Math.min(m.content.length, HISTORY_CHARS), 0)
  return {
    messages: given.length,
    dropped: since.length - given.length,
    tokens: Math.round(chars / CHARS_PER_TOKEN),
    canCompact: since.length >= MIN_COMPACT_MESSAGES,
  }
}

export type ReplyState = 'text' | 'waiting' | 'failed' | 'empty'

/** How to show an assistant reply: its text, a waiting note, why it failed, or that nothing came back. */
export function replyState(m: ChatMessage): ReplyState {
  if (m.content) return 'text'
  if (m.run_status === 'queued' || m.run_status === 'running') return 'waiting'
  if (m.run_status === 'failed' || m.run_status === 'cancelled') return 'failed'
  return 'empty'
}

export type DetailRow = { label: string; value: string; block?: boolean }

const str = (v: unknown): string => (typeof v === 'string' ? v : v === undefined || v === null ? '' : String(v))

/** Which agent drafted a card, when an agent chat's specialist (or its lead) proposed a tracker or KM2 change */
const proposedBy = (pl: Record<string, unknown>): DetailRow[] => (pl.proposed_by ? [{ label: 'Proposed by', value: str(pl.proposed_by) }] : [])

/** The facts of agent chat's tracker and KM2 cards (slice 3d): where the change goes and exactly what it posts or saves. */
function changeDetails(p: Proposal): DetailRow[] | null {
  const pl = p.payload ?? {}
  const via = [{ label: 'Connection', value: str(pl.connection_name) }]
  switch (p.type) {
    case 'tracker_comment':
      return [...via, { label: 'Issue', value: str(pl.issue) }, { label: 'Comment', value: str(pl.text), block: true }, ...proposedBy(pl)]
    case 'tracker_suggestion':
      return [...via, { label: 'Issue', value: str(pl.issue) },
        { label: 'Suggested change', value: `${str(pl.field).replace(/_/g, ' ')}: ${str(pl.value)}` },
        { label: 'Reason', value: str(pl.reason), block: true }, ...proposedBy(pl)]
    case 'tracker_issue':
      return [...via, { label: 'Project', value: str(pl.project) }, { label: 'Title', value: str(pl.title) },
        ...(pl.parent ? [{ label: 'Parent', value: str(pl.parent) }] : []),
        ...(pl.description ? [{ label: 'Description', value: str(pl.description), block: true }] : []), ...proposedBy(pl)]
    case 'km2_save':
      return [...via, { label: 'Folder', value: str(pl.folder) || 'the connection’s default folder' }, { label: 'Title', value: str(pl.title) },
        { label: 'Document id', value: `${str(pl.external_ref)} (saving again with this id replaces the document)` },
        { label: 'Document', value: str(pl.content), block: true }, ...proposedBy(pl)]
  }
  return null
}

/** What Studio itself adds when it posts or saves an agent chat card (chat.attribution on the server), so the person sees
 * everything that will be sent before confirming. person is who would confirm it. */
export function studioAdds(p: Proposal, person: string): string | null {
  const pl = p.payload ?? {}
  const who = person || 'you'
  const signed = `(Posted by ${who} from a Crew Studio agent chat${pl.proposed_by ? `; drafted by ${str(pl.proposed_by)}` : ''}.)`
  switch (p.type) {
    case 'tracker_comment':
    case 'tracker_issue':
      return `Studio will add: ${signed}`
    case 'tracker_suggestion':
      return `Studio will post it as “Suggested change from ${who} (Crew Studio): set …” and add: ${signed}`
    case 'km2_save':
      return `Studio will add to the document’s details: saved_by ${who}${pl.proposed_by ? `, drafted_by ${str(pl.proposed_by)}` : ''}.`
  }
  return null
}

/** The facts a person needs to decide on a proposal, by type. Unknown types show their payload. */
export function proposalDetails(p: Proposal): DetailRow[] {
  const pl = p.payload ?? {}
  const change = changeDetails(p)
  if (change) return change
  if (p.type === 'start_work_order') {
    const inputs = (pl.inputs ?? {}) as Record<string, unknown>
    return [
      // Agent chat names the crew to run; crew chat runs its own crew
      ...(pl.crew_name ? [{ label: 'Crew', value: str(pl.crew_name) }] : []),
      { label: 'Title', value: str(pl.title) },
      { label: 'Brief', value: str(pl.brief), block: true },
      ...Object.entries(inputs).map(([k, v]) => ({ label: `Input ${k}`, value: str(v) })),
    ]
  }
  if (p.type === 'notes_put') {
    const base = Number(pl.base_version ?? 0)
    return [
      { label: 'Note', value: str(pl.name) },
      ...(pl.subject ? [{ label: 'Subject', value: str(pl.subject) }] : []),
      { label: 'Replaces', value: base > 0 ? `version ${base}` : 'nothing (new note)' },
      { label: 'New text', value: str(pl.content), block: true },
    ]
  }
  if (p.type === 'tracking_add') {
    const children: Record<string, string> = { all: 'all levels of child tickets', direct: 'direct children only', none: 'the item only' }
    return [
      { label: 'Item', value: str(pl.item_ref) },
      ...(pl.project ? [{ label: 'Project', value: str(pl.project) }] : []),
      { label: 'Track', value: children[str(pl.include_children)] ?? str(pl.include_children) },
      { label: 'Connection', value: str(pl.connection) || "the crew's tracker" },
      ...(pl.note ? [{ label: 'Note', value: str(pl.note) }] : []),
      ...(pl.lead_account_id ? [{ label: 'Lead', value: str(pl.lead_account_id) }] : []),
    ]
  }
  if (p.type === 'start_handoff') {
    const role = str(pl.worker_role)
    const limits = pl.budget_usd !== undefined && pl.timeout_minutes !== undefined
      ? [{ label: 'Limits', value: `up to ${usd(Number(pl.budget_usd))}, up to ${str(pl.timeout_minutes)} minutes` }] : []
    return [
      { label: 'Worker', value: role && role !== str(pl.worker_name) ? `${str(pl.worker_name)} (${role})` : str(pl.worker_name) },
      { label: 'Title', value: str(pl.title) },
      { label: 'Brief', value: str(pl.brief), block: true },
      ...limits,
    ]
  }
  if (p.type === 'tracking_remove') {
    return [{ label: 'Stop tracking', value: str(pl.item_ref) }, ...(pl.connection ? [{ label: 'Connection', value: str(pl.connection) }] : [])]
  }
  return [{ label: 'Details', value: JSON.stringify(pl, null, 2), block: true }]
}

/** external: a page in the tracker (opened in a new tab), not one of Crew Studio's */
export type ResultLink = { to: string; label: string; external?: boolean } | null

/** Only web links from a tracker's answer become links */
const webURL = (v: unknown): string | null => (typeof v === 'string' && /^https?:\/\//i.test(v) ? v : null)

/** Where a confirmed proposal's result can be seen: its work order, or the crew's State tab for a note. */
export function resultLink(type: string, result: Record<string, unknown> | null, crewId: string): ResultLink {
  if (!result) return null
  if (type === 'start_work_order' && typeof result.work_order_id === 'string') {
    return { to: `/work-orders/${result.work_order_id}`, label: `Open WO-${str(result.work_order_number)}` }
  }
  if (type === 'notes_put' && result.version !== undefined) {
    return { to: `/crews/${crewId}?tab=state`, label: `Saved as version ${str(result.version)}` }
  }
  if (type === 'start_handoff' && typeof result.run_id === 'string') {
    return { to: `/runs/${result.run_id}`, label: 'Hand-off run' }
  }
  if (type === 'tracker_comment' || type === 'tracker_suggestion' || type === 'tracker_issue') {
    const url = webURL(result.url)
    if (!url) return null
    // When the answer never came, the link is only to the issue: the post may not be there
    const posted = type !== 'tracker_issue' && result.outcome !== 'unknown'
    return { to: url, label: posted ? `Posted on ${str(result.issue)}` : `Open ${str(result.issue)}`, external: true }
  }
  if ((type === 'tracking_add' || type === 'tracking_remove') && typeof result.item_ref === 'string') {
    return { to: `/crews/${crewId}?tab=state`, label: type === 'tracking_add' ? `Tracking ${result.item_ref}` : `Stopped tracking ${result.item_ref}` }
  }
  return null
}

/** What a confirmed card says about its result besides a link: that a post or save was sent but its answer never came
 * back, so it may or may not have been made (agent chat's tracker and KM2 cards). */
export function resultNote(result: Record<string, unknown> | null): string | null {
  return result?.outcome === 'unknown' ? str(result.message) || 'It is not known whether the change was made.' : null
}

/** Whether a confirmed change can still be undone from the chat. */
export function canUndo(m: ChatMessage): boolean {
  return m.proposal_state === 'confirmed' && !m.undone_at && m.proposal_result?.undoable === true
}
