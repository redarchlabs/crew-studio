import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { ChevronLeft, Save, Trash2 } from 'lucide-react'
import type { EntityType } from '../api/types'
import { Button } from './Button'
import { ConfirmButton, HistoryLink } from './misc'

type EditorHeaderProps = {
  backTo: string
  backLabel: string
  title: string
  isNew: boolean
  dirty: boolean
  saving: boolean
  entityType: EntityType
  entityId?: string
  onSave: () => void
  onDelete?: () => void
  deleting?: boolean
  extra?: ReactNode
}

export function EditorHeader({ backTo, backLabel, title, isNew, dirty, saving, entityType, entityId, onSave, onDelete, deleting, extra }: EditorHeaderProps) {
  return (
    <header className="sticky top-0 z-10 -mx-4 mb-5 flex flex-wrap items-center gap-3 border-b border-line bg-bg/90 px-4 py-3 backdrop-blur md:-mx-6 md:px-6">
      <div className="min-w-0 flex-1">
        <Link to={backTo} className="mb-0.5 inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
          <ChevronLeft className="size-3.5" /> {backLabel}
        </Link>
        <div className="flex items-center gap-2">
          <h1 className="truncate text-[19px] font-semibold tracking-tight">{title}</h1>
          {dirty && <span className="rounded bg-warn-soft px-1.5 py-px text-[11px] text-warn">unsaved</span>}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {!isNew && <HistoryLink type={entityType} id={entityId} />}
        {extra}
        {!isNew && onDelete && (
          <ConfirmButton icon={<Trash2 className="size-3.5" />} onConfirm={onDelete} busy={deleting} size="md">
            Delete
          </ConfirmButton>
        )}
        <Button variant="primary" icon={<Save className="size-4" />} busy={saving} disabled={!isNew && !dirty} onClick={onSave}>
          {isNew ? 'Create' : 'Save'}
        </Button>
      </div>
    </header>
  )
}
