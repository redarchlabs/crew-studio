import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router'
import { ClipboardList, Plus, Search } from 'lucide-react'
import { crewsApi, workOrdersApi } from '../../api/endpoints'
import type { Crew, WorkOrderSource, WorkOrderStatus } from '../../api/types'
import { LinkButton } from '../../components/Button'
import { PageHeader } from '../../components/PageHeader'
import { Pagination } from '../../components/Pagination'
import { AsyncView, EmptyState } from '../../components/States'
import { Can, useAuth } from '../../lib/auth'
import { sortByName } from '../../lib/catalog'
import { useAsync } from '../../lib/useAsync'
import { useNow } from '../../lib/useNow'
import { usePollWhile } from '../../lib/usePollWhile'
import { HistoryLink } from '../../components/misc'
import { WORK_ORDER_SOURCES, WORK_ORDER_STATUSES, isActive } from './workOrderLabels'
import { WorkOrdersTable } from './WorkOrdersTable'

const PAGE_SIZE = 25
const POLL_MS = 4000
const SEARCH_DEBOUNCE_MS = 300

type Filters = { status: string; source: string; crew_id: string; q: string; offset: number }

function readFilters(params: URLSearchParams): Filters {
  return {
    status: params.get('status') ?? '',
    source: params.get('source') ?? '',
    crew_id: params.get('crew_id') ?? '',
    q: params.get('q') ?? '',
    offset: Math.max(0, Number(params.get('offset')) || 0),
  }
}

function toParams(f: Filters): URLSearchParams {
  const qs = new URLSearchParams()
  for (const [k, v] of Object.entries(f)) if (v !== '' && v !== 0) qs.set(k, String(v))
  return qs
}

export function WorkOrdersPage() {
  const [params, setParams] = useSearchParams()
  const filters = readFilters(params)
  const [search, setSearch] = useState(filters.q)
  const res = useAsync(
    (signal) =>
      workOrdersApi.list(
        {
          status: (filters.status || undefined) as WorkOrderStatus | undefined,
          source: (filters.source || undefined) as WorkOrderSource | undefined,
          crew_id: filters.crew_id || undefined,
          q: filters.q || undefined,
          limit: PAGE_SIZE,
          offset: filters.offset,
        },
        signal,
      ),
    [filters.status, filters.source, filters.crew_id, filters.q, filters.offset],
  )
  const anyActive = res.data?.items.some((wo) => isActive(wo.status)) ?? false
  const now = useNow(anyActive, 5000)
  usePollWhile(anyActive, res.reload, POLL_MS)

  const update = (next: Partial<Filters>) => setParams(toParams({ ...filters, offset: 0, ...next }), { replace: true })

  // Debounce the search box into ?q=
  useEffect(() => {
    if (search === filters.q) return
    const t = setTimeout(() => setParams((p) => toParams({ ...readFilters(p), q: search.trim(), offset: 0 }), { replace: true }), SEARCH_DEBOUNCE_MS)
    return () => clearTimeout(t)
  }, [search, filters.q, setParams])

  const filtered = Boolean(filters.status || filters.source || filters.crew_id || filters.q)

  return (
    <>
      <PageHeader
        eyebrow="Console"
        title="Work orders"
        description="Requests to run a crew: a brief, input files, inputs and a budget. Each submit is an attempt you can inspect."
        actions={
          <>
            <HistoryLink type="work_order" />
            <Can perm="work_orders.create">
              <LinkButton to="/work-orders/new" variant="primary" icon={<Plus className="size-4" />}>New work order</LinkButton>
            </Can>
          </>
        }
      />
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="flex min-w-[220px] flex-1 items-center gap-2 rounded-md border border-line bg-raised px-2.5 sm:max-w-sm">
          <Search className="size-3.5 text-faint" />
          <input className="w-full bg-transparent py-1.5 outline-none" placeholder="Search title or WO-123…" aria-label="Search work orders" value={search} onChange={(e) => setSearch(e.target.value)} />
        </div>
        <FilterSelect label="Status" value={filters.status} options={WORK_ORDER_STATUSES} onChange={(status) => update({ status })} />
        <FilterSelect label="Source" value={filters.source} options={WORK_ORDER_SOURCES} onChange={(source) => update({ source })} />
        <CrewFilterSelect value={filters.crew_id} onChange={(crew_id) => update({ crew_id })} />
      </div>
      <AsyncView {...res}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState
              icon={<ClipboardList className="size-6" />}
              title={filtered ? 'No work orders match these filters' : 'No work orders yet'}
              hint={filtered ? 'Try clearing a filter.' : 'Create one to run a crew with a brief and files. Schedules and triggers create them automatically.'}
              action={!filtered && <Can perm="work_orders.create"><LinkButton to="/work-orders/new" variant="primary">New work order</LinkButton></Can>}
            />
          ) : (
            <>
              <WorkOrdersTable items={page.items} now={now} />
              <Pagination meta={page.meta} onOffset={(offset) => setParams(toParams({ ...filters, offset }))} />
            </>
          )
        }
      </AsyncView>
    </>
  )
}

function FilterSelect({ label, value, options, onChange }: { label: string; value: string; options: readonly string[]; onChange: (v: string) => void }) {
  return (
    <div className="min-w-[130px] flex-1 sm:flex-none">
      <select aria-label={`Filter by ${label.toLowerCase()}`} className="field-input" value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="">{label}: all</option>
        {options.map((o) => <option key={o} value={o}>{label}: {o}</option>)}
      </select>
    </div>
  )
}

/** Crews the person can see, for the crew filter; empty without crews.view. */
function useVisibleCrews() {
  const { can } = useAuth()
  const canView = can('crews.view')
  return useAsync((signal): Promise<Crew[]> => (canView ? crewsApi.list(signal).then(sortByName) : Promise.resolve([])), [canView])
}

/** "All crews" or one crew, sent as ?crew_id=. A crew id from the URL that is not listed stays selected. */
function CrewFilterSelect({ value, onChange }: { value: string; onChange: (crewId: string) => void }) {
  const crews = useVisibleCrews()
  const list = crews.data ?? []
  if (!value && list.length === 0 && !crews.error) return null
  const unlisted = value && !list.some((c) => c.id === value) ? (crews.data ? 'Crew not visible' : 'Selected crew') : null
  return (
    <div className="min-w-[160px] flex-1 sm:flex-none">
      <select aria-label="Filter by crew" className="field-input" value={value} onChange={(e) => onChange(e.target.value)} disabled={Boolean(crews.error) && !value} title={crews.error ? 'Could not load crews' : undefined}>
        <option value="">All crews</option>
        {list.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
        {unlisted && <option value={value}>{unlisted}</option>}
      </select>
    </div>
  )
}
