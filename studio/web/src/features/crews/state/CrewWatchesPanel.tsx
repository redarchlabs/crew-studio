import { Link } from 'react-router'
import { Radar } from 'lucide-react'
import { watchesApi } from '../../../api/endpoints.watch'
import { Panel } from '../../../components/PageHeader'
import { AsyncView } from '../../../components/States'
import { compact } from '../../../lib/format'
import { useAsync } from '../../../lib/useAsync'
import { sourceSummary } from '../../schedules/watch/WatchPanel'
import { WatchStatusLine } from '../../schedules/watch/WatchStatus'

/** The watches on the crew's schedules: what each reads, when it last checked or changed, and what it saved. */
export function CrewWatchesPanel({ crewId }: { crewId: string }) {
  const res = useAsync((signal) => watchesApi.ofCrew(crewId, signal), [crewId])
  return (
    <Panel title={<span className="inline-flex items-center gap-2"><Radar className="size-4 text-faint" />Watches</span>}>
      <AsyncView {...res}>
        {(watches) =>
          watches.length === 0 ? (
            <p className="text-[12.5px] text-faint">No schedule of this crew has a watch. Add one in a schedule&apos;s Watch panel so it only runs when something changed.</p>
          ) : (
            <ul className="flex flex-col divide-y divide-line">
              {watches.map((w) => (
                <li key={w.id} className="flex flex-col gap-1 py-2 first:pt-0 last:pb-0">
                  <div className="text-[13px]">
                    <Link className="font-medium hover:text-accent" to={`/schedules/${w.schedule_id}`}>{w.schedule_name}</Link>
                    <span className="text-muted"> · {sourceSummary(w)}</span>
                  </div>
                  <WatchStatusLine watch={w} />
                  <div className="text-[12px] text-muted">
                    Last {w.stats.days} days: {w.stats.runs_skipped} {w.stats.runs_skipped === 1 ? 'run' : 'runs'} skipped, {w.stats.runs_started} started
                    {w.stats.avg_run_tokens > 0 && <> · about {compact(w.stats.est_tokens_saved)} tokens saved</>}
                  </div>
                </li>
              ))}
            </ul>
          )
        }
      </AsyncView>
    </Panel>
  )
}
