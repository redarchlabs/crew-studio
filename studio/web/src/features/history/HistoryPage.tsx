import { Fragment, useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { ChevronDown, ChevronRight, History, X } from 'lucide-react'
import { historyApi } from '../../api/endpoints'
import { type EntityType, type Revision } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { Pagination } from '../../components/Pagination'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip, type Tone } from '../../components/StatusBadge'
import { JsonBlock } from '../../components/misc'
import { useAuth } from '../../lib/auth'
import { dateTime } from '../../lib/format'
import { useAsync } from '../../lib/useAsync'
import { ENTITY_LABELS, entityPath, snapshotName, visibleEntityTypes } from './historyLinks'

const PAGE_SIZE = 50

const ACTION_TONE: Record<Revision['action'], Tone> = { create: 'ok', update: 'blue', delete: 'danger' }

export function HistoryPage() {
  const { can } = useAuth()
  const canHistory = can('history.view')
  const canAudit = can('admin.audit')
  const types = visibleEntityTypes(canHistory, canAudit)
  const [params, setParams] = useSearchParams()
  const rawType = params.get('entity_type')
  // Ignore a type the person may not read (e.g. a shared link to role history) rather than showing a 403
  const entityType = types.find((t) => t === rawType)
  const entityId = params.get('entity_id') ?? undefined
  const offset = Math.max(0, Number(params.get('offset')) || 0)
  // Auditors without history.view can only read admin records, so they must always pick a type
  const effectiveType: EntityType | undefined = entityType ?? (canHistory ? undefined : types[0])
  const res = useAsync(
    (signal) => historyApi.list({ entity_type: effectiveType, entity_id: entityId, limit: PAGE_SIZE, offset }, signal),
    [effectiveType, entityId, offset],
  )
  const [open, setOpen] = useState<number | null>(null)

  const setQuery = (next: { entity_type?: string; entity_id?: string | null; offset?: number }) => {
    const qs = new URLSearchParams()
    const t = next.entity_type ?? entityType ?? ''
    const id = next.entity_id === null ? '' : (next.entity_id ?? entityId ?? '')
    if (t) qs.set('entity_type', t)
    if (id) qs.set('entity_id', id)
    if (next.offset) qs.set('offset', String(next.offset))
    setParams(qs)
  }

  return (
    <>
      <PageHeader
        eyebrow="Configure"
        title="History"
        description={
          canAudit
            ? 'Every create, update and delete, with who made it, including people, roles, areas, keys and sign-in settings.'
            : 'Every create, update and delete of your LLMs, prices, settings, agents, tasks, crews, work orders, schedules and triggers, with who made it.'
        }
        actions={
          <select aria-label="Filter by entity type" className="field-input w-full sm:w-52" value={effectiveType ?? ''} onChange={(e) => setQuery({ entity_type: e.target.value, entity_id: null })}>
            {canHistory && <option value="">All types</option>}
            {types.map((t) => <option key={t} value={t}>{ENTITY_LABELS[t]}</option>)}
          </select>
        }
      />
      {entityId && (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-[12px] text-muted">
          Showing one {effectiveType ? ENTITY_LABELS[effectiveType].toLowerCase() : 'entity'}: <span className="break-all font-mono">{entityId}</span>
          <button type="button" className="inline-flex items-center gap-1 rounded border border-line px-1.5 py-0.5 hover:text-fg" onClick={() => setQuery({ entity_id: null })}><X className="size-3" /> clear</button>
        </div>
      )}
      <AsyncView {...res}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState icon={<History className="size-6" />} title="No revisions" hint="Changes appear here as people edit." />
          ) : (
            <>
              <div className="overflow-x-auto rounded-lg border border-line bg-panel">
                <table className="w-full min-w-[760px] text-left">
                  <thead className="border-b border-line">
                    <tr className="eyebrow [&>th]:px-3 [&>th]:py-2 [&>th]:font-semibold">
                      <th className="w-6" /><th>When</th><th>Who</th><th>Type</th><th>Action</th><th>Entity</th><th />
                    </tr>
                  </thead>
                  <tbody>
                    {page.items.map((rev) => {
                      const isOpen = open === rev.id
                      const path = entityPath(rev)
                      return (
                        <Fragment key={rev.id}>
                          <tr className="cursor-pointer border-b border-line hover:bg-panel-2/50 [&>td]:px-3 [&>td]:py-1.5" onClick={() => setOpen(isOpen ? null : rev.id)} aria-expanded={isOpen}>
                            <td>{isOpen ? <ChevronDown className="size-3.5 text-faint" /> : <ChevronRight className="size-3.5 text-faint" />}</td>
                            <td className="whitespace-nowrap text-[12px] text-muted">{dateTime(rev.created_at)}</td>
                            <td className="max-w-40 truncate text-[12.5px]" title={rev.actor_name || undefined}>{rev.actor_name || <span className="text-faint">—</span>}</td>
                            <td><Chip>{ENTITY_LABELS[rev.entity_type] ?? rev.entity_type}</Chip></td>
                            <td><Chip tone={ACTION_TONE[rev.action]}>{rev.action}</Chip></td>
                            <td className="max-w-xs truncate font-medium" onClick={(e) => path && e.stopPropagation()}>
                              {path ? <Link className="hover:text-accent" to={path}>{snapshotName(rev)}</Link> : snapshotName(rev)}
                            </td>
                            <td className="whitespace-nowrap text-right text-[12px]" onClick={(e) => e.stopPropagation()}>
                              <button type="button" className="text-muted hover:text-accent" onClick={() => setQuery({ entity_type: rev.entity_type, entity_id: rev.entity_id })}>only this</button>
                              {path && <Link className="ml-3 text-muted hover:text-accent" to={path}>open</Link>}
                            </td>
                          </tr>
                          {isOpen && (
                            <tr className="border-b border-line bg-bg/40">
                              <td colSpan={7} className="p-3"><JsonBlock value={rev.snapshot} className="max-h-[480px]" /></td>
                            </tr>
                          )}
                        </Fragment>
                      )
                    })}
                  </tbody>
                </table>
              </div>
              <Pagination meta={page.meta} onOffset={(o) => setQuery({ offset: o })} />
            </>
          )
        }
      </AsyncView>
    </>
  )
}
