import { Link } from 'react-router'
import { workOrdersApi } from '../../api/endpoints'
import type { WorkOrderSource } from '../../api/types'
import { Panel } from '../../components/PageHeader'
import { AsyncView } from '../../components/States'
import { useAsync } from '../../lib/useAsync'
import { useNow } from '../../lib/useNow'
import { usePollWhile } from '../../lib/usePollWhile'
import { isActive } from './workOrderLabels'
import { WorkOrdersTable } from './WorkOrdersTable'

const LIMIT = 10

/** The newest work orders a schedule or trigger created. */
export function RecentWorkOrders({ source, sourceId, refreshKey = 0 }: { source: WorkOrderSource; sourceId: string; refreshKey?: number }) {
  const res = useAsync((signal) => workOrdersApi.list({ source, source_id: sourceId, limit: LIMIT, offset: 0 }, signal), [source, sourceId, refreshKey])
  const active = res.data?.items.some((wo) => isActive(wo.status)) ?? false
  const now = useNow(active, 5000)
  usePollWhile(active, res.reload, 5000)
  return (
    <Panel
      title="Recent work orders"
      actions={res.data && res.data.meta.total > LIMIT ? <Link className="text-[12px] text-accent" to={`/work-orders?source=${source}`}>All {source} work orders</Link> : undefined}
      bodyClassName="p-3"
    >
      <AsyncView {...res}>
        {(page) =>
          page.items.length === 0 ? (
            <p className="text-[12px] text-faint">None yet.</p>
          ) : (
            <WorkOrdersTable items={page.items} now={now} compact />
          )
        }
      </AsyncView>
    </Panel>
  )
}
