import type { ComponentProps, ReactNode } from 'react'
import { Link } from 'react-router'
import { ChevronLeft, Lock } from 'lucide-react'
import { EditorHeader } from './EditorHeader'
import { HistoryLink } from './misc'

type ItemEditorHeaderProps = ComponentProps<typeof EditorHeader> & {
  /** False when the person may not save this item: the header drops Save/Delete and says so */
  canSave: boolean
  /** Delete is offered only when this is true (and the item exists) */
  canDelete: boolean
  extra?: ReactNode
}

/** EditorHeader that respects permissions: read-only viewers get a plain header instead of dead buttons. */
export function ItemEditorHeader({ canSave, canDelete, onDelete, ...props }: ItemEditorHeaderProps) {
  if (canSave) return <EditorHeader {...props} onDelete={canDelete ? onDelete : undefined} />
  const { backTo, backLabel, title, isNew, entityType, entityId, extra } = props
  return (
    <header className="sticky top-0 z-10 -mx-4 mb-5 flex flex-wrap items-center gap-3 border-b border-line bg-bg/90 px-4 py-3 backdrop-blur md:-mx-6 md:px-6">
      <div className="min-w-0 flex-1">
        <Link to={backTo} className="mb-0.5 inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
          <ChevronLeft className="size-3.5" /> {backLabel}
        </Link>
        <div className="flex items-center gap-2">
          <h1 className="truncate text-[19px] font-semibold tracking-tight">{title}</h1>
          <span className="inline-flex items-center gap-1 rounded bg-panel-2 px-1.5 py-px text-[11px] text-muted"><Lock className="size-3" /> read-only</span>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {!isNew && <HistoryLink type={entityType} id={entityId} />}
        {extra}
      </div>
    </header>
  )
}
