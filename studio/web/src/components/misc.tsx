import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { Check, ChevronDown, ChevronRight, Copy, History } from 'lucide-react'
import type { EntityType } from '../api/types'
import { copyText } from '../lib/download'
import { notifyError } from '../lib/notify'
import { Button, type ButtonSize } from './Button'

export function HistoryLink({ type, id }: { type: EntityType; id?: string }) {
  const qs = new URLSearchParams({ entity_type: type, ...(id ? { entity_id: id } : {}) })
  return (
    <Link to={`/history?${qs.toString()}`} className="inline-flex items-center gap-1 text-[12px] text-muted hover:text-accent">
      <History className="size-3.5" /> History
    </Link>
  )
}

export function CopyButton({ text, label = 'Copy', size = 'sm' }: { text: string; label?: string; size?: ButtonSize }) {
  const [done, setDone] = useState(false)
  useEffect(() => {
    if (!done) return
    const t = setTimeout(() => setDone(false), 1400)
    return () => clearTimeout(t)
  }, [done])
  return (
    <Button
      size={size}
      variant="ghost"
      icon={done ? <Check className="size-3.5 text-ok" /> : <Copy className="size-3.5" />}
      onClick={() => {
        copyText(text).then(
          () => setDone(true),
          (err: unknown) => notifyError('Copy failed', err),
        )
      }}
    >
      {done ? 'Copied' : label}
    </Button>
  )
}

/** Two-step destructive button: first click arms it, second confirms. */
export function ConfirmButton({ onConfirm, children, icon, busy, size = 'sm', disabled }: { onConfirm: () => void; children: ReactNode; icon?: ReactNode; busy?: boolean; size?: ButtonSize; disabled?: boolean }) {
  const [armed, setArmed] = useState(false)
  useEffect(() => {
    if (!armed) return
    const t = setTimeout(() => setArmed(false), 3000)
    return () => clearTimeout(t)
  }, [armed])
  return (
    <Button
      size={size}
      variant={armed ? 'danger' : 'ghost'}
      icon={icon}
      busy={busy}
      disabled={disabled}
      onClick={() => {
        if (armed) {
          setArmed(false)
          onConfirm()
        } else setArmed(true)
      }}
    >
      {armed ? 'Confirm?' : children}
    </Button>
  )
}

export function Collapsible({ title, children, defaultOpen = false, meta }: { title: ReactNode; children: ReactNode; defaultOpen?: boolean; meta?: ReactNode }) {
  const [open, setOpen] = useState(defaultOpen)
  return (
    <div className="rounded-md border border-line bg-panel">
      <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-panel-2">
        {open ? <ChevronDown className="size-4 text-faint" /> : <ChevronRight className="size-4 text-faint" />}
        <span className="flex-1 font-medium">{title}</span>
        {meta && <span className="text-[12px] text-faint">{meta}</span>}
      </button>
      {open && <div className="border-t border-line p-3">{children}</div>}
    </div>
  )
}

export function JsonBlock({ value, className = '' }: { value: unknown; className?: string }) {
  return (
    <pre className={`overflow-auto rounded-md border border-line bg-raised p-3 font-mono text-[11.5px] leading-relaxed text-muted ${className}`}>
      {typeof value === 'string' ? value : JSON.stringify(value, null, 2)}
    </pre>
  )
}

export function Stat({ label, value, sub }: { label: ReactNode; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="eyebrow">{label}</div>
      <div className="mt-0.5 truncate font-mono text-[15px] font-medium tabular">{value}</div>
      {sub && <div className="truncate text-[11.5px] text-faint">{sub}</div>}
    </div>
  )
}
