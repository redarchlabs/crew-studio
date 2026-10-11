import { useState, type ReactNode } from 'react'
import {
  ArrowRightLeft,
  Bot,
  ChevronDown,
  ChevronRight,
  CircleCheck,
  Clapperboard,
  CircleX,
  Cpu,
  Database,
  Flag,
  FoldVertical,
  FolderOpen,
  ListChecks,
  LoaderCircle,
  Play,
  Search,
  TriangleAlert,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import { Markdown } from '../../../components/Markdown'
import { JsonBlock } from '../../../components/misc'
import { int, timeOfDay, truncate, usd } from '../../../lib/format'
import { formatBytes } from '../../workorders/files/attachments'
import { LlmCallDetail } from './LlmCallDetail'
import { bool, num, obj, str } from './payload'
import type { TimelineItem } from './timelineReducer'

type Look = { icon: LucideIcon; tone: string }

function lookFor(item: TimelineItem): Look {
  const { type } = item.event
  switch (type) {
    case 'crew_started':
    case 'run_status':
      return { icon: Play, tone: 'text-accent' }
    case 'task_started':
      return { icon: ListChecks, tone: 'text-blue' }
    case 'agent_started':
      return { icon: Bot, tone: 'text-teal' }
    case 'tool_started':
      return item.result?.type === 'tool_error' ? { icon: Wrench, tone: 'text-danger' } : { icon: Wrench, tone: 'text-violet' }
    case 'llm_call':
      return { icon: Cpu, tone: 'text-muted' }
    case 'search':
      return { icon: Search, tone: 'text-blue' }
    case 'video':
      return { icon: Clapperboard, tone: 'text-violet' }
    case 'delegation':
      return { icon: ArrowRightLeft, tone: 'text-danger' }
    case 'task_completed':
      return { icon: CircleCheck, tone: 'text-ok' }
    case 'crew_completed':
      return { icon: Flag, tone: 'text-ok' }
    case 'task_failed':
    case 'crew_failed':
    case 'tool_error':
      return { icon: CircleX, tone: 'text-danger' }
    case 'budget':
      return { icon: TriangleAlert, tone: 'text-warn' }
    case 'artifacts':
      return str(item.event.payload, 'error') ? { icon: FolderOpen, tone: 'text-danger' } : { icon: FolderOpen, tone: 'text-teal' }
    case 'state':
      return { icon: Database, tone: 'text-teal' }
    case 'context_compacted':
      return { icon: FoldVertical, tone: 'text-blue' }
    default:
      return { icon: ChevronRight, tone: 'text-faint' }
  }
}

const Who = ({ children }: { children: ReactNode }) => <span className="font-medium text-fg">{children}</span>
const Mono = ({ children }: { children: ReactNode }) => <span className="font-mono text-[11.5px] text-muted">{children}</span>

/** One-line summary plus optional expandable detail for an event. */
function describe(item: TimelineItem): { summary: ReactNode; detail?: ReactNode } {
  const p = item.event.payload
  const agent = str(p, 'agent_role') ?? 'agent'
  switch (item.event.type) {
    case 'run_status':
      return { summary: <>Run is <Who>{str(p, 'status')}</Who>{str(p, 'error') && <span className="text-danger"> — {str(p, 'error')}</span>}</> }
    case 'crew_started':
      return { summary: <>Crew <Who>{str(p, 'crew_name')}</Who> started</> }
    case 'task_started':
      return { summary: <>Task started, assigned to <Who>{agent}</Who></> }
    case 'agent_started':
      return { summary: <><Who>{agent}</Who> started working</> }
    case 'tool_started': {
      const result = item.result
      const state = !result ? <LoaderCircle className="inline size-3 animate-spin text-accent" /> : result.type === 'tool_error' ? <span className="text-danger">failed</span> : <span className="text-ok">done</span>
      const output = result ? (result.type === 'tool_error' ? str(result.payload, 'error') : str(result.payload, 'output')) : null
      return {
        summary: <><Who>{agent}</Who> used <Mono>{str(p, 'tool_name')}</Mono> {state}</>,
        detail: (
          <div className="flex flex-col gap-2">
            <div className="eyebrow">Arguments</div>
            <JsonBlock value={p.tool_args ?? {}} className="max-h-60" />
            {result && <div className="eyebrow">{result.type === 'tool_error' ? 'Error' : 'Output'}</div>}
            {result && <JsonBlock value={output ?? ''} className={`max-h-80 whitespace-pre-wrap ${result.type === 'tool_error' ? 'text-danger' : ''}`} />}
          </div>
        ),
      }
    }
    case 'llm_call': {
      const usage = obj(p, 'usage')
      const cached = usage ? num(usage, 'cached_prompt_tokens') : null
      const written = usage ? num(usage, 'cache_write_tokens') : null
      return {
        summary: (
          <>
            <Who>{agent}</Who> called <Mono>{str(p, 'model')}</Mono>{' '}
            <Mono>
              {int(usage ? num(usage, 'prompt_tokens') : null)}→{int(usage ? num(usage, 'completion_tokens') : null)} tok{cached ? ` (${int(cached)} cached)` : ''}{written ? ` (${int(written)} cache write)` : ''} · {usd(num(p, 'cost_usd'))}
            </Mono>
          </>
        ),
        detail: <LlmCallDetail seq={item.seq} />,
      }
    }
    case 'search':
      return {
        summary: (
          <>
            <Who>{agent}</Who> searched <span className="text-fg">“{truncate(str(p, 'query') ?? '', 90)}”</span>{' '}
            <Mono>{bool(p, 'grounded') ? 'grounded · ' : ''}{usd(num(p, 'cost_usd'))}</Mono>
          </>
        ),
        detail: <JsonBlock value={p} />,
      }
    case 'video':
      return {
        summary: (
          <>
            <Who>{agent}</Who> {bool(p, 'made') ? 'made video' : 'got no video for'}{' '}
            <span className="text-fg">{str(p, 'file') ?? ''}</span>{' '}
            <Mono>
              {int(num(p, 'duration_seconds'))}s · {str(p, 'resolution') ?? ''} · {usd(num(p, 'cost_usd'))}
            </Mono>
          </>
        ),
        detail: <JsonBlock value={p} />,
      }
    case 'delegation':
      return {
        summary: (
          <span className="text-danger">
            <Who>{str(p, 'from_agent')}</Who> delegated to <Who>{str(p, 'to_agent')}</Who>
            {str(p, 'question') && <span className="text-muted"> — {truncate(str(p, 'question') ?? '', 100)}</span>}
          </span>
        ),
        detail: <div className="whitespace-pre-wrap text-[12.5px]">{str(p, 'question')}<div className="mt-1"><Mono>via {str(p, 'tool_name')}</Mono></div></div>,
      }
    case 'task_completed':
      return { summary: <>Task completed by <Who>{agent}</Who></>, detail: <Markdown>{str(p, 'output') ?? ''}</Markdown> }
    case 'task_failed':
      return { summary: <span className="text-danger">Task failed: {str(p, 'error')}</span> }
    case 'crew_completed':
      return { summary: <>Crew completed · <Mono>{usd(num(p, 'cost_usd'))}</Mono></>, detail: <Markdown>{str(p, 'output') ?? ''}</Markdown> }
    case 'crew_failed':
      return { summary: <span className="text-danger">Crew failed{bool(p, 'budget_stopped') ? ' (budget stop)' : ''}: {str(p, 'error')}</span> }
    case 'context_compacted':
      if (bool(p, 'failed')) return { summary: <span className="text-warn">Could not compact <Who>{agent}</Who>'s context: {str(p, 'error')}</span> }
      return {
        summary: (
          <>
            Compacted <Who>{agent}</Who>'s context: about <Mono>{(num(p, 'tokens_before') ?? 0).toLocaleString()}</Mono> → <Mono>{(num(p, 'tokens_after') ?? 0).toLocaleString()}</Mono> tokens
            <span className="text-muted"> ({str(p, 'reason') === 'auto' ? 'over the limit in Settings' : 'asked'})</span>
          </>
        ),
      }
    case 'budget': {
      const state = str(p, 'state') === 'hard_stopped' ? 'hard stop — LLM calls halted' : 'searches stopped'
      return { summary: <span className="text-warn">Budget {state} at {usd(num(p, 'spent_usd'))} of {usd(num(p, 'budget_usd'))}</span> }
    }
    case 'artifacts': {
      const files = num(p, 'files') ?? 0
      const error = str(p, 'error')
      return {
        summary: error
          ? <span className="text-danger">Saving files failed: {error}</span>
          : <>Saved <Who>{int(files)}</Who> file{files === 1 ? '' : 's'} the agents wrote <Mono>{formatBytes(num(p, 'bytes'))}</Mono></>,
      }
    }
    case 'state':
      // A notes or action log call, as the run-state API recorded it ("Notes: read weekly-report v12")
      return { summary: <><Who>{str(p, 'agent_role') ?? 'Crew'}</Who> · {str(p, 'message')}</>, detail: str(p, 'detail') ? <div className="text-[12.5px] text-muted">{str(p, 'detail')}</div> : undefined }
    default:
      return { summary: <Mono>{item.event.type}</Mono>, detail: <JsonBlock value={p} /> }
  }
}

export function TimelineItemRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false)
  const { icon: Icon, tone } = lookFor(item)
  const { summary, detail } = describe(item)
  const highlight = item.event.type === 'delegation' ? 'bg-danger-soft/50' : item.event.type === 'budget' ? 'bg-warn-soft/50' : ''
  return (
    <li className={`rounded-md ${highlight}`}>
      <div
        className={`flex items-start gap-2 px-2 py-1 ${detail ? 'cursor-pointer hover:bg-panel-2' : ''} rounded-md`}
        onClick={() => detail && setOpen((o) => !o)}
        role={detail ? 'button' : undefined}
        aria-expanded={detail ? open : undefined}
        tabIndex={detail ? 0 : undefined}
        onKeyDown={(e) => {
          if (detail && (e.key === 'Enter' || e.key === ' ')) {
            e.preventDefault()
            setOpen((o) => !o)
          }
        }}
      >
        <span className="w-16 shrink-0 pt-px font-mono text-[10.5px] text-faint tabular">{timeOfDay(item.event.created_at)}</span>
        <Icon className={`mt-0.5 size-3.5 shrink-0 ${tone}`} />
        <div className="min-w-0 flex-1 text-[12.5px] leading-snug">{summary}</div>
        {detail && (open ? <ChevronDown className="size-3.5 shrink-0 text-faint" /> : <ChevronRight className="size-3.5 shrink-0 text-faint" />)}
      </div>
      {open && detail && <div className="mb-2 ml-[5.5rem] mr-2 mt-1 min-w-0">{detail}</div>}
    </li>
  )
}
