import { useEffect, useState } from 'react'
import { Download, FileText, RotateCcw } from 'lucide-react'
import { reportsApi } from '../../api/endpoints.delivery'
import type { DeliveryStatus, Report, ReportDelivery } from '../../api/types.delivery'
import { Button } from '../../components/Button'
import { Markdown } from '../../components/Markdown'
import { AsyncView, EmptyState } from '../../components/States'
import { Chip, type Tone } from '../../components/StatusBadge'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'
import { KIND_LABELS } from './destinationsForm'
import { DueReport } from './duereport/DueReport'

const STATUS: Record<DeliveryStatus, { label: string; tone: Tone }> = {
  sent: { label: 'sent', tone: 'ok' },
  would_send: { label: 'would send', tone: 'blue' },
  failed: { label: 'failed', tone: 'danger' },
  pending: { label: 'sending', tone: 'warn' },
}

/** A report's cadence and period, such as "Daily · 2026-10-06" or "Weekly · 2026-W41". */
export function cadenceLabel(r: Pick<Report, 'cadence' | 'period' | 'week'>): string {
  const daily = r.cadence === 'daily'
  return `${daily ? 'Daily' : 'Weekly'} · ${r.period || r.week}`
}

/** Where a delivery went, for people. */
export function deliveryTarget(d: ReportDelivery): string {
  if (d.kind === 'work_order') return 'Work order'
  const kind = d.kind === 'tracker' ? 'Tracker' : KIND_LABELS[d.kind]
  if (d.kind === 'email') return `${kind}: ${d.connection_name} (${d.recipients.length} recipient${d.recipients.length === 1 ? '' : 's'})`
  return `${kind}: ${d.connection_name || 'deleted connection'}`
}

function DeliveryRow({ d }: { d: ReportDelivery }) {
  const s = STATUS[d.status]
  return (
    <tr className="border-t border-line align-top" data-testid="delivery-row">
      <td className="py-1.5 pr-3 text-[13px]">{deliveryTarget(d)}</td>
      <td className="py-1.5 pr-3"><Chip tone={s.tone}>{s.label}</Chip></td>
      <td className="py-1.5 text-[12.5px] text-muted">{d.message}{d.attempts > 1 ? ` (${d.attempts} attempts)` : ''}</td>
    </tr>
  )
}

type View = 'report' | 'text'

/** Report | Text: the designed report when a snapshot was taken, the Markdown as delivered otherwise. */
function ReportBody({ report, workOrderId }: { report: Report; workOrderId: string }) {
  const snapshot = report.snapshot
  const [view, setView] = useState<View>('report')
  if (!snapshot) return <div className="mt-3 rounded-md border border-line bg-raised p-3"><Markdown>{report.markdown}</Markdown></div>
  const tab = (v: View, label: string) => (
    <button type="button" role="tab" aria-selected={view === v} onClick={() => setView(v)}
      className={`rounded px-2.5 py-1 text-[12.5px] ${view === v ? 'bg-panel font-medium text-fg shadow-sm' : 'text-muted hover:text-fg'}`}>{label}</button>
  )
  return (
    <div className="mt-3 flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div role="tablist" aria-label="Report view" className="inline-flex gap-0.5 rounded-md border border-line bg-raised p-0.5">
          {tab('report', 'Report')}
          {tab('text', 'Text')}
        </div>
        <a href={reportsApi.htmlUrl(workOrderId, report.id)} download className="inline-flex items-center gap-1.5 text-[12.5px] text-muted hover:text-fg">
          <Download aria-hidden className="size-3.5" />Download HTML
        </a>
      </div>
      {view === 'report' ? <DueReport report={report} snapshot={snapshot} />
        : <div className="rounded-md border border-line bg-raised p-3"><Markdown>{report.markdown}</Markdown></div>}
    </div>
  )
}

function ReportCard({ report, workOrderId }: { report: Report; workOrderId: string }) {
  const [open, setOpen] = useState(false)
  return (
    <section className="rounded-lg border border-line bg-panel p-4" data-testid="report-card">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <FileText className="size-4 text-faint" />
          <span className="font-medium">{report.title}</span>
          <Chip tone={report.cadence === 'daily' ? 'blue' : 'neutral'}>{cadenceLabel(report)}</Chip>
          {report.dry_run && <Chip tone="warn">dry run</Chip>}
        </div>
        <Button size="sm" variant="ghost" onClick={() => setOpen((o) => !o)}>{open ? 'Hide report' : 'Show report'}</Button>
      </div>
      <table className="mt-3 w-full">
        <tbody>{report.deliveries.map((d) => <DeliveryRow key={d.id} d={d} />)}</tbody>
      </table>
      {open && <ReportBody report={report} workOrderId={workOrderId} />}
    </section>
  )
}

/** The reports a work order's runs delivered, each destination's status, and "Retry failed deliveries". */
export function ReportsTab({ workOrderId, canRetry, live }: { workOrderId: string; canRetry: boolean; live: boolean }) {
  const res = useAsync((signal) => reportsApi.list(workOrderId, signal), [workOrderId])
  const [busy, setBusy] = useState(false)
  const { reload } = res
  // A running attempt may deliver more; refresh when it finishes
  useEffect(() => {
    if (!live) reload()
  }, [live, reload])
  const retry = async () => {
    setBusy(true)
    try {
      const out = await reportsApi.retry(workOrderId)
      res.setData(() => out)
      if (out.retried === 0) notifySuccess('Nothing to retry')
      else notifySuccess(`Retried ${out.retried} ${out.retried === 1 ? 'delivery' : 'deliveries'}; ${out.sent} sent`)
    } catch (err) {
      notifyError('Could not retry the deliveries', err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <AsyncView {...res}>
      {(view) => {
        if (view.reports.length === 0) {
          return <EmptyState icon={<FileText className="size-6" />} title="No reports" hint="Reports appear here when a run delivers one with the Deliver Report tool." />
        }
        const retryable = view.reports.reduce((n, r) => n + r.retryable, 0)
        return (
          <div className="flex flex-col gap-3">
            {canRetry && retryable > 0 && (
              <div className="flex items-center gap-3 rounded-md border border-danger/40 bg-danger-soft px-3 py-2 text-[12.5px]">
                <span>{retryable} {retryable === 1 ? 'delivery' : 'deliveries'} failed. Retrying sends only those; what was sent is not sent again.</span>
                <Button size="sm" variant="primary" icon={<RotateCcw className="size-3.5" />} busy={busy} onClick={() => void retry()}>Retry failed deliveries</Button>
              </div>
            )}
            {view.reports.map((r) => <ReportCard key={r.id} report={r} workOrderId={workOrderId} />)}
          </div>
        )
      }}
    </AsyncView>
  )
}
