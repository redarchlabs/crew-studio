import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { Play, StepForward } from 'lucide-react'
import { crewsApi } from '../../api/endpoints'
import type { Crew, ResumeHint, Run } from '../../api/types'
import { Button } from '../../components/Button'
import { useAuth } from '../../lib/auth'
import { shortId } from '../../lib/format'
import { notifyError } from '../../lib/notify'
import { RunCrewModal } from '../crews/RunCrewModal'
import { ResumeRunModal, resumeLabel } from './ResumeRunModal'

const hasHint = (run: Run): run is Run & { resumable: ResumeHint } => run.resumable !== undefined

/**
 * For a finished crew run: "Resume from task N" when it failed or was cancelled part-way (reusing the tasks it finished),
 * and "Run again" with the same inputs (a work order's attempt is re-run from its work order instead).
 */
export function RunActions({ run }: { run: Run }) {
  const navigate = useNavigate()
  const { can } = useAuth()
  const [resuming, setResuming] = useState(false)
  const [crew, setCrew] = useState<Crew | null>(null)
  const [loadingCrew, setLoadingCrew] = useState(false)
  const mayRunAgain = run.kind !== 'chat' && run.kind !== 'handoff' && run.crew_id !== null && run.work_order_id === null && can('crews.run')

  const runAgain = async () => {
    if (!run.crew_id) return
    setLoadingCrew(true)
    try {
      setCrew(await crewsApi.get(run.crew_id))
    } catch (err) {
      notifyError('Could not load the crew', err)
    } finally {
      setLoadingCrew(false)
    }
  }

  return (
    <>
      {hasHint(run) && (
        <Button variant="primary" icon={<StepForward className="size-4" />} title={`Reuses what this run finished and runs ${run.resumable.task_name} and the rest`} onClick={() => setResuming(true)}>
          {resumeLabel(run.resumable)}
        </Button>
      )}
      {mayRunAgain && (
        <Button icon={<Play className="size-4" />} busy={loadingCrew} title="Start a new run of the crew with these inputs" onClick={() => void runAgain()}>
          Run again
        </Button>
      )}
      {resuming && hasHint(run) && (
        <ResumeRunModal run={run} onClose={() => setResuming(false)} onResumed={(next) => navigate(`/runs/${next.id}`)} />
      )}
      {crew && <RunCrewModal crew={crew} initial={{ inputs: run.inputs, budget_usd: run.budget_usd }} onClose={() => setCrew(null)} />}
    </>
  )
}

/** Links between a resumed run and the run it resumed. */
export function ResumeLinks({ run }: { run: Run }) {
  const from = run.resumed_from_run_id
  const later = run.resumed_by ?? []
  if (!from && later.length === 0) return null
  const reused = run.resume_position ?? 0
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 text-[12.5px] text-muted">
      {from && (
        <span>
          Resumed from run <Link className="font-mono text-accent hover:underline" to={`/runs/${from}`}>{shortId(from)}</Link>
          {reused > 0 && <> · reused {reused === 1 ? 'task 1' : `tasks 1–${reused}`}</>}
        </span>
      )}
      {later.length > 0 && (
        <span>
          Resumed as{' '}
          {later.map((id, i) => (
            <span key={id}>
              {i > 0 && ', '}
              <Link className="font-mono text-accent hover:underline" to={`/runs/${id}`}>{shortId(id)}</Link>
            </span>
          ))}
        </span>
      )}
    </div>
  )
}
