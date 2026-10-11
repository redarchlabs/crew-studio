import {
  ArrowRightLeft,
  Bot,
  Clapperboard,
  CircleCheck,
  CircleHelp,
  CircleX,
  Cpu,
  Database,
  FoldVertical,
  FolderOpen,
  ListChecks,
  Play,
  Reply,
  Search,
  TriangleAlert,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import type { JournalEntryKind } from '../../../../api/types'
import { avatarClass, initials } from './journalModel'

export const KIND_LOOK: Record<JournalEntryKind, { icon: LucideIcon; tone: string; label: string }> = {
  run: { icon: Play, tone: 'text-accent', label: 'run' },
  task_started: { icon: ListChecks, tone: 'text-blue', label: 'task started' },
  task_completed: { icon: CircleCheck, tone: 'text-ok', label: 'task completed' },
  task_failed: { icon: CircleX, tone: 'text-danger', label: 'task failed' },
  agent_started: { icon: Bot, tone: 'text-teal', label: 'agent started' },
  llm_call: { icon: Cpu, tone: 'text-muted', label: 'LLM call' },
  tool_call: { icon: Wrench, tone: 'text-violet', label: 'tool call' },
  tool_result: { icon: Reply, tone: 'text-violet', label: 'tool result' },
  tool_error: { icon: CircleX, tone: 'text-danger', label: 'tool error' },
  search: { icon: Search, tone: 'text-blue', label: 'search' },
  video: { icon: Clapperboard, tone: 'text-violet', label: 'video' },
  delegation: { icon: ArrowRightLeft, tone: 'text-danger', label: 'delegation' },
  consultation: { icon: CircleHelp, tone: 'text-accent', label: 'question to the lead' },
  budget: { icon: TriangleAlert, tone: 'text-warn', label: 'budget' },
  artifacts: { icon: FolderOpen, tone: 'text-teal', label: 'files saved' },
  state: { icon: Database, tone: 'text-teal', label: 'crew state' },
  compaction: { icon: FoldVertical, tone: 'text-blue', label: 'context compacted' },
}

export function KindIcon({ kind, className = 'size-3.5' }: { kind: JournalEntryKind; className?: string }) {
  const look = KIND_LOOK[kind] ?? KIND_LOOK.run
  const Icon = look.icon
  return <Icon className={`shrink-0 ${look.tone} ${className}`} aria-label={look.label} />
}

/** Initials avatar for an agent role ("" is the crew). */
export function AgentAvatar({ role, size = 'size-6' }: { role: string; size?: string }) {
  return (
    <span title={role || 'Crew'} className={`inline-flex ${size} shrink-0 items-center justify-center rounded-full font-mono text-[10px] font-semibold ${avatarClass(role)}`}>
      {initials(role)}
    </span>
  )
}
