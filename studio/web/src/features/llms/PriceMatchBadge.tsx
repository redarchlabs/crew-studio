import { Link } from 'react-router'
import { TriangleAlert } from 'lucide-react'
import type { ModelPrice } from '../../api/types'
import { matchPrice } from '../../lib/pricing'
import { usd } from '../../lib/format'

/** Shows which price entry an LLM model resolves to, or warns when none does. */
export function PriceMatchBadge({ model, prices, verbose = false }: { model: string; prices: readonly ModelPrice[]; verbose?: boolean }) {
  const match = matchPrice(model, prices)
  if (!match) {
    return (
      <span className="inline-flex items-center gap-1 text-[12px] text-warn" title="Runs with this LLM cannot be costed or budgeted accurately.">
        <TriangleAlert className="size-3.5" />
        No price entry{verbose && <> · <Link className="underline" to="/pricing">add one</Link></>}
      </span>
    )
  }
  const { price, via } = match
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1.5 text-[12px]" title={via === 'exact' ? 'Exact model match' : 'Matched after removing the provider prefix'}>
      <span className="rounded bg-teal-soft px-1.5 py-px font-mono text-[11px] text-teal">{price.model}</span>
      <span className="font-mono text-faint tabular">
        {usd(price.input_per_mtok)} in · {usd(price.output_per_mtok)} out
      </span>
      {verbose && <span className="text-faint">({via === 'exact' ? 'exact match' : 'matched without provider prefix'})</span>}
    </span>
  )
}
