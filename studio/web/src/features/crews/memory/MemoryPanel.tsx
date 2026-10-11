import { Link } from 'react-router'
import type { LLM } from '../../../api/types'
import { useAuth } from '../../../lib/auth'
import { MemoryList } from './MemoryList'
import { MemorySettingsPanel } from './MemorySettingsPanel'

/** The crew page's Memory tab: how the crew uses memory, and the memories it keeps. Needs state.view. */
export function MemoryPanel({ crewId, llms }: { crewId: string; llms: readonly LLM[] }) {
  const { can } = useAuth()
  const canManage = can('state.manage')
  return (
    <div className="flex flex-col gap-4">
      <p className="text-[12.5px] text-muted">
        Memory holds soft lessons the crew picks up by doing its work, such as &ldquo;Sam usually replies late on Fridays&rdquo;. Before each task the crew
        recalls the few that look relevant, as quoted data rather than instructions. Exact records belong in the action log and notes on the State tab,
        and knowledge others rely on belongs in KM2. <Link className="text-accent" to="/help/memory">How memory works</Link>
      </p>
      <MemorySettingsPanel crewId={crewId} llms={llms} canEdit={can('crews.edit') && canManage} />
      <MemoryList crewId={crewId} canManage={canManage} />
    </div>
  )
}
