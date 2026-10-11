import type { ReactNode } from 'react'
import { CircleAlert, Inbox, LoaderCircle, RefreshCw } from 'lucide-react'
import { ApiError, errorMessage } from '../api/client'
import { Button } from './Button'

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="flex items-center gap-2 px-4 py-10 text-muted" role="status">
      <LoaderCircle className="size-4 animate-spin text-accent" />
      <span>{label}</span>
    </div>
  )
}

type EmptyProps = { title: string; hint?: ReactNode; action?: ReactNode; icon?: ReactNode }

export function EmptyState({ title, hint, action, icon }: EmptyProps) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-line px-6 py-12 text-center">
      <div className="text-faint">{icon ?? <Inbox className="size-6" />}</div>
      <div className="font-medium">{title}</div>
      {hint && <div className="max-w-md text-muted">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export function ErrorState({ error, onRetry, title = 'Could not load this page' }: { error: unknown; onRetry?: () => void; title?: string }) {
  const code = error instanceof ApiError ? error.code : null
  return (
    <div className="flex items-start gap-3 rounded-lg border border-danger/40 bg-danger-soft px-4 py-3" role="alert">
      <CircleAlert className="mt-0.5 size-4 shrink-0 text-danger" />
      <div className="min-w-0 flex-1">
        <div className="font-medium text-fg">{title}</div>
        <div className="text-muted">{errorMessage(error)}</div>
        {code && <div className="mt-1 font-mono text-[11px] text-faint">{code}</div>}
      </div>
      {onRetry && (
        <Button size="sm" icon={<RefreshCw className="size-3.5" />} onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  )
}

/** Renders loading / error / content for an async resource. */
export function AsyncView<T>({
  data,
  error,
  loading,
  reload,
  children,
}: {
  data: T | undefined
  error: unknown
  loading: boolean
  reload: () => void
  children: (data: T) => ReactNode
}) {
  if (error) return <ErrorState error={error} onRetry={reload} />
  if (data === undefined) return loading ? <Loading /> : null
  return <>{children(data)}</>
}
