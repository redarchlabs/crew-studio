import type { ReactNode } from 'react'
import type { RunStatus } from '../api/types'

const STYLES: Record<RunStatus, string> = {
  queued: 'bg-blue-soft text-blue',
  running: 'bg-accent-soft text-accent',
  succeeded: 'bg-ok-soft text-ok',
  failed: 'bg-danger-soft text-danger',
  cancelled: 'bg-panel-2 text-muted',
}

export function StatusBadge({ status }: { status: RunStatus }) {
  const live = status === 'running' || status === 'queued'
  return (
    <span className={`inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 font-mono text-[11px] font-medium uppercase tracking-wide ${STYLES[status]}`}>
      <span className={`size-1.5 rounded-full bg-current ${live ? 'pulse-dot' : ''}`} />
      {status}
    </span>
  )
}

type Tone = 'neutral' | 'accent' | 'teal' | 'blue' | 'violet' | 'ok' | 'warn' | 'danger'
const TONES: Record<Tone, string> = {
  neutral: 'bg-panel-2 text-muted border-line',
  accent: 'bg-accent-soft text-accent border-transparent',
  teal: 'bg-teal-soft text-teal border-transparent',
  blue: 'bg-blue-soft text-blue border-transparent',
  violet: 'bg-violet-soft text-violet border-transparent',
  ok: 'bg-ok-soft text-ok border-transparent',
  warn: 'bg-warn-soft text-warn border-transparent',
  danger: 'bg-danger-soft text-danger border-transparent',
}

export function Chip({ children, tone = 'neutral', title, className = '' }: { children: ReactNode; tone?: Tone; title?: string; className?: string }) {
  return (
    <span title={title} className={`inline-flex max-w-full items-center gap-1 truncate rounded border px-1.5 py-px text-[11px] ${TONES[tone]} ${className}`}>
      {children}
    </span>
  )
}

export type { Tone }
