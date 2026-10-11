import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { Pencil, Send } from 'lucide-react'
import { workOrdersApi } from '../../api/endpoints'
import type { Run, WorkOrder } from '../../api/types'
import { Button } from '../../components/Button'
import { AsyncView, EmptyState, ErrorState, Loading } from '../../components/States'
import { Tabs, type TabDef } from '../../components/Tabs'
import { useAuth } from '../../lib/auth'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { SKIP_GUARD } from '../../lib/useUnsavedGuard'
import { ReportsTab } from '../delivery/ReportsTab'
import { FilesTab } from '../runs/tabs/FilesTab'
import { JournalTab } from '../runs/tabs/journal/JournalTab'
import { LlmCallsContext } from '../runs/useLlmCalls'
import { toWorkOrderInput } from './composer/composerForm'
import { WorkOrderComposer } from './composer/WorkOrderComposer'
import { AttemptSelector } from './detail/AttemptSelector'
import { BriefTab } from './detail/BriefTab'
import { ConversationTab } from './detail/ConversationTab'
import { DebugTab } from './detail/DebugTab'
import { QuestionsPanel } from './detail/QuestionsPanel'
import { useAttempt } from './detail/useAttempt'
import { WorkOrderHeader, type HeaderBusy } from './detail/WorkOrderHeader'
import { ResumeRunModal } from '../runs/ResumeRunModal'
import { WorkflowTab } from './detail/WorkflowTab'
import { useTargetCatalog } from './target/useTargetCatalog'
import { woNumber } from './workOrderLabels'

const TAB_KEYS = ['brief', 'conversation', 'workflow', 'journal', 'files', 'reports', 'debug'] as const
type TabKey = (typeof TAB_KEYS)[number]

const isTabKey = (v: string | null): v is TabKey => v !== null && (TAB_KEYS as readonly string[]).includes(v)

type DetailProps = { workOrder: WorkOrder; reload: () => void; patch: (update: (wo: WorkOrder) => WorkOrder) => void }

/** Adds a just-started attempt to the work order without waiting for a refetch. */
function withNewRun(wo: WorkOrder, run: Run): WorkOrder {
  return { ...wo, status: run.status, current_run_id: run.id, run_count: wo.run_count + 1, runs: [run, ...(wo.runs ?? []).filter((r) => r.id !== run.id)] }
}

function WorkOrderDetail({ workOrder: wo, reload, patch }: DetailProps) {
  const navigate = useNavigate()
  const { can } = useAuth()
  const canViewRuns = can('runs.view')
  const [params, setParams] = useSearchParams()
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState<HeaderBusy>(null)
  const [resuming, setResuming] = useState(false)
  const runs = wo.runs ?? []
  const wanted = params.get('run')
  const runId = runs.some((r) => r.id === wanted) ? wanted : (wo.current_run_id ?? runs[0]?.id ?? null)
  const attempt = useAttempt(wo, runId, canViewRuns, reload)

  const canFollowUp = can('crews.chat') || can('work_orders.create')
  const allowed = (k: TabKey) => (k !== 'debug' || canViewRuns) && (k !== 'conversation' || canFollowUp)
  const tab: TabKey = isTabKey(params.get('tab')) && allowed(params.get('tab') as TabKey) ? (params.get('tab') as TabKey) : 'brief'
  const setParam = (updates: Record<string, string>) =>
    setParams((p) => {
      const next = new URLSearchParams(p)
      for (const [k, v] of Object.entries(updates)) next.set(k, v)
      return next
    }, { replace: true })

  const isCurrent = runId !== null && runId === wo.current_run_id
  // Only the latest attempt can be resumed; the server says where (run detail's "resumable")
  const resumable = isCurrent && attempt.run?.resumable ? attempt.run.resumable : null
  const status = isCurrent && attempt.status ? attempt.status : wo.status
  const spent = attempt.live ? (attempt.timeline.spentUsd ?? attempt.run?.cost_usd ?? 0) : (attempt.run?.cost_usd ?? attempt.timeline.spentUsd ?? 0)

  const act = async (kind: NonNullable<HeaderBusy>, fn: () => Promise<void>, failure: string) => {
    setBusy(kind)
    try {
      await fn()
    } catch (err) {
      notifyError(failure, err)
    } finally {
      setBusy(null)
    }
  }
  const onRunStarted = (run: Run, extra: Record<string, string> = {}) => {
    patch((prev) => withNewRun(prev, run))
    setParam({ run: run.id, ...extra })
    reload()
  }
  const submit = () => act('submit', async () => {
    const run = await workOrdersApi.submit(wo.id)
    notifySuccess(wo.status === 'draft' ? `${woNumber(wo.number)} submitted` : `Started attempt ${wo.run_count + 1}`)
    onRunStarted(run, wo.status === 'draft' ? { tab: 'workflow' } : {})
  }, 'Could not start the work order')
  const cancel = () => act('cancel', async () => {
    await workOrdersApi.cancel(wo.id)
    notifySuccess('Cancelling the current attempt')
    attempt.reloadRun()
    reload()
  }, 'Could not cancel the work order')
  const remove = () => act('delete', async () => {
    await workOrdersApi.remove(wo.id)
    notifySuccess(`Deleted ${woNumber(wo.number)}`)
    navigate('/work-orders', { state: SKIP_GUARD })
  }, 'Could not delete the work order')

  if (editing) {
    return (
      <EditWorkOrder
        workOrder={wo}
        onDone={(run) => {
          setEditing(false)
          if (run) onRunStarted(run)
          else reload()
        }}
      />
    )
  }

  const tabs: TabDef<TabKey>[] = [
    { key: 'brief', label: 'Brief', badge: wo.attachments?.length || undefined },
    ...(canFollowUp ? [{ key: 'conversation' as const, label: 'Conversation' }] : []),
    { key: 'workflow', label: 'Workflow' },
    { key: 'journal', label: 'Journal', badge: attempt.journal.data?.entries.length || undefined },
    { key: 'files', label: 'Files' },
    { key: 'reports', label: 'Reports' },
    ...(canViewRuns ? [{ key: 'debug' as const, label: 'Debug' }] : []),
  ]

  return (
    <LlmCallsContext.Provider value={attempt.llmCalls}>
      <WorkOrderHeader
        workOrder={wo}
        status={status}
        run={attempt.run}
        runStatus={attempt.status}
        spent={spent}
        budgetState={attempt.timeline.budgetState}
        tally={attempt.timeline.tally}
        stream={attempt.stream}
        busy={busy}
        onSubmit={() => void submit()}
        onCancel={() => void cancel()}
        onEdit={() => setEditing(true)}
        onDelete={() => void remove()}
        resumable={resumable}
        onResume={() => setResuming(true)}
      />
      {resuming && attempt.run && resumable && (
        <ResumeRunModal
          run={{ ...attempt.run, resumable }}
          onClose={() => setResuming(false)}
          onResumed={(run) => {
            setResuming(false)
            notifySuccess(`Started attempt ${wo.run_count + 1}, resuming from task ${resumable.start_position + 1}`)
            onRunStarted(run)
          }}
        />
      )}
      {runs.length > 1 && <AttemptSelector runs={runs} selectedId={runId} liveStatus={attempt.status} onSelect={(id) => setParam({ run: id })} />}
      {wo.status === 'draft' || !runId ? (
        <div className="flex flex-col gap-4">
          <EmptyState
            icon={<Send className="size-6" />}
            title="Not submitted yet"
            hint="This is a draft. Submit it to start a run; the workflow, journal and files of each attempt appear here."
            action={can('work_orders.create') && (
              <div className="flex gap-2">
                <Button icon={<Pencil className="size-4" />} onClick={() => setEditing(true)}>Edit</Button>
                <Button variant="primary" icon={<Send className="size-4" />} busy={busy === 'submit'} onClick={() => void submit()}>Submit</Button>
              </div>
            )}
          />
          <BriefTab workOrder={wo} run={null} journal={undefined} />
        </div>
      ) : (
        <>
          <QuestionsPanel workOrderId={wo.id} refreshKey={attempt.timeline.humanEvents} />
          <Tabs tabs={tabs} active={tab} onChange={(k) => setParam({ tab: k })} />
          <div className="pt-4">
            {tab === 'brief' && <BriefTab workOrder={wo} run={attempt.run} journal={attempt.journal.data} />}
            {tab === 'conversation' && (
              <ConversationTab workOrderId={wo.id} crewId={wo.crew_id} active={status === 'queued' || status === 'running'}
                onRevised={(run) => onRunStarted(run, { tab: 'workflow' })} />
            )}
            {tab === 'workflow' && <WorkflowTab workOrder={wo} run={attempt.run} status={attempt.status} live={attempt.live} interactions={attempt.interactions} journal={attempt.journal} canViewRuns={canViewRuns} />}
            {tab === 'journal' && <JournalTab journal={attempt.journal.data} error={attempt.journal.error} loading={attempt.journal.loading} reload={attempt.journal.reload} live={attempt.live} compactRunId={attempt.status === 'running' && can('work_orders.cancel') ? runId : null} />}
            {tab === 'files' && <FilesTab inputs={wo.attachments ?? []} runId={canViewRuns ? runId : null} live={attempt.live} />}
            {tab === 'reports' && <ReportsTab workOrderId={wo.id} canRetry={can('work_orders.create')} live={attempt.live} />}
            {tab === 'debug' && attempt.run && <DebugTab run={attempt.run} live={attempt.live} timeline={attempt.timeline} stream={attempt.stream} />}
          </div>
        </>
      )}
    </LlmCallsContext.Provider>
  )
}

/** The composer, loaded with the target catalog, for editing an existing work order. */
function EditWorkOrder({ workOrder, onDone }: { workOrder: WorkOrder; onDone: (run: Run | null) => void }) {
  const catalog = useTargetCatalog()
  return (
    <AsyncView {...catalog}>
      {(cat) => (
        <WorkOrderComposer
          workOrder={workOrder}
          initial={toWorkOrderInput(workOrder)}
          catalog={cat}
          backTo="/work-orders"
          backLabel="Work orders"
          onCancel={() => onDone(null)}
          onSaved={() => onDone(null)}
          onSubmitted={(_, run) => onDone(run)}
        />
      )}
    </AsyncView>
  )
}

export default function WorkOrderPage() {
  const { id = '' } = useParams()
  const res = useAsync((signal) => workOrdersApi.get(id, signal), [id])
  if (res.error && !res.data) {
    return (
      <div className="flex flex-col gap-3">
        <ErrorState error={res.error} onRetry={res.reload} title="Could not load this work order" />
        <Link to="/work-orders" className="text-accent">Back to work orders</Link>
      </div>
    )
  }
  if (!res.data) return res.loading ? <Loading label="Loading work order…" /> : <EmptyState title="Work order not found" />
  return <WorkOrderDetail key={res.data.id} workOrder={res.data} reload={res.reload} patch={(fn) => res.setData((prev) => (prev ? fn(prev) : prev))} />
}
