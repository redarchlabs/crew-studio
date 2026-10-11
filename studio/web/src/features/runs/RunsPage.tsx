import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router'
import { Play } from 'lucide-react'
import { crewsApi, runsApi } from '../../api/endpoints'
import { RUN_KINDS, RUN_STATUSES, TRIGGER_TYPES, type Run } from '../../api/types'
import { PageHeader } from '../../components/PageHeader'
import { Pagination } from '../../components/Pagination'
import { AsyncView, EmptyState } from '../../components/States'
import { useAuth } from '../../lib/auth'
import { sortByName } from '../../lib/catalog'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { useNow } from '../../lib/useNow'
import { isTerminal } from './timeline/payload'
import { RunsTable } from './RunsTable'
import { isFiltered, KIND_LABELS, parseRunsFilter, runsFilterToParams, TRIGGER_LABELS, type RunsFilter } from './runsQuery'

const PAGE_SIZE = 25
const POLL_MS = 4000

export function RunsPage() {
  const { can } = useAuth()
  const [params, setParams] = useSearchParams()
  const filter = parseRunsFilter(params)
  const { crew_id: crewId, status, trigger_type: triggerType, kind, offset } = filter
  // The crew filter needs the crew list, which only crews.view may read
  const showCrews = can('crews.view')
  const crews = useAsync((signal) => (showCrews ? crewsApi.list(signal) : Promise.resolve([])), [showCrews])
  const res = useAsync(
    (signal) => runsApi.list({ crew_id: crewId || undefined, status: status || undefined, trigger_type: triggerType || undefined, kind: kind || undefined, limit: PAGE_SIZE, offset }, signal),
    [crewId, status, triggerType, kind, offset],
  )
  const [busyId, setBusyId] = useState<string | null>(null)
  const anyActive = res.data?.items.some((r) => !isTerminal(r.status)) ?? false
  const now = useNow(anyActive)

  // Poll while something on this page is still queued or running.
  const { reload } = res
  useEffect(() => {
    if (!anyActive) return
    const t = setInterval(reload, POLL_MS)
    return () => clearInterval(t)
  }, [anyActive, reload])

  // Changing a filter returns to the first page
  const setQuery = (next: Partial<RunsFilter>) => setParams(runsFilterToParams({ ...filter, offset: 0, ...next }))

  const replace = (run: Run) => res.setData((prev) => prev && { ...prev, items: prev.items.map((r) => (r.id === run.id ? { ...r, ...run } : r)) })

  const cancel = async (run: Run) => {
    setBusyId(run.id)
    try {
      replace(await runsApi.cancel(run.id))
      notifySuccess('Run cancelled')
    } catch (err) {
      notifyError('Could not cancel the run', err)
    } finally {
      setBusyId(null)
    }
  }

  const remove = async (run: Run) => {
    setBusyId(run.id)
    try {
      await runsApi.remove(run.id)
      notifySuccess('Run deleted')
      res.reload()
    } catch (err) {
      notifyError('Could not delete the run', err)
    } finally {
      setBusyId(null)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Console"
        title="Runs"
        description="Every crew execution, chat turn and hand-off, newest first."
        actions={
          <>
            {showCrews && (
              <select aria-label="Filter by crew" className="field-input w-full sm:w-48" value={crewId} onChange={(e) => setQuery({ crew_id: e.target.value })}>
                <option value="">All crews</option>
                {sortByName(crews.data ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
              </select>
            )}
            <select aria-label="Filter by status" className="field-input w-full sm:w-36" value={status} onChange={(e) => setQuery({ status: e.target.value as RunsFilter['status'] })}>
              <option value="">Any status</option>
              {RUN_STATUSES.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
            <select aria-label="Filter by trigger" className="field-input w-full sm:w-36" value={triggerType} onChange={(e) => setQuery({ trigger_type: e.target.value as RunsFilter['trigger_type'] })}>
              <option value="">Any trigger</option>
              {TRIGGER_TYPES.map((t) => <option key={t} value={t}>{TRIGGER_LABELS[t]}</option>)}
            </select>
            <select aria-label="Filter by kind" className="field-input w-full sm:w-36" value={kind} onChange={(e) => setQuery({ kind: e.target.value as RunsFilter['kind'] })}>
              <option value="">Runs and chat</option>
              {RUN_KINDS.map((k) => <option key={k} value={k}>{KIND_LABELS[k]}</option>)}
            </select>
          </>
        }
      />
      <AsyncView {...res}>
        {(page) =>
          page.items.length === 0 ? (
            <EmptyState
              icon={<Play className="size-6" />}
              title={isFiltered(filter) ? 'No runs match these filters' : 'No runs yet'}
              hint={isFiltered(filter) ? 'Try another status, trigger, kind or crew.' : 'Start a run from a crew’s page, a work order or the overview.'}
            />
          ) : (
            <>
              <RunsTable
                runs={page.items}
                now={now}
                onCancel={can('runs.cancel') ? (r) => void cancel(r) : undefined}
                onDelete={can('runs.delete') ? (r) => void remove(r) : undefined}
                busyId={busyId}
              />
              <Pagination meta={page.meta} onOffset={(o) => setQuery({ offset: o })} />
            </>
          )
        }
      </AsyncView>
    </>
  )
}
