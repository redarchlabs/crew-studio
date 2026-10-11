import { Search } from 'lucide-react'
import type { StatusFilter } from './accountList'

type AccountFiltersProps = { query: string; onQuery: (q: string) => void; status: StatusFilter; onStatus: (s: StatusFilter) => void; placeholder: string }

export function AccountFilters({ query, onQuery, status, onStatus, placeholder }: AccountFiltersProps) {
  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <div className="flex w-full max-w-sm items-center gap-2 rounded-md border border-line bg-raised px-2.5">
        <Search className="size-3.5 text-faint" />
        <input className="w-full bg-transparent py-1.5 outline-none" placeholder={placeholder} value={query} onChange={(e) => onQuery(e.target.value)} />
      </div>
      <select aria-label="Filter by status" className="field-input w-full sm:w-36" value={status} onChange={(e) => onStatus(e.target.value === 'active' || e.target.value === 'disabled' ? e.target.value : '')}>
        <option value="">Any status</option>
        <option value="active">Active</option>
        <option value="disabled">Disabled</option>
      </select>
    </div>
  )
}
