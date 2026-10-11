import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import type { InteractionNode, JournalEntry } from '../../../api/types'
import { IconButton } from '../../../components/Button'
import { Stat } from '../../../components/misc'
import { compact, dateTime, durationMs, int, usd } from '../../../lib/format'
import { LlmCallDetail } from '../../runs/timeline/LlmCallDetail'
import { toolKey } from '../../runs/timeline/spendTally'
import { EntryBody, JournalEntryRow } from '../../runs/tabs/journal/JournalEntryRow'
import { AgentAvatar, KIND_LOOK, KindIcon } from '../../runs/tabs/journal/journalKinds'

const MAX_RELATED = 200

/** Journal entries that involve a diagram node. */
export function entriesForNode(node: Pick<InteractionNode, 'kind' | 'label'>, entries: readonly JournalEntry[]): JournalEntry[] {
  const toolKinds = new Set(['tool_call', 'tool_result', 'tool_error', 'search', 'video'])
  switch (node.kind) {
    case 'agent':
      return entries.filter((e) => e.agent_role === node.label || (e.kind === 'delegation' && e.title.includes(node.label)))
    case 'task':
      return entries.filter((e) => e.task_name === node.label)
    case 'tool': {
      // Entries may spell the tool either way ("Google Search" / "google_search")
      const key = toolKey(node.label)
      return entries.filter((e) => toolKinds.has(e.kind) && (toolKey(e.title).includes(key) || toolKey(e.body).includes(key)))
    }
    case 'model':
      return entries.filter((e) => e.model === node.label)
    case 'crew':
      return entries.filter((e) => e.agent_role === '')
  }
}

/** Side panel on wide screens, bottom sheet on phones. */
export function PanelShell({ title, onClose, children }: { title: ReactNode; onClose: () => void; children: ReactNode }) {
  return (
    <aside className="fixed inset-x-0 bottom-0 z-40 flex max-h-[75vh] flex-col rounded-t-xl border-t border-line-strong bg-panel shadow-[var(--shadow)] lg:static lg:z-auto lg:max-h-[700px] lg:rounded-lg lg:border lg:border-line lg:shadow-none">
      <div className="flex items-center gap-2 border-b border-line px-3 py-2">
        <div className="min-w-0 flex-1">{title}</div>
        <IconButton label="Close details" icon={<X className="size-4" />} onClick={onClose} />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-3">{children}</div>
    </aside>
  )
}

export function NodeDetails({ node, entries }: { node: InteractionNode; entries: readonly JournalEntry[] }) {
  const related = entriesForNode(node, entries)
  const s = node.stats
  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-2">
        <Stat label="LLM calls" value={int(s.llm_calls)} />
        <Stat label="Tool calls" value={int(s.tool_calls)} />
        <Stat label="Tokens" value={compact(s.prompt_tokens + s.completion_tokens)} sub={`${int(s.prompt_tokens)} in · ${int(s.completion_tokens)} out`} />
        <Stat label="Cost" value={usd(s.cost_usd)} />
        <Stat label="Duration" value={durationMs(s.duration_ms)} />
        {node.status && <Stat label="Status" value={node.status} />}
      </div>
      <div className="eyebrow">Journal ({related.length})</div>
      {related.length === 0 ? (
        <p className="text-[12px] text-faint">No journal entries for this {node.kind} yet.</p>
      ) : (
        <ol className="flex flex-col gap-px">
          {related.slice(0, MAX_RELATED).map((e) => <JournalEntryRow key={e.seq} entry={e} />)}
        </ol>
      )}
      {related.length > MAX_RELATED && <p className="text-[12px] text-faint">Showing the first {MAX_RELATED}. The Journal tab has them all.</p>}
    </div>
  )
}

export function EntryDetails({ entry }: { entry: JournalEntry }) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2 text-[12px] text-muted">
        <AgentAvatar role={entry.agent_role} />
        <span className="font-medium text-fg">{entry.agent_role || 'Crew'}</span>
        {entry.task_name && <span>· {entry.task_name}</span>}
        <span className="ml-auto font-mono text-[11px] text-faint">{dateTime(entry.at)}</span>
      </div>
      <div className="flex flex-wrap gap-3">
        <Stat label="Kind" value={KIND_LOOK[entry.kind]?.label ?? entry.kind} />
        {entry.model && <Stat label="Model" value={entry.model} />}
        {entry.tokens > 0 && <Stat label="Tokens" value={int(entry.tokens)} />}
        {entry.cost_usd > 0 && <Stat label="Cost" value={usd(entry.cost_usd)} />}
      </div>
      {entry.body && <EntryBody entry={entry} />}
      {entry.kind === 'llm_call' && (
        <>
          <div className="eyebrow">Full prompt and response</div>
          <LlmCallDetail seq={entry.seq} />
        </>
      )}
    </div>
  )
}

export function EntryTitle({ entry }: { entry: JournalEntry }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <KindIcon kind={entry.kind} />
      <span className="truncate text-[13px] font-medium" title={entry.title}>{entry.title}</span>
    </div>
  )
}
