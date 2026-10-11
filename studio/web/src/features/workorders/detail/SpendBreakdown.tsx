import { compact, int, usd } from '../../../lib/format'
import { tallyTotal, type SpendTally } from '../../runs/timeline/spendTally'

function Part({ label, cost, detail }: { label: string; cost: number; detail: string }) {
  return (
    <span className="inline-flex items-baseline gap-1.5">
      <span className="text-muted">{label}</span>
      <span className="font-mono tabular text-fg">{usd(cost)}</span>
      <span className="font-mono text-[11px] text-faint">{detail}</span>
    </span>
  )
}

/** The attempt's spend split by source, so search and video costs are visible next to LLM costs. */
export function SpendBreakdown({ tally }: { tally: SpendTally }) {
  const cached = (tally.cachedTokens > 0 ? `, ${compact(tally.cachedTokens)} cached` : '')
    + (tally.cacheWriteTokens > 0 ? `, ${compact(tally.cacheWriteTokens)} cache writes` : '')
  return (
    <div aria-label="Spend breakdown" className="flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border border-line bg-panel px-3 py-2 text-[12px]">
      <span className="eyebrow">Spend</span>
      <Part label="LLM" cost={tally.llmUsd} detail={`${int(tally.llmCalls)} calls · ${compact(tally.promptTokens + tally.completionTokens)} tok${cached}`} />
      <Part label="Search" cost={tally.searchUsd} detail={`${int(tally.searches)} searches · ${compact(tally.searchTokens)} tok`} />
      {tally.videos > 0 && <Part label="Video" cost={tally.videoUsd} detail={`${int(tally.videos)} videos`} />}
      <span className="ml-auto inline-flex items-baseline gap-1.5">
        <span className="text-muted">Total</span>
        <span className="font-mono tabular font-semibold text-fg">{usd(tallyTotal(tally))}</span>
      </span>
    </div>
  )
}
