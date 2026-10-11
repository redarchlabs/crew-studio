import { useState } from 'react'
import { ChevronDown, ChevronRight, Cpu } from 'lucide-react'
import type { JournalEntry } from '../../../../api/types'
import { Button } from '../../../../components/Button'
import { Markdown } from '../../../../components/Markdown'
import { compact, timeOfDay, usd } from '../../../../lib/format'
import { LlmCallDetail } from '../../timeline/LlmCallDetail'
import { AgentAvatar, KindIcon } from './journalKinds'

const MARKDOWN_KINDS = new Set(['task_completed', 'run'])

/** Body text of an entry: markdown for outputs, preformatted for everything else. */
export function EntryBody({ entry }: { entry: JournalEntry }) {
  if (!entry.body) return <p className="text-[12px] text-faint">No details recorded.</p>
  if (MARKDOWN_KINDS.has(entry.kind)) return <Markdown className="text-[12.5px]">{entry.body}</Markdown>
  return <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md border border-line bg-raised px-2.5 py-2 font-mono text-[11.5px] leading-relaxed">{entry.body}</pre>
}

/** For llm_call entries: loads the full messages and response on demand. */
export function FullPromptToggle({ seq }: { seq: number }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="flex flex-col gap-2">
      <Button size="sm" variant="secondary" className="self-start" icon={<Cpu className="size-3.5" />} onClick={() => setOpen((o) => !o)}>
        {open ? 'Hide full prompt/response' : 'Show full prompt/response'}
      </Button>
      {open && <LlmCallDetail seq={seq} />}
    </div>
  )
}

export function EntryMeta({ entry }: { entry: JournalEntry }) {
  if (entry.cost_usd <= 0 && entry.tokens <= 0 && !entry.model) return null
  const figures = [entry.tokens > 0 && `${compact(entry.tokens)} tok`, entry.cost_usd > 0 && usd(entry.cost_usd)].filter(Boolean).join(' · ')
  return (
    <span className="shrink-0 font-mono text-[10.5px] text-faint tabular">
      {entry.model && <span className="hidden sm:inline">{entry.model}{figures && ' · '}</span>}
      {figures}
    </span>
  )
}

/** One journal line: time, agent avatar, kind icon, title, cost; expands to the body. */
export function JournalEntryRow({ entry }: { entry: JournalEntry }) {
  const [open, setOpen] = useState(false)
  const expandable = Boolean(entry.body) || entry.kind === 'llm_call'
  const tint = entry.kind === 'delegation' ? 'bg-danger-soft/40' : entry.kind === 'budget' || entry.kind === 'tool_error' || entry.kind === 'task_failed' ? 'bg-warn-soft/40' : ''
  return (
    <li className={`rounded-md ${tint}`}>
      <button
        type="button"
        disabled={!expandable}
        aria-expanded={expandable ? open : undefined}
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left enabled:hover:bg-panel-2 disabled:cursor-default"
      >
        <span className="w-14 shrink-0 font-mono text-[10.5px] text-faint tabular">{timeOfDay(entry.at)}</span>
        <AgentAvatar role={entry.agent_role} size="size-5" />
        <KindIcon kind={entry.kind} />
        <span className="min-w-0 flex-1 truncate text-[12.5px]" title={entry.title}>{entry.title}</span>
        <EntryMeta entry={entry} />
        {expandable ? open ? <ChevronDown className="size-3.5 shrink-0 text-faint" /> : <ChevronRight className="size-3.5 shrink-0 text-faint" /> : <span className="w-3.5" />}
      </button>
      {open && (
        <div className="mb-2 ml-2 mr-2 mt-1 flex min-w-0 flex-col gap-2 sm:ml-[6.5rem]">
          {entry.agent_role && <div className="text-[11.5px] text-muted">{entry.agent_role}{entry.task_name ? ` · ${entry.task_name}` : ''}</div>}
          {entry.body && <EntryBody entry={entry} />}
          {entry.kind === 'llm_call' && <FullPromptToggle seq={entry.seq} />}
        </div>
      )}
    </li>
  )
}
