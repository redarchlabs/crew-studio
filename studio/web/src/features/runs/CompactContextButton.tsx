import { useState } from 'react'
import { FoldVertical } from 'lucide-react'
import { runsApi } from '../../api/endpoints'
import { Button } from '../../components/Button'
import { notifyError, notifySuccess } from '../../lib/notify'

/** Asks a running attempt to compact its agents' context: each agent summarises its work so far before its next LLM call. */
export function CompactContextButton({ runId }: { runId: string }) {
  const [busy, setBusy] = useState(false)
  const compact = async () => {
    setBusy(true)
    try {
      await runsApi.compact(runId)
      notifySuccess("Asked the agents to compact their context; it shows in the journal when it's done")
    } catch (err) {
      notifyError('Could not compact the context', err)
    } finally {
      setBusy(false)
    }
  }
  return (
    <Button
      size="sm"
      variant="ghost"
      icon={<FoldVertical className="size-3.5" />}
      busy={busy}
      title="Each agent replaces its conversation so far with a summary before its next LLM call, so later calls resend fewer tokens. The summary itself is one extra LLM call."
      onClick={() => void compact()}
    >
      Compact context
    </Button>
  )
}
