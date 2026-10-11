import { Lock } from 'lucide-react'
import type { ReactNode } from 'react'
import type { Permission } from '../api/types'

/** Short banner shown above a disabled form: what the person lacks to change it. */
export function ReadOnlyNote({ perm, children }: { perm?: Permission; children?: ReactNode }) {
  return (
    <div className="mb-4 flex items-start gap-2 rounded-md border border-line bg-panel-2 px-3 py-2 text-[12.5px] text-muted" role="note">
      <Lock className="mt-0.5 size-3.5 shrink-0 text-faint" />
      <span>
        {children ?? (
          <>
            Read-only: you need <code className="rounded bg-panel px-1 font-mono text-[11.5px]">{perm}</code> to change this.
          </>
        )}
      </span>
    </div>
  )
}

/** Disables every control inside when the person may not change the form (native fieldset behaviour). */
export function LockableFieldset({ locked, children, className = '' }: { locked: boolean; children: ReactNode; className?: string }) {
  return (
    <fieldset disabled={locked} className={`m-0 min-w-0 border-0 p-0 ${className}`}>
      {children}
    </fieldset>
  )
}
