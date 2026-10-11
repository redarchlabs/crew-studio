import { useAuth } from '../../../lib/auth'
import { ActionLogPanel } from './ActionLogPanel'
import { CrewConnectionsPanel } from './CrewConnectionsPanel'
import { CrewWatchesPanel } from './CrewWatchesPanel'
import { KM2SavesPanel } from './KM2SavesPanel'
import { NotesPanel } from './NotesPanel'
import { TrackingListPanel } from './TrackingListPanel'

/** The crew page's State tab: what the crew remembers (notes), what it did (action log), and what it may reach. */
export function CrewStatePanel({ crewId }: { crewId: string }) {
  const { can } = useAuth()
  const canManage = can('state.manage')
  return (
    <div className="flex flex-col gap-4">
      <p className="text-[12.5px] text-muted">
        Crew state lasts between runs. Scheduled runs keep their own notes and log entries; runs started by hand use the crew-wide ones.
        Every change agents make is also written in that run&apos;s journal.
      </p>
      {can('connections.view') && <CrewConnectionsPanel crewId={crewId} canEdit={can('crews.edit')} />}
      {can('state.view') && <CrewWatchesPanel crewId={crewId} />}
      {can('state.view') && <TrackingListPanel crewId={crewId} canManage={canManage} />}
      {can('state.view') && <NotesPanel crewId={crewId} canManage={canManage} />}
      {can('state.view') && <KM2SavesPanel crewId={crewId} />}
      {can('state.view') && <ActionLogPanel crewId={crewId} canManage={canManage} />}
    </div>
  )
}

/** Whether someone sees the State tab at all. */
export function canSeeCrewState(can: (perm: 'state.view' | 'connections.view') => boolean): boolean {
  return can('state.view') || can('connections.view')
}
