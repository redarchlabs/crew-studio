import { ChevronLeft, ChevronRight } from 'lucide-react'
import type { PageMeta } from '../api/types'
import { IconButton } from './Button'

type PaginationProps = { meta: PageMeta; onOffset: (offset: number) => void }

export function Pagination({ meta, onOffset }: PaginationProps) {
  const { total, limit, offset } = meta
  if (total === 0) return null
  const from = offset + 1
  const to = Math.min(offset + limit, total)
  const prev = Math.max(0, offset - limit)
  const hasPrev = offset > 0
  const hasNext = offset + limit < total
  return (
    <div className="flex items-center justify-end gap-2 px-1 py-2 text-[12px] text-muted">
      <span className="tabular">
        {from}–{to} of {total}
      </span>
      <IconButton label="Previous page" icon={<ChevronLeft className="size-4" />} disabled={!hasPrev} onClick={() => onOffset(prev)} />
      <IconButton label="Next page" icon={<ChevronRight className="size-4" />} disabled={!hasNext} onClick={() => onOffset(offset + limit)} />
    </div>
  )
}
