import { useState, type FormEvent } from 'react'
import { StepForward } from 'lucide-react'
import { ApiError } from '../../api/client'
import { resumeApi } from '../../api/endpoints.resume'
import type { ResumeHint, Run } from '../../api/types'
import { Button } from '../../components/Button'
import { FormErrors, NumberField, Toggle } from '../../components/Field'
import { Modal } from '../../components/Modal'
import { usd } from '../../lib/format'
import { notifyError } from '../../lib/notify'

/** "Resume from task 3" for a run that could be resumed */
export function resumeLabel(hint: ResumeHint): string {
  return `Resume from task ${hint.start_position + 1}`
}

function reusedText(hint: ResumeHint): string {
  const tasks = hint.reused_tasks === 1 ? 'task 1' : `tasks 1–${hint.reused_tasks}`
  return hint.reused_cost_usd > 0 ? `${tasks} (${usd(hint.reused_cost_usd)} when they ran)` : tasks
}

type ResumeRunModalProps = { run: Run & { resumable: ResumeHint }; onClose: () => void; onResumed: (run: Run) => void }

/** Asks how to resume a failed or cancelled run, then starts the new run. */
export function ResumeRunModal({ run, onClose, onResumed }: ResumeRunModalProps) {
  const hint = run.resumable
  const [budget, setBudget] = useState<number | null>(null)
  const [useCurrent, setUseCurrent] = useState(false)
  const [error, setError] = useState('')
  const [serverErrors, setServerErrors] = useState<string[]>([])
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (budget !== null && (Number.isNaN(budget) || budget <= 0)) {
      setError('Budget must be greater than 0')
      return
    }
    setError('')
    setServerErrors([])
    setBusy(true)
    try {
      onResumed(await resumeApi.resume(run.id, { ...(budget !== null ? { budget_usd: budget } : {}), use_current: useCurrent }))
    } catch (err) {
      if (err instanceof ApiError && err.details.length > 0) setServerErrors(err.details)
      notifyError('Could not resume the run', err)
      setBusy(false)
    }
  }

  return (
    <Modal title={`${resumeLabel(hint)}: ${hint.task_name}`} onClose={onClose}>
      <form className="flex flex-col gap-3" onSubmit={submit} noValidate>
        <p className="text-[13px]">
          A new run reuses {reusedText(hint)} from this run at no cost, then runs <strong>{hint.task_name}</strong> and the tasks after it.
        </p>
        {run.cost_usd !== null && run.cost_usd >= run.budget_usd && (
          <p className="rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px] text-warn">
            This run spent {usd(run.cost_usd)} of its {usd(run.budget_usd)} budget and may have stopped on it. The new run gets a budget of
            its own; raise it below if the remaining tasks need more.
          </p>
        )}
        <p className="rounded-md border border-warn/40 bg-warn-soft px-3 py-2 text-[12.5px] text-warn">
          {hint.task_name} runs again from the start{hint.spent_on_task_usd ? ` (it had spent ${usd(hint.spent_on_task_usd)} before it stopped)` : ''}.
          Anything it already did, such as a note it saved or a comment it posted, may happen a second time.
        </p>
        {(hint.warnings ?? []).length > 0 && (
          <div className="rounded-md border border-danger/40 bg-danger-soft px-3 py-2 text-[12.5px] text-danger" role="alert">
            <p>As it was, the remaining tasks are likely to fail:</p>
            <ul className="mt-1 list-disc pl-5">
              {hint.warnings!.map((w) => <li key={w}>{w}</li>)}
            </ul>
            <p className="mt-1">Fix this first, or use the crew's current settings.</p>
          </div>
        )}
        <Toggle
          label="Use the crew's current settings"
          checked={useCurrent}
          onChange={setUseCurrent}
          hint="Off: the remaining tasks run with the agents, tasks and LLMs this run started with. On: they run with the crew as it is now, for example after switching a model. The finished tasks must still be the crew's first tasks."
        />
        <NumberField
          label="Budget (USD)"
          value={budget}
          onChange={setBudget}
          nullable
          min={0}
          step="any"
          error={error}
          hint={`Covers the new run's own spending. Leave empty to use this run's budget of ${usd(run.budget_usd)}.`}
        />
        <FormErrors messages={serverErrors} />
        <div className="flex justify-end gap-2 pt-1">
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" icon={<StepForward className="size-4" />} busy={busy}>{resumeLabel(hint)}</Button>
        </div>
      </form>
    </Modal>
  )
}
