import type { ReactNode } from 'react'
import { CircleAlert, Info } from 'lucide-react'

/** The Crew Studio mark (same drawing as the sidebar brand). */
export function BrandMark({ className = 'size-10' }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden="true">
      <rect width="32" height="32" rx="7" fill="var(--panel-2)" stroke="var(--line-strong)" />
      <circle cx="9" cy="16" r="3.5" fill="var(--accent)" />
      <circle cx="23" cy="9" r="3.5" fill="var(--teal)" />
      <circle cx="23" cy="23" r="3.5" fill="var(--teal)" />
      <path d="M12 15l8-5M12 17l8 5" stroke="var(--fg-muted)" strokeWidth="1.6" />
    </svg>
  )
}

/** Full-page centred card used by the sign-in and first-run screens (outside the app shell). */
export function AuthCard({ title, subtitle, children, footer }: { title: string; subtitle?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  return (
    <div className="flex min-h-full items-start justify-center overflow-y-auto bg-bg px-4 py-10 sm:items-center">
      <div className="w-full max-w-sm">
        <div className="mb-5 flex flex-col items-center gap-2 text-center">
          <BrandMark />
          <div className="leading-tight">
            <div className="text-[17px] font-semibold tracking-tight">Crew Studio</div>
            <div className="font-mono text-[10.5px] text-faint">crewai · console</div>
          </div>
        </div>
        <main className="rounded-xl border border-line bg-panel p-5 shadow-[var(--shadow)]">
          <h1 className="text-[16px] font-semibold tracking-tight">{title}</h1>
          {subtitle && <p className="mt-1 text-[12.5px] text-muted">{subtitle}</p>}
          <div className="mt-4">{children}</div>
        </main>
        {footer && <div className="mt-4 text-center text-[11.5px] text-faint">{footer}</div>}
      </div>
    </div>
  )
}

/** Inline message box; the app's toasts live in the shell, so these screens show feedback in place. */
export function AuthNotice({ tone = 'error', children }: { tone?: 'error' | 'info'; children: ReactNode }) {
  const error = tone === 'error'
  return (
    <div
      role={error ? 'alert' : 'status'}
      className={`mb-4 flex items-start gap-2 rounded-md border px-3 py-2 text-[12.5px] ${error ? 'border-danger/40 bg-danger-soft text-fg' : 'border-line bg-panel-2 text-muted'}`}
    >
      {error ? <CircleAlert className="mt-0.5 size-3.5 shrink-0 text-danger" /> : <Info className="mt-0.5 size-3.5 shrink-0 text-blue" />}
      <span className="min-w-0">{children}</span>
    </div>
  )
}
