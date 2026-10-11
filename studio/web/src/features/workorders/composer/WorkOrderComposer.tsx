import { Link } from 'react-router'
import { ChevronLeft, RotateCcw, Save, Send } from 'lucide-react'
import type { Run, WorkOrder, WorkOrderInput } from '../../../api/types'
import { Button } from '../../../components/Button'
import { FormErrors, TextField } from '../../../components/Field'
import { Panel } from '../../../components/PageHeader'
import { useUnsavedGuard } from '../../../lib/useUnsavedGuard'
import { BudgetField } from '../target/BudgetField'
import { InputsFields } from '../target/InputsFields'
import { TargetPicker } from '../target/TargetPicker'
import type { TargetCatalogData } from '../target/useTargetCatalog'
import { WoBadge, WorkOrderStatusBadge } from '../workOrderLabels'
import { AttachmentTray } from './AttachmentTray'
import { BriefEditor } from './BriefEditor'
import { hasTarget, inputErrors } from './composerForm'
import { useComposer } from './useComposer'

type WorkOrderComposerProps = {
  /** The work order being edited, or null for a new one */
  workOrder: WorkOrder | null
  initial: WorkOrderInput
  catalog: TargetCatalogData
  backTo: string
  backLabel: string
  onSubmitted: (workOrder: WorkOrder, run: Run) => void
  onSaved?: (workOrder: WorkOrder) => void
  onCancel?: () => void
}

/**
 * Create or edit a work order: title, crew, brief with files, inputs and budget. Used by the new-work-order
 * page and for editing an existing one (drafts and finished work orders; not while an attempt is active).
 */
export function WorkOrderComposer({ workOrder, initial, catalog, backTo, backLabel, onSubmitted, onSaved, onCancel }: WorkOrderComposerProps) {
  const c = useComposer(workOrder, initial, catalog)
  const { value: v, errors, saved } = c
  const f = errors.fields
  useUnsavedGuard(c.dirty && c.busy === null)

  const isDraft = !saved || saved.status === 'draft'
  const crew = catalog.crews.find((x) => x.id === v.crew_id)
  const targetError = f.target ?? f.crew_id
  const waitingHint = hasTarget(v) ? 'waiting' : 'uploads once a crew is chosen'

  const onSave = async () => {
    const wo = await c.saveDraft()
    if (wo) onSaved?.(wo)
  }
  const onSubmit = async () => {
    const result = await c.submit()
    if (result) onSubmitted(result.workOrder, result.run)
  }

  return (
    <>
      <header className="sticky top-0 z-20 -mx-4 mb-5 flex flex-wrap items-center gap-3 border-b border-line bg-bg/90 px-4 py-3 backdrop-blur md:-mx-6 md:px-6">
        <div className="min-w-0 flex-1">
          <Link to={backTo} className="mb-0.5 inline-flex items-center gap-1 text-[12px] text-muted hover:text-fg">
            <ChevronLeft className="size-3.5" /> {backLabel}
          </Link>
          <div className="flex flex-wrap items-center gap-2">
            {saved && <WoBadge number={saved.number} />}
            <h1 className="truncate text-[19px] font-semibold tracking-tight">{workOrder ? 'Edit work order' : 'New work order'}</h1>
            {saved && <WorkOrderStatusBadge status={saved.status} />}
            {c.dirty && <span className="rounded bg-warn-soft px-1.5 py-px text-[11px] text-warn">unsaved</span>}
            {!workOrder && saved && <Link to={`/work-orders/${saved.id}`} className="text-[12px] text-accent">Open draft</Link>}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {onCancel && <Button variant="ghost" onClick={onCancel}>Cancel</Button>}
          <Button icon={<Save className="size-4" />} busy={c.busy === 'draft'} disabled={c.busy !== null} onClick={() => void onSave()}>
            {isDraft ? 'Save draft' : 'Save changes'}
          </Button>
          <Button variant="primary" icon={isDraft ? <Send className="size-4" /> : <RotateCcw className="size-4" />} busy={c.busy === 'submit'} disabled={c.busy !== null} onClick={() => void onSubmit()}>
            {isDraft ? 'Submit' : 'Save & re-run'}
          </Button>
        </div>
      </header>
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-4">
          <Panel title="Work order">
            <div className="flex flex-col gap-3">
              <TextField label="Title" value={v.title} onChange={(x) => c.set('title', x)} error={f.title} maxLength={200} placeholder="Quarterly vendor review for Acme" />
              <TargetPicker value={v} onChange={c.setTarget} catalog={catalog} error={targetError} />
            </div>
          </Panel>
          <Panel title="Brief">
            <BriefEditor
              value={v.brief}
              onChange={(x) => c.set('brief', x)}
              error={f.brief}
              onFiles={(files) => void c.queue.add(files)}
              takenNames={c.queue.takenNames}
              hint="Given to every task. Images and PDFs go to the model; small text files are quoted; other files are copied into the run's workspace."
              footer={<AttachmentTray queue={c.queue} waitingHint={waitingHint} />}
            />
          </Panel>
          <FormErrors messages={errors.general} />
        </div>
        <aside className="flex min-w-0 flex-col gap-4">
          <Panel title="Inputs">
            <InputsFields derived={c.derived} values={v.inputs} onChange={(x) => c.set('inputs', x)} errors={inputErrors(f)} noTarget={!hasTarget(v)} />
            {f.inputs && <div className="mt-2 text-[12px] text-danger" role="alert">{f.inputs}</div>}
          </Panel>
          <Panel title="Budget">
            <BudgetField value={v.budget_usd} onChange={(x) => c.set('budget_usd', x)} crewBudget={crew?.budget_usd} error={f.budget_usd} />
          </Panel>
          <p className="px-1 text-[12px] text-faint">
            {isDraft
              ? 'Submitting starts a run straight away. Save a draft to come back to it later.'
              : 'Saving changes applies to the next attempt; earlier attempts keep what they ran with.'}
          </p>
        </aside>
      </div>
    </>
  )
}
