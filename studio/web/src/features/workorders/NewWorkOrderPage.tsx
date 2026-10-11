import { useMemo } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { AsyncView } from '../../components/States'
import { notifySuccess } from '../../lib/notify'
import { SKIP_GUARD } from '../../lib/useUnsavedGuard'
import { emptyWorkOrder } from './composer/composerForm'
import { WorkOrderComposer } from './composer/WorkOrderComposer'
import { useTargetCatalog } from './target/useTargetCatalog'
import { woNumber } from './workOrderLabels'

/** /work-orders/new; ?crew=<id> (or ?crew_id=<id>) preselects the crew. */
export function NewWorkOrderPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const catalog = useTargetCatalog()
  const crewId = params.get('crew') || params.get('crew_id')
  const initial = useMemo(() => emptyWorkOrder(crewId), [crewId])

  return (
    <AsyncView {...catalog}>
      {(cat) => (
        <WorkOrderComposer
          workOrder={null}
          initial={initial}
          catalog={cat}
          backTo="/work-orders"
          backLabel="Work orders"
          onSubmitted={(wo) => {
            notifySuccess(`${woNumber(wo.number)} submitted`)
            navigate(`/work-orders/${wo.id}`, { state: SKIP_GUARD })
          }}
        />
      )}
    </AsyncView>
  )
}
