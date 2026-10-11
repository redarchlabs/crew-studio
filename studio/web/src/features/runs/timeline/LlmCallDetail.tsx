import { useEffect } from 'react'
import { ErrorState, Loading } from '../../../components/States'
import { LlmCallBody } from '../tabs/MessageView'
import { useLlmCalls } from '../useLlmCalls'

/** Loads (on first expand) and shows the full prompt/response of the call logged at `seq`. */
export function LlmCallDetail({ seq }: { seq: number }) {
  const { calls, loading, error, load } = useLlmCalls()
  useEffect(() => load({ wantSeq: seq }), [load, seq])
  const call = calls?.find((c) => c.seq === seq)
  if (call) return <LlmCallBody call={call} />
  if (error) return <ErrorState error={error} onRetry={() => load({ force: true })} title="Could not load LLM call" />
  if (loading || !calls) return <Loading label="Loading prompt and response…" />
  return <p className="text-[12px] text-faint">This call’s messages are not available yet. They are recorded when the call finishes.</p>
}
